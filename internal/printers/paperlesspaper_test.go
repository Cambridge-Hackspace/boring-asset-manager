package printers

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// openPaperLabel is a label in Spectra 6's inks: a stripe of each, white
// last, across a panel.
func openPaperLabel(w, h int) *image.Paletted {
	inks := labels.PaletteBWRYGB.Colors()
	img := image.NewPaletted(image.Rect(0, 0, w, h), nil)
	for _, c := range inks {
		img.Palette = append(img.Palette, c)
	}
	for y := range h {
		for x := range w {
			img.Pix[y*img.Stride+x] = uint8(x * len(inks) / w)
		}
	}
	return img
}

func TestAnOpenPaperFrameIsGivenItsInksAsPanelCodes(t *testing.T) {
	d, err := Lookup(DriverPaperlessPaper)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		model string
		w, h  int
	}{{"epd7", 800, 480}, {"openpaper13", 1200, 1600}} {
		plan, err := d.Plan(Settings{Model: c.model, Media: c.model}, Fit{})
		if err != nil {
			t.Fatal(err)
		}
		label := openPaperLabel(c.w, c.h)
		b, err := plan.(InkPlan).EncodeInks([]*image.Paletted{label})
		if err != nil {
			t.Fatal(err)
		}
		// An uncompressed 4-bit bitmap, top down, of the panel's exact size,
		// as the offline firmware shows without dithering it.
		le := binary.LittleEndian
		row := (c.w*4 + 31) / 32 * 4
		if string(b[:2]) != "BM" || le.Uint32(b[14:]) != 40 || int32(le.Uint32(b[18:])) != int32(c.w) || int32(le.Uint32(b[22:])) != int32(-c.h) ||
			le.Uint16(b[28:]) != 4 || le.Uint32(b[30:]) != 0 || len(b) != int(le.Uint32(b[10:]))+row*c.h || int(le.Uint32(b[2:])) != len(b) {
			t.Fatalf("%s: header % x", c.model, b[:54])
		}
		if DisplayContentType(b) != "image/bmp" {
			t.Fatalf("%s: %s", c.model, DisplayContentType(b))
		}
		// Each dot is its ink's code: black 0, blue 1, green 2, red 3,
		// yellow 5, white 6.
		codes := map[string]uint8{}
		pix := b[le.Uint32(b[10:]):]
		for i, ink := range labels.PaletteBWRYGB.Inks() {
			x := (2*i + 1) * c.w / 12
			codes[ink] = pix[x/2] >> (4 * (1 - x%2)) & 0x0F
		}
		want := map[string]uint8{labels.InkBlack: 0, labels.InkBlue: 1, labels.InkGreen: 2, labels.InkRed: 3, labels.InkYellow: 5, labels.InkWhite: 6}
		for ink, code := range want {
			if codes[ink] != code {
				t.Errorf("%s: %s is %d, want %d", c.model, ink, codes[ink], code)
			}
		}
		// It reads back as it was drawn.
		back, err := readOpenPaper(b)
		if err != nil {
			t.Fatal(err)
		}
		for y := range c.h {
			for x := range c.w {
				if back.At(x, y) != label.At(x, y) {
					t.Fatalf("%s: (%d, %d) is %v, not %v", c.model, x, y, back.At(x, y), label.At(x, y))
				}
			}
		}
	}
	// A blank frame is white.
	blank, err := BlankDisplay(DriverPaperlessPaper, Settings{Model: "epd7", Media: "epd7"})
	if err != nil {
		t.Fatal(err)
	}
	if img, err := readOpenPaper(blank); err != nil || img.ColorIndexAt(400, 240) != 6 {
		t.Fatalf("blank: %v", err)
	}
}

func TestPaperlessPaperConnections(t *testing.T) {
	c, err := NormalizeConnection(Connection{Type: ConnPaperlessPaper, Address: " 65A1F0C2E4B3D5A6B7C8D9E0 "})
	if err != nil || c.Address != "65a1f0c2e4b3d5a6b7c8d9e0" {
		t.Fatalf("%+v %v", c, err)
	}
	for _, bad := range []Connection{
		{Type: ConnPaperlessPaper, Address: "65a1f0c2e4b3d5a6b7c8d9e"},
		{Type: ConnPaperlessPaper, Address: "https://web.paperlesspaper.de/papers/x"},
		{Type: ConnPaperlessPaper, Address: "65a1f0c2e4b3d5a6b7c8d9e0", Relay: "workshop"},
	} {
		if _, err := NormalizeConnection(bad); !errors.Is(err, ErrInvalidPrinter) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	// A frame pulls, or is pushed through the cloud, and takes nothing else.
	for typ, ok := range map[string]bool{ConnPull: true, ConnPaperlessPaper: true, ConnWebhook: false, ConnTCP: false} {
		addr := map[string]string{ConnPaperlessPaper: "65a1f0c2e4b3d5a6b7c8d9e0", ConnWebhook: "ch", ConnTCP: "10.0.0.5"}[typ]
		err := Validate("Frame", DriverPaperlessPaper, Connection{Type: typ, Address: addr}, Settings{Model: "epd7", Media: "epd7"})
		if (err == nil) != ok {
			t.Errorf("%s: %v", typ, err)
		}
	}
	// The server delivers to it, not a transport.
	if err := NewDispatcher(nil).Send(context.Background(), Connection{Type: ConnPaperlessPaper, Address: "65a1f0c2e4b3d5a6b7c8d9e0"}, nil); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("send: %v", err)
	}
}

// fakeCloud is paperlesspaper's cloud, keeping each upload, and answering
// the first few with the statuses given.
type fakeCloud struct {
	mu       sync.Mutex
	answers  []int
	uploads  []*multipart.Form
	paths    []string
	keys     []string
	retryAft string
}

func (f *fakeCloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.Method+" "+r.URL.Path)
	f.keys = append(f.keys, r.Header.Get("x-api-key"))
	_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	form, err := multipart.NewReader(r.Body, params["boundary"]).ReadForm(1 << 22)
	if err == nil {
		f.uploads = append(f.uploads, form)
	}
	if len(f.answers) > 0 {
		code := f.answers[0]
		f.answers = f.answers[1:]
		if f.retryAft != "" {
			w.Header().Set("Retry-After", f.retryAft)
		}
		w.WriteHeader(code)
		_, _ = io.WriteString(w, `{"message":"said so"}`)
		return
	}
	_, _ = io.WriteString(w, `{"id":"65a1f0c2e4b3d5a6b7c8d9e0"}`)
}

func TestAnImageIsPushedToPaperlessPapersCloud(t *testing.T) {
	cloud := &fakeCloud{}
	srv := httptest.NewServer(cloud)
	defer srv.Close()
	label := openPaperLabel(1200, 1600)
	var bmp bytes.Buffer
	if err := writeOpenPaper(&bmp, label); err != nil {
		t.Fatal(err)
	}
	account := PaperlessPaperAccount{Base: srv.URL + "/v1/", APIKey: "pk_test"}
	if err := PushPaperlessPaper(context.Background(), srv.Client(), account, "65a1f0c2e4b3d5a6b7c8d9e0", bmp.Bytes()); err != nil {
		t.Fatal(err)
	}
	if len(cloud.uploads) != 1 || cloud.paths[0] != "POST /v1/papers/uploadSingleImage/65a1f0c2e4b3d5a6b7c8d9e0" || cloud.keys[0] != "pk_test" {
		t.Fatalf("%v %v", cloud.paths, cloud.keys)
	}
	form := cloud.uploads[0]
	// Sent even if like the last, and as a portrait, the frame's own way up.
	if v := form.Value; v["forceUpload"][0] != "true" || v["settings"][0] != `{"orientation":"portrait"}` {
		t.Fatalf("fields %v", v)
	}
	for _, field := range []string{"picture", "pictureDevice"} {
		fh := form.File[field]
		if len(fh) != 1 {
			t.Fatalf("%s: %v", field, fh)
		}
		f, _ := fh[0].Open()
		img, err := png.Decode(f)
		f.Close()
		if err != nil || img.Bounds().Dx() != 1200 || img.Bounds().Dy() != 1600 {
			t.Fatalf("%s: %v %v", field, img.Bounds(), err)
		}
		// In the frame's six inks, exactly.
		for x := 0; x < 1200; x += 100 {
			if img.At(x, 0) != label.Palette[label.Pix[x]] {
				t.Fatalf("%s: (%d, 0) is %v", field, x, img.At(x, 0))
			}
		}
	}
}

func TestPaperlessPapersRefusalsAndBusyAnswers(t *testing.T) {
	defer func(w time.Duration) { paperlessWait = w }(paperlessWait)
	paperlessWait = 10 * time.Millisecond
	var bmp bytes.Buffer
	if err := writeOpenPaper(&bmp, openPaperLabel(800, 480)); err != nil {
		t.Fatal(err)
	}
	push := func(cloud *fakeCloud, key string) error {
		srv := httptest.NewServer(cloud)
		defer srv.Close()
		return PushPaperlessPaper(context.Background(), srv.Client(), PaperlessPaperAccount{Base: srv.URL, APIKey: key}, "65a1f0c2e4b3d5a6b7c8d9e0", bmp.Bytes())
	}
	// Busy, or failing, it is tried again, waiting as it says.
	cloud := &fakeCloud{answers: []int{http.StatusTooManyRequests, http.StatusBadGateway}, retryAft: "0"}
	if err := push(cloud, "pk"); err != nil || len(cloud.paths) != 3 {
		t.Fatalf("%v after %d", err, len(cloud.paths))
	}
	if v := cloud.uploads[2].Value["settings"][0]; v != `{"orientation":"landscape"}` {
		t.Fatalf("settings %s", v)
	}
	// But not for ever.
	cloud = &fakeCloud{answers: []int{503, 503, 503, 503}}
	if err := push(cloud, "pk"); err == nil || len(cloud.paths) != paperlessTries || !strings.Contains(err.Error(), "said so") {
		t.Fatalf("%v after %d", err, len(cloud.paths))
	}
	// A refusal is final, and says what it was.
	for code, want := range map[int]string{401: "refused the API key", 403: "refused the API key", 404: "no such paper", 400: "HTTP 400"} {
		cloud = &fakeCloud{answers: []int{code}}
		if err := push(cloud, "pk_secret"); err == nil || len(cloud.paths) != 1 || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "pk_secret") {
			t.Errorf("%d: %v after %d", code, err, len(cloud.paths))
		}
	}
	// With no key, nothing is sent.
	cloud = &fakeCloud{}
	if err := push(cloud, ""); err == nil || !strings.Contains(err.Error(), "API key is not set") || len(cloud.paths) != 0 {
		t.Fatalf("no key: %v", err)
	}
}
