package epsonlw

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
)

// The stream's bytes. A command is ESC {, its length (the bytes from its
// letter through the closing brace), its letter and parameters, a checksum
// (the sum of the letter and parameters, low byte) and a closing brace. A
// raster line is ESC . 00 00 00 01, its dots (two bytes, little-endian) and
// its bits, most significant first.
const (
	esc        = 0x1B
	cmdOpen    = '{'
	cmdClose   = '}'
	rasterLine = '.'
	formFeed   = 0x0C

	cmdReset    = '!' // reset the printer
	cmdStatus   = 'Q' // ask for status: 05 00 on, 00 00 off
	cmdPrintEnd = '@' // ends the job; Epson's app also sends it first
	cmdJob      = '{' // the job's signature, 00 00 'S' 'T'
	cmdCut      = 'C' // the cut, four bytes
	cmdDensity  = 'D' // the density, plus 5
	cmdJobEnd   = 'G' // closes the job's setup; its meaning is unknown
	cmdLength   = 'L' // the page's length in lines, four bytes
	cmdMargin   = 'T' // the margin in lines, two bytes
	cmdLevel3   = 'H' // a level-3 page command, always 05; its meaning is unknown
	cmdSpeed    = 's' // the print speed, on level 3: 1 is high
)

var (
	statusOn  = []byte{0x05, 0x00}
	signature = []byte{0x00, 0x00, 'S', 'T'}
)

// Cut says when the tape is cut.
type Cut string

const (
	// CutEach cuts each label off as it finishes.
	CutEach Cut = "each_label"
	// CutEnd cuts only after the job's last label.
	CutEnd Cut = "last_label"
	// CutNever leaves the tape uncut.
	CutNever Cut = "never"
)

// Cuts lists the cuts.
func Cuts() []Cut { return []Cut{CutEach, CutEnd, CutNever} }

// bytes are the cut command's parameters, for models without a half cutter.
func (c Cut) bytes() ([]byte, bool) {
	switch c {
	case CutEach:
		return []byte{1, 1, 1, 1}, true
	case CutEnd:
		return []byte{1, 0, 1, 1}, true
	case CutNever:
		return []byte{0, 0, 0, 0}, true
	}
	return nil, false
}

// JobOptions are the per-printer choices that change the command stream.
type JobOptions struct {
	Cut Cut
	// Density darkens the print above 0 and lightens it below.
	Density int
	// MarginLines is the tape fed before and after each label.
	MarginLines int
}

// MinDensity and MaxDensity bound the density, as the command encodes it.
const MinDensity, MaxDensity = -5, 5

// DefaultMarginMM is the margin unless one is set.
const DefaultMarginMM = 2

// MaxMarginMM is the longest margin.
const MaxMarginMM = 127

// DefaultOptions cut each label off, at the printer's own density, with
// DefaultMarginMM around it.
func DefaultOptions(mo Model) JobOptions {
	return JobOptions{Cut: CutEach, MarginLines: mo.Dots(DefaultMarginMM)}
}

// Check reports whether the model takes the options.
func (o JobOptions) Check(mo Model) error {
	if _, ok := o.Cut.bytes(); !ok {
		return fmt.Errorf("cut_after must be %s, %s or %s", CutEach, CutEnd, CutNever)
	}
	if o.Density < MinDensity || o.Density > MaxDensity {
		return fmt.Errorf("density must be %d to %d", MinDensity, MaxDensity)
	}
	if o.MarginLines < 0 || o.MarginLines > mo.Dots(MaxMarginMM) {
		return fmt.Errorf("the %s takes margins of 0-%d mm", mo.Name, MaxMarginMM)
	}
	return nil
}

// command appends one ESC { command.
func command(buf *bytes.Buffer, letter byte, params ...byte) {
	sum := letter
	for _, p := range params {
		sum += p
	}
	buf.Write([]byte{esc, cmdOpen, byte(len(params) + 3), letter})
	buf.Write(params)
	buf.Write([]byte{sum, cmdClose})
}

// EncodeJob builds the command stream that prints pages, each a label
// already prepared for the layout (see Layout.Prepare): PrintableDots wide,
// one row per raster line, and dark wherever a pixel is below 50% gray.
//
// The stream is laid out as Epson's app lays it out, by the community
// descriptions: a reset and a request for status; the job's setup (print
// end, signature, cut, density and G); then for each label a request for
// status, its length and margin, H and the speed on level 3, its lines with
// the margin's blank lines before and after, and a form feed; and a print
// end after the last.
func EncodeJob(l Layout, pages []*image.Gray, opts JobOptions) ([]byte, error) {
	mo := l.Model
	if len(pages) == 0 {
		return nil, fmt.Errorf("epsonlw: a job needs at least one page")
	}
	if err := opts.Check(mo); err != nil {
		return nil, fmt.Errorf("epsonlw: %w", err)
	}
	for i, page := range pages {
		b := page.Bounds()
		if b.Dx() != l.PrintableDots {
			return nil, fmt.Errorf("epsonlw: page %d is %d dots wide, %s prints %d", i, b.Dx(), l.Media.Name, l.PrintableDots)
		}
		if b.Dy() < 1 || b.Dy() > mo.MaxLines() {
			return nil, fmt.Errorf("epsonlw: page %d is %d lines long, the %s prints 1-%d", i, b.Dy(), mo.Name, mo.MaxLines())
		}
	}
	cut, _ := opts.Cut.bytes()

	var buf bytes.Buffer
	command(&buf, cmdReset)
	command(&buf, cmdStatus, statusOn...)
	command(&buf, cmdPrintEnd)
	command(&buf, cmdJob, signature...)
	command(&buf, cmdCut, cut...)
	command(&buf, cmdDensity, byte(opts.Density+5))
	command(&buf, cmdJobEnd)

	line := make([]byte, (l.PrintableDots+7)/8)
	blank := make([]byte, len(line))
	header := []byte{esc, rasterLine, 0, 0, 0, 1, byte(l.PrintableDots), byte(l.PrintableDots >> 8)}
	feed := func(n int) {
		for range n {
			buf.Write(header)
			buf.Write(blank)
		}
	}
	for _, page := range pages {
		b := page.Bounds()
		command(&buf, cmdStatus, statusOn...)
		command(&buf, cmdLength, binary.LittleEndian.AppendUint32(nil, uint32(b.Dy()+2*opts.MarginLines))...)
		command(&buf, cmdMargin, binary.LittleEndian.AppendUint16(nil, uint16(opts.MarginLines))...)
		if mo.Level == 3 {
			command(&buf, cmdLevel3, 0x05)
			command(&buf, cmdSpeed, 0x01)
		}
		feed(opts.MarginLines)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			l.packLine(line, page, y)
			buf.Write(header)
			buf.Write(line)
		}
		feed(opts.MarginLines)
		buf.WriteByte(formFeed)
	}
	command(&buf, cmdPrintEnd)
	return buf.Bytes(), nil
}

// packLine fills line with one row of the page, most significant bit first.
func (l Layout) packLine(line []byte, page *image.Gray, y int) {
	for i := range line {
		line[i] = 0
	}
	row := page.Pix[(y-page.Rect.Min.Y)*page.Stride:]
	for x := 0; x < l.PrintableDots; x++ {
		if row[x] < 0x80 {
			line[x/8] |= 0x80 >> uint(x%8)
		}
	}
}

// Command is an ESC { command recovered by DecodeJob.
type Command struct {
	Letter byte
	Params []byte
}

// Page is one label recovered by DecodeJob.
type Page struct {
	// Setup is the commands sent before its lines.
	Setup []Command
	// Lines holds every raster line, the margins' blank ones too.
	Lines [][]byte
}

// Job is a command stream recovered by DecodeJob.
type Job struct {
	// Setup is the commands sent before the first page's, and End those
	// after the last form feed.
	Setup, End []Command
	Pages      []Page
}

// DecodeJob parses a command stream produced by EncodeJob for the layout,
// checking each command's length and checksum, and each line's length. It
// exists for tests and diagnostics, such as a fake printer.
func (l Layout) DecodeJob(stream []byte) (Job, error) {
	var job Job
	var page *Page
	lineBytes := (l.PrintableDots + 7) / 8
	s := stream
	for len(s) > 0 {
		switch {
		case s[0] == formFeed:
			if page == nil || len(page.Lines) == 0 {
				return Job{}, fmt.Errorf("epsonlw: form feed without a page")
			}
			job.Pages = append(job.Pages, *page)
			page = nil
			s = s[1:]
		case len(s) >= 8 && s[0] == esc && s[1] == rasterLine:
			if !bytes.Equal(s[2:6], []byte{0, 0, 0, 1}) {
				return Job{}, fmt.Errorf("epsonlw: raster line header % X", s[:8])
			}
			if dots := int(binary.LittleEndian.Uint16(s[6:8])); dots != l.PrintableDots {
				return Job{}, fmt.Errorf("epsonlw: a raster line of %d dots, %s prints %d", dots, l.Media.Name, l.PrintableDots)
			}
			if page == nil || len(s) < 8+lineBytes {
				return Job{}, fmt.Errorf("epsonlw: raster line outside a page, or truncated")
			}
			page.Lines = append(page.Lines, append([]byte(nil), s[8:8+lineBytes]...))
			s = s[8+lineBytes:]
		case len(s) >= 6 && s[0] == esc && s[1] == cmdOpen:
			n := int(s[2])
			if n < 3 || len(s) < 3+n || s[2+n] != cmdClose {
				return Job{}, fmt.Errorf("epsonlw: damaged command at % X", s[:min(len(s), 6)])
			}
			c := Command{Letter: s[3], Params: append([]byte(nil), s[4:1+n]...)}
			sum := c.Letter
			for _, p := range c.Params {
				sum += p
			}
			if sum != s[1+n] {
				return Job{}, fmt.Errorf("epsonlw: command %q has checksum %02X, want %02X", c.Letter, s[1+n], sum)
			}
			switch {
			case page != nil && len(page.Lines) > 0:
				return Job{}, fmt.Errorf("epsonlw: command %q among a page's lines", c.Letter)
			case page != nil:
				page.Setup = append(page.Setup, c)
			case c.Letter == cmdStatus && len(job.Setup) > 0 && job.Setup[len(job.Setup)-1].Letter == cmdJobEnd:
				// A page's setup begins with a request for status.
				page = &Page{Setup: []Command{c}}
			case len(job.Pages) > 0:
				job.End = append(job.End, c)
			default:
				job.Setup = append(job.Setup, c)
			}
			s = s[3+n:]
		default:
			return Job{}, fmt.Errorf("epsonlw: unknown bytes at % X", s[:min(len(s), 3)])
		}
	}
	if page != nil {
		return Job{}, fmt.Errorf("epsonlw: the last page has no form feed")
	}
	if len(job.Pages) == 0 || len(job.End) != 1 || job.End[0].Letter != cmdPrintEnd {
		return Job{}, fmt.Errorf("epsonlw: the job does not end with one print end after its pages")
	}
	return job, nil
}

// PageImage turns raster lines back into a PrintableDots-wide image,
// undoing packLine.
func (l Layout) PageImage(lines [][]byte) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, l.PrintableDots, len(lines)))
	for y, line := range lines {
		for x := 0; x < l.PrintableDots; x++ {
			if line[x/8]&(0x80>>uint(x%8)) == 0 {
				img.Pix[y*img.Stride+x] = 0xFF
			}
		}
	}
	return img
}
