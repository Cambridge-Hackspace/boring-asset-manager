package ruida

import (
	"bytes"
	"flag"
	"image"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/*.rd")

// dots is a 1-bit image from rows of '#' (black) and '.'.
func dots(rows ...string) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, len(rows[0]), len(rows)))
	for y, row := range rows {
		for x, c := range row {
			v := uint8(0xFF)
			if c == '#' {
				v = 0
			}
			img.Pix[y*img.Stride+x] = v
		}
	}
	return img
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to write it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs: got %d bytes, want %d\n% X", name, len(got), len(want), got)
	}
}

func tag(bed Bed) Program {
	return Program{
		Bed:     bed,
		Raster:  &Raster{X: 10, Y: 20, Width: 5, Height: 3, Dots: dots("##.##", ".....", "#...#")},
		Engrave: Setting{Power: 20, Speed: 300, Passes: 1},
		Paths:   []Path{{Points: []Point{{8, 18}, {17, 18}, {17, 25}, {8, 25}}, Closed: true}},
		Cut:     Setting{Power: 60, Speed: 15, Passes: 2},
	}
}

func TestSwizzling(t *testing.T) {
	for magic := 0; magic < 256; magic++ {
		for b := 0; b < 256; b++ {
			if got := Unswizzle(Swizzle(byte(b), byte(magic)), byte(magic)); got != byte(b) {
				t.Fatalf("magic %#x: %#x came back %#x", magic, b, got)
			}
		}
	}
	// As the controllers are seen to answer, with the usual key: ACK as
	// 0xC6, ERR as 0x46.
	if Swizzle(ACK, DefaultMagic) != 0xC6 || Swizzle(ERR, DefaultMagic) != 0x46 {
		t.Errorf("ACK %#x, ERR %#x", Swizzle(ACK, DefaultMagic), Swizzle(ERR, DefaultMagic))
	}
	// Zero, the commonest byte in a file, swizzles to the key plus one.
	if Swizzle(0, 0x11) != 0x12 {
		t.Errorf("0 with 0x11: %#x", Swizzle(0, 0x11))
	}
}

func TestValues(t *testing.T) {
	if got := value35(123456); !bytes.Equal(got, []byte{0, 0, 7, 68, 64}) || Value35(got) != 123456 {
		t.Errorf("123456 µm: % X", got)
	}
	if got := power(100); !bytes.Equal(got, []byte{0x7F, 0x7F}) {
		t.Errorf("full power: % X", got)
	}
	if got := power(50); !bytes.Equal(got, value14(8192)) {
		t.Errorf("half power: % X", got)
	}
	if got := speed(15); Value35(got) != 15000 {
		t.Errorf("15 mm/s: % X", got)
	}
	if got := GetSetting(MemMachineStatus); !bytes.Equal(got, []byte{0xDA, 0x00, 0x04, 0x00}) {
		t.Errorf("status request: % X", got)
	}
}

func TestPacketsCarryTheirChecksum(t *testing.T) {
	p := Packet([]byte{0xDA, 0x00, 0x04, 0x00}, DefaultMagic)
	sw := SwizzleAll([]byte{0xDA, 0x00, 0x04, 0x00}, DefaultMagic)
	var sum int
	for _, b := range sw {
		sum += int(b)
	}
	if !bytes.Equal(p[2:], sw) || int(p[0])<<8|int(p[1]) != sum {
		t.Errorf("packet % X", p)
	}
}

func TestFileGoldens(t *testing.T) {
	for _, c := range []struct {
		name string
		bed  Bed
	}{
		{"tag-top-right.rd", Bed{WidthMM: 600, HeightMM: 400, FlipX: true}},
		{"tag-top-left.rd", Bed{WidthMM: 600, HeightMM: 400}},
	} {
		got, err := File(tag(c.bed))
		if err != nil {
			t.Fatal(err)
		}
		golden(t, c.name, got)
	}
}

func TestAFileIsForThePanel(t *testing.T) {
	f, err := File(tag(Bed{WidthMM: 600, HeightMM: 400, FlipX: true}))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(f, StartProcess) {
		t.Error("the file starts itself")
	}
	if f[len(f)-1] != 0xD7 {
		t.Error("no end of file")
	}
	// The sum: every byte before it, and the end of file.
	i := bytes.LastIndex(f, SetFileSum())
	var want int64 = 0xD7
	for _, b := range f[:i] {
		want += int64(b)
	}
	if got := Value35(f[i+2:]); got != want {
		t.Errorf("sum %d, want %d", got, want)
	}
	// Every value byte has its top bit clear, as the format needs, so
	// commands are told from values.
	if bytes.Count(f, cmdCutAbs) != 4+2*4 {
		t.Errorf("%d cuts, want 4 engraved and 8 cut", bytes.Count(f, cmdCutAbs))
	}
}

func TestAFileRefusesWhatLeavesTheBed(t *testing.T) {
	if _, err := File(tag(Bed{WidthMM: 12, HeightMM: 400})); err == nil {
		t.Error("a job off the bed was written")
	}
	p := tag(Bed{WidthMM: 600, HeightMM: 400})
	p.Engrave.Power = 0
	if _, err := File(p); err == nil {
		t.Error("an engraving at no power was written")
	}
	if _, err := File(Program{Bed: Bed{WidthMM: 600, HeightMM: 400}}); err == nil {
		t.Error("an empty job was written")
	}
}

func TestChunksEndBetweenCommands(t *testing.T) {
	f, err := File(tag(Bed{WidthMM: 600, HeightMM: 400}))
	if err != nil {
		t.Fatal(err)
	}
	chunks := Chunks(f, 64)
	if !bytes.Equal(bytes.Join(chunks, nil), f) {
		t.Fatal("the chunks are not the file")
	}
	for i, c := range chunks {
		if len(c) > 64 {
			t.Errorf("chunk %d is %d bytes", i, len(c))
		}
		if c[0]&0x80 == 0 {
			t.Errorf("chunk %d begins inside a command: % X", i, c[:4])
		}
	}
}

func TestUploadNamesTheFile(t *testing.T) {
	got := Upload("BAM", []byte{0xD7})
	if !bytes.Equal(got, []byte{0xE8, 0x02, 0xE7, 0x01, 'B', 'A', 'M', 0, 0xD7}) {
		t.Errorf("% X", got)
	}
}
