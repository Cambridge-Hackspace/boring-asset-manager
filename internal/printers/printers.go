// Package printers holds network label printers: the drivers that turn
// rendered labels into a printer's own language, the validation of printer
// definitions, and the transport that delivers jobs (README §4.4).
package printers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
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

// Validate checks a printer definition: the generic fields here, the rest by
// its driver.
func Validate(name, driver, host string, port int, settings Settings) error {
	if n := strings.TrimSpace(name); n == "" || len(n) > 255 {
		return fmt.Errorf("%w: name must be 1-255 characters", ErrInvalidPrinter)
	}
	if net.ParseIP(host) == nil && (len(host) > 253 || !hostnameRegex.MatchString(host)) {
		return fmt.Errorf("%w: host must be an IP address or hostname", ErrInvalidPrinter)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("%w: port must be 1-65535", ErrInvalidPrinter)
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
	Send(ctx context.Context, host string, port int, job []byte) error
}

// ErrUnreachable is wrapped when a job cannot be delivered.
var ErrUnreachable = errors.New("printer unreachable")

// Dispatcher is the raw TCP Transport. It sends jobs one at a time per
// printer address, so concurrent jobs never interleave on a device.
type Dispatcher struct {
	DialTimeout  time.Duration
	WriteTimeout time.Duration

	mu    sync.Mutex
	slots map[string]chan struct{}
	// dial replaces net.Dialer in tests.
	dial func(ctx context.Context, addr string) (net.Conn, error)
}

// NewDispatcher returns a dispatcher with conservative timeouts.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{DialTimeout: 5 * time.Second, WriteTimeout: 60 * time.Second, slots: map[string]chan struct{}{}}
}

func (d *Dispatcher) slot(addr string) chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	s, ok := d.slots[addr]
	if !ok {
		s = make(chan struct{}, 1)
		d.slots[addr] = s
	}
	return s
}

// Send delivers job to host:port, waiting for any job already going to that
// address. The job counts as sent once the printer has accepted every byte.
func (d *Dispatcher) Send(ctx context.Context, host string, port int, job []byte) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	slot := d.slot(addr)
	select {
	case slot <- struct{}{}:
		defer func() { <-slot }()
	case <-ctx.Done():
		return fmt.Errorf("%w: waiting for the previous job: %v", ErrUnreachable, ctx.Err())
	}

	dial := d.dial
	if dial == nil {
		dial = func(ctx context.Context, addr string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: d.DialTimeout}
			return dialer.DialContext(ctx, "tcp", addr)
		}
	}
	conn, err := dial(ctx, addr)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(d.WriteTimeout)); err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if _, err := conn.Write(job); err != nil {
		return fmt.Errorf("%w: sending job: %v", ErrUnreachable, err)
	}
	// Signal the end of the job before closing.
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
	}
	return nil
}
