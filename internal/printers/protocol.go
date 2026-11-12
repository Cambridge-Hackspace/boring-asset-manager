package printers

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/niimbot"
)

// Most printers take a job as one stream of bytes. Some answer each command
// instead, and the next waits for the answer: the job is sent in a
// conversation with the printer, which its connection names as its
// protocol, so that a relay delivering it needs no drivers. A ble
// connection names it in its profile (ble.go), and a device connection
// after its path, as /dev/ttyACM0?protocol=niimbot.

// ProtocolDriver is a driver whose printers hold a conversation over a
// device connection, such as a USB or Bluetooth serial port.
type ProtocolDriver interface {
	Protocol() string
}

// checkProtocol says whether a protocol is one a connection may name.
func checkProtocol(p string) error {
	if p != ProtocolNiimbot {
		return fmt.Errorf("protocol must be %s", ProtocolNiimbot)
	}
	return nil
}

// DeviceAddress is a device connection's address, parsed: the device's
// path, and the protocol the job is sent in, if any.
type DeviceAddress struct {
	Path     string
	Protocol string
}

// ParseDeviceAddress reads a device connection's address: an absolute
// path, then optionally ?protocol= and a protocol.
func ParseDeviceAddress(s string) (DeviceAddress, error) {
	p, query, hasQuery := strings.Cut(s, "?")
	if !strings.HasPrefix(p, "/") {
		return DeviceAddress{}, errors.New("device address must be an absolute path such as /dev/usb/lp0")
	}
	a := DeviceAddress{Path: path.Clean(p)}
	if !hasQuery {
		return a, nil
	}
	values, err := url.ParseQuery(query)
	if err != nil || len(values) != 1 || len(values["protocol"]) != 1 {
		return a, errors.New("device address: after the path may come ?protocol= and a protocol, and nothing else")
	}
	if err := checkProtocol(values["protocol"][0]); err != nil {
		return a, fmt.Errorf("device address: %v", err)
	}
	a.Protocol = values["protocol"][0]
	return a, nil
}

// String is the address in canonical form.
func (a DeviceAddress) String() string {
	if a.Protocol == "" {
		return a.Path
	}
	return a.Path + "?protocol=" + a.Protocol
}

// DevicePath is the path a device connection's address names, which the
// allowlists match.
func DevicePath(address string) string {
	if a, err := ParseDeviceAddress(address); err == nil {
		return a.Path
	}
	return address
}

// deviceFor is a device connection as it is sent on: with the driver's
// protocol, where the connection names none.
func deviceFor(d Driver, c Connection) (Connection, error) {
	a, err := ParseDeviceAddress(c.Address)
	if err != nil {
		return c, fmt.Errorf("%w: connection: %v", ErrInvalidPrinter, err)
	}
	if pd, ok := d.(ProtocolDriver); ok && a.Protocol == "" {
		a.Protocol = pd.Protocol()
	}
	c.Address = a.String()
	return c, nil
}

// ConversationPort is a printer's connection as a conversation uses it:
// what is written to the printer, and what it sends back.
type ConversationPort = niimbot.Port

// conversationPort is the same, as this package names it.
type conversationPort = ConversationPort

// converse sends a job in a protocol's conversation.
func (d *Dispatcher) converse(ctx context.Context, protocol string, port conversationPort, job []byte) error {
	t := niimbot.DefaultTiming
	t.Timeout = d.WriteTimeout
	if d.conversationTiming != nil {
		t = *d.conversationTiming
	}
	return converse(ctx, protocol, port, job, t)
}

// Converse sends a job in a protocol's conversation over a port the
// caller holds, such as a browser relaying it to the printer over its own
// Bluetooth (docs/printing.md#from-a-browser). The printer has timeout to
// answer each command.
func Converse(ctx context.Context, protocol string, port ConversationPort, job []byte, timeout time.Duration) error {
	t := niimbot.DefaultTiming
	t.Timeout = timeout
	return converse(ctx, protocol, port, job, t)
}

func converse(ctx context.Context, protocol string, port conversationPort, job []byte, t niimbot.Timing) error {
	switch protocol {
	case ProtocolNiimbot:
		return niimbot.Converse(ctx, port, job, t)
	}
	return fmt.Errorf("unknown protocol %q", protocol)
}

// converseDevice sends a job in a conversation with a printer on a serial
// device, which is set to pass bytes through unchanged (openSerial).
func (d *Dispatcher) converseDevice(ctx context.Context, a DeviceAddress, job []byte) error {
	f, err := openSerial(a.Path)
	if err != nil {
		return err
	}
	port := &serialPort{f: f, timeout: d.WriteTimeout, received: make(chan []byte, 64), done: make(chan struct{})}
	go port.read()
	err = d.converse(ctx, a.Protocol, port, job)
	if cerr := port.close(); err == nil {
		err = cerr
	}
	return err
}

// serialPort is a conversation's port over a serial device.
type serialPort struct {
	f        *os.File
	timeout  time.Duration
	received chan []byte
	done     chan struct{}
	once     sync.Once
}

func (s *serialPort) Write(ctx context.Context, b []byte) error {
	// Character devices may not support deadlines; then writes block until
	// the printer takes the data.
	_ = s.f.SetWriteDeadline(time.Now().Add(s.timeout))
	if _, err := s.f.Write(b); err != nil {
		return fmt.Errorf("sending job: %v", err)
	}
	return ctx.Err()
}

func (s *serialPort) Received() <-chan []byte { return s.received }

// read passes on what the printer sends until the device is closed.
func (s *serialPort) read() {
	defer close(s.received)
	buf := make([]byte, 512)
	for {
		n, err := s.f.Read(buf)
		if n > 0 {
			select {
			case s.received <- append([]byte(nil), buf[:n]...):
			case <-s.done:
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *serialPort) close() error {
	var err error
	s.once.Do(func() {
		close(s.done)
		err = s.f.Close()
	})
	return err
}
