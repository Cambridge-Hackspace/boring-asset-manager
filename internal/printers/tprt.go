package printers

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Some network tape printers take a job on TCP only after a handshake over
// UDP on the same port: KING JIM's Epson-built TEPRA PRO SR5900P does, and
// Epson's LW-PX800, which is built on the same platform, may. A tcp
// connection names it as its protocol, as printer.lan:9100?protocol=tprt,
// so that a relay delivering it needs no drivers.
//
// The handshake was reconstructed, as facts only, from hikalium/sr5900p
// (MIT), which prints on an SR5900P over it. No code is taken from it, and
// it has not been tried on an Epson printer. Each UDP message is a 32-byte header of
// eight big-endian words, "TPRT" (the printer answers "tprt"), 0, 1, 0x20,
// the command, the length of the data after the header, an IP address and
// a token (both 0 in requests), then the data. A job:
//
//  1. asks for status (command 1), which answers 20 bytes;
//  2. starts a job (2), which answers 02 00 00;
//  3. connects over TCP, then sends commands 0x0101 and 0x0100, which
//     answers 10 (or 00);
//  4. writes the job over TCP;
//  5. asks for status until the printer is no longer printing;
//  6. stops the job (3), which answers 03 00 00.
//
// Half a second passes between steps 2, 3 and 4, as hikalium/sr5900p
// waits.

// ProtocolTPRT is the handshake a tcp connection may name.
const ProtocolTPRT = "tprt"

const (
	tprtStatus = 0x0001
	tprtStart  = 0x0002
	tprtStop   = 0x0003
	tprtNotify = 0x0101
	tprtData   = 0x0100
	// tprtHeader is the header's length.
	tprtHeader = 32
)

// The status's bytes: what the printer is doing, and what is wrong.
const (
	tprtStatusState   = 1
	tprtStatusError   = 2
	tprtStatePrinting = 2
	tprtErrNoTape     = 0x06
	tprtErrCoverOpen  = 0x21
)

// TPRTTiming is how a TPRT job is paced.
type TPRTTiming struct {
	// Pause is the wait between starting the job, connecting and writing.
	Pause time.Duration
	// Poll is how often the printer is asked whether it is still printing.
	Poll time.Duration
	// Reply is how long the printer has to answer each message.
	Reply time.Duration
}

// DefaultTPRTTiming waits as hikalium/sr5900p does.
var DefaultTPRTTiming = TPRTTiming{Pause: 500 * time.Millisecond, Poll: 500 * time.Millisecond, Reply: 3 * time.Second}

// TCPAddress is a tcp connection's address, parsed: host:port, and the
// protocol the job is sent in, if any.
type TCPAddress struct {
	HostPort string
	Protocol string
}

// ParseTCPAddress reads a normalized tcp connection's address: host:port,
// then optionally ?protocol= and a protocol.
func ParseTCPAddress(s string) (TCPAddress, error) {
	hostPort, query, hasQuery := strings.Cut(s, "?")
	a := TCPAddress{HostPort: hostPort}
	if !hasQuery {
		return a, nil
	}
	values, err := url.ParseQuery(query)
	if err != nil || len(values) != 1 || len(values["protocol"]) != 1 {
		return a, errors.New("tcp address: after the host and port may come ?protocol= and a protocol, and nothing else")
	}
	if p := values["protocol"][0]; p != ProtocolTPRT {
		return a, fmt.Errorf("tcp address: protocol must be %s", ProtocolTPRT)
	}
	a.Protocol = ProtocolTPRT
	return a, nil
}

// String is the address in canonical form.
func (a TCPAddress) String() string {
	if a.Protocol == "" {
		return a.HostPort
	}
	return a.HostPort + "?protocol=" + a.Protocol
}

// TCPHostPort is the host and port a tcp connection's address names, which
// allowlists match.
func TCPHostPort(address string) string {
	a, _ := ParseTCPAddress(address)
	return a.HostPort
}

// TCPProtocolDriver is a driver whose printers may need a protocol on a tcp
// connection.
type TCPProtocolDriver interface {
	TCPProtocols() []string
}

// checkTCPProtocol refuses a protocol on a tcp connection whose driver
// takes none.
func checkTCPProtocol(d Driver, c Connection) error {
	if c.Type != ConnTCP {
		return nil
	}
	a, err := ParseTCPAddress(c.Address)
	if err != nil || a.Protocol == "" {
		return err
	}
	if pd, ok := d.(TCPProtocolDriver); ok {
		for _, p := range pd.TCPProtocols() {
			if p == a.Protocol {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: connection: this printer takes no ?protocol=%s on a tcp connection", ErrInvalidPrinter, a.Protocol)
}

// tprtMessage is a request, or the header of an answer.
func tprtMessage(cmd uint32) []byte {
	b := []byte("TPRT")
	for _, w := range []uint32{0, 1, tprtHeader, cmd, 0, 0, 0} {
		b = binary.BigEndian.AppendUint32(b, w)
	}
	return b
}

// tprtSession is a job's UDP exchange with the printer.
type tprtSession struct {
	conn   net.Conn
	timing TPRTTiming
}

// ask sends a command and returns the data of its answer.
func (s tprtSession) ask(ctx context.Context, cmd uint32) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(s.timing.Reply)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = s.conn.SetDeadline(deadline)
	if _, err := s.conn.Write(tprtMessage(cmd)); err != nil {
		return nil, fmt.Errorf("handshake: %v", err)
	}
	buf := make([]byte, 512)
	n, err := s.conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("handshake: the printer did not answer command %#04x: %v", cmd, err)
	}
	if n < tprtHeader || !bytes.Equal(buf[:4], []byte("tprt")) {
		return nil, fmt.Errorf("handshake: the printer answered command %#04x with % X", cmd, buf[:min(n, 8)])
	}
	return buf[tprtHeader:n], nil
}

// status asks for status, and says what is wrong, if anything.
func (s tprtSession) status(ctx context.Context) (printing bool, err error) {
	data, err := s.ask(ctx, tprtStatus)
	if err != nil {
		return false, err
	}
	if len(data) <= tprtStatusError {
		return false, fmt.Errorf("handshake: a status of %d bytes", len(data))
	}
	switch e := data[tprtStatusError]; e {
	case 0:
	case tprtErrNoTape:
		return false, errors.New("the printer has no tape")
	case tprtErrCoverOpen:
		return false, errors.New("the printer's cover is open")
	default:
		return false, fmt.Errorf("the printer reports error %#02x", e)
	}
	return data[tprtStatusState] == tprtStatePrinting, nil
}

// expect sends a command whose answer must be want.
func (s tprtSession) expect(ctx context.Context, cmd uint32, want ...[]byte) error {
	data, err := s.ask(ctx, cmd)
	if err != nil {
		return err
	}
	for _, w := range want {
		if bytes.Equal(data, w) {
			return nil
		}
	}
	return fmt.Errorf("handshake: the printer answered command %#04x with % X", cmd, data)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// sendTPRT sends a job over TCP after the TPRT handshake, and waits until
// it has printed.
func (d *Dispatcher) sendTPRT(ctx context.Context, hostPort string, job []byte) (err error) {
	timing := DefaultTPRTTiming
	if d.tprtTiming != nil {
		timing = *d.tprtTiming
	}
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return err
	}
	if _, err := strconv.Atoi(port); err != nil {
		return err
	}
	dialer := net.Dialer{Timeout: d.DialTimeout}
	udp, err := dialer.DialContext(ctx, "udp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	defer udp.Close()
	s := tprtSession{conn: udp, timing: timing}

	printing, err := s.status(ctx)
	if err != nil {
		return err
	}
	if printing {
		return errors.New("the printer is printing another job")
	}
	if err := s.expect(ctx, tprtStart, []byte{tprtStart, 0, 0}); err != nil {
		return err
	}
	// Once started, the job is always stopped, so the printer takes the next.
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timing.Reply)
		defer cancel()
		if serr := s.expect(stopCtx, tprtStop, []byte{tprtStop, 0, 0}); err == nil {
			err = serr
		}
	}()
	if err := sleepCtx(ctx, timing.Pause); err != nil {
		return err
	}
	dial := d.dial
	if dial == nil {
		dial = func(ctx context.Context, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", addr)
		}
	}
	conn, err := dial(ctx, hostPort)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := sleepCtx(ctx, timing.Pause); err != nil {
		return err
	}
	if _, err := s.ask(ctx, tprtNotify); err != nil {
		return err
	}
	if err := s.expect(ctx, tprtData, []byte{0x10}, []byte{0x00}); err != nil {
		return err
	}
	if err := sleepCtx(ctx, timing.Pause); err != nil {
		return err
	}
	if err := d.writeJob(conn, conn.SetWriteDeadline, job); err != nil {
		return err
	}
	for {
		if err := sleepCtx(ctx, timing.Poll); err != nil {
			return err
		}
		printing, err := s.status(ctx)
		if err != nil {
			return err
		}
		if !printing {
			return nil
		}
	}
}
