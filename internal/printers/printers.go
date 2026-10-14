// Package printers holds label printers: the drivers that turn rendered
// labels into a printer's own language, the validation of printer
// definitions, and the transports that deliver jobs (README §4.4).
package printers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DefaultPort is the raw printing port.
const DefaultPort = 9100

// ErrInvalidPrinter is wrapped by every validation failure.
var ErrInvalidPrinter = errors.New("invalid printer")

// Settings are the driver-specific parts of a printer definition.
type Settings struct {
	Model string
	Media string
	// Options holds driver options as a JSON object; empty means defaults.
	Options json.RawMessage
}

var hostnameRegex = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)

// Validate checks a printer definition: the generic fields and connection
// here, the rest by its driver. Whether this server can use the connection
// is the Transport's Check.
func Validate(name, driver string, conn Connection, settings Settings) error {
	if n := strings.TrimSpace(name); n == "" || len(n) > 255 {
		return fmt.Errorf("%w: name must be 1-255 characters", ErrInvalidPrinter)
	}
	if _, err := NormalizeConnection(conn); err != nil {
		return err
	}
	d, err := Lookup(driver)
	if err != nil {
		return err
	}
	if err := d.Validate(settings); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPrinter, err)
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// Transport delivers an encoded job to a printer.
type Transport interface {
	// Check reports whether this server may use a normalized connection,
	// as an error wrapping ErrInvalidPrinter when it may not.
	Check(c Connection) error
	// Send delivers a job over a normalized connection.
	Send(ctx context.Context, c Connection, job []byte) error
}

// ErrUnreachable is wrapped when a job cannot be delivered.
var ErrUnreachable = errors.New("printer unreachable")

// Dispatcher is the Transport for every connection type. It sends jobs one
// at a time per destination, so concurrent jobs never interleave on a device.
type Dispatcher struct {
	DialTimeout  time.Duration
	WriteTimeout time.Duration
	// AllowedDevices are the path patterns (path.Match syntax) a device
	// connection may name. None means device connections are refused.
	AllowedDevices []string

	mu    sync.Mutex
	slots map[Connection]chan struct{}
	// dial replaces net.Dialer in tests.
	dial func(ctx context.Context, addr string) (net.Conn, error)
	// client sends IPP requests.
	client *http.Client
}

// NewDispatcher returns a dispatcher with conservative timeouts that may
// write to devices matching allowedDevices.
func NewDispatcher(allowedDevices []string) *Dispatcher {
	return &Dispatcher{
		DialTimeout: 5 * time.Second, WriteTimeout: 60 * time.Second,
		AllowedDevices: allowedDevices,
		slots:          map[Connection]chan struct{}{},
		client:         &http.Client{Timeout: 90 * time.Second},
	}
}

// Check implements Transport.
func (d *Dispatcher) Check(c Connection) error {
	switch c.Type {
	case ConnDevice:
		for _, pattern := range d.AllowedDevices {
			if ok, _ := path.Match(pattern, c.Address); ok {
				return nil
			}
		}
		if len(d.AllowedDevices) == 0 {
			return fmt.Errorf("%w: device connections are off; list allowed device paths in the server's [printers] devices setting", ErrInvalidPrinter)
		}
		return fmt.Errorf("%w: %s is not among the server's allowed devices (%s)", ErrInvalidPrinter, c.Address, strings.Join(d.AllowedDevices, ", "))
	case ConnWindows:
		if !spoolerAvailable {
			return fmt.Errorf("%w: windows connections need BAM to run on Windows", ErrInvalidPrinter)
		}
	}
	return nil
}

func (d *Dispatcher) slot(c Connection) chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	s, ok := d.slots[c]
	if !ok {
		s = make(chan struct{}, 1)
		d.slots[c] = s
	}
	return s
}

// Send implements Transport, waiting for any job already going to the same
// destination. The job counts as sent once the destination has accepted
// every byte.
func (d *Dispatcher) Send(ctx context.Context, c Connection, job []byte) error {
	if err := d.Check(c); err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	slot := d.slot(c)
	select {
	case slot <- struct{}{}:
		defer func() { <-slot }()
	case <-ctx.Done():
		return fmt.Errorf("%w: waiting for the previous job: %v", ErrUnreachable, ctx.Err())
	}

	var err error
	switch c.Type {
	case ConnTCP:
		err = d.sendTCP(ctx, c.Address, job)
	case ConnDevice:
		err = d.sendDevice(c.Address, job)
	case ConnIPP:
		err = d.sendIPP(ctx, c.Address, job)
	case ConnWindows:
		err = spool(c.Address, job)
	default:
		err = fmt.Errorf("unknown connection type %q", c.Type)
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	return nil
}

func (d *Dispatcher) sendTCP(ctx context.Context, addr string, job []byte) error {
	dial := d.dial
	if dial == nil {
		dial = func(ctx context.Context, addr string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: d.DialTimeout}
			return dialer.DialContext(ctx, "tcp", addr)
		}
	}
	conn, err := dial(ctx, addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(d.WriteTimeout)); err != nil {
		return err
	}
	if _, err := conn.Write(job); err != nil {
		return fmt.Errorf("sending job: %v", err)
	}
	// Signal the end of the job before closing.
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
	}
	return nil
}

// sendDevice writes a job to an existing device; it never creates a file.
func (d *Dispatcher) sendDevice(name string, job []byte) error {
	f, err := os.OpenFile(name, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	// Character devices may not support deadlines; then writes block until
	// the printer takes the data.
	_ = f.SetWriteDeadline(time.Now().Add(d.WriteTimeout))
	if _, err := f.Write(job); err != nil {
		f.Close()
		return fmt.Errorf("sending job: %v", err)
	}
	return f.Close()
}
