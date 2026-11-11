package printers

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// screenLabel is a label of a panel's size: black on its left half, white
// on its right.
func screenLabel(w, h int) *image.Gray {
	g := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := w / 2; x < w; x++ {
			g.Pix[y*g.Stride+x] = 0xFF
		}
	}
	return g
}

func TestATemplateOfAPanelsSizeFitsItsDisplayAndRendersToItsPixels(t *testing.T) {
	s := Settings{Model: "7.5in-800x480", Media: "7.5in-800x480"}
	g, err := GeometryOf(DriverDisplay, s)
	if err != nil {
		t.Fatal(err)
	}
	if g.DPI != 125 || !g.Screen || !g.DieCut() {
		t.Fatalf("geometry = %+v", g)
	}
	// A template of the screen's size, as the editor makes it from the
	// screen listed as the display's media.
	label := LabelSpec{WidthMM: g.WidthMM, HeightMM: g.LengthMM, DPI: 125, Content: []RectMM{{X: 0, Y: 0, Width: 2, Height: 2}}}
	plan, fit, err := PlanLabel(DriverDisplay, s, label)
	if err != nil {
		t.Fatal(err)
	}
	// A screen does not shift as die-cut labels do: content at its very
	// edge is no warning.
	if fit.Rotate || len(fit.Warnings) != 0 || fit.EdgeMM != 0 || len(fit.NearEdge) != 0 {
		t.Fatalf("fit = %+v", fit)
	}
	img, err := plan.Encode([]*image.Gray{screenLabel(10, 10), screenLabel(800, 480)})
	if err != nil {
		t.Fatal(err)
	}
	if DisplayContentType(img) != "image/png" {
		t.Fatalf("content type %s", DisplayContentType(img))
	}
	shown, err := png.Decode(bytes.NewReader(img))
	if err != nil {
		t.Fatal(err)
	}
	// The last label is what the display shows.
	p, ok := shown.(*image.Paletted)
	if !ok || shown.Bounds() != image.Rect(0, 0, 800, 480) || len(p.Palette) != 2 {
		t.Fatalf("shown %T %v", shown, shown.Bounds())
	}
	if p.ColorIndexAt(10, 10) != 0 || p.ColorIndexAt(790, 10) != 1 {
		t.Fatal("the label was not drawn as given")
	}

	// A template of another size does not fit, and says the screen's.
	_, _, err = PlanLabel(DriverDisplay, s, LabelSpec{WidthMM: 62, HeightMM: 29, DPI: 125})
	if !errors.Is(err, ErrDoesNotFit) || !strings.Contains(err.Error(), "the screen, 7.5-inch, 800 × 480, is 162.6 × 97.5 mm") {
		t.Fatalf("err = %v", err)
	}
	_, _, err = PlanLabel(DriverDisplay, s, LabelSpec{WidthMM: g.WidthMM, HeightMM: g.LengthMM, DPI: 300})
	if !errors.Is(err, ErrDoesNotFit) || !strings.Contains(err.Error(), "the 7.5-inch, 800 × 480 panel draws at 125 DPI") {
		t.Fatalf("err = %v", err)
	}
}

func TestAPortraitTemplateTurnsOntoALandscapeScreen(t *testing.T) {
	s := Settings{Model: "2.9in-296x128", Media: "2.9in-296x128", Options: json.RawMessage(`{"format":"bmp"}`)}
	g, _ := GeometryOf(DriverDisplay, s)
	plan, fit, err := PlanLabel(DriverDisplay, s, LabelSpec{WidthMM: g.LengthMM, HeightMM: g.WidthMM, DPI: g.DPI})
	if err != nil || !fit.Rotate {
		t.Fatalf("fit %+v, %v", fit, err)
	}
	// 128 wide and 296 high: its top row black, the rest white.
	label := image.NewGray(image.Rect(0, 0, 128, 296))
	for i := range label.Pix {
		label.Pix[i] = 0xFF
	}
	for x := 0; x < 128; x++ {
		label.Pix[x] = 0
	}
	img, err := plan.Encode([]*image.Gray{label})
	if err != nil {
		t.Fatal(err)
	}
	w, h, bpp, pix := readBMP(t, img)
	if w != 296 || h != 128 || bpp != 1 {
		t.Fatalf("bitmap %d × %d at %d bits", w, h, bpp)
	}
	// Turned counterclockwise, its top row is the screen's left column.
	if pix(0, 0) != 0 || pix(0, 127) != 0 || pix(1, 0) != 1 || pix(295, 64) != 1 {
		t.Fatal("the label was not turned counterclockwise")
	}
}

// readBMP reads the header of a bitmap writeBMP made, and its dots.
func readBMP(t *testing.T, b []byte) (w, h, bpp int, pix func(x, y int) int) {
	t.Helper()
	if !bytes.HasPrefix(b, []byte("BM")) || int(binary.LittleEndian.Uint32(b[2:])) != len(b) {
		t.Fatalf("not a whole bitmap: % x", b[:min(len(b), 16)])
	}
	offset := int(binary.LittleEndian.Uint32(b[10:]))
	w, h = int(int32(binary.LittleEndian.Uint32(b[18:]))), int(int32(binary.LittleEndian.Uint32(b[22:])))
	bpp = int(binary.LittleEndian.Uint16(b[28:]))
	row := (w*bpp + 31) / 32 * 4
	if offset+row*h != len(b) {
		t.Fatalf("bitmap is %d bytes, want %d", len(b), offset+row*h)
	}
	return w, h, bpp, func(x, y int) int {
		line := b[offset+(h-1-y)*row:]
		switch bpp {
		case 1:
			return int(line[x/8]>>(7-x%8)) & 1
		case 4:
			return int(line[x/2]>>(4*(1-x%2))) & 0xF
		}
		return int(line[x])
	}
}

func TestAColorPanelShowsItsInks(t *testing.T) {
	s := Settings{Model: "4.2in-400x300-bwry", Media: "4.2in-400x300-bwry"}
	g, _ := GeometryOf(DriverDisplay, s)
	plan, fit, err := PlanLabel(DriverDisplay, s, LabelSpec{WidthMM: g.WidthMM, HeightMM: g.LengthMM, DPI: g.DPI, Inks: []string{labels.InkRed, labels.InkBlue}})
	if err != nil {
		t.Fatal(err)
	}
	// Blue it lacks, and prints black.
	if fit.Palette != labels.PaletteBWRY || len(fit.Warnings) != 1 || !strings.Contains(fit.Warnings[0], "blue prints black") {
		t.Fatalf("fit = %+v", fit)
	}
	inks, ok := plan.(InkPlan)
	if !ok || inks.Palette() != labels.PaletteBWRY {
		t.Fatalf("plan %T", plan)
	}
	palette := labels.PaletteBWRY.Colors()
	label := image.NewPaletted(image.Rect(0, 0, 400, 300), nil)
	for _, c := range palette {
		label.Palette = append(label.Palette, c)
	}
	label.Pix[0] = 2 // red
	label.Pix[1] = 3 // yellow
	img, err := inks.EncodeInks([]*image.Paletted{label})
	if err != nil {
		t.Fatal(err)
	}
	shown, err := png.Decode(bytes.NewReader(img))
	if err != nil {
		t.Fatal(err)
	}
	p := shown.(*image.Paletted)
	if len(p.Palette) != 4 || p.ColorIndexAt(0, 0) != 2 || p.ColorIndexAt(1, 0) != 3 || p.ColorIndexAt(2, 0) != 0 {
		t.Fatalf("palette %v, dots %d %d %d", p.Palette, p.ColorIndexAt(0, 0), p.ColorIndexAt(1, 0), p.ColorIndexAt(2, 0))
	}
}

func TestABlankDisplayIsWhite(t *testing.T) {
	img, err := BlankDisplay(DriverTRMNL, Settings{Model: "og", Media: "og", Options: json.RawMessage(`{"format":"bmp"}`)})
	if err != nil {
		t.Fatal(err)
	}
	w, h, bpp, pix := readBMP(t, img)
	if w != 800 || h != 480 || bpp != 1 || pix(0, 0) != 1 || pix(799, 479) != 1 {
		t.Fatalf("blank %d × %d at %d bits", w, h, bpp)
	}
	// TRMNL's OG takes a file of at most 90,000 bytes.
	if len(img) > 90000 {
		t.Fatalf("a TRMNL bitmap is %d bytes", len(img))
	}
	if _, err := BlankDisplay(DriverBrotherQL, Settings{Model: "QL-810W", Media: "dk-11201"}); err == nil {
		t.Fatal("a printer made a blank screen")
	}
}

func TestDisplaysTakeOnlyTheirOwnConnections(t *testing.T) {
	screen := func(driver, model, options string) Settings {
		return Settings{Model: model, Media: model, Options: json.RawMessage(options)}
	}
	for _, c := range []struct {
		why    string
		driver string
		conn   Connection
		s      Settings
		ok     bool
	}{
		{"a display that pulls", DriverDisplay, Connection{Type: ConnPull}, screen(DriverDisplay, "7.5in-800x480", ""), true},
		{"a display pushed by webhook", DriverDisplay, Connection{Type: ConnWebhook, Address: "6f1c3c3e-1d2b-4c56-9a8e-0b1c2d3e4f50"}, screen(DriverDisplay, "2.9in-296x128-bwr", `{"format":"bmp","rotate_180":true}`), true},
		{"a display's refresh", DriverDisplay, Connection{Type: ConnPull}, screen(DriverDisplay, "7.5in-800x480", `{"refresh_seconds":600}`), true},
		{"a refresh too often", DriverDisplay, Connection{Type: ConnPull}, screen(DriverDisplay, "7.5in-800x480", `{"refresh_seconds":5}`), false},
		{"a format it does not make", DriverDisplay, Connection{Type: ConnPull}, screen(DriverDisplay, "7.5in-800x480", `{"format":"jpeg"}`), false},
		{"a display on a printer's port", DriverDisplay, Connection{Type: ConnTCP, Address: "10.0.0.5:9100"}, screen(DriverDisplay, "7.5in-800x480", ""), false},
		{"a pull display with an address", DriverDisplay, Connection{Type: ConnPull, Address: "AA:BB:CC:DD:EE:FF"}, screen(DriverDisplay, "7.5in-800x480", ""), false},
		{"another screen as its media", DriverDisplay, Connection{Type: ConnPull}, Settings{Model: "7.5in-800x480", Media: "2.9in-296x128"}, false},
		{"a printer that pulls", DriverBrotherQL, Connection{Type: ConnPull}, Settings{Model: "QL-810W", Media: "dk-11201"}, false},
		{"a printer through a webhook", DriverZPL, Connection{Type: ConnWebhook, Address: "abc"}, Settings{Model: "Zebra GK420d", Media: "4x6"}, false},
		{"a tag", DriverOpenEPaperLink, Connection{Type: ConnOpenEPaperLink, Address: "192.168.1.50?tag=0197E5CB3B38"}, screen(DriverOpenEPaperLink, "2.9in", ""), true},
		{"a tag that pulls", DriverOpenEPaperLink, Connection{Type: ConnPull}, screen(DriverOpenEPaperLink, "2.9in", ""), false},
		{"a tag refreshed by pulling", DriverOpenEPaperLink, Connection{Type: ConnOpenEPaperLink, Address: "ap.lan?tag=0197E5CB3B38"}, screen(DriverOpenEPaperLink, "2.9in", `{"refresh_seconds":600}`), false},
		{"a TRMNL", DriverTRMNL, Connection{Type: ConnPull, Address: "aa-bb-cc-dd-ee-ff"}, screen(DriverTRMNL, "og", ""), true},
		{"a TRMNL without its MAC", DriverTRMNL, Connection{Type: ConnPull}, screen(DriverTRMNL, "og", ""), false},
		{"a TRMNL pushed", DriverTRMNL, Connection{Type: ConnWebhook, Address: "abc"}, screen(DriverTRMNL, "og", ""), false},
	} {
		err := Validate("Shelf", c.driver, c.conn, c.s)
		if (err == nil) != c.ok || (err != nil && !errors.Is(err, ErrInvalidPrinter)) {
			t.Errorf("%s: %v, want valid %v", c.why, err, c.ok)
		}
	}
}

func TestDisplayConnectionsNormalize(t *testing.T) {
	for _, c := range []struct {
		in, want string
		typ      string
	}{
		{"192.168.1.50?tag=0197E5CB3B38", "http://192.168.1.50?tag=00000197E5CB3B38", ConnOpenEPaperLink},
		{"http://ap.lan:8080/?tag=00:00:01:97:e5:cb:3b:38", "http://ap.lan:8080?tag=00000197E5CB3B38", ConnOpenEPaperLink},
		{"https://ap.lan?tag=00000197E5CB3B38", "https://ap.lan?tag=00000197E5CB3B38", ConnOpenEPaperLink},
		{"aa:bb:cc:dd:ee:ff", "AA:BB:CC:DD:EE:FF", ConnPull},
		{"", "", ConnPull},
	} {
		got, err := NormalizeConnection(Connection{Type: c.typ, Address: c.in})
		if err != nil || got.Address != c.want {
			t.Errorf("%s %q: %q, %v; want %q", c.typ, c.in, got.Address, err, c.want)
		}
	}
	for _, c := range []Connection{
		{Type: ConnOpenEPaperLink, Address: "ap.lan"},
		{Type: ConnOpenEPaperLink, Address: "ap.lan?tag=12345"},
		{Type: ConnOpenEPaperLink, Address: "ftp://ap.lan?tag=0197E5CB3B38"},
		{Type: ConnOpenEPaperLink, Address: "ap.lan/path?tag=0197E5CB3B38"},
		{Type: ConnOpenEPaperLink, Address: "ap.lan?tag=0197E5CB3B38&dither=1"},
		{Type: ConnPull, Address: "not a mac"},
		{Type: ConnPull, Relay: "shop"},
		{Type: ConnWebhook, Address: "id", Relay: "shop"},
		{Type: ConnWebhook, Address: "a/b"},
	} {
		if _, err := NormalizeConnection(c); !errors.Is(err, ErrInvalidPrinter) {
			t.Errorf("%+v: %v, want refused", c, err)
		}
	}
	// A tag is reached through a relay as a TCP printer is.
	if _, err := NormalizeConnection(Connection{Type: ConnOpenEPaperLink, Address: "ap.lan?tag=0197E5CB3B38", Relay: "shop"}); err != nil {
		t.Fatal(err)
	}
}

// fakeAccessPoint is an OpenEPaperLink access point: it takes uploads at
// /imgupload, busy for the first ones it is told to be.
type fakeAccessPoint struct {
	busy    atomic.Int32
	unknown atomic.Bool
	uploads atomic.Int32
	mac     string
	dither  string
	jpeg    []byte
}

func (ap *fakeAccessPoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/imgupload" {
		http.NotFound(w, r)
		return
	}
	if ap.unknown.Load() {
		http.Error(w, "mac not found", http.StatusBadRequest)
		return
	}
	if ap.busy.Add(-1) >= 0 {
		w.Header().Set("Retry-After", "0")
		http.Error(w, "Busy, come back later", http.StatusConflict)
		return
	}
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		http.Error(w, "parameters incomplete", http.StatusBadRequest)
		return
	}
	form := multipart.NewReader(r.Body, params["boundary"])
	for {
		part, err := form.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		b, _ := io.ReadAll(part)
		switch part.FormName() {
		case "mac":
			ap.mac = string(b)
		case "dither":
			ap.dither = string(b)
		case "file":
			if part.Header.Get("Content-Type") != "image/jpeg" {
				http.Error(w, "not a JPEG", http.StatusBadRequest)
				return
			}
			ap.jpeg = b
		}
	}
	if ap.mac == "" || ap.jpeg == nil {
		http.Error(w, "parameters incomplete", http.StatusBadRequest)
		return
	}
	ap.uploads.Add(1)
	_, _ = io.WriteString(w, "Ok, saved")
}

func TestAPushReachesAnOpenEPaperLinkAccessPoint(t *testing.T) {
	ap := &fakeAccessPoint{}
	ap.busy.Store(2)
	srv := httptest.NewServer(ap)
	defer srv.Close()

	s := Settings{Model: "2.9in", Media: "2.9in"}
	g, _ := GeometryOf(DriverOpenEPaperLink, s)
	plan, fit, err := PlanLabel(DriverOpenEPaperLink, s, LabelSpec{WidthMM: g.WidthMM, HeightMM: g.LengthMM, DPI: g.DPI, Inks: []string{labels.InkRed}})
	if err != nil || fit.Palette != labels.PaletteBWR || len(fit.Warnings) != 0 {
		t.Fatalf("fit %+v, %v", fit, err)
	}
	label := image.NewPaletted(image.Rect(0, 0, 296, 128), nil)
	for _, c := range labels.PaletteBWR.Colors() {
		label.Palette = append(label.Palette, c)
	}
	job, err := plan.(InkPlan).EncodeInks([]*image.Paletted{label})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := NormalizeConnection(Connection{Type: ConnOpenEPaperLink, Address: srv.URL + "?tag=0197E5CB3B38"})
	if err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(nil)
	// Busy twice, it is tried until it takes it.
	if err := d.Send(context.Background(), conn, job); err != nil {
		t.Fatal(err)
	}
	if ap.uploads.Load() != 1 || ap.mac != "00000197E5CB3B38" || ap.dither != "0" {
		t.Fatalf("uploads %d, mac %q, dither %q", ap.uploads.Load(), ap.mac, ap.dither)
	}
	// A baseline JPEG of the tag's exact size, as the access point needs.
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(ap.jpeg))
	if err != nil || cfg.Width != 296 || cfg.Height != 128 {
		t.Fatalf("image %+v, %v", cfg, err)
	}
	if bytes.Contains(ap.jpeg[:min(len(ap.jpeg), 4096)], []byte{0xFF, 0xC2}) {
		t.Fatal("the JPEG is progressive")
	}

	// Busy for longer than it is tried, the job fails, saying why.
	ap.busy.Store(int32(oeplTries))
	err = d.Send(context.Background(), conn, job)
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("err = %v", err)
	}
	// A tag the access point does not know fails at once.
	ap.unknown.Store(true)
	start := time.Now()
	if err := d.Send(context.Background(), conn, job); err == nil || !strings.Contains(err.Error(), "mac not found") || time.Since(start) > 5*time.Second {
		t.Fatalf("err = %v", err)
	}
}

func TestDisplayDriversDescribeThemselves(t *testing.T) {
	for _, info := range Drivers() {
		display := IsDisplay(info.Driver)
		if display != (info.Display != nil) {
			t.Fatalf("%s: display %v, info %+v", info.Driver, display, info.Display)
		}
		if !display {
			continue
		}
		for _, model := range info.Models {
			var screens int
			for _, m := range info.Media {
				if m.ID == model && len(m.Models) == 1 && m.Models[0] == model && m.DieCut {
					screens++
				}
			}
			if screens != 1 || info.ModelDPI[model] == 0 {
				t.Errorf("%s %s: %d screens, %d DPI", info.Driver, model, screens, info.ModelDPI[model])
			}
		}
	}
	if !IsDisplay(DriverTRMNL) || IsDisplay(DriverBrotherQL) || IsDisplay("nothing") {
		t.Fatal("IsDisplay")
	}
	if n, ok := DisplayRefresh(Settings{Options: json.RawMessage(`{"refresh_seconds":300}`)}); !ok || n != 300 {
		t.Fatalf("refresh %d %v", n, ok)
	}
	if _, ok := DisplayRefresh(Settings{}); ok {
		t.Fatal("a refresh with no options")
	}
}
