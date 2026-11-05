package ptouch

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brotherql"
)

// Raster command bytes (section 3 of each reference).
var (
	cmdInitialize   = []byte{0x1B, '@'}
	cmdRasterMode   = []byte{0x1B, 'i', 'a', 0x01}
	cmdPrintInfo    = []byte{0x1B, 'i', 'z'}
	cmdVariousMode  = []byte{0x1B, 'i', 'M'}
	cmdCutEvery     = []byte{0x1B, 'i', 'A'}
	cmdAdvancedMode = []byte{0x1B, 'i', 'K'}
	cmdMargin       = []byte{0x1B, 'i', 'd'}
	cmdCompressTIFF = []byte{'M', 0x02}
	cmdPrint        = byte(0x0C) // print, more pages follow
	cmdPrintAndFeed = byte(0x1A) // print the last page
)

// Mode bits (section 4, "Various mode settings" and "Advanced mode
// settings").
const (
	modeAutoCut = 0x40 // various mode
	modeMirror  = 0x80 // various mode

	advancedHalfCut = 0x04
	advancedNoChain = 0x08
)

// Print information valid flags (section 4, "Print information command").
const (
	infoKind     = 0x02
	infoWidth    = 0x04
	infoRecovery = 0x80
)

// Cut says when the printer cuts the tape off.
type Cut string

const (
	// CutEach cuts each label off as it finishes.
	CutEach Cut = "each_label"
	// CutEnd cuts only after the job's last label.
	CutEnd Cut = "last_label"
)

// JobOptions are the per-printer choices that change the command stream.
type JobOptions struct {
	Cut Cut
	// HalfCut cuts through the tape between labels but not its backing, so
	// a job comes out as one strip to peel labels from.
	HalfCut bool
	// Chain leaves the last label in the printer, unfed and uncut, so the
	// next job wastes no tape before its first label.
	Chain bool
	// Mirror prints labels mirrored, to be read through clear tape.
	Mirror bool
	// MarginDots is the tape fed before and after each label.
	MarginDots int
}

// Check reports whether the model takes the options.
func (o JobOptions) Check(mo Model) error {
	if o.Cut != CutEach && o.Cut != CutEnd {
		return fmt.Errorf("cut_after must be %s or %s", CutEach, CutEnd)
	}
	if o.HalfCut && !mo.HalfCut {
		return fmt.Errorf("the %s does not half cut", mo.Name)
	}
	if o.MarginDots < mo.MinMarginDots || o.MarginDots > mo.Dots(127) {
		return fmt.Errorf("the %s takes margins of %g-127 mm", mo.Name, roundTenth(mo.MM(mo.MinMarginDots)))
	}
	return nil
}

func roundTenth(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

// bytesPerLine is the length of one raster line on the model's head.
func (mo Model) bytesPerLine() int { return mo.Pins / 8 }

// EncodeJob builds the command stream that prints pages, each a label
// already prepared for the layout (see Layout.Prepare): PrintableDots wide,
// one row per raster line, and dark wherever a pixel is below 50% gray.
//
// The stream follows each reference's section 2.1: invalidate, initialize,
// then for each page raster mode, print information, various mode, "cut
// each * labels", advanced mode, the margin, TIFF compression, the raster
// lines and a print command.
//
// The print information names the cassette's width and asks the printer to
// check it, so a printer with another width loaded refuses the job rather
// than printing it, as Brother's examples do. A tube is checked by its type
// instead: its reported width is that of a tape.
func EncodeJob(l Layout, pages []*image.Gray, opts JobOptions) ([]byte, error) {
	mo, m := l.Model, l.Media
	if len(pages) == 0 {
		return nil, fmt.Errorf("ptouch: a job needs at least one page")
	}
	if err := opts.Check(mo); err != nil {
		return nil, fmt.Errorf("ptouch: %w", err)
	}
	flags := byte(infoRecovery | infoWidth)
	if m.Kind != TZe {
		flags = infoRecovery | infoKind
	}

	var buf bytes.Buffer
	buf.Write(make([]byte, mo.Invalidate))
	buf.Write(cmdInitialize)
	for i, page := range pages {
		b := page.Bounds()
		if b.Dx() != l.PrintableDots {
			return nil, fmt.Errorf("ptouch: page %d is %d dots wide, %s prints %d", i, b.Dx(), m.Name, l.PrintableDots)
		}
		if b.Dy() < mo.MinLines || b.Dy() > mo.MaxLinesFor(m) {
			return nil, fmt.Errorf("ptouch: page %d is %d lines long, the %s prints %d-%d", i, b.Dy(), mo.Name, mo.MinLines, mo.MaxLinesFor(m))
		}

		buf.Write(cmdRasterMode)

		buf.Write(cmdPrintInfo)
		info := [10]byte{flags, m.mediaType(), byte(m.infoWidth), 0}
		binary.LittleEndian.PutUint32(info[4:8], uint32(b.Dy()))
		switch {
		case mo.LastPage && i == len(pages)-1:
			info[8] = 2
		case i > 0:
			info[8] = 1
		}
		buf.Write(info[:])

		various := byte(0)
		if opts.Cut == CutEach {
			various |= modeAutoCut
		}
		if opts.Mirror {
			various |= modeMirror
		}
		buf.Write(cmdVariousMode)
		buf.WriteByte(various)
		if opts.Cut == CutEach && mo.CutEvery {
			buf.Write(cmdCutEvery)
			buf.WriteByte(1)
		}
		advanced := byte(0)
		if opts.HalfCut {
			advanced |= advancedHalfCut
		}
		if !opts.Chain {
			advanced |= advancedNoChain
		}
		buf.Write(cmdAdvancedMode)
		buf.WriteByte(advanced)
		buf.Write(cmdMargin)
		_ = binary.Write(&buf, binary.LittleEndian, uint16(opts.MarginDots))
		buf.Write(cmdCompressTIFF)

		line := make([]byte, mo.bytesPerLine())
		for y := b.Min.Y; y < b.Max.Y; y++ {
			l.packLine(line, page, y)
			packed := brotherql.PackBits(line)
			buf.WriteByte(mo.Raster)
			_ = binary.Write(&buf, binary.LittleEndian, uint16(len(packed)))
			buf.Write(packed)
		}

		if i == len(pages)-1 {
			buf.WriteByte(cmdPrintAndFeed)
		} else {
			buf.WriteByte(cmdPrint)
		}
	}
	return buf.Bytes(), nil
}

// packLine fills line with one raster row. The head's first pin is on the
// right of the tape as it leaves the printer, so the row is written right to
// left after the pins that sit beside the tape.
func (l Layout) packLine(line []byte, page *image.Gray, y int) {
	for i := range line {
		line[i] = 0
	}
	row := page.Pix[(y-page.Rect.Min.Y)*page.Stride:]
	for x := 0; x < l.PrintableDots; x++ {
		if row[x] >= 0x80 {
			continue
		}
		pin := l.RightMarginDots + (l.PrintableDots - 1 - x)
		line[pin/8] |= 0x80 >> uint(pin%8)
	}
}

// Page is one label recovered from a command stream by DecodeJob.
type Page struct {
	InfoFlags, MediaType, WidthMM byte
	// PageFlag is the print information's page number: 0 for the first.
	PageFlag byte
	// Various and Advanced are the mode settings' bytes, and CutEvery the
	// "cut each * labels" count, 0 when not sent.
	Various, Advanced, CutEvery byte
	MarginDots                  int
	// Lines holds each raster line, uncompressed.
	Lines [][]byte
	// Last reports whether the page ended the job.
	Last bool
}

// DecodeJob parses a command stream produced by EncodeJob for the model. It
// exists for tests and diagnostics, such as a fake printer.
func (mo Model) DecodeJob(stream []byte) ([]Page, error) {
	if len(stream) < mo.Invalidate || !bytes.Equal(stream[:mo.Invalidate], make([]byte, mo.Invalidate)) {
		return nil, fmt.Errorf("ptouch: stream does not start with %d invalidate bytes", mo.Invalidate)
	}
	s := stream[mo.Invalidate:]
	if !bytes.HasPrefix(s, cmdInitialize) {
		return nil, fmt.Errorf("ptouch: missing initialize command")
	}
	s = s[len(cmdInitialize):]

	var pages []Page
	var cur *Page
	var expectLines uint32
	take := func(n int) ([]byte, error) {
		if len(s) < n {
			return nil, fmt.Errorf("ptouch: stream truncated")
		}
		out := s[:n]
		s = s[n:]
		return out, nil
	}
	setting := func(prefix []byte) (byte, error) {
		cmd, err := take(len(prefix) + 1)
		if err != nil {
			return 0, err
		}
		if cur == nil {
			return 0, fmt.Errorf("ptouch: % X before print information", prefix)
		}
		return cmd[len(prefix)], nil
	}
	for len(s) > 0 {
		switch {
		case bytes.HasPrefix(s, cmdRasterMode):
			s = s[len(cmdRasterMode):]
		case bytes.HasPrefix(s, cmdPrintInfo):
			s = s[len(cmdPrintInfo):]
			info, err := take(10)
			if err != nil {
				return nil, err
			}
			pages = append(pages, Page{InfoFlags: info[0], MediaType: info[1], WidthMM: info[2], PageFlag: info[8]})
			cur = &pages[len(pages)-1]
			expectLines = binary.LittleEndian.Uint32(info[4:8])
		case bytes.HasPrefix(s, cmdVariousMode):
			v, err := setting(cmdVariousMode)
			if err != nil {
				return nil, err
			}
			cur.Various = v
		case bytes.HasPrefix(s, cmdCutEvery):
			if !mo.CutEvery {
				return nil, fmt.Errorf(`ptouch: the %s does not take "cut each * labels"`, mo.Name)
			}
			v, err := setting(cmdCutEvery)
			if err != nil {
				return nil, err
			}
			cur.CutEvery = v
		case bytes.HasPrefix(s, cmdAdvancedMode):
			v, err := setting(cmdAdvancedMode)
			if err != nil {
				return nil, err
			}
			if v&advancedHalfCut != 0 && !mo.HalfCut {
				return nil, fmt.Errorf("ptouch: the %s does not half cut", mo.Name)
			}
			cur.Advanced = v
		case bytes.HasPrefix(s, cmdMargin):
			cmd, err := take(5)
			if err != nil {
				return nil, err
			}
			if cur == nil {
				return nil, fmt.Errorf("ptouch: margin before print information")
			}
			cur.MarginDots = int(binary.LittleEndian.Uint16(cmd[3:]))
		case bytes.HasPrefix(s, cmdCompressTIFF):
			s = s[len(cmdCompressTIFF):]
		case s[0] == mo.Raster:
			hdr, err := take(3)
			if err != nil {
				return nil, err
			}
			data, err := take(int(binary.LittleEndian.Uint16(hdr[1:])))
			if err != nil {
				return nil, err
			}
			line, err := brotherql.UnpackBits(data)
			if err != nil {
				return nil, err
			}
			if len(line) != mo.bytesPerLine() || cur == nil {
				return nil, fmt.Errorf("ptouch: raster line of %d bytes", len(line))
			}
			cur.Lines = append(cur.Lines, line)
		case s[0] == cmdPrint || s[0] == cmdPrintAndFeed:
			if cur == nil || uint32(len(cur.Lines)) != expectLines {
				return nil, fmt.Errorf("ptouch: page ended after an unexpected number of lines")
			}
			cur.Last = s[0] == cmdPrintAndFeed
			s = s[1:]
			if cur.Last && len(s) > 0 {
				return nil, fmt.Errorf("ptouch: %d bytes after the final print command", len(s))
			}
		default:
			return nil, fmt.Errorf("ptouch: unknown command byte 0x%02X", s[0])
		}
	}
	if len(pages) == 0 || !pages[len(pages)-1].Last {
		return nil, fmt.Errorf("ptouch: stream does not end with a print command")
	}
	return pages, nil
}

// PageImage turns a decoded page back into a PrintableDots-wide image,
// undoing packLine.
func (l Layout) PageImage(p Page) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, l.PrintableDots, len(p.Lines)))
	for y, line := range p.Lines {
		for x := 0; x < l.PrintableDots; x++ {
			pin := l.RightMarginDots + (l.PrintableDots - 1 - x)
			if line[pin/8]&(0x80>>uint(pin%8)) == 0 {
				img.Pix[y*img.Stride+x] = 0xFF
			}
		}
	}
	return img
}
