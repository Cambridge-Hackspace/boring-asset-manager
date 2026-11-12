package niimbot

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite testdata/*.bin")

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPacketsAreFramedWithAnXORChecksum(t *testing.T) {
	for _, c := range []struct {
		p    Packet
		want string
	}{
		// A request with a single 1: the checksum is the command again.
		{Packet{CmdRFIDInfo, []byte{1}}, "55 55 1a 01 01 1a aa aa"},
		// Connect alone is prefixed with 03.
		{Packet{CmdConnect, []byte{1}}, "03 55 55 c1 01 01 c1 aa aa"},
		// 13 ^ 04 ^ 00 f0 ^ 00 60 = 87: a 240-row page, 96 dots wide.
		{Packet{CmdSetPageSize, []byte{0x00, 0xF0, 0x00, 0x60}}, "55 55 13 04 00 f0 00 60 87 aa aa"},
		{Packet{CmdPrintStart, nil}, "55 55 01 00 01 aa aa"},
	} {
		if got := c.p.Bytes(); !bytes.Equal(got, unhex(t, c.want)) {
			t.Errorf("% x, want %s", got, c.want)
		}
	}

	p, n, err := parse(unhex(t, "55 55 31 01 01 31 aa aa"))
	if err != nil || n != 8 || p.Cmd != 0x31 || !bytes.Equal(p.Data, []byte{1}) {
		t.Errorf("parse = %+v, %d, %v", p, n, err)
	}
	if _, _, err := parse(unhex(t, "55 55 31 01 01 30 aa aa")); !errors.Is(err, ErrPacket) {
		t.Errorf("a bad checksum: %v", err)
	}
	if _, _, err := parse(unhex(t, "55 55 31 01 01 31 aa ab")); !errors.Is(err, ErrPacket) {
		t.Errorf("a bad tail: %v", err)
	}
	if _, n, err := parse(unhex(t, "55 55 31 01 01")); n != 0 || err != nil {
		t.Errorf("a part of a packet = %d, %v", n, err)
	}
}

func TestSplitCutsAJobIntoItsPackets(t *testing.T) {
	job := append(Packet{CmdConnect, one}.Bytes(), Packet{CmdSetDensity, []byte{3}}.Bytes()...)
	got, err := Split(job)
	if err != nil || len(got) != 2 || got[0][0] != connectPrefix || got[1][2] != CmdSetDensity {
		t.Fatalf("Split = % x, %v", got, err)
	}
	for _, bad := range []string{
		"55 55 21 01 03",          // ends part way
		"55 55 21 01 03 22 aa aa", // bad checksum
		"03 55 55 21 01 03 23 aa aa",
		"aa 55 55 21 01 03 23 aa aa",
	} {
		if _, err := Split(unhex(t, bad)); !errors.Is(err, ErrPacket) {
			t.Errorf("Split(%s) = %v", bad, err)
		}
	}
}

func TestTheReaderGathersPacketsFromPieces(t *testing.T) {
	var r Reader
	// One packet split across two notifications, as a serial port may give
	// it, then noise and two packets at once.
	if got := r.Add(unhex(t, "55 55 d9 09 1f 90 04 4c 00 00 01 00 00")); len(got) != 0 {
		t.Fatalf("a part gave %v", got)
	}
	got := r.Add(unhex(t, "16 aa aa"))
	if len(got) != 1 || got[0].Cmd != 0xD9 || len(got[0].Data) != 9 {
		t.Fatalf("the rest gave %+v", got)
	}
	got = r.Add(append(unhex(t, "00 55 55 31 01 01 30 aa aa 13"), append(Packet{0x31, one}.Bytes(), Packet{0xE4, one}.Bytes()...)...))
	if len(got) != 2 || got[0].Cmd != 0x31 || got[1].Cmd != 0xE4 {
		t.Fatalf("noise, a bad packet and two good ones gave %+v", got)
	}
}

func TestModelsRecordTheirHeadsAndTasks(t *testing.T) {
	for name, want := range map[string]struct {
		dots    int
		task    Task
		density [3]int
	}{
		"D11": {96, TaskD110, [3]int{1, 3, 2}}, "D110": {96, TaskD110, [3]int{1, 3, 2}}, "D101": {192, TaskB1, [3]int{1, 3, 2}},
		"B18": {96, TaskB1, [3]int{1, 3, 2}}, "B1": {384, TaskB1, [3]int{1, 5, 3}}, "B21": {384, TaskB21V1, [3]int{1, 5, 3}},
		"B3S": {576, TaskB1, [3]int{1, 5, 3}}, "K3": {640, TaskB1, [3]int{1, 5, 3}},
	} {
		m, ok := LookupModel(name)
		if !ok || m.DPI != 203 || m.HeadDots != want.dots || m.Task != want.task ||
			[3]int{m.MinDensity, m.MaxDensity, m.Density} != want.density || m.MaxRollMM+0.5 < m.HeadMM() {
			t.Errorf("%s = %+v", name, m)
		}
	}
	if len(Models()) != 8 {
		t.Errorf("Models() = %v", Models())
	}
	d11, _ := LookupModel("D11")
	b21, _ := LookupModel("B21")
	b1, _ := LookupModel("B1")
	b18, _ := LookupModel("B18")
	for _, c := range []struct {
		mo   Model
		roll string
		ok   bool
	}{
		{d11, "12x40", true}, {d11, "50x30", false}, {d11, "b18-14x30", false},
		{b18, "b18-14x30", true}, {b18, "12x40", false}, {b18, "b18-14-continuous", true},
		{b21, "50-continuous", true}, {b1, "50-continuous", false}, {b1, "40x30", true},
	} {
		m, ok := LookupMedia(c.roll)
		if !ok {
			t.Fatalf("no roll %s in %v", c.roll, MediaIDs())
		}
		if err := c.mo.Takes(m); (err == nil) != c.ok {
			t.Errorf("%s takes %s: %v", c.mo.Name, c.roll, err)
		}
	}
	if err := b1.Takes(SizedMedia(b1, "wide", "Wide", 58, 40)); err == nil {
		t.Error("the B1 takes 58 mm stock")
	}
}

func TestPixelCountsSplitTheHeadInThirds(t *testing.T) {
	row := make([]byte, 12) // a D11's 96 dots
	row[0], row[5], row[11] = 0xFF, 0x0F, 0x01
	if got := pixelCounts(row, 96, false); !bytes.Equal(got, []byte{8, 4, 1}) {
		t.Errorf("split = % x", got)
	}
	if got := pixelCounts(row, 96, true); !bytes.Equal(got, []byte{0, 13, 0}) {
		t.Errorf("total = % x", got)
	}
	// The K3's 80 bytes are not three whole thirds of 26: counted in total.
	wide := bytes.Repeat([]byte{0xFF}, 80)
	if got := pixelCounts(wide, 640, false); !bytes.Equal(got, []byte{0, 0x80, 0x02}) {
		t.Errorf("K3 = % x", got)
	}
}

// markedLabel draws a dark square near the top-left corner of a white
// label, and a bar along its bottom.
func markedLabel(w, h int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	for y := 8; y < 18; y++ {
		for x := 8; x < 18; x++ {
			img.Pix[y*img.Stride+x] = 0
		}
	}
	for y := h - 6; y < h-2; y++ {
		for x := 4; x < w-4; x++ {
			img.Pix[y*img.Stride+x] = 0
		}
	}
	return img
}

func TestPrepareCentersTheLabelOnTheHead(t *testing.T) {
	b1, _ := LookupModel("B1")
	page := Prepare(b1, markedLabel(320, 240), Placement{})
	if page.Bounds().Dx() != 384 || page.Bounds().Dy() != 240 {
		t.Fatalf("page is %v", page.Bounds())
	}
	// 32 dots of white each side of a 40 mm label.
	if page.Pix[10*page.Stride+32+8] != 0 || page.Pix[10*page.Stride+32+7] != 0xFF {
		t.Error("the mark moved")
	}
	d11, _ := LookupModel("D11")
	// A 40 × 12 mm label turned so its height runs across the head.
	page = Prepare(d11, markedLabel(320, 96), Placement{Rotate: true})
	if page.Bounds().Dx() != 96 || page.Bounds().Dy() != 320 {
		t.Fatalf("turned page is %v", page.Bounds())
	}
	// The top-left corner turns to the bottom-left.
	if page.Pix[(319-10)*page.Stride+10] != 0 {
		t.Error("the mark did not turn counterclockwise")
	}
}

// goldenJobs are a label on a roll for each model, as each family prints.
var goldenJobs = []struct {
	file, model, roll string
	w, h              int
	rotate            bool
}{
	{"d11-12x22.bin", "D11", "12x22", 176, 96, true},
	{"d110-12x40.bin", "D110", "12x40", 320, 96, true},
	{"d101-15x30.bin", "D101", "15x30", 240, 120, true},
	{"b18-14x40.bin", "B18", "b18-14x40", 320, 112, true},
	{"b1-40x30.bin", "B1", "40x30", 320, 240, false},
	{"b21-50x30.bin", "B21", "50x30", 400, 240, false},
	{"b21-50-continuous.bin", "B21", "50-continuous", 400, 160, false},
	{"b3s-50x50.bin", "B3S", "50x50", 400, 400, false},
	{"k3-50x30.bin", "K3", "50x30", 400, 240, false},
}

func TestGoldenJobs(t *testing.T) {
	for _, g := range goldenJobs {
		t.Run(g.file, func(t *testing.T) {
			mo, _ := LookupModel(g.model)
			m, _ := LookupMedia(g.roll)
			if err := mo.Takes(m); err != nil {
				t.Fatal(err)
			}
			page := Prepare(mo, markedLabel(g.w, g.h), Placement{Rotate: g.rotate})
			// Two pages, so each task's per-page and per-job steps show.
			job, err := EncodeJob(mo, []*image.Gray{page, page}, JobOptions{Density: mo.Density, Task: mo.Task, LabelType: m.LabelType()})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", g.file)
			if *update {
				if err := os.WriteFile(path, job, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(job, want) {
				t.Errorf("%s: %d bytes differ from the %d in %s (run with -update after checking)", g.model, len(job), len(want), path)
			}
			if _, err := Split(job); err != nil {
				t.Error(err)
			}
		})
	}
}

// commandsOf lists a job's commands in order.
func commandsOf(t *testing.T, job []byte) []byte {
	t.Helper()
	packets, err := Split(job)
	if err != nil {
		t.Fatal(err)
	}
	var out []byte
	for _, raw := range packets {
		out = append(out, trimPrefix(raw)[2])
	}
	return out
}

func TestEachTaskStartsAndEndsPagesItsOwnWay(t *testing.T) {
	b21, _ := LookupModel("B21")
	page := Prepare(b21, markedLabel(400, 40), Placement{})
	steps := func(task Task) string {
		job, err := EncodeJob(b21, []*image.Gray{page, page}, JobOptions{Density: 3, Task: task, LabelType: LabelGaps})
		if err != nil {
			t.Fatal(err)
		}
		var parts []string
		for _, cmd := range commandsOf(t, job) {
			if cmd != CmdBitmapRows && cmd != CmdEmptyRows {
				parts = append(parts, name(cmd))
			} else if parts[len(parts)-1] != "rows" {
				parts = append(parts, "rows")
			}
		}
		return strings.Join(parts, ", ")
	}
	head := "connect, the roll's RFID tag, the density, the label type, print start, "
	for task, want := range map[Task]string{
		TaskD110:  head + strings.Repeat("print clear, page start, the page size, the quantity, rows, page end, the print status, ", 2) + "print end",
		TaskB1:    head + strings.Repeat("page start, the page size, rows, page end, ", 2) + "the print status, print end",
		TaskB21V1: head + strings.Repeat("page start, the page size, rows, page end, ", 2) + "print end",
	} {
		if got := steps(task); got != want {
			t.Errorf("%s:\n got %s\nwant %s", task, got, want)
		}
	}
	// The B1's print start gives the pages; its page size, a quantity.
	job, _ := EncodeJob(b21, []*image.Gray{page, page}, JobOptions{Density: 3, Task: TaskB1, LabelType: LabelGaps})
	packets, _ := Split(job)
	if got := packets[4]; !bytes.Equal(got, Packet{CmdPrintStart, []byte{0, 2, 0, 0, 0, 0, 0}}.Bytes()) {
		t.Errorf("B1 print start = % x", got)
	}
	if got := packets[6]; !bytes.Equal(got, Packet{CmdSetPageSize, []byte{0, 40, 1, 0x80, 0, 1}}.Bytes()) {
		t.Errorf("B1 page size = % x", got)
	}
	if _, err := EncodeJob(b21, []*image.Gray{page}, JobOptions{Density: 6, Task: TaskB1}); err == nil {
		t.Error("density 6 accepted")
	}
	if _, err := EncodeJob(b21, []*image.Gray{page}, JobOptions{Density: 3, Task: "d11_v1"}); err == nil {
		t.Error("an unknown task accepted")
	}
	d11, _ := LookupModel("D11")
	if _, err := EncodeJob(d11, []*image.Gray{page}, JobOptions{Density: 2, Task: TaskD110}); err == nil {
		t.Error("a page wider than the head accepted")
	}
}

func TestRowsRepeatAndBlankRowsAreSentAsSuch(t *testing.T) {
	d11, _ := LookupModel("D11")
	page := image.NewGray(image.Rect(0, 0, 96, 300))
	for i := range page.Pix {
		page.Pix[i] = 0xFF
	}
	for y := 10; y < 280; y++ {
		page.Pix[y*page.Stride] = 0
	}
	var buf bytes.Buffer
	writeRows(&buf, d11, page, false)
	packets, err := Split(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, raw := range packets {
		p, _, _ := parse(raw)
		got = append(got, hex.EncodeToString(p.Data[:3]))
	}
	// 10 blank, 255 and then 15 the same, 20 blank.
	if len(packets) != 4 || got[0] != "00000a" || got[1][:4] != "000a" || got[2][:4] != "0109" || got[3] != "011814" {
		t.Fatalf("rows = %v", got)
	}
	p, _, _ := parse(packets[1])
	if p.Cmd != CmdBitmapRows || p.Data[5] != 255 || p.Data[6] != 0x80 || len(p.Data) != 6+12 {
		t.Errorf("first bitmap row = % x", p.Data)
	}
}

// simPrinter is a simulated printer: it answers each command as Niimbot's
// printers are described to, reports printing a page a poll at a time, and
// can be told to stay silent.
type simPrinter struct {
	mu      sync.Mutex
	reader  Reader
	out     chan []byte
	got     []byte // the commands received, in order
	printed int
	ended   int
	// notReady is how many times page end and print end answer 0 first.
	notReady int
	// silent is a command the printer never answers.
	silent byte
	// fail is an error code to report at page end.
	fail byte
	// roll is the RFID answer's data; nil for no tag.
	roll []byte
	// stuck keeps the printer from printing.
	stuck bool
}

func newSim() *simPrinter { return &simPrinter{out: make(chan []byte, 1024)} }

func (s *simPrinter) Received() <-chan []byte { return s.out }

// send replies, split in two, as a notification or serial read may.
func (s *simPrinter) send(cmd byte, data ...byte) {
	b := Packet{Cmd: cmd, Data: data}.Bytes()
	s.out <- b[:3]
	s.out <- b[3:]
}

func (s *simPrinter) Write(_ context.Context, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b[0] == connectPrefix {
		b = b[1:]
	}
	for _, p := range s.reader.Add(b) {
		s.got = append(s.got, p.Cmd)
		if p.Cmd == s.silent {
			continue
		}
		reply := commands[p.Cmd].reply
		switch p.Cmd {
		case CmdEmptyRows, CmdBitmapRows:
		case CmdConnect:
			s.send(reply, 2)
		case CmdRFIDInfo:
			if s.roll == nil {
				s.send(reply, 0)
			} else {
				s.send(reply, s.roll...)
			}
		case CmdPageEnd, CmdPrintEnd:
			if s.fail != 0 {
				s.send(CmdPrintError, s.fail)
				continue
			}
			if s.notReady > 0 {
				s.notReady--
				s.send(reply, 0)
				continue
			}
			if p.Cmd == CmdPageEnd {
				s.ended++
			}
			s.send(reply, 1)
		case CmdPrintStatus:
			if s.printed < s.ended && !s.stuck {
				s.printed++
			}
			// Unasked news the conversation passes over.
			s.send(0xDD, 0, 0, 0, 0, 0, 0, 0, 0, 0, 4)
			s.send(reply, byte(s.printed>>8), byte(s.printed), 100, 100)
		default:
			s.send(reply, 1)
		}
	}
	return nil
}

func (s *simPrinter) commands() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.got...)
}

var fast = Timing{Timeout: 200 * time.Millisecond, Poll: time.Millisecond}

func job(t *testing.T, model string, pages int, labelType byte) []byte {
	t.Helper()
	mo, _ := LookupModel(model)
	page := Prepare(mo, markedLabel(mo.HeadDots, 40), Placement{})
	list := make([]*image.Gray, pages)
	for i := range list {
		list[i] = page
	}
	b, err := EncodeJob(mo, list, JobOptions{Density: mo.Density, Task: mo.Task, LabelType: labelType})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestConverseFollowsThePrintersAnswers(t *testing.T) {
	for _, model := range []string{"D110", "B1", "B21"} {
		t.Run(model, func(t *testing.T) {
			s := newSim()
			s.notReady = 2
			j := job(t, model, 3, LabelGaps)
			if err := Converse(context.Background(), s, j, fast); err != nil {
				t.Fatal(err)
			}
			if s.printed != 3 && model != "B21" {
				t.Errorf("printed %d of 3", s.printed)
			}
			got, sent := s.commands(), commandsOf(t, j)
			// Everything sent, in order, with page end asked twice more
			// while the printer was not ready, and the print status until
			// the pages were printed.
			var asked []byte
			for _, cmd := range got {
				if len(asked) == 0 || cmd != asked[len(asked)-1] || (cmd != CmdPageEnd && cmd != CmdPrintStatus && cmd != CmdPrintEnd) {
					asked = append(asked, cmd)
				}
			}
			var want []byte
			for _, cmd := range sent {
				if len(want) == 0 || cmd != want[len(want)-1] || (cmd != CmdPageEnd && cmd != CmdPrintStatus && cmd != CmdPrintEnd) {
					want = append(want, cmd)
				}
			}
			if !bytes.Equal(asked, want) || len(got) < len(sent)+2 {
				t.Errorf("printer got % x\n for a job of % x", got, sent)
			}
		})
	}
}

func TestConverseWaitsForEachPageOnTheD110(t *testing.T) {
	s := newSim()
	if err := Converse(context.Background(), s, job(t, "D110", 2, LabelGaps), fast); err != nil {
		t.Fatal(err)
	}
	// The second page starts only once the first has printed.
	got := s.commands()
	first := bytes.IndexByte(got, CmdPrintStatus)
	second := bytes.LastIndexByte(got, CmdPrintClear)
	if first < 0 || second < first {
		t.Errorf("commands % x", got)
	}
}

func TestAMissingAnswerTimesOutCleanly(t *testing.T) {
	for _, cmd := range []byte{CmdConnect, CmdSetDensity, CmdPageEnd, CmdPrintStatus} {
		s := newSim()
		s.silent = cmd
		start := time.Now()
		err := Converse(context.Background(), s, job(t, "B1", 1, LabelGaps), fast)
		if !errors.Is(err, ErrPrinter) || !strings.Contains(err.Error(), "did not answer "+name(cmd)+" within 200ms") {
			t.Errorf("silent %s: %v", name(cmd), err)
		}
		if time.Since(start) > 2*time.Second {
			t.Errorf("silent %s took %s", name(cmd), time.Since(start))
		}
		// Once printing started, the print is ended for the next job.
		if got := s.commands(); cmd == CmdPageEnd && got[len(got)-1] != CmdPrintEnd {
			t.Errorf("after the timeout: % x", got)
		}
	}
}

func TestAPrinterThatStopsPrintingFailsTheJob(t *testing.T) {
	s := newSim()
	s.stuck = true
	err := Converse(context.Background(), s, job(t, "B1", 1, LabelGaps), fast)
	if !errors.Is(err, ErrPrinter) || !strings.Contains(err.Error(), "printed 0 of 1 pages and then made no progress") {
		t.Errorf("stuck: %v", err)
	}
}

func TestAPrinterErrorFailsTheJob(t *testing.T) {
	s := newSim()
	s.fail = 0x01
	err := Converse(context.Background(), s, job(t, "B21", 1, LabelGaps), fast)
	if !errors.Is(err, ErrPrinter) || !strings.Contains(err.Error(), "its cover is open") {
		t.Errorf("error: %v", err)
	}
}

func TestConverseStopsWhenCanceled(t *testing.T) {
	s := newSim()
	s.silent = CmdPageEnd
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	if err := Converse(ctx, s, job(t, "B1", 1, LabelGaps), Timing{Timeout: time.Minute}); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: %v", err)
	}
}

// rfid is an RFID answer for a roll of a label type.
func rfid(labelType byte) []byte {
	d := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	d = append(d, 13)
	d = append(d, "6972842743589"...)
	d = append(d, 4)
	d = append(d, "PZ1G"...)
	d = binary.BigEndian.AppendUint16(d, 230)
	d = binary.BigEndian.AppendUint16(d, 12)
	return append(d, labelType)
}

func TestTheLoadedRollMustBeOfTheKindTheJobIsFor(t *testing.T) {
	r, ok, err := ParseRoll(Packet{Data: rfid(LabelGaps)})
	if err != nil || !ok || r.Barcode != "6972842743589" || r.Serial != "PZ1G" || r.Total != 230 || r.Used != 12 || r.LabelType != LabelGaps {
		t.Fatalf("ParseRoll = %+v, %t, %v", r, ok, err)
	}
	if _, ok, _ := ParseRoll(Packet{Data: []byte{0}}); ok {
		t.Error("no tag read")
	}
	if _, _, err := ParseRoll(Packet{Data: rfid(1)[:12]}); err == nil {
		t.Error("a short answer read")
	}

	for _, c := range []struct {
		roll  []byte
		job   byte
		wants string
	}{
		{rfid(LabelGaps), LabelGaps, ""},
		{rfid(LabelTransparent), LabelGaps, ""},
		{nil, LabelContinuous, ""},
		{rfid(LabelContinuous), LabelGaps, "has a continuous roll loaded (6972842743589), but is set up for labels with gaps"},
		{rfid(LabelGaps), LabelContinuous, "has a roll of labels with gaps loaded (6972842743589), but is set up for a continuous one"},
	} {
		s := newSim()
		s.roll = c.roll
		err := Converse(context.Background(), s, job(t, "B21", 1, c.job), fast)
		if c.wants == "" && err != nil || c.wants != "" && (err == nil || !strings.Contains(err.Error(), c.wants)) {
			t.Errorf("roll % x, job type %d: %v", c.roll, c.job, err)
		}
		// Refused before anything prints.
		if c.wants != "" && bytes.IndexByte(s.commands(), CmdPrintStart) >= 0 {
			t.Errorf("printing started: % x", s.commands())
		}
	}
}
