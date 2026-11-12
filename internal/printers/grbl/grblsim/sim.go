// Package grblsim simulates a GRBL laser controller for tests, over TCP: a
// receive buffer of a set size that lines wait in until the controller
// takes them, ok and error:N answers, alarms, status reports, holds and
// resets, as GRBL 1.1's interface describes them. It records what it was
// sent, and whether its buffer was ever overfilled. Nothing here burns
// anything.
package grblsim

import (
	"bytes"
	"fmt"
	"maps"
	"net"
	"slices"
	"strings"
	"sync"
	"time"
)

// Banner is the welcome it sends on connecting and after a reset.
const Banner = "Grbl 1.1h ['$' for help]"

// Sim is a simulated controller.
type Sim struct {
	ln net.Listener

	mu sync.Mutex
	// RX is the receive buffer's size: 128 bytes, as on an Arduino,
	// unless a test sets another.
	RX int
	// LineTime is how long it takes over each line.
	LineTime time.Duration
	// Settings are what $$ lists, by number.
	Settings map[int]string
	// Refuse answers error:N, with N its result, to a line it gives a
	// number for.
	Refuse func(line string) int
	// NoBanner keeps it from welcoming, as one reached over a network
	// that does not restart when connected to.
	NoBanner bool

	state    string
	held     bool
	buf      []byte
	maxBuf   int
	overflow bool
	lines    []string
	realtime []byte
	conn     net.Conn
	conns    int
	wake     chan struct{}
	closed   bool
}

// New starts a simulated controller, idle, in laser mode ($32=1) with full
// power at S1000 ($30=1000).
func New() *Sim {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	s := &Sim{
		ln: ln, RX: 128, LineTime: 200 * time.Microsecond,
		Settings: map[int]string{30: "1000", 31: "0", 32: "1"},
		state:    "Idle", wake: make(chan struct{}, 1),
	}
	go s.accept()
	go s.run()
	return s
}

// Addr is its address, host:port.
func (s *Sim) Addr() string { return s.ln.Addr().String() }

// Close stops it.
func (s *Sim) Close() {
	s.mu.Lock()
	s.closed = true
	if s.conn != nil {
		s.conn.Close()
	}
	s.mu.Unlock()
	s.ln.Close()
	s.poke()
}

// Set changes it, under its lock.
func (s *Sim) Set(f func(*Sim)) {
	s.mu.Lock()
	f(s)
	s.mu.Unlock()
	s.poke()
}

func (s *Sim) poke() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Lines are the lines it has taken, in order, besides $ commands.
func (s *Sim) Lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.lines)
}

// Realtime are the real-time commands it was sent, besides ?.
func (s *Sim) Realtime() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Clone(s.realtime)
}

// MaxBuffered is the most its receive buffer has held, and Overflowed
// whether it was ever sent more than it holds.
func (s *Sim) MaxBuffered() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxBuf
}

// Overflowed reports whether it was ever sent more than its buffer holds.
func (s *Sim) Overflowed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.overflow
}

// Connections is how many times it has been connected to.
func (s *Sim) Connections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conns
}

// State is its state as its status reports give it.
func (s *Sim) State() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// SetState puts it in a state, such as Door:0.
func (s *Sim) SetState(state string) {
	s.Set(func(s *Sim) { s.state = state })
}

// Alarm raises an alarm: the job stops, and what it had buffered is lost.
func (s *Sim) Alarm(code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state, s.buf, s.held = "Alarm", nil, false
	s.write(fmt.Sprintf("ALARM:%d", code))
}

// write sends a line to the connection, held with mu.
func (s *Sim) write(line string) {
	if s.conn != nil {
		_, _ = s.conn.Write([]byte(line + "\r\n"))
	}
}

func (s *Sim) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.conn != nil {
			s.conn.Close()
		}
		s.conn, s.buf, s.held = c, nil, false
		s.conns++
		if !s.NoBanner {
			s.write("")
			s.write(Banner)
		}
		s.mu.Unlock()
		go s.read(c)
	}
}

func (s *Sim) read(c net.Conn) {
	b := make([]byte, 256)
	for {
		n, err := c.Read(b)
		if err != nil {
			return
		}
		s.mu.Lock()
		for _, ch := range b[:n] {
			switch ch {
			case '?':
				s.write("<" + s.state + "|MPos:0.000,0.000,0.000|FS:0,0>")
			case '!':
				s.realtime = append(s.realtime, ch)
				if s.state == "Run" || s.state == "Idle" {
					s.held, s.state = true, "Hold:0"
				}
			case '~':
				s.realtime = append(s.realtime, ch)
				if s.held {
					s.held, s.state = false, "Run"
				}
			case 0x18:
				s.realtime = append(s.realtime, ch)
				// Reset while moving loses the position, and locks it.
				if s.state == "Run" {
					s.state = "Alarm"
				} else if s.state != "Alarm" {
					s.state = "Idle"
				}
				s.buf, s.held = nil, false
				s.write("")
				s.write(Banner)
			default:
				s.buf = append(s.buf, ch)
				s.maxBuf = max(s.maxBuf, len(s.buf))
				if len(s.buf) > s.RX {
					s.overflow = true
				}
			}
		}
		s.mu.Unlock()
		s.poke()
	}
}

// run takes lines off the buffer, LineTime each, while it is not held.
func (s *Sim) run() {
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		i := bytes.IndexByte(s.buf, '\n')
		if i < 0 || s.held {
			if i < 0 && s.state == "Run" {
				s.state = "Idle"
			}
			s.mu.Unlock()
			select {
			case <-s.wake:
			case <-time.After(10 * time.Millisecond):
			}
			continue
		}
		wait := s.LineTime
		s.mu.Unlock()
		time.Sleep(wait)
		s.mu.Lock()
		// The buffer may have been emptied by a reset or an alarm meanwhile.
		if i = bytes.IndexByte(s.buf, '\n'); i < 0 || s.held {
			s.mu.Unlock()
			continue
		}
		line := strings.TrimSpace(string(s.buf[:i]))
		s.buf = slices.Clone(s.buf[i+1:])
		s.take(line)
		s.mu.Unlock()
	}
}

// take acts on a line, held with mu.
func (s *Sim) take(line string) {
	switch {
	case line == "$$":
		for _, n := range slices.Sorted(maps.Keys(s.Settings)) {
			s.write(fmt.Sprintf("$%d=%s", n, s.Settings[n]))
		}
		s.write("ok")
	case line == "$X":
		if s.state == "Alarm" {
			s.state = "Idle"
		}
		s.write("ok")
	case s.state == "Alarm":
		s.write("error:9")
	case s.Refuse != nil && s.Refuse(line) != 0:
		s.write(fmt.Sprintf("error:%d", s.Refuse(line)))
	default:
		s.lines = append(s.lines, line)
		if s.state == "Idle" {
			s.state = "Run"
		}
		s.write("ok")
	}
}
