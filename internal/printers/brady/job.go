package brady

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"math"
	"time"
)

// Magic starts every packet of a job: the job's header, then each page.
// Each is this, the length of its JSON as 4 bytes little-endian, then the
// JSON.
var Magic = []byte{0x7F, 0x42, 0xEE, 0x41, 0xA9, 0x1D, 0x40, 0x90, 0x9B, 0xEC, 0xFF, 0x7A, 0x66, 0x14, 0xCC, 0x22}

// clientID names BAM! as the job's client, as a GUID without dashes.
const clientID = "b4a0f3c26e1d4c8a9f5b7d2e3c1a0b9d"

// now is the clock jobs are stamped with; tests replace it.
var now = time.Now

// Header is a job's first packet.
type Header struct {
	JobID         string `json:"JobID"`
	JobName       string `json:"JobName"`
	JobCreator    string `json:"JobCreator"`
	JobTime       string `json:"JobTime"`
	NumberOfPages int    `json:"NumberOfPages"`
	SubstratePart string `json:"SubstratePart"`
	JobType       string `json:"JobType"`
	JobSource     string `json:"JobSource"`
	JobClientID   string `json:"JobClientId"`
	// PostPrint is when the printer cuts: "Cut;1" after each label,
	// "Cut;0" after the job, or "SetByPrinter".
	PostPrint string `json:"UserPostPrintOperation"`
}

// Page is one label's packet.
type Page struct {
	PrintFileName string `json:"PrintFileName"`
	JobID         string `json:"JobID"`
	PageNumber    int    `json:"PageNumber"`
	// LabelWidth and LabelHeight are in thousandths of an inch.
	LabelWidth  int      `json:"LabelWidth"`
	LabelHeight int      `json:"LabelHeight"`
	Pages       PageBody `json:"Pages"`
}

// PageBody is a page's image and what follows it.
type PageBody struct {
	Layers             []Layer             `json:"Layers"`
	PrePrintOperations []map[string]string `json:"PrePrintOperations"`
	// PostPrintOperations is {"Cut": "Shear"} where the page is cut off,
	// {"SetByPrinter": "SetByPrinter"} when the printer decides, or none.
	PostPrintOperations []map[string]string `json:"PostPrintOperations"`
}

// Layer is one image of a page: a BMP, LZ4-compressed, in base64.
type Layer struct {
	Bitmap      string  `json:"Bitmap"`
	Compression string  `json:"Compression"`
	Color       *string `json:"Color"`
}

// EncodeJob builds the job that prints pages, each prepared for the model
// (see Prepare) and dark wherever a pixel is below 50% gray, on the media
// loaded.
func EncodeJob(mo Model, m Media, pages []*image.Gray, opts JobOptions) ([]byte, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("brady: a job needs at least one page")
	}
	if err := opts.Check(); err != nil {
		return nil, fmt.Errorf("brady: %w", err)
	}
	// Copies repeat the same image; encode each one once.
	bitmaps := map[*image.Gray]string{}
	sum := sha256.New()
	for _, p := range pages {
		if _, ok := bitmaps[p]; !ok {
			bmp := writeBMP(p, mo.DPI)
			bitmaps[p] = base64.StdEncoding.EncodeToString(compressBlock(bmp))
		}
		sum.Write([]byte(bitmaps[p]))
	}
	// The job's ID is drawn from what it prints and when, so that a job
	// sent twice is two jobs.
	stamp := now()
	sum.Write([]byte(stamp.Format(time.RFC3339Nano)))
	id := hex.EncodeToString(sum.Sum(nil)[:16])

	name := opts.Name
	if name == "" {
		name = "BAM! label"
	}
	h := Header{
		JobID: id, JobName: name, JobCreator: "BAM!", JobTime: stamp.Format("20060102150405"),
		NumberOfPages: len(pages), SubstratePart: opts.Part, JobType: "Print", JobSource: "Mobile", JobClientID: clientID,
	}
	switch opts.Cut {
	case CutEach:
		h.PostPrint = "Cut;1"
	case CutEnd:
		h.PostPrint = "Cut;0"
	default:
		h.PostPrint = "SetByPrinter"
	}
	var out bytes.Buffer
	if err := writePacket(&out, h); err != nil {
		return nil, err
	}
	for i, p := range pages {
		pg := Page{
			PrintFileName: fmt.Sprintf("Page%d.prn", i+1), JobID: id, PageNumber: i,
			LabelWidth: thousandths(p.Rect.Dx(), mo.DPI), LabelHeight: thousandths(p.Rect.Dy(), mo.DPI),
			Pages: PageBody{
				Layers:              []Layer{{Bitmap: bitmaps[p], Compression: "lz4"}},
				PrePrintOperations:  []map[string]string{},
				PostPrintOperations: []map[string]string{},
			},
		}
		if m.DieCut() {
			// A die-cut label is as long as the cartridge's.
			pg.LabelHeight = int(math.Round(m.LengthMM / 25.4 * 1000))
		}
		switch {
		case opts.Cut == CutPrinter:
			pg.Pages.PostPrintOperations = append(pg.Pages.PostPrintOperations, map[string]string{"SetByPrinter": "SetByPrinter"})
		case opts.Cut == CutEach || i == len(pages)-1:
			pg.Pages.PostPrintOperations = append(pg.Pages.PostPrintOperations, map[string]string{"Cut": "Shear"})
		}
		if err := writePacket(&out, pg); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

// thousandths is dots at dpi in thousandths of an inch.
func thousandths(dots, dpi int) int { return int(math.Round(float64(dots) * 1000 / float64(dpi))) }

func writePacket(out *bytes.Buffer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("brady: %v", err)
	}
	out.Write(Magic)
	_ = binary.Write(out, binary.LittleEndian, uint32(len(body)))
	out.Write(body)
	return nil
}

// writeBMP writes img as a Windows BMP: 1 bit per pixel, rows top-down,
// each padded to 4 bytes, with black as color 0 and white as color 1.
func writeBMP(img *image.Gray, dpi int) []byte {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	stride := (w + 31) / 32 * 4
	const headers = 14 + 40 + 8
	b := make([]byte, headers+stride*h)
	le := binary.LittleEndian
	b[0], b[1] = 'B', 'M'
	le.PutUint32(b[2:], uint32(len(b)))
	le.PutUint32(b[10:], headers)
	le.PutUint32(b[14:], 40)
	le.PutUint32(b[18:], uint32(int32(w)))
	le.PutUint32(b[22:], uint32(int32(-h))) // negative: top-down
	le.PutUint16(b[26:], 1)
	le.PutUint16(b[28:], 1)
	le.PutUint32(b[34:], uint32(stride*h))
	perMeter := uint32(math.Round(float64(dpi) / 0.0254))
	le.PutUint32(b[38:], perMeter)
	le.PutUint32(b[42:], perMeter)
	le.PutUint32(b[46:], 2)
	le.PutUint32(b[50:], 2)
	// Color 0 is black, already zero; color 1 is white.
	copy(b[58:], []byte{0xFF, 0xFF, 0xFF, 0})
	for y := 0; y < h; y++ {
		row := b[headers+y*stride:]
		pix := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			if pix[x] >= 0x80 {
				row[x/8] |= 0x80 >> uint(x%8)
			}
		}
	}
	return b
}

// readBMP reads a BMP that writeBMP wrote.
func readBMP(b []byte) (*image.Gray, error) {
	le := binary.LittleEndian
	if len(b) < 62 || b[0] != 'B' || b[1] != 'M' || int(le.Uint32(b[2:])) != len(b) || le.Uint32(b[10:]) != 62 || le.Uint32(b[14:]) != 40 ||
		le.Uint16(b[28:]) != 1 || le.Uint32(b[30:]) != 0 || !bytes.Equal(b[54:62], []byte{0, 0, 0, 0, 0xFF, 0xFF, 0xFF, 0}) {
		return nil, fmt.Errorf("brady: not a 1-bit black-and-white BMP")
	}
	w, h := int(int32(le.Uint32(b[18:]))), -int(int32(le.Uint32(b[22:])))
	stride := (w + 31) / 32 * 4
	if w <= 0 || h <= 0 || 62+stride*h != len(b) {
		return nil, fmt.Errorf("brady: BMP of %d × %d is not top-down, or is cut short", w, h)
	}
	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := b[62+y*stride:]
		for x := 0; x < w; x++ {
			if row[x/8]&(0x80>>uint(x%8)) != 0 {
				img.Pix[y*img.Stride+x] = 0xFF
			}
		}
	}
	return img, nil
}

// Job is a job recovered by DecodeJob.
type Job struct {
	Header Header
	Pages  []Page
	// Images are the pages' images, decoded.
	Images []*image.Gray
}

// DecodeJob parses a job produced by EncodeJob. It exists for tests and
// diagnostics, such as a fake printer that checks what it received.
func DecodeJob(b []byte) (Job, error) {
	var job Job
	for i := 0; len(b) > 0; i++ {
		if len(b) < len(Magic)+4 || !bytes.Equal(b[:len(Magic)], Magic) {
			return job, fmt.Errorf("brady: packet %d does not start with the magic", i)
		}
		n := int(binary.LittleEndian.Uint32(b[len(Magic):]))
		b = b[len(Magic)+4:]
		if n > len(b) {
			return job, fmt.Errorf("brady: packet %d is cut short", i)
		}
		body := b[:n]
		b = b[n:]
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if i == 0 {
			if err := dec.Decode(&job.Header); err != nil {
				return job, fmt.Errorf("brady: header: %v", err)
			}
			continue
		}
		var p Page
		if err := dec.Decode(&p); err != nil {
			return job, fmt.Errorf("brady: page %d: %v", i, err)
		}
		if len(p.Pages.Layers) != 1 || p.Pages.Layers[0].Compression != "lz4" {
			return job, fmt.Errorf("brady: page %d has %d layers", i, len(p.Pages.Layers))
		}
		packed, err := base64.StdEncoding.DecodeString(p.Pages.Layers[0].Bitmap)
		if err != nil {
			return job, fmt.Errorf("brady: page %d: %v", i, err)
		}
		bmp, err := decompressBlock(packed)
		if err != nil {
			return job, fmt.Errorf("brady: page %d: %v", i, err)
		}
		img, err := readBMP(bmp)
		if err != nil {
			return job, fmt.Errorf("brady: page %d: %v", i, err)
		}
		job.Pages = append(job.Pages, p)
		job.Images = append(job.Images, img)
	}
	if len(job.Pages) == 0 || job.Header.NumberOfPages != len(job.Pages) {
		return job, fmt.Errorf("brady: the header counts %d pages, and %d follow", job.Header.NumberOfPages, len(job.Pages))
	}
	return job, nil
}
