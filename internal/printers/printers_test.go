package printers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brotherql"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/dymolw"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/tspl"
)

func TestValidate(t *testing.T) {
	brother := func(model, media, options string) Settings {
		return Settings{Model: model, Media: media, Options: json.RawMessage(options)}
	}
	ok := func(name, driver, host string, port int, s Settings) {
		t.Helper()
		if err := Validate(name, driver, host, port, s); err != nil {
			t.Errorf("rejected valid printer: %v", err)
		}
	}
	bad := func(why, name, driver, host string, port int, s Settings) {
		t.Helper()
		if err := Validate(name, driver, host, port, s); !errors.Is(err, ErrInvalidPrinter) {
			t.Errorf("%s: got %v, want ErrInvalidPrinter", why, err)
		}
	}
	ok("Front desk", DriverBrotherQL, "192.168.1.40", 9100, brother("QL-810W", "dk-11201", ""))
	ok("Workshop", DriverBrotherQL, "ql-workshop.lan", 9100, brother("QL-820NWB", "dk-22205", `{"cut":false}`))
	ok("IPv6", DriverBrotherQL, "fd00::40", 9100, brother("QL-810W", "dk-22210", "null"))
	bad("blank name", " ", DriverBrotherQL, "10.0.0.1", 9100, brother("QL-810W", "dk-11201", ""))
	bad("driver", "P", "zebra", "10.0.0.1", 9100, brother("QL-810W", "dk-11201", ""))
	bad("model", "P", DriverBrotherQL, "10.0.0.1", 9100, brother("QL-700", "dk-11201", ""))
	bad("media", "P", DriverBrotherQL, "10.0.0.1", 9100, brother("QL-810W", "dk-9999", ""))
	bad("unknown option", "P", DriverBrotherQL, "10.0.0.1", 9100, brother("QL-810W", "dk-11201", `{"cutt":true}`))
	bad("option type", "P", DriverBrotherQL, "10.0.0.1", 9100, brother("QL-810W", "dk-11201", `{"cut":"yes"}`))
	bad("host with scheme", "P", DriverBrotherQL, "tcp://10.0.0.1", 9100, brother("QL-810W", "dk-11201", ""))
	bad("host with port", "P", DriverBrotherQL, "printer:9100", 9100, brother("QL-810W", "dk-11201", ""))
	bad("empty host", "P", DriverBrotherQL, "", 9100, brother("QL-810W", "dk-11201", ""))
	bad("port", "P", DriverBrotherQL, "10.0.0.1", 70000, brother("QL-810W", "dk-11201", ""))

	ok("Dymo", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 450", "30252", ""))
	ok("Dymo dark", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 550", "30334", `{"density":"dark"}`))
	ok("Dymo twin", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 450 Twin Turbo", "30252", `{"roll":"right"}`))
	ok("Dymo XL", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 4XL", "1744907", ""))
	bad("Dymo model", "P", DriverDymoLabelWriter, "lw.lan", 9100, brother("QL-810W", "30252", ""))
	bad("Dymo media", "P", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 450", "dk-11201", ""))
	bad("4-inch roll on a 56 mm head", "P", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 450", "1744907", ""))
	bad("roll on a single-roll model", "P", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 450", "30252", `{"roll":"left"}`))
	bad("unknown roll", "P", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter Twin Turbo", "30252", `{"roll":"middle"}`))
	bad("unknown density", "P", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 450", "30252", `{"density":"max"}`))
	bad("Dymo unknown option", "P", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 450", "30252", `{"cut":true}`))
	ok("Rollo", DriverTSPL, "rollo.lan", 9100, brother("Rollo X1038", "4x6", ""))
	ok("Rollo options", DriverTSPL, "rollo.lan", 9100, brother("Rollo X1040", "2x1", `{"density":10,"rotate_180":true}`))
	bad("TSPL model", "P", DriverTSPL, "rollo.lan", 9100, brother("LabelWriter 450", "4x6", ""))
	bad("TSPL media", "P", DriverTSPL, "rollo.lan", 9100, brother("Rollo X1038", "30252", ""))
	bad("TSPL density", "P", DriverTSPL, "rollo.lan", 9100, brother("Rollo X1038", "4x6", `{"density":16}`))
	bad("TSPL unknown option", "P", DriverTSPL, "rollo.lan", 9100, brother("Rollo X1038", "4x6", `{"speed":4}`))
}

func TestDriversDescribeEveryRoll(t *testing.T) {
	d := Drivers()
	if len(d) != 3 || d[0].Driver != DriverBrotherQL || len(d[0].Media) != 3 || len(d[0].Options) != 1 {
		t.Fatalf("Drivers() = %+v", d)
	}
	if d[1].Driver != DriverDymoLabelWriter || len(d[1].Media) != len(dymolw.MediaIDs()) || len(d[1].Options) != 2 || len(d[1].Options[0].Values) != 4 {
		t.Fatalf("Dymo driver info = %+v", d[1])
	}
	if d[2].Driver != DriverTSPL || len(d[2].Media) != len(tspl.MediaIDs()) || len(d[2].Options) != 2 {
		t.Fatalf("TSPL driver info = %+v", d[2])
	}
}

type namedDriver struct {
	brotherQL
	name string
}

func (n namedDriver) Info() DriverInfo { return DriverInfo{Driver: n.name} }

func TestRegisterRejectsDuplicatesAndBlankNames(t *testing.T) {
	if err := Register(namedDriver{name: DriverBrotherQL}); err == nil {
		t.Error("registered brother_ql twice")
	}
	if err := Register(namedDriver{name: ""}); err == nil {
		t.Error("registered a driver without a name")
	}
}

func TestBrotherPlan(t *testing.T) {
	d, err := Lookup(DriverBrotherQL)
	if err != nil {
		t.Fatal(err)
	}
	settings := Settings{Model: "QL-810W", Media: "dk-11201"}

	if _, err := d.Plan(settings, LabelSpec{WidthMM: 40, HeightMM: 20, DPI: 300}); !errors.Is(err, ErrDoesNotFit) {
		t.Errorf("wrong size: got %v, want ErrDoesNotFit", err)
	}

	label := image.NewGray(image.Rect(0, 0, 1063, 343)) // 90 × 29 mm, all black
	for _, cut := range []bool{true, false} {
		settings.Options = json.RawMessage(`{"cut":` + strconv.FormatBool(cut) + `}`)
		plan, err := d.Plan(settings, LabelSpec{WidthMM: 90, HeightMM: 29, DPI: 300})
		if err != nil {
			t.Fatal(err)
		}
		job, err := plan.Encode([]*image.Gray{label, label})
		if err != nil {
			t.Fatal(err)
		}
		pages, err := brotherql.DecodeJob(job)
		if err != nil {
			t.Fatal(err)
		}
		if len(pages) != 2 || len(pages[0].Lines) != 991 || pages[0].AutoCut != cut {
			t.Errorf("cut=%v: decoded %d pages, %d lines, auto-cut %v", cut, len(pages), len(pages[0].Lines), pages[0].AutoCut)
		}
	}
}

// fakePrinter records what each connection sent.
type fakePrinter struct {
	ln   net.Listener
	mu   sync.Mutex
	jobs [][]byte
}

func newFakePrinter(t *testing.T) *fakePrinter {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fp := &fakePrinter{ln: ln}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				data, _ := io.ReadAll(conn)
				fp.mu.Lock()
				fp.jobs = append(fp.jobs, data)
				fp.mu.Unlock()
			}()
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return fp
}

func (fp *fakePrinter) port() int { return fp.ln.Addr().(*net.TCPAddr).Port }

func TestSendDeliversWholeJob(t *testing.T) {
	fp := newFakePrinter(t)
	job := bytes.Repeat([]byte("raster"), 50000)
	if err := NewDispatcher().Send(context.Background(), "127.0.0.1", fp.port(), job); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fp.mu.Lock()
		n := len(fp.jobs)
		fp.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if len(fp.jobs) != 1 || !bytes.Equal(fp.jobs[0], job) {
		t.Fatalf("printer received %d jobs", len(fp.jobs))
	}
}

// countingConn counts itself open until closed, and slows writes so that
// overlapping jobs would be caught.
type countingConn struct {
	net.Conn
	open   *atomic.Int32
	closed atomic.Bool
}

func (c *countingConn) Write(b []byte) (int, error) {
	time.Sleep(10 * time.Millisecond)
	return c.Conn.Write(b)
}

func (c *countingConn) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		c.open.Add(-1)
	}
	return c.Conn.Close()
}

func TestSendSerializesJobsPerPrinter(t *testing.T) {
	printers := []*fakePrinter{newFakePrinter(t), newFakePrinter(t)}

	var mu sync.Mutex
	open := map[string]*atomic.Int32{} // connections currently open, per address
	peak := map[string]int32{}
	dials := 0

	d := NewDispatcher()
	d.dial = func(ctx context.Context, addr string) (net.Conn, error) {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		defer mu.Unlock()
		dials++
		if open[addr] == nil {
			open[addr] = &atomic.Int32{}
		}
		if n := open[addr].Add(1); n > peak[addr] {
			peak[addr] = n
		}
		return &countingConn{Conn: conn, open: open[addr]}, nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		for _, p := range printers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := d.Send(context.Background(), "127.0.0.1", p.port(), []byte{byte(i)}); err != nil {
					t.Error(err)
				}
			}()
		}
	}
	wg.Wait()

	if dials != 12 {
		t.Errorf("dialed %d times, want 12", dials)
	}
	for addr, n := range peak {
		if n != 1 {
			t.Errorf("%s had %d connections open at once, want 1", addr, n)
		}
	}
}

func TestSendReportsUnreachablePrinters(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // nothing listens any more

	err = NewDispatcher().Send(context.Background(), "127.0.0.1", port, []byte("job"))
	if !errors.Is(err, ErrUnreachable) {
		t.Errorf("got %v, want ErrUnreachable", err)
	}
}

func TestSendGivesUpWhileWaitingWhenCanceled(t *testing.T) {
	d := NewDispatcher()
	slot := d.slot("127.0.0.1:1")
	slot <- struct{}{} // another job holds the printer
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := d.Send(ctx, "127.0.0.1", 1, []byte("job")); !errors.Is(err, ErrUnreachable) {
		t.Errorf("got %v, want ErrUnreachable after cancellation", err)
	}
}

func TestDymoPlan(t *testing.T) {
	d, err := Lookup(DriverDymoLabelWriter)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Plan(Settings{Model: "LabelWriter 450", Media: "30252"}, LabelSpec{WidthMM: 90, HeightMM: 29, DPI: 300}); !errors.Is(err, ErrDoesNotFit) {
		t.Errorf("Brother-sized label: got %v, want ErrDoesNotFit", err)
	}

	defer func(orig func() uint32) { newJobID = orig }(newJobID)
	newJobID = func() uint32 { return 42 }

	label := image.NewGray(image.Rect(0, 0, 1051, 331)) // 89 × 28 mm, all black
	for _, c := range []struct {
		model, options string
		protocol       dymolw.Protocol
		density, roll  byte
		jobID          uint32
	}{
		{"LabelWriter 450", "", dymolw.ProtocolV1, 'e', 0, 0},
		{"LabelWriter 450 Twin Turbo", `{"roll":"right","density":"light"}`, dymolw.ProtocolV1, 'c', '2', 0},
		{"LabelWriter 550", `{"density":"medium"}`, dymolw.ProtocolV2, 'd', 0, 42},
	} {
		plan, err := d.Plan(Settings{Model: c.model, Media: "30252", Options: json.RawMessage(c.options)}, LabelSpec{WidthMM: 89, HeightMM: 28, DPI: 300})
		if err != nil {
			t.Fatal(err)
		}
		stream, err := plan.Encode([]*image.Gray{label, label})
		if err != nil {
			t.Fatal(err)
		}
		job, err := dymolw.DecodeJob(c.protocol, stream, 331)
		if err != nil {
			t.Fatalf("%s: %v", c.model, err)
		}
		if job.Density != c.density || job.Roll != c.roll || job.JobID != c.jobID || len(job.Pages) != 2 {
			t.Errorf("%s: decoded %+v", c.model, job)
		}
	}
}

func TestTSPLPlan(t *testing.T) {
	d, err := Lookup(DriverTSPL)
	if err != nil {
		t.Fatal(err)
	}
	settings := Settings{Model: "Rollo X1038", Media: "4x6", Options: json.RawMessage(`{"density":11}`)}
	if _, err := d.Plan(settings, LabelSpec{WidthMM: 101.6, HeightMM: 152.4, DPI: 300}); !errors.Is(err, ErrDoesNotFit) {
		t.Errorf("300 DPI template on a 203 DPI printer: got %v, want ErrDoesNotFit", err)
	}
	plan, err := d.Plan(settings, LabelSpec{WidthMM: 152.4, HeightMM: 101.6, DPI: 203})
	if err != nil {
		t.Fatal(err)
	}
	label := image.NewGray(image.Rect(0, 0, 1219, 812)) // landscape 6 × 4 in, all black
	stream, err := plan.Encode([]*image.Gray{label, label, label})
	if err != nil {
		t.Fatal(err)
	}
	job, err := tspl.DecodeJob(stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Labels) != 1 || job.Labels[0].Copies != 3 || job.Labels[0].Image.Rect.Dy() != 1219 {
		t.Errorf("decoded %+v", job.Labels)
	}
	if !strings.Contains(strings.Join(job.Setup, "\n"), "DENSITY 11") {
		t.Errorf("setup %q lacks the density", job.Setup)
	}
}
