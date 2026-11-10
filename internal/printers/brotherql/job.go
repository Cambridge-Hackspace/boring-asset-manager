package brotherql

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
)

// Raster command bytes, common to every model's raster command reference.
var (
	cmdInitialize   = []byte{0x1B, '@'}
	cmdRasterMode   = []byte{0x1B, 'i', 'a', 0x01}
	cmdPrintInfo    = []byte{0x1B, 'i', 'z'}
	cmdVariousMode  = []byte{0x1B, 'i', 'M'}
	cmdCutEvery     = []byte{0x1B, 'i', 'A'}
	cmdExpandedMode = []byte{0x1B, 'i', 'K'}
	cmdMargin       = []byte{0x1B, 'i', 'd'}
	cmdCompressTIFF = []byte{'M', 0x02}
	cmdRasterLine   = byte('g')
	cmdColorLine    = byte('w')  // a raster line of one color, in two-color printing
	cmdPrint        = byte(0x0C) // print, more pages follow
	cmdPrintAndFeed = byte(0x1A) // print the last page
)

const (
	mediaContinuous = 0x0A
	mediaDieCut     = 0x0B

	autoCut  = 0x40 // various mode
	cutAtEnd = 0x08 // expanded mode
	twoColor = 0x01 // expanded mode

	// The colors of a two-color raster line: black, printed with high
	// energy, and red, with low.
	colorBlack = 0x01
	colorRed   = 0x02
)

// JobOptions are the per-printer choices that change the command stream.
type JobOptions struct {
	// Cut cuts each label off as it finishes. Without it the tape feeds
	// on uncut, to be torn or cut by hand. Only models with a cutter cut.
	Cut bool
}

// bytesPerLine is the length of one raster line on the model's head.
func (mo Model) bytesPerLine() int { return mo.Pins / 8 }

// EncodeJob builds the command stream that prints pages, each a label
// already prepared for the layout (see Layout.Prepare): PrintableDots wide,
// one row per raster line, and dark wherever a pixel is below 50% gray.
//
// The stream is the one each model's raster command reference describes:
// invalidate, initialize, then for each page the raster mode switch, print
// information, the cut settings, the feed margin, TIFF compression, the
// raster lines and a print command, leaving out what the model does not
// take. Uncompressed raster lines span the whole head, as the references
// give their length.
//
// On a layout that prints two colors (Layout.TwoColor), every page is sent
// in two colors, with nothing in red; EncodeTwoColorJob sends red too.
func EncodeJob(l Layout, pages []*image.Gray, opts JobOptions) ([]byte, error) {
	return encodeJob(l, pages, nil, opts)
}

// EncodeTwoColorJob is EncodeJob for a layout that prints two colors: each
// page as black, dark wherever black prints, and red, dark wherever red
// prints, both prepared for the layout. Where both are dark, black prints.
func EncodeTwoColorJob(l Layout, black, red []*image.Gray, opts JobOptions) ([]byte, error) {
	if !l.TwoColor() {
		return nil, fmt.Errorf("brotherql: the %s prints only black on %s", l.Model.Name, l.Media.Name)
	}
	if len(red) != len(black) {
		return nil, fmt.Errorf("brotherql: %d pages in black and %d in red", len(black), len(red))
	}
	return encodeJob(l, black, red, opts)
}

// encodeJob encodes pages, and in two colors, their red.
func encodeJob(l Layout, pages, red []*image.Gray, opts JobOptions) ([]byte, error) {
	mo, m := l.Model, l.Media
	if len(pages) == 0 {
		return nil, fmt.Errorf("brotherql: a job needs at least one page")
	}
	colors := l.TwoColor()
	if opts.Cut && !mo.Cutter {
		return nil, fmt.Errorf("brotherql: the %s has no cutter", mo.Name)
	}
	var buf bytes.Buffer
	buf.Write(make([]byte, mo.Invalidate))
	buf.Write(cmdInitialize)

	mediaType := byte(mediaContinuous)
	if m.DieCut() {
		mediaType = mediaDieCut
	}
	width := m.infoWidth
	if width == 0 {
		width = int(m.WidthMM + 0.5)
	}
	length := min(int(m.LengthMM+0.5), 255)

	for i, page := range pages {
		b := page.Bounds()
		if b.Dx() != l.PrintableDots {
			return nil, fmt.Errorf("brotherql: page %d is %d dots wide, %s prints %d", i, b.Dx(), m.Name, l.PrintableDots)
		}
		if m.DieCut() && b.Dy() != l.PrintableLines {
			return nil, fmt.Errorf("brotherql: page %d has %d lines, %s labels have %d", i, b.Dy(), m.Name, l.PrintableLines)
		}
		var redPage *image.Gray
		if red != nil {
			redPage = red[i]
			if redPage.Bounds().Size() != b.Size() {
				return nil, fmt.Errorf("brotherql: page %d is %v in black and %v in red", i, b.Size(), redPage.Bounds().Size())
			}
		}

		if mo.ModeSwitch {
			buf.Write(cmdRasterMode)
		}

		buf.Write(cmdPrintInfo)
		info := [10]byte{mo.InfoFlags, mediaType, byte(width), byte(length)}
		binary.LittleEndian.PutUint32(info[4:8], uint32(b.Dy()))
		if i > 0 {
			info[8] = 1 // not the starting page
		}
		buf.Write(info[:])

		if mo.VariousMode {
			buf.Write(cmdVariousMode)
			if opts.Cut {
				buf.WriteByte(autoCut)
			} else {
				buf.WriteByte(0)
			}
		}
		if opts.Cut && mo.CutEvery {
			buf.Write(cmdCutEvery)
			buf.WriteByte(1)
		}
		if mo.Expanded {
			buf.Write(cmdExpandedMode)
			var mode byte
			if opts.Cut {
				mode |= cutAtEnd
			}
			if colors {
				mode |= twoColor
			}
			buf.WriteByte(mode)
		}
		buf.Write(cmdMargin)
		_ = binary.Write(&buf, binary.LittleEndian, uint16(l.FeedDots))
		if mo.Compression {
			buf.Write(cmdCompressTIFF)
		}

		line := make([]byte, mo.bytesPerLine())
		send := func(command, color byte) {
			data := line
			if mo.Compression {
				data = packBits(line)
			}
			buf.WriteByte(command)
			buf.WriteByte(color)
			buf.WriteByte(byte(len(data)))
			buf.Write(data)
		}
		for y := b.Min.Y; y < b.Max.Y; y++ {
			l.packLine(line, page, y)
			if !colors {
				send(cmdRasterLine, 0x00)
				continue
			}
			send(cmdColorLine, colorBlack)
			// Red where black does not print.
			l.packRedLine(line, page, redPage, y)
			send(cmdColorLine, colorRed)
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

// packRedLine fills line with one raster row of red: dark in red where
// black, on page, is not.
func (l Layout) packRedLine(line []byte, page, red *image.Gray, y int) {
	for i := range line {
		line[i] = 0
	}
	if red == nil {
		return
	}
	row := page.Pix[(y-page.Rect.Min.Y)*page.Stride:]
	reds := red.Pix[(y-red.Rect.Min.Y)*red.Stride:]
	for x := 0; x < l.PrintableDots; x++ {
		if reds[x] >= 0x80 || row[x] < 0x80 {
			continue
		}
		pin := l.RightMarginDots + (l.PrintableDots - 1 - x)
		line[pin/8] |= 0x80 >> uint(pin%8)
	}
}

// Page is one label recovered from a command stream by DecodeJob.
type Page struct {
	MediaType byte
	WidthMM   byte
	LengthMM  byte
	// InfoFlags is the print information's valid flag.
	InfoFlags byte
	// AutoCut reports whether the page switched the cutter on.
	AutoCut bool
	// FeedDots is the margin the page asked for.
	FeedDots int
	// Lines holds each raster line, uncompressed: in two colors, those of
	// black.
	Lines [][]byte
	// TwoColor reports whether the page is printed in two colors, and
	// RedLines then holds each raster line of red.
	TwoColor bool
	RedLines [][]byte
	// Last reports whether the page ended the job.
	Last bool
}

// ql810W is the model DecodeJob and PageImage assume.
func ql810W() Model { return models["QL-810W"] }

// DecodeJob parses a command stream produced by EncodeJob for the QL-800
// series, which the fake printers in tests stand in for. It exists for
// tests and diagnostics; Model.DecodeJob reads any model's.
func DecodeJob(stream []byte) ([]Page, error) { return ql810W().DecodeJob(stream) }

// DecodeJob parses a command stream produced by EncodeJob for the model,
// refusing commands the model does not take.
func (mo Model) DecodeJob(stream []byte) ([]Page, error) {
	if len(stream) < mo.Invalidate || !bytes.Equal(stream[:mo.Invalidate], make([]byte, mo.Invalidate)) {
		return nil, fmt.Errorf("brotherql: stream does not start with %d invalidate bytes", mo.Invalidate)
	}
	s := stream[mo.Invalidate:]
	if !bytes.HasPrefix(s, cmdInitialize) {
		return nil, fmt.Errorf("brotherql: missing initialize command")
	}
	s = s[len(cmdInitialize):]

	var pages []Page
	var cur *Page
	var expectLines uint32
	compressed := false
	take := func(n int) ([]byte, error) {
		if len(s) < n {
			return nil, fmt.Errorf("brotherql: stream truncated")
		}
		out := s[:n]
		s = s[n:]
		return out, nil
	}
	unsupported := func(what string) error {
		return fmt.Errorf("brotherql: the %s does not take %s", mo.Name, what)
	}

	for len(s) > 0 {
		switch {
		case bytes.HasPrefix(s, cmdRasterMode):
			if !mo.ModeSwitch {
				return nil, unsupported("raster mode switching")
			}
			s = s[len(cmdRasterMode):]
		case bytes.HasPrefix(s, cmdPrintInfo):
			s = s[len(cmdPrintInfo):]
			info, err := take(10)
			if err != nil {
				return nil, err
			}
			pages = append(pages, Page{InfoFlags: info[0], MediaType: info[1], WidthMM: info[2], LengthMM: info[3]})
			cur = &pages[len(pages)-1]
			expectLines = binary.LittleEndian.Uint32(info[4:8])
			compressed = false
		case bytes.HasPrefix(s, cmdVariousMode):
			if !mo.VariousMode {
				return nil, unsupported("various mode")
			}
			cmd, err := take(4)
			if err != nil {
				return nil, err
			}
			if cur == nil {
				return nil, fmt.Errorf("brotherql: mode setting before print information")
			}
			cur.AutoCut = cmd[3]&autoCut != 0
			if cur.AutoCut && !mo.Cutter {
				return nil, unsupported("cutting")
			}
		case bytes.HasPrefix(s, cmdCutEvery):
			if !mo.CutEvery {
				return nil, unsupported(`"cut each * labels"`)
			}
			if _, err := take(4); err != nil {
				return nil, err
			}
		case bytes.HasPrefix(s, cmdExpandedMode):
			if !mo.Expanded {
				return nil, unsupported("expanded mode")
			}
			cmd, err := take(4)
			if err != nil {
				return nil, err
			}
			if cmd[3]&twoColor != 0 {
				if !mo.TwoColor {
					return nil, unsupported("two-color printing")
				}
				if cur == nil {
					return nil, fmt.Errorf("brotherql: mode setting before print information")
				}
				cur.TwoColor = true
			}
		case bytes.HasPrefix(s, cmdMargin):
			cmd, err := take(5)
			if err != nil {
				return nil, err
			}
			if cur == nil {
				return nil, fmt.Errorf("brotherql: margin before print information")
			}
			cur.FeedDots = int(binary.LittleEndian.Uint16(cmd[3:]))
		case bytes.HasPrefix(s, cmdCompressTIFF):
			if !mo.Compression {
				return nil, unsupported("compression")
			}
			s = s[len(cmdCompressTIFF):]
			compressed = true
		case s[0] == cmdColorLine:
			hdr, err := take(3)
			if err != nil {
				return nil, err
			}
			if cur == nil || !cur.TwoColor {
				return nil, fmt.Errorf("brotherql: a two-color raster line outside two-color printing")
			}
			data, err := take(int(hdr[2]))
			if err != nil {
				return nil, err
			}
			line := data
			if compressed {
				if line, err = unpackBits(data); err != nil {
					return nil, err
				}
			}
			if len(line) != mo.bytesPerLine() {
				return nil, fmt.Errorf("brotherql: raster line of %d bytes", len(line))
			}
			switch hdr[1] {
			case colorBlack:
				cur.Lines = append(cur.Lines, line)
			case colorRed:
				cur.RedLines = append(cur.RedLines, line)
			default:
				return nil, fmt.Errorf("brotherql: raster line of color 0x%02X", hdr[1])
			}
		case s[0] == cmdRasterLine:
			hdr, err := take(3)
			if err != nil {
				return nil, err
			}
			data, err := take(int(hdr[2]))
			if err != nil {
				return nil, err
			}
			line := data
			if compressed {
				if line, err = unpackBits(data); err != nil {
					return nil, err
				}
			}
			if len(line) != mo.bytesPerLine() || cur == nil {
				return nil, fmt.Errorf("brotherql: raster line of %d bytes", len(line))
			}
			cur.Lines = append(cur.Lines, line)
		case s[0] == cmdPrint || s[0] == cmdPrintAndFeed:
			if cur == nil || uint32(len(cur.Lines)) != expectLines || cur.TwoColor && uint32(len(cur.RedLines)) != expectLines {
				return nil, fmt.Errorf("brotherql: page ended after an unexpected number of lines")
			}
			cur.Last = s[0] == cmdPrintAndFeed
			s = s[1:]
			if cur.Last && len(s) > 0 {
				return nil, fmt.Errorf("brotherql: %d bytes after the final print command", len(s))
			}
		default:
			return nil, fmt.Errorf("brotherql: unknown command byte 0x%02X", s[0])
		}
	}
	if len(pages) == 0 || !pages[len(pages)-1].Last {
		return nil, fmt.Errorf("brotherql: stream does not end with a print command")
	}
	return pages, nil
}

// PageImage turns a page decoded from a QL-800 series stream back into a
// PrintableDots-wide image for the DK roll m. Layout.PageImage does so for
// any model.
func PageImage(m Media, p Page) *image.Gray {
	l, err := ql810W().Layout(m)
	if err != nil {
		panic(err)
	}
	return l.PageImage(p)
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
