// Package ruidasim simulates a Ruida controller for tests, over UDP: it
// checks each packet's checksum, answering ACK or NAK swizzled, keeps the
// files sent it by name, answers status requests, and runs the file last
// sent when a test presses Start on its panel, as the community's
// descriptions of the protocol have the controllers do. It records what it
// was sent. Nothing here burns anything.
package ruidasim

import (
	"bytes"
	"net"
	"slices"
	"sync"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/ruida"
)

// Sim is a simulated controller.
type Sim struct {
	conn  *net.UDPConn
	Magic byte

	mu sync.Mutex
	// NAKs is how many packets to come it answers NAK, as if their
	// checksums were wrong; Drops how many it does not answer at all;
	// Refuse how many it answers ERR.
	NAKs, Drops, Refuse int
	// NAKPacket is the packet, counting from 1, it answers NAK, once.
	NAKPacket int

	packets      int
	badChecksums int
	resent       int
	lastPacket   []byte
	receiving    string
	buf          []byte
	files        map[string][]byte
	current      string
	running      bool
	actions      []string
	started      bool
	sumsOK       map[string]bool
}

// New starts a simulated controller with the default swizzle key.
func New() *Sim {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		panic(err)
	}
	s := &Sim{conn: conn, Magic: ruida.DefaultMagic, files: map[string][]byte{}, sumsOK: map[string]bool{}}
	go s.serve()
	return s
}

// Addr is its address, host:port.
func (s *Sim) Addr() string { return s.conn.LocalAddr().String() }

// Close stops it.
func (s *Sim) Close() { s.conn.Close() }

// Set changes it, under its lock.
func (s *Sim) Set(f func(*Sim)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s)
}

// File is a file it keeps, by name, and whether its sum was right.
func (s *Sim) File(name string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Clone(s.files[name]), s.sumsOK[name]
}

// Actions are the control commands it was sent: stop, pause, resume, and
// start, which BAM! must never send.
func (s *Sim) Actions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.actions)
}

// BadChecksums is how many packets came with a checksum that did not
// match, and Packets how many came at all.
func (s *Sim) BadChecksums() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.badChecksums
}

// Packets is how many packets came.
func (s *Sim) Packets() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.packets
}

// Resent is how many packets came again, the same as the one before.
func (s *Sim) Resent() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resent
}

// PressStart is the panel's Start: the file last sent runs.
func (s *Sim) PressStart() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != "" {
		s.running = true
	}
}

// Finish ends the job running.
func (s *Sim) Finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
}

func (s *Sim) serve() {
	buf := make([]byte, 4096)
	for {
		n, from, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		for _, reply := range s.handle(slices.Clone(buf[:n])) {
			_, _ = s.conn.WriteToUDP(reply, from)
		}
	}
}

// handle takes a packet, and returns the answers to send, swizzled.
func (s *Sim) handle(p []byte) [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	replies := s.answer(p)
	for i, r := range replies {
		replies[i] = ruida.SwizzleAll(r, s.Magic)
	}
	return replies
}

// answer is what to answer a packet, held with mu.
func (s *Sim) answer(p []byte) [][]byte {
	s.packets++
	if len(p) < 3 {
		return nil
	}
	if bytes.Equal(p, s.lastPacket) {
		s.resent++
	}
	s.lastPacket = p
	if uint16(p[0])<<8|uint16(p[1]) != ruida.Checksum(p[2:]) {
		s.badChecksums++
		return [][]byte{{ruida.NAK}}
	}
	switch {
	case s.NAKPacket == s.packets:
		s.NAKPacket = 0
		return [][]byte{{ruida.NAK}}
	case s.Drops > 0:
		s.Drops--
		return nil
	case s.NAKs > 0:
		s.NAKs--
		return [][]byte{{ruida.NAK}}
	case s.Refuse > 0:
		s.Refuse--
		return [][]byte{{ruida.ERR}}
	}
	data := ruida.UnswizzleAll(p[2:], s.Magic)
	out := [][]byte{{ruida.ACK}}
	if s.receiving != "" {
		s.take(data)
		return out
	}
	name := ruida.FileTransferName()
	switch {
	case bytes.HasPrefix(data, name):
		rest := data[len(name):]
		end := bytes.IndexByte(rest, 0)
		if end < 0 {
			return out
		}
		s.receiving, s.buf = string(rest[:end]), nil
		s.take(rest[end+1:])
	case len(data) >= 4 && data[0] == 0xDA && data[1] == 0x00:
		var v int64
		if s.running {
			v = ruida.MachineRunning
		}
		reply := []byte{0xDA, 0x01, data[2], data[3]}
		for i := 4; i >= 0; i-- {
			reply = append(reply, byte(v>>(7*i))&0x7F)
		}
		out = append(out, reply)
	case bytes.Equal(data, ruida.StartProcess):
		s.actions = append(s.actions, "start")
		s.running = s.current != ""
	case bytes.Equal(data, ruida.StopProcess()):
		s.actions = append(s.actions, "stop")
		s.running = false
	case bytes.Equal(data, ruida.PauseProcess()):
		s.actions = append(s.actions, "pause")
	case bytes.Equal(data, ruida.RestoreProcess()):
		s.actions = append(s.actions, "resume")
	}
	return out
}

// take adds to the file being received, keeping it when it ends.
func (s *Sim) take(data []byte) {
	s.buf = append(s.buf, data...)
	if bytes.Contains(s.buf, ruida.StartProcess) {
		s.actions = append(s.actions, "start in a file")
	}
	if len(s.buf) == 0 || s.buf[len(s.buf)-1] != 0xD7 {
		return
	}
	// The file's sum: the bytes before E5 05, and the end of file.
	ok := false
	sum := ruida.SetFileSum()
	if i := bytes.LastIndex(s.buf, sum); i >= 0 && len(s.buf) >= i+len(sum)+6 {
		var want int64 = 0xD7
		for _, b := range s.buf[:i] {
			want += int64(b)
		}
		ok = ruida.Value35(s.buf[i+len(sum):]) == want
	}
	s.files[s.receiving] = s.buf
	s.sumsOK[s.receiving] = ok
	s.current, s.receiving, s.buf = s.receiving, "", nil
}
