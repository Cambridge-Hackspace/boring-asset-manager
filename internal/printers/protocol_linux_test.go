//go:build linux

package printers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/niimbot"
)

// openPTY opens a pseudo-terminal: the printer's end, and the path of the
// end BAM! opens as a serial device.
func openPTY(t *testing.T) (*os.File, string) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminals: %v", err)
	}
	t.Cleanup(func() { master.Close() })
	conn, _ := master.SyscallConn()
	var n int
	var cerr error
	if err := conn.Control(func(fd uintptr) {
		if cerr = unix.IoctlSetPointerInt(int(fd), unix.TIOCSPTLCK, 0); cerr == nil {
			n, cerr = unix.IoctlGetInt(int(fd), unix.TIOCGPTN)
		}
	}); err != nil || cerr != nil {
		t.Skipf("no pseudo-terminal: %v %v", err, cerr)
	}
	return master, fmt.Sprintf("/dev/pts/%d", n)
}

// A Niimbot printer on a serial device, such as a B21 over USB, is
// conversed with through the terminal, set raw: a job holding newlines and
// carriage returns arrives as sent.
func TestNiimbotPrintsOverASerialDevice(t *testing.T) {
	master, name := openPTY(t)
	plan, _, err := PlanLabel(DriverNiimbot, Settings{Model: "B21", Media: "40x30"}, LabelSpec{WidthMM: 40, HeightMM: 30, DPI: 203})
	if err != nil {
		t.Fatal(err)
	}
	// Rows of 0x0A and 0x0D bytes, which a terminal not set raw changes.
	label := image.NewGray(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			// 0x0A on odd rows, 0x0D on even ones.
			if bit := x % 8; y%2 == 1 && (bit == 4 || bit == 6) || y%2 == 0 && (bit == 4 || bit == 5 || bit == 7) {
				label.Pix[y*label.Stride+x] = 0
			} else {
				label.Pix[y*label.Stride+x] = 0xFF
			}
		}
	}
	job, err := plan.Encode([]*image.Gray{label})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(job, []byte{0x0A, 0x0A}) || !bytes.Contains(job, []byte{0x0D, 0x0D}) {
		t.Fatal("the job tests nothing")
	}

	// The printer: it answers every command, and reports print end
	// finished.
	received := make(chan []byte, 1)
	go func() {
		var all []byte
		var r niimbot.Reader
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if err != nil {
				received <- all
				return
			}
			all = append(all, buf[:n]...)
			for _, p := range r.Add(buf[:n]) {
				if reply, ok := niimbotReplies[p.Cmd]; ok {
					data := []byte{1}
					if p.Cmd == niimbot.CmdRFIDInfo {
						data = []byte{0}
					}
					_, _ = master.Write(niimbot.Packet{Cmd: reply, Data: data}.Bytes())
				}
				if p.Cmd == niimbot.CmdPrintEnd {
					received <- all
					return
				}
			}
		}
	}()

	d := NewDispatcher([]string{"/dev/pts/*"})
	d.conversationTiming = &niimbot.Timing{Timeout: 2 * time.Second, Poll: time.Millisecond}
	conn, err := ConnectionFor(DriverNiimbot, Connection{Type: ConnDevice, Address: name})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Send(context.Background(), conn, job); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-received:
		if !bytes.Equal(got, job) {
			t.Errorf("the printer got %d bytes, not the job's %d", len(got), len(job))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the printer got nothing")
	}

	// A printer that never answers times out cleanly.
	master2, name2 := openPTY(t)
	go func() { _, _ = master2.Read(make([]byte, 64)) }()
	d.conversationTiming = &niimbot.Timing{Timeout: 50 * time.Millisecond}
	err = d.Send(context.Background(), Connection{Type: ConnDevice, Address: name2 + "?protocol=niimbot"}, job)
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "did not answer connect within 50ms") {
		t.Errorf("silent printer: %v", err)
	}
}
