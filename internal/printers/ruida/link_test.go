package ruida_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/ruida"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/ruida/ruidasim"
)

var fast = ruida.UDPTiming{Ack: 200 * time.Millisecond, Tries: 3}

func newSim(t *testing.T) (*ruidasim.Sim, *ruida.UDP) {
	t.Helper()
	sim := ruidasim.New()
	t.Cleanup(sim.Close)
	return sim, &ruida.UDP{Addr: sim.Addr(), Magic: ruida.DefaultMagic, Timing: fast}
}

// file is a job big enough to take several packets.
func file(t *testing.T) []byte {
	t.Helper()
	var paths []ruida.Path
	for i := range 200 {
		y := float64(i%50) + 1
		paths = append(paths, ruida.Path{Points: []ruida.Point{{X: 1, Y: y}, {X: 50, Y: y}, {X: 50, Y: y + 0.5}}})
	}
	f, err := ruida.File(ruida.Program{Bed: ruida.Bed{WidthMM: 600, HeightMM: 400}, Paths: paths, Cut: ruida.Setting{Power: 50, Speed: 20, Passes: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(f) < 3*ruida.ChunkSize {
		t.Fatalf("the file is only %d bytes", len(f))
	}
	return f
}

func TestAnUploadIsAckedPacketByPacketAndKeptByName(t *testing.T) {
	sim, link := newSim(t)
	f := file(t)
	if err := link.Send(context.Background(), ruida.Upload("BAM", f)); err != nil {
		t.Fatal(err)
	}
	got, sumOK := sim.File("BAM")
	if !bytes.Equal(got, f) || !sumOK {
		t.Fatalf("kept %d bytes of %d, sum right %v", len(got), len(f), sumOK)
	}
	if n := sim.Packets(); n < 4 {
		t.Errorf("%d packets", n)
	}
	if sim.BadChecksums() != 0 {
		t.Errorf("%d bad checksums", sim.BadChecksums())
	}
	for _, a := range sim.Actions() {
		if strings.Contains(a, "start") {
			t.Errorf("the controller was started: %v", sim.Actions())
		}
	}
}

func TestStatusIsReadFromMemory(t *testing.T) {
	sim, link := newSim(t)
	if err := link.Send(context.Background(), ruida.Upload("BAM", file(t))); err != nil {
		t.Fatal(err)
	}
	v, err := link.Read(context.Background(), ruida.MemMachineStatus)
	if err != nil || v&ruida.MachineRunning != 0 {
		t.Fatalf("idle: %#x %v", v, err)
	}
	sim.PressStart()
	if v, err = link.Read(context.Background(), ruida.MemMachineStatus); err != nil || v&ruida.MachineRunning == 0 {
		t.Fatalf("running: %#x %v", v, err)
	}
}

func TestTheFirstPacketIsSentAgainOnANAK(t *testing.T) {
	sim, link := newSim(t)
	sim.Set(func(s *ruidasim.Sim) { s.NAKs = 2 })
	f := file(t)
	if err := link.Send(context.Background(), ruida.Upload("BAM", f)); err != nil {
		t.Fatal(err)
	}
	if got, _ := sim.File("BAM"); !bytes.Equal(got, f) {
		t.Error("the file was not kept whole")
	}
	if sim.Resent() != 2 {
		t.Errorf("sent again %d times", sim.Resent())
	}
}

func TestATransferThatFaltersPastItsFirstPacketIsAbandoned(t *testing.T) {
	sim, link := newSim(t)
	f := file(t)
	// The first packet taken, the second asked for again.
	sim.Set(func(s *ruidasim.Sim) { s.NAKPacket = 2 })
	err := link.Send(context.Background(), ruida.Upload("BAM", f))
	if err == nil || !strings.Contains(err.Error(), "abandoned") {
		t.Fatalf("sent: %v", err)
	}
	if got, _ := sim.File("BAM"); got != nil {
		t.Error("a broken file was kept")
	}
	if sim.Resent() != 0 {
		t.Error("a packet past the first was sent again")
	}
}

func TestARefusalOrSilenceIsAnError(t *testing.T) {
	sim, link := newSim(t)
	sim.Set(func(s *ruidasim.Sim) { s.Refuse = 1 })
	if err := link.Send(context.Background(), ruida.StopProcess()); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("refused: %v", err)
	}
	sim.Set(func(s *ruidasim.Sim) { s.Drops = 10 })
	if _, err := link.Read(context.Background(), ruida.MemMachineStatus); err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("silent: %v", err)
	}
	// One silence, then an answer: a status request is asked again.
	sim.Set(func(s *ruidasim.Sim) { s.Drops = 1 })
	if _, err := link.Read(context.Background(), ruida.MemMachineStatus); err != nil {
		t.Errorf("asked again: %v", err)
	}
}

func TestAnotherKeyIsSpoken(t *testing.T) {
	sim, link := newSim(t)
	sim.Set(func(s *ruidasim.Sim) { s.Magic = 0x11 })
	link.Magic = 0x11
	if err := link.Send(context.Background(), ruida.PauseProcess()); err != nil {
		t.Fatal(err)
	}
	if a := sim.Actions(); len(a) != 1 || a[0] != "pause" {
		t.Errorf("actions %v", a)
	}
	link.Magic = ruida.DefaultMagic
	if err := link.Send(context.Background(), ruida.PauseProcess()); err == nil {
		t.Error("a controller with another key took it")
	}
}

// usbController is a controller on the far end of a pipe, as over USB:
// swizzled bytes, no checksum and no ACK, and an answer only to a status
// request. What it was sent comes on the channel once it is closed.
func usbController(t *testing.T, running bool) (*ruida.Stream, chan []byte) {
	t.Helper()
	got := make(chan []byte, 16)
	open := func() (io.ReadWriteCloser, error) {
		near, far := net.Pipe()
		go func() {
			defer far.Close()
			var all []byte
			b := make([]byte, 4096)
			for {
				n, err := far.Read(b)
				data := ruida.UnswizzleAll(b[:n], ruida.DefaultMagic)
				all = append(all, data...)
				if bytes.Equal(data, ruida.GetSetting(ruida.MemMachineStatus)) {
					v := byte(0)
					if running {
						v = 1
					}
					_, _ = far.Write(ruida.SwizzleAll([]byte{0xDA, 0x01, 0x04, 0x00, 0, 0, 0, 0, v}, ruida.DefaultMagic))
				}
				if err != nil {
					got <- all
					return
				}
			}
		}()
		return near, nil
	}
	return &ruida.Stream{Key: t.Name(), Magic: ruida.DefaultMagic, Answer: time.Second, Open: open}, got
}

func TestOverUSBTheFileIsSwizzledWithoutChecksums(t *testing.T) {
	s, got := usbController(t, false)
	f := file(t)
	if err := s.Send(context.Background(), ruida.Upload("BAM", f)); err != nil {
		t.Fatal(err)
	}
	if sent := <-got; !bytes.Equal(sent, ruida.Upload("BAM", f)) {
		t.Errorf("sent %d bytes", len(sent))
	}
	s, _ = usbController(t, true)
	v, err := s.Read(context.Background(), ruida.MemMachineStatus)
	if err != nil || v != ruida.MachineRunning {
		t.Errorf("status %#x %v", v, err)
	}
}
