package brotherql

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
)

// Raster command bytes, from Brother's QL-800 series raster command
// reference.
var (
	cmdInitialize    = []byte{0x1B, '@'}
	cmdRasterMode    = []byte{0x1B, 'i', 'a', 0x01}
	cmdPrintInfo     = []byte{0x1B, 'i', 'z'}
	cmdVariousMode   = []byte{0x1B, 'i', 'M'}
	cmdCutEvery      = []byte{0x1B, 'i', 'A'}
	cmdExpandedMode  = []byte{0x1B, 'i', 'K'}
	cmdMargin        = []byte{0x1B, 'i', 'd'}
	cmdCompressTIFF  = []byte{'M', 0x02}
	cmdRasterLine    = byte('g')
	cmdPrint         = byte(0x0C) // print, more pages follow
	cmdPrintAndFeed  = byte(0x1A) // print the last page
	invalidateLength = 200
)

const (
	mediaContinuous = 0x0A
	mediaDieCut     = 0x0B

	// Print information validity flags: recovery on, media type, width,
	// length and quality fields valid.
	printInfoFlags = 0x80 | 0x02 | 0x04 | 0x08 | 0x40

	autoCut  = 0x40 // various mode
	cutAtEnd = 0x08 // expanded mode
)

// EncodeJob builds the command stream that prints pages, each a label
// already prepared for m (see Media.Prepare): PrintableDots wide, one row per
// raster line, and dark wherever a pixel is below 50% gray. Every label is
// cut off as it finishes.
func EncodeJob(m Media, pages []*image.Gray) ([]byte, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("brotherql: a job needs at least one page")
	}
	var buf bytes.Buffer
	buf.Write(make([]byte, invalidateLength))
	buf.Write(cmdInitialize)

	mediaType := byte(mediaContinuous)
	if m.DieCut() {
		mediaType = mediaDieCut
	}
	margin := uint16(m.FeedMarginDots)

	for i, page := range pages {
		b := page.Bounds()
		if b.Dx() != m.PrintableDots {
			return nil, fmt.Errorf("brotherql: page %d is %d dots wide, %s prints %d", i, b.Dx(), m.Name, m.PrintableDots)
		}
		if m.DieCut() && b.Dy() != m.PrintableLines {
			return nil, fmt.Errorf("brotherql: page %d has %d lines, %s labels have %d", i, b.Dy(), m.Name, m.PrintableLines)
		}

		buf.Write(cmdRasterMode)

		buf.Write(cmdPrintInfo)
		info := [10]byte{printInfoFlags, mediaType, byte(m.WidthMM), byte(m.LengthMM)}
		binary.LittleEndian.PutUint32(info[4:8], uint32(b.Dy()))
		if i > 0 {
			info[8] = 1 // not the starting page
		}
		buf.Write(info[:])

		buf.Write(cmdVariousMode)
		buf.WriteByte(autoCut)
		buf.Write(cmdCutEvery)
		buf.WriteByte(1)
		buf.Write(cmdExpandedMode)
		buf.WriteByte(cutAtEnd)
		buf.Write(cmdMargin)
		_ = binary.Write(&buf, binary.LittleEndian, margin)
		buf.Write(cmdCompressTIFF)

		line := make([]byte, bytesPerLine)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			packLine(line, m, page, y)
			packed := packBits(line)
			buf.WriteByte(cmdRasterLine)
			buf.WriteByte(0x00)
			buf.WriteByte(byte(len(packed)))
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
func packLine(line []byte, m Media, page *image.Gray, y int) {
	for i := range line {
		line[i] = 0
	}
	row := page.Pix[(y-page.Rect.Min.Y)*page.Stride:]
	for x := 0; x < m.PrintableDots; x++ {
		if row[x] >= 0x80 {
			continue
		}
		pin := m.RightMarginDots + (m.PrintableDots - 1 - x)
		line[pin/8] |= 0x80 >> uint(pin%8)
	}
}

// Page is one label recovered from a command stream by DecodeJob.
type Page struct {
	MediaType byte
	WidthMM   byte
	LengthMM  byte
	// Lines holds each raster line's 90 bytes, uncompressed.
	Lines [][]byte
	// Last reports whether the page ended the job.
	Last bool
}

// DecodeJob parses a command stream produced by EncodeJob. It exists for
// tests and diagnostics, such as a fake printer that checks what it received.
func DecodeJob(stream []byte) ([]Page, error) {
	if len(stream) < invalidateLength || !bytes.Equal(stream[:invalidateLength], make([]byte, invalidateLength)) {
		return nil, fmt.Errorf("brotherql: stream does not start with %d invalidate bytes", invalidateLength)
	}
	s := stream[invalidateLength:]
	if !bytes.HasPrefix(s, cmdInitialize) {
		return nil, fmt.Errorf("brotherql: missing initialize command")
	}
	s = s[len(cmdInitialize):]

	var pages []Page
	var cur *Page
	var expectLines uint32
	take := func(n int) ([]byte, error) {
		if len(s) < n {
			return nil, fmt.Errorf("brotherql: stream truncated")
		}
		out := s[:n]
		s = s[n:]
		return out, nil
	}

	for len(s) > 0 {
		switch {
		case bytes.HasPrefix(s, cmdRasterMode):
			s = s[len(cmdRasterMode):]
			pages = append(pages, Page{})
			cur = &pages[len(pages)-1]
		case bytes.HasPrefix(s, cmdPrintInfo):
			s = s[len(cmdPrintInfo):]
			info, err := take(10)
			if err != nil {
				return nil, err
			}
			if cur == nil {
				return nil, fmt.Errorf("brotherql: print information before raster mode")
			}
			cur.MediaType, cur.WidthMM, cur.LengthMM = info[1], info[2], info[3]
			expectLines = binary.LittleEndian.Uint32(info[4:8])
		case bytes.HasPrefix(s, cmdVariousMode), bytes.HasPrefix(s, cmdCutEvery), bytes.HasPrefix(s, cmdExpandedMode):
			if _, err := take(4); err != nil {
				return nil, err
			}
		case bytes.HasPrefix(s, cmdMargin):
			if _, err := take(5); err != nil {
				return nil, err
			}
		case bytes.HasPrefix(s, cmdCompressTIFF):
			s = s[len(cmdCompressTIFF):]
		case s[0] == cmdRasterLine:
			hdr, err := take(3)
			if err != nil {
				return nil, err
			}
			data, err := take(int(hdr[2]))
			if err != nil {
				return nil, err
			}
			line, err := unpackBits(data)
			if err != nil {
				return nil, err
			}
			if len(line) != bytesPerLine || cur == nil {
				return nil, fmt.Errorf("brotherql: raster line of %d bytes", len(line))
			}
			cur.Lines = append(cur.Lines, line)
		case s[0] == cmdPrint || s[0] == cmdPrintAndFeed:
			if cur == nil || uint32(len(cur.Lines)) != expectLines {
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

// PageImage turns a decoded page back into a PrintableDots-wide image for m,
// undoing packLine.
func PageImage(m Media, p Page) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, m.PrintableDots, len(p.Lines)))
	for y, line := range p.Lines {
		for x := 0; x < m.PrintableDots; x++ {
			pin := m.RightMarginDots + (m.PrintableDots - 1 - x)
			if line[pin/8]&(0x80>>uint(pin%8)) == 0 {
				img.Pix[y*img.Stride+x] = 0xFF
			}
		}
	}
	return img
}
