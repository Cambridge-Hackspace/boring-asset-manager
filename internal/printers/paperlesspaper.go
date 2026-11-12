package printers

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// paperlesspaper's OpenPaper frames (docs/printing.md#paperlesspaper):
// Spectra 6 e-paper on an ESP32-C6, which sleeps between wakes. On
// paperlesspaper's offline firmware a frame fetches an image URL as it
// wakes, BAM!'s pull URL; on its stock firmware it shows what was uploaded
// to paperlesspaper's cloud for its paper. These facts come from
// paperlesspaper's documentation and its offline firmware's source.

// openPaperCodes is the code the panel draws each ink with. A 4-bit bitmap
// whose dots are these codes is shown as it is; 4 draws nothing.
var openPaperCodes = map[string]uint8{
	labels.InkBlack:  0,
	labels.InkBlue:   1,
	labels.InkGreen:  2,
	labels.InkRed:    3,
	labels.InkYellow: 5,
	labels.InkWhite:  6,
}

// openPaperPalette is the bitmap's color table, by code, so that the file
// shows rightly anywhere else too. The frame itself reads only the codes.
var openPaperPalette = func() color.Palette {
	p := make(color.Palette, 7)
	p[4] = color.RGBA{A: 0xFF}
	for ink, code := range openPaperCodes {
		p[code] = labels.InkColor(ink)
	}
	return p
}()

// writeOpenPaper writes a screen in Spectra 6's inks as the bitmap the
// offline firmware shows without dithering: uncompressed, 4 bits a dot,
// top down (a negative height), each dot its ink's code.
func writeOpenPaper(w io.Writer, screen *image.Paletted) error {
	codes := make([]uint8, len(screen.Palette))
	for i, c := range screen.Palette {
		code, ok := uint8(0), false
		for ink, v := range openPaperCodes {
			if labels.InkColor(ink) == color.RGBAModel.Convert(c) {
				code, ok = v, true
			}
		}
		if !ok {
			return fmt.Errorf("an OpenPaper frame has no ink %v", c)
		}
		codes[i] = code
	}
	width, height := screen.Rect.Dx(), screen.Rect.Dy()
	row := (width*4 + 31) / 32 * 4
	colors := len(openPaperPalette)
	offset := 14 + 40 + 4*colors
	var head bytes.Buffer
	head.WriteString("BM")
	for _, v := range []any{uint32(offset + row*height), uint32(0), uint32(offset),
		uint32(40), int32(width), int32(-height), uint16(1), uint16(4), uint32(0), uint32(row * height), uint32(0), uint32(0), uint32(colors), uint32(0)} {
		_ = binary.Write(&head, binary.LittleEndian, v)
	}
	for _, c := range openPaperPalette {
		r, g, b, _ := c.RGBA()
		head.Write([]byte{byte(b >> 8), byte(g >> 8), byte(r >> 8), 0})
	}
	if _, err := w.Write(head.Bytes()); err != nil {
		return err
	}
	line := make([]byte, row)
	for y := range height {
		clear(line)
		for x := range width {
			line[x/2] |= codes[screen.Pix[y*screen.Stride+x]] << (4 * (1 - x%2))
		}
		if _, err := w.Write(line); err != nil {
			return err
		}
	}
	return nil
}

// readOpenPaper reads back what writeOpenPaper wrote, its dots as codes
// into openPaperPalette.
func readOpenPaper(b []byte) (*image.Paletted, error) {
	bad := errors.New("not an OpenPaper bitmap")
	if len(b) < 54 || string(b[:2]) != "BM" {
		return nil, bad
	}
	le := binary.LittleEndian
	offset := int(le.Uint32(b[10:]))
	width, height := int(int32(le.Uint32(b[18:]))), -int(int32(le.Uint32(b[22:])))
	if le.Uint16(b[28:]) != 4 || le.Uint32(b[30:]) != 0 || width <= 0 || height <= 0 {
		return nil, bad
	}
	row := (width*4 + 31) / 32 * 4
	if offset < 54 || len(b) < offset+row*height {
		return nil, bad
	}
	img := image.NewPaletted(image.Rect(0, 0, width, height), openPaperPalette)
	for y := range height {
		line := b[offset+y*row:]
		for x := range width {
			code := line[x/2] >> (4 * (1 - x%2)) & 0x0F
			if int(code) >= len(openPaperPalette) || code == 4 {
				return nil, bad
			}
			img.Pix[y*img.Stride+x] = code
		}
	}
	return img, nil
}

// paperID is a paperlesspaper paper's ID: 24 hexadecimal digits.
var paperID = regexp.MustCompile(`^[0-9a-f]{24}$`)

// DefaultPaperlessPaperBase is paperlesspaper's cloud API.
const DefaultPaperlessPaperBase = "https://api.paperlesspaper.de/v1"

// PaperlessPaperAccount is what pushing to paperlesspaper's cloud needs
// beyond the paper: its API, and the key to it.
type PaperlessPaperAccount struct {
	Base   string
	APIKey string
}

// paperlessTries is how many times an upload is tried while the cloud is
// busy or failing, and paperlessWait the longest it is waited for between
// them.
var (
	paperlessTries = 3
	paperlessWait  = 10 * time.Second
)

// PushPaperlessPaper uploads an OpenPaper bitmap, as a PNG, to a paper in
// paperlesspaper's cloud, which its frame shows when it next wakes. The
// image is the frame's own size and inks, so the cloud is told its
// orientation and to send it even if it looks like the last.
func PushPaperlessPaper(ctx context.Context, client *http.Client, a PaperlessPaperAccount, paper string, bitmap []byte) error {
	if a.APIKey == "" {
		return errors.New("paperlesspaper's API key is not set: set it under Admin › Displays")
	}
	img, err := readOpenPaper(bitmap)
	if err != nil {
		return err
	}
	var pic bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&pic, img); err != nil {
		return err
	}
	orientation := "landscape"
	if img.Rect.Dy() > img.Rect.Dx() {
		orientation = "portrait"
	}
	settings, _ := json.Marshal(map[string]string{"orientation": orientation})
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for _, field := range []string{"picture", "pictureDevice"} {
		part, err := form.CreateFormFile(field, "bam.png")
		if err != nil {
			return err
		}
		if _, err := part.Write(pic.Bytes()); err != nil {
			return err
		}
	}
	for _, f := range [][2]string{{"settings", string(settings)}, {"forceUpload", "true"}} {
		if err := form.WriteField(f[0], f[1]); err != nil {
			return err
		}
	}
	if err := form.Close(); err != nil {
		return err
	}
	url := strings.TrimRight(a.Base, "/") + "/papers/uploadSingleImage/" + paper
	for try := 1; ; try++ {
		wait, err := uploadPaperlessPaper(ctx, client, url, a.APIKey, form.FormDataContentType(), body.Bytes())
		if err == nil {
			return nil
		}
		if wait < 0 || try >= paperlessTries {
			return err
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return fmt.Errorf("%v, and the job was given up: %v", err, ctx.Err())
		}
	}
}

// uploadPaperlessPaper makes one upload. When the cloud is busy or failing
// it says how long to wait before trying again; otherwise the wait is
// negative.
func uploadPaperlessPaper(ctx context.Context, client *http.Client, url, key, contentType string, body []byte) (time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return -1, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("x-api-key", key)
	resp, err := client.Do(req)
	if err != nil {
		return 2 * time.Second, fmt.Errorf("uploading to paperlesspaper: %v", err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	said := strings.TrimSpace(string(answer))
	// Its errors are JSON with a message.
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(answer, &e) == nil && e.Message != "" {
		said = e.Message
	}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return 0, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return -1, fmt.Errorf("paperlesspaper refused the API key (HTTP %d): %s", resp.StatusCode, said)
	case resp.StatusCode == http.StatusNotFound:
		return -1, fmt.Errorf("paperlesspaper has no such paper, or not for this API key: %s", said)
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		wait := 2 * time.Second
		if n, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && n >= 0 {
			wait = time.Duration(n) * time.Second
		}
		return min(wait, paperlessWait), fmt.Errorf("paperlesspaper answered HTTP %s: %s", resp.Status, said)
	}
	return -1, fmt.Errorf("paperlesspaper answered HTTP %s: %s", resp.Status, said)
}
