package escpos

import (
	"bytes"
	"fmt"
	"image"
)

// Placement says how a label image maps onto the roll.
type Placement struct {
	// Rotate turns the image 90° counterclockwise so its height runs across
	// the roll.
	Rotate bool
	// Rotate180 turns it the other way up as well.
	Rotate180 bool
}

// Prepare turns a rendered label into a page as wide as the head of
// headDots: rotated as the placement says, then centered on the head, or
// against its end for a right-aligned model. A label wider than the head
// is cut to it around its middle.
func Prepare(mo Model, headDots int, label *image.Gray, p Placement) *image.Gray {
	src := label
	if p.Rotate {
		src = rotateCounterclockwise(src)
	}
	if p.Rotate180 {
		src = rotate180(src)
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	page := image.NewGray(image.Rect(0, 0, headDots, h))
	for i := range page.Pix {
		page.Pix[i] = 0xFF
	}
	srcX, dstX, n := 0, 0, w
	switch {
	case w > headDots:
		srcX, n = (w-headDots)/2, headDots
	case mo.RightAligned:
		dstX = headDots - w
	default:
		dstX = (headDots - w) / 2
	}
	for y := 0; y < h; y++ {
		copy(page.Pix[y*page.Stride+dstX:y*page.Stride+dstX+n], src.Pix[y*src.Stride+srcX:])
	}
	return page
}

func rotateCounterclockwise(src *image.Gray) *image.Gray {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewGray(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// (x, y) moves to (y, w-1-x).
			dst.Pix[(w-1-x)*dst.Stride+y] = src.Pix[y*src.Stride+x]
		}
	}
	return dst
}

func rotate180(src *image.Gray) *image.Gray {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Pix[(h-1-y)*dst.Stride+w-1-x] = src.Pix[y*src.Stride+x]
		}
	}
	return dst
}

// Cut is how plain ESC/POS printers cut each label off.
type Cut string

const (
	CutNone    Cut = "none"
	CutPartial Cut = "partial"
	CutFull    Cut = "full"
)

// Cuts lists the cuts.
func Cuts() []Cut { return []Cut{CutNone, CutPartial, CutFull} }

// Phomemo's M series density and speed ranges.
const (
	MinDensity, MaxDensity = 1, 15
	MinSpeed, MaxSpeed     = 1, 5
)

// JobOptions are the per-printer choices that change the job.
type JobOptions struct {
	// Cut and FeedMM are for plain ESC/POS: the cut after each label, and
	// the paper fed after it first, as blank lines.
	Cut    Cut
	FeedMM int
	// Density (1-15) and Speed (1-5) are for Phomemo's M series, which
	// are sent them with each label.
	Density, Speed int
}

// Check says whether the options suit the model.
func (o JobOptions) Check(mo Model) error {
	if mo.Family == FamilyGeneric {
		switch o.Cut {
		case CutNone, CutPartial, CutFull:
		default:
			return fmt.Errorf("escpos: cut %q is not none, partial or full", o.Cut)
		}
		if o.FeedMM < 0 || o.FeedMM > 100 {
			return fmt.Errorf("escpos: feed %d mm is outside 0-100", o.FeedMM)
		}
	}
	if mo.Family == FamilyPhomemoM {
		if o.Density < MinDensity || o.Density > MaxDensity {
			return fmt.Errorf("escpos: density %d is outside %d-%d", o.Density, MinDensity, MaxDensity)
		}
		if o.Speed < MinSpeed || o.Speed > MaxSpeed {
			return fmt.Errorf("escpos: speed %d is outside %d-%d", o.Speed, MinSpeed, MaxSpeed)
		}
	}
	return nil
}

// Commands, as Epson's reference and the community's descriptions of
// Phomemo's printers name them.
var (
	cmdInit = []byte{0x1B, 0x40} // ESC @
	// ESC a 1: center, on Phomemo's paper printers.
	cmdCenter = []byte{0x1B, 0x61, 0x01}
	// GS V 66 0 and GS V 65 0: feed to the cutter, then cut partly or
	// fully.
	cmdPartialCut = []byte{0x1D, 0x56, 0x42, 0x00}
	cmdFullCut    = []byte{0x1D, 0x56, 0x41, 0x00}
	// Phomemo's M series: the end of a label.
	cmdPhomemoMEnd = []byte{0x1F, 0xF0, 0x05, 0x00, 0x1F, 0xF0, 0x03, 0x00}
	// Phomemo's D series: the preamble captured from its app, whose
	// meaning nobody has found, and ESC d 0, which ends the label at the
	// next gap.
	cmdPhomemoDStart = []byte{0x1F, 0x11, 0x24, 0x00}
	cmdPhomemoDEnd   = []byte{0x1B, 0x64, 0x00}
	// Phomemo's paper printers: the header and footer captured from the
	// M02's app, which the T02 shares. The footer is ESC d 2 twice.
	cmdPhomemoTStart = []byte{0x1F, 0x11, 0x02, 0x04}
	cmdPhomemoTEnd   = []byte{0x1B, 0x64, 0x02, 0x1B, 0x64, 0x02}
)

// Phomemo's M series media types (US DC1 n).
const (
	mediaGaps       = 0x0A
	mediaContinuous = 0x0B
)

// bandLines is the most lines one GS v 0 carries where a label is sent in
// bands: as Phomemo's paper printers are sent them, and few enough for the
// smallest buffer of a plain ESC/POS printer.
const bandLines = 255

// EncodeJob builds the ESC/POS stream that prints pages, each prepared for
// the model (see Prepare) and dark wherever a pixel is below 50% gray.
// ESC/POS has no copies, so each page is sent whole.
func EncodeJob(mo Model, m Media, pages []*image.Gray, opts JobOptions) ([]byte, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("escpos: a job needs at least one page")
	}
	if err := opts.Check(mo); err != nil {
		return nil, err
	}
	head := pages[0].Rect.Dx()
	for i, page := range pages {
		if page.Rect.Dx() != head || head == 0 || page.Rect.Dy() == 0 {
			return nil, fmt.Errorf("escpos: page %d is %d × %d dots; each page must be prepared for the head", i, page.Rect.Dx(), page.Rect.Dy())
		}
		if mo.Family.Phomemo() && head != mo.HeadDots {
			return nil, fmt.Errorf("escpos: page %d is %d dots wide, the %s head has %d", i, head, mo.Name, mo.HeadDots)
		}
	}

	var buf bytes.Buffer
	switch mo.Family {
	case FamilyGeneric:
		buf.Write(cmdInit)
		feed := mo.Dots(float64(opts.FeedMM))
		for _, page := range pages {
			raster(&buf, page, bandLines, feed)
			switch opts.Cut {
			case CutPartial:
				buf.Write(cmdPartialCut)
			case CutFull:
				buf.Write(cmdFullCut)
			}
		}
	case FamilyPhomemoM:
		media := byte(mediaGaps)
		if !m.DieCut() {
			media = mediaContinuous
		}
		for _, page := range pages {
			// ESC N 13 speed, ESC N 4 density, US DC1 media type.
			buf.Write([]byte{0x1B, 0x4E, 0x0D, byte(opts.Speed), 0x1B, 0x4E, 0x04, byte(opts.Density), 0x1F, 0x11, media})
			raster(&buf, page, 0, 0)
			buf.Write(cmdPhomemoMEnd)
		}
	case FamilyPhomemoD:
		for _, page := range pages {
			buf.Write(cmdPhomemoDStart)
			buf.Write(cmdInit)
			raster(&buf, page, 0, 0)
			buf.Write(cmdPhomemoDEnd)
		}
	case FamilyPhomemoT:
		buf.Write(cmdInit)
		buf.Write(cmdCenter)
		buf.Write(cmdPhomemoTStart)
		for _, page := range pages {
			raster(&buf, page, bandLines, 0)
		}
		buf.Write(cmdPhomemoTEnd)
	default:
		return nil, fmt.Errorf("escpos: the %s speaks no known dialect", mo.Name)
	}
	return buf.Bytes(), nil
}

// raster writes a page as GS v 0 raster images, in bands of at most band
// lines (0 for one image), followed by feed blank lines. A set bit prints
// a dot, the first the leftmost.
func raster(buf *bytes.Buffer, page *image.Gray, band, feed int) {
	w, h := page.Rect.Dx(), page.Rect.Dy()
	widthBytes := (w + 7) / 8
	total := h + feed
	if band == 0 {
		band = total
	}
	for y0 := 0; y0 < total; y0 += band {
		lines := min(band, total-y0)
		buf.Write([]byte{0x1D, 0x76, 0x30, 0x00, byte(widthBytes), byte(widthBytes >> 8), byte(lines), byte(lines >> 8)})
		row := make([]byte, widthBytes)
		for y := y0; y < y0+lines; y++ {
			clear(row)
			if y < h {
				src := page.Pix[y*page.Stride:]
				for x := 0; x < w; x++ {
					if src[x] < 0x80 {
						row[x/8] |= 0x80 >> uint(x%8)
					}
				}
			}
			buf.Write(row)
		}
	}
}
