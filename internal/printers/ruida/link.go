package ruida

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Link is how a controller is reached: over Ethernet (UDP) or USB.
type Link interface {
	// Send sends commands, unswizzled.
	Send(ctx context.Context, payload []byte) error
	// Read reads a word of the controller's memory.
	Read(ctx context.Context, mem [2]byte) (int64, error)
}

// Ports: the controller listens on PortController, and answers to
// PortReply.
const (
	PortController = 50200
	PortReply      = 40200
)

// ChunkSize is the most a packet carries: the protocol allows 1472 bytes
// with the checksum, and RDWorks and LightBurn cut near 1000, between
// commands.
const ChunkSize = 1000

// UDPTiming is how long the controller is waited for.
type UDPTiming struct {
	// Ack is how long a packet's answer may take.
	Ack time.Duration
	// Tries is how many times a packet that may be sent again is.
	Tries int
}

// DefaultUDPTiming is the timing unless one is given.
var DefaultUDPTiming = UDPTiming{Ack: 2 * time.Second, Tries: 3}

// UDP reaches a controller over Ethernet: each packet a checksum and its
// swizzled payload, each answered by an ACK, or a NAK asking for it again.
// A transfer's packets after the first are not sent again: the controller
// is not known to take them twice, so a transfer that falters is
// abandoned.
type UDP struct {
	// Addr is the controller's host:port.
	Addr string
	// LocalPort is the port answers come to: PortReply, or 0 for any.
	LocalPort int
	Magic     byte
	Timing    UDPTiming
}

var (
	udpPortsMu sync.Mutex
	udpPorts   = map[int]*sync.Mutex{}
)

// portLock is held while this server talks through a local port: only one
// socket may have it, and answers through it are for one exchange.
func portLock(port int) *sync.Mutex {
	udpPortsMu.Lock()
	defer udpPortsMu.Unlock()
	m, ok := udpPorts[port]
	if !ok {
		m = &sync.Mutex{}
		udpPorts[port] = m
	}
	return m
}

func (u *UDP) timing() UDPTiming {
	if u.Timing == (UDPTiming{}) {
		return DefaultUDPTiming
	}
	return u.Timing
}

// open opens a socket on the local port, with the port held.
func (u *UDP) open(ctx context.Context) (*net.UDPConn, *net.UDPAddr, func(), error) {
	remote, err := net.ResolveUDPAddr("udp", u.Addr)
	if err != nil {
		return nil, nil, nil, err
	}
	lock := portLock(u.LocalPort)
	lock.Lock()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: u.LocalPort})
	if err != nil {
		lock.Unlock()
		return nil, nil, nil, fmt.Errorf("listening for the controller's answers on port %d: %w", u.LocalPort, err)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	return conn, remote, func() {
		stop()
		conn.Close()
		lock.Unlock()
	}, nil
}

// exchange sends a packet and waits for its ACK and, when want names a
// memory address, the controller's answer.
func (u *UDP) exchange(ctx context.Context, conn *net.UDPConn, remote *net.UDPAddr, payload []byte, again bool, want *[2]byte) ([]byte, error) {
	t := u.timing()
	tries := 1
	if again {
		tries = max(t.Tries, 1)
	}
	packet := Packet(payload, u.Magic)
	buf := make([]byte, 2048)
	var last error
	for range tries {
		if _, err := conn.WriteToUDP(packet, remote); err != nil {
			return nil, err
		}
		deadline := time.Now().Add(t.Ack)
		acked := false
	answers:
		for {
			_ = conn.SetReadDeadline(deadline)
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				last = errors.New("the controller did not answer")
				if acked {
					last = errors.New("the controller did not answer what was asked")
				}
				break answers
			}
			if !from.IP.Equal(remote.IP) {
				continue
			}
			data := UnswizzleAll(buf[:n], u.Magic)
			if len(data) == 1 {
				switch data[0] {
				case ACK:
					if want == nil {
						return nil, nil
					}
					acked = true
				case NAK:
					last = errors.New("the controller found the packet's checksum wrong")
					break answers
				case ERR:
					return nil, errors.New("the controller refused the packet")
				}
				continue
			}
			if want != nil && len(data) >= 9 && data[0] == 0xDA && data[1] == 0x01 && data[2] == want[0] && data[3] == want[1] {
				return data, nil
			}
		}
	}
	return nil, last
}

// Send sends a payload in packets of ChunkSize bytes at most, only the
// first of which is sent again when it is not taken.
func (u *UDP) Send(ctx context.Context, payload []byte) error {
	conn, remote, done, err := u.open(ctx)
	if err != nil {
		return err
	}
	defer done()
	chunks := Chunks(payload, ChunkSize)
	for i, c := range chunks {
		if _, err := u.exchange(ctx, conn, remote, c, i == 0, nil); err != nil {
			if i > 0 {
				return fmt.Errorf("packet %d of %d: %w; the transfer was abandoned", i+1, len(chunks), err)
			}
			return err
		}
	}
	return nil
}

// Read reads a word of memory.
func (u *UDP) Read(ctx context.Context, mem [2]byte) (int64, error) {
	conn, remote, done, err := u.open(ctx)
	if err != nil {
		return 0, err
	}
	defer done()
	data, err := u.exchange(ctx, conn, remote, GetSetting(mem), true, &mem)
	if err != nil {
		return 0, err
	}
	return Value35(data[4:9]), nil
}

// Stream reaches a controller over USB, a serial port: the payload
// swizzled, with no checksum and no answer but to what is asked.
type Stream struct {
	// Open opens the port, for each exchange.
	Open  func() (io.ReadWriteCloser, error)
	Magic byte
	// Answer is how long an answer may take.
	Answer time.Duration
	// Key names the port, for holding it while it is used.
	Key string
}

var (
	streamLocksMu sync.Mutex
	streamLocks   = map[string]*sync.Mutex{}
)

func (s *Stream) lock() *sync.Mutex {
	streamLocksMu.Lock()
	defer streamLocksMu.Unlock()
	m, ok := streamLocks[s.Key]
	if !ok {
		m = &sync.Mutex{}
		streamLocks[s.Key] = m
	}
	return m
}

func (s *Stream) open() (io.ReadWriteCloser, func(), error) {
	l := s.lock()
	l.Lock()
	rw, err := s.Open()
	if err != nil {
		l.Unlock()
		return nil, nil, err
	}
	return rw, func() { rw.Close(); l.Unlock() }, nil
}

// Send writes the payload.
func (s *Stream) Send(ctx context.Context, payload []byte) error {
	rw, done, err := s.open()
	if err != nil {
		return err
	}
	defer done()
	stop := context.AfterFunc(ctx, func() { rw.Close() })
	defer stop()
	_, err = rw.Write(SwizzleAll(payload, s.Magic))
	return err
}

// deadliner is a port that takes deadlines.
type deadliner interface {
	SetReadDeadline(time.Time) error
}

// Read asks for a word of memory, and reads the answer.
func (s *Stream) Read(ctx context.Context, mem [2]byte) (int64, error) {
	rw, done, err := s.open()
	if err != nil {
		return 0, err
	}
	defer done()
	stop := context.AfterFunc(ctx, func() { rw.Close() })
	defer stop()
	answer := s.Answer
	if answer <= 0 {
		answer = DefaultUDPTiming.Ack
	}
	if d, ok := rw.(deadliner); ok {
		_ = d.SetReadDeadline(time.Now().Add(answer))
	} else {
		t := time.AfterFunc(answer, func() { rw.Close() })
		defer t.Stop()
	}
	if _, err := rw.Write(SwizzleAll(GetSetting(mem), s.Magic)); err != nil {
		return 0, err
	}
	want := []byte{0xDA, 0x01, mem[0], mem[1]}
	var got []byte
	b := make([]byte, 64)
	for {
		n, err := rw.Read(b)
		got = append(got, UnswizzleAll(b[:n], s.Magic)...)
		if i := bytes.Index(got, want); i >= 0 && len(got) >= i+9 {
			return Value35(got[i+4 : i+9]), nil
		}
		if err != nil {
			return 0, fmt.Errorf("the controller did not answer: %w", err)
		}
	}
}
