package dymolw

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
)

const (
	esc = 0x1B
	syn = 0x16 // an uncompressed raster line follows
	etb = 0x17 // a run-length compressed raster line follows

	// minSync is the shortest resynchronizing ESC run the manual allows: one
	// more than the 84 bytes of a full line on a 56 mm head.
	minSync = 85

	// lengthSlack is added to a die-cut label's length so the printer reaches
	// the sense hole before it stops looking for it (about 3 mm).
	lengthSlack = 36

	// maxSkip is the most blank lines one skip command covers.
	maxSkip = 255
)

// Density is the print darkness.
type Density string

const (
	DensityLight  Density = "light"
	DensityMedium Density = "medium"
	DensityNormal Density = "normal"
	DensityDark   Density = "dark"
)

var densityCommands = map[Density]byte{DensityLight: 'c', DensityMedium: 'd', DensityNormal: 'e', DensityDark: 'g'}

// Densities lists the print densities, lightest first.
func Densities() []Density {
	return []Density{DensityLight, DensityMedium, DensityNormal, DensityDark}
}

// Roll selects the roll on twin-roll models.
type Roll string

const (
	RollAuto  Roll = "auto"
	RollLeft  Roll = "left"
	RollRight Roll = "right"
)

var rollCommands = map[Roll]byte{RollAuto: '0', RollLeft: '1', RollRight: '2'}

// Rolls lists the roll selections.
func Rolls() []Roll { return []Roll{RollAuto, RollLeft, RollRight} }

// JobOptions are the per-printer choices that change the command stream.
type JobOptions struct {
	// Density defaults to normal.
	Density Density
	// Roll is only sent to twin-roll models, and defaults to auto.
	Roll Roll
	// JobID tags a version 2 job; the printer reports it back in status.
	JobID uint32
}

// PageDots is the width of a page prepared for the model: the label width,
// cut on version 1 models to the most bytes per line the head takes.
func (m Media) PageDots(mo Model) int {
	if mo.Protocol == ProtocolV1 {
		return min(m.WidthDots, mo.HeadDots)
	}
	return m.WidthDots
}

// EncodeJob builds the command stream that prints pages, each a label
// already prepared for the model and roll (see Media.Prepare), dark wherever
// a pixel is below 50% gray.
func EncodeJob(mo Model, m Media, pages []*image.Gray, opts JobOptions) ([]byte, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("dymolw: a job needs at least one page")
	}
	for i, page := range pages {
		b := page.Bounds()
		if b.Dx() != m.PageDots(mo) {
			return nil, fmt.Errorf("dymolw: page %d is %d dots wide, %s on the %s prints %d", i, b.Dx(), m.Name, mo.Name, m.PageDots(mo))
		}
		if m.DieCut() && b.Dy() != m.LengthDots {
			return nil, fmt.Errorf("dymolw: page %d has %d lines, %s labels have %d", i, b.Dy(), m.Name, m.LengthDots)
		}
	}
	density, ok := densityCommands[opts.Density]
	if opts.Density == "" {
		density, ok = densityCommands[DensityNormal], true
	}
	if !ok {
		return nil, fmt.Errorf("dymolw: unknown density %q", opts.Density)
	}
	roll, ok := rollCommands[opts.Roll]
	if opts.Roll == "" {
		roll, ok = rollCommands[RollAuto], true
	}
	if !ok {
		return nil, fmt.Errorf("dymolw: unknown roll %q", opts.Roll)
	}

	switch mo.Protocol {
	case ProtocolV1:
		return encodeV1(mo, m, pages, density, roll), nil
	case ProtocolV2:
		return encodeV2(m, pages, density, opts.JobID), nil
	}
	return nil, fmt.Errorf("dymolw: %s has no protocol", mo.Name)
}

// packRow sets a bit, most significant first, for each dark pixel of row y.
func packRow(page *image.Gray, y int, out []byte) {
	for i := range out {
		out[i] = 0
	}
	row := page.Pix[(y-page.Rect.Min.Y)*page.Stride:]
	for x := 0; x < page.Rect.Dx(); x++ {
		if row[x] < 0x80 {
			out[x/8] |= 0x80 >> uint(x%8)
		}
	}
}

// encodeV1 follows the LabelWriter 450 Series Technical Reference Manual:
// resynchronize, choose quality, density and roll, set the label length, then
// send each label's raster lines with a short form feed between labels and a
// form feed after the last.
func encodeV1(mo Model, m Media, pages []*image.Gray, density, roll byte) []byte {
	var buf bytes.Buffer
	// One more ESC than the longest raster line ends any line or command
	// the printer is part way through (manual, "Data Synchronization").
	buf.Write(bytes.Repeat([]byte{esc}, mo.HeadDots/8+1))
	buf.Write([]byte{esc, 'h'}) // text mode, 300 × 300 DPI
	buf.Write([]byte{esc, density})
	if mo.TwinRoll {
		buf.Write([]byte{esc, 'q', roll})
	}
	// A negative length selects continuous feed; on die-cut rolls it bounds
	// the search for the next label's sense hole.
	length := uint16(0xFFFF)
	if m.DieCut() {
		length = uint16(m.LengthDots + lengthSlack)
	}
	buf.Write([]byte{esc, 'L', byte(length >> 8), byte(length)})

	lw := lineWriter{buf: &buf, dotTab: -1, bytesPerLine: -1}
	for i, page := range pages {
		rows := make([][]byte, page.Rect.Dy())
		for y := range rows {
			rows[y] = make([]byte, (page.Rect.Dx()+7)/8)
			packRow(page, page.Rect.Min.Y+y, rows[y])
		}
		// A form feed finds the next die-cut label by itself, so blank
		// lines at the end of one need not be sent; continuous labels keep
		// their length.
		lw.page(rows, !m.DieCut())
		if i < len(pages)-1 {
			buf.Write([]byte{esc, 'G'}) // next label into print position
		}
	}
	buf.Write([]byte{esc, 'E'}) // last label out to the tear bar
	return buf.Bytes()
}

// lineWriter sends packed raster lines. The printer holds the dot tab and
// bytes per line until they change, so they are only sent when they do.
type lineWriter struct {
	buf                  *bytes.Buffer
	dotTab, bytesPerLine int
}

// page sends one label's lines: blank lines as skips, the rest without their
// blank bytes at either end, compressed when that is shorter.
func (lw *lineWriter) page(rows [][]byte, keepTrailingBlanks bool) {
	blank := 0
	for _, row := range rows {
		lead, trail := blanks(row)
		if lead == len(row) {
			blank++
			continue
		}
		writeSkip(lw.buf, blank)
		blank = 0

		data := row[lead : len(row)-trail]
		if lead != lw.dotTab {
			lw.buf.Write([]byte{esc, 'B', byte(lead)})
			lw.dotTab = lead
		}
		if len(data) != lw.bytesPerLine {
			lw.buf.Write([]byte{esc, 'D', byte(len(data))})
			lw.bytesPerLine = len(data)
		}
		if rle := compressLine(data); len(rle) < len(data) {
			lw.buf.WriteByte(etb)
			lw.buf.Write(rle)
		} else {
			lw.buf.WriteByte(syn)
			lw.buf.Write(data)
		}
	}
	if keepTrailingBlanks {
		writeSkip(lw.buf, blank)
	}
}

func writeSkip(buf *bytes.Buffer, n int) {
	for n > 0 {
		k := min(n, maxSkip)
		buf.Write([]byte{esc, 'f', 1, byte(k)})
		n -= k
	}
}

// blanks counts the zero bytes at each end of a line.
func blanks(line []byte) (lead, trail int) {
	for lead < len(line) && line[lead] == 0 {
		lead++
	}
	if lead == len(line) {
		return lead, 0
	}
	for line[len(line)-1-trail] == 0 {
		trail++
	}
	return lead, trail
}

// compressLine run-length encodes a line bit by bit: each byte is a run of
// 1-128 equal dots, the top bit its color and the rest its length less one.
func compressLine(data []byte) []byte {
	var out []byte
	bits := len(data) * 8
	bit := func(i int) bool { return data[i/8]&(0x80>>uint(i%8)) != 0 }
	for i := 0; i < bits; {
		dark := bit(i)
		run := 1
		for i+run < bits && run < 128 && bit(i+run) == dark {
			run++
		}
		code := byte(run - 1)
		if dark {
			code |= 0x80
		}
		out = append(out, code)
		i += run
	}
	return out
}

// encodeV2 writes the second-generation language: a job header, then for
// each label its length, index, print data header, uncompressed lines and a
// short form feed, and finally a form feed and an end-of-job command.
func encodeV2(m Media, pages []*image.Gray, density byte, jobID uint32) []byte {
	var buf bytes.Buffer
	le32 := func(v uint32) { _ = binary.Write(&buf, binary.LittleEndian, v) }
	le16 := func(v uint16) { _ = binary.Write(&buf, binary.LittleEndian, v) }

	buf.Write([]byte{esc, 's'})
	le32(jobID)
	buf.Write([]byte{esc, density})
	buf.Write([]byte{esc, 'h'})                         // text quality
	buf.Write([]byte{esc, 'M', 0, 0, 0, 0, 0, 0, 0, 0}) // default media type

	length := uint16(0) // die-cut: the printer finds the label gap
	if !m.DieCut() {
		length = 0xFFFF
	}
	for i, page := range pages {
		lines := page.Rect.Dy()
		// The form feed covers blank lines at the end of a die-cut label.
		if m.DieCut() {
			for lines > 0 && blankRow(page, page.Rect.Min.Y+lines-1) {
				lines--
			}
		}
		dots := (page.Rect.Dx() + 7) / 8 * 8

		buf.Write([]byte{esc, 'L'})
		le16(length)
		buf.Write([]byte{esc, 'n'})
		le16(uint16(i + 1)) // label index
		// Print data header: 1 bit per pixel, then a byte that is undocumented
		// and always 2.
		buf.Write([]byte{esc, 'D', 1, 2})
		le32(uint32(lines))
		le32(uint32(dots))
		row := make([]byte, dots/8)
		for y := page.Rect.Min.Y; y < page.Rect.Min.Y+lines; y++ {
			packRow(page, y, row)
			buf.Write(row)
		}
		buf.Write([]byte{esc, 'G'})
	}
	buf.Write([]byte{esc, 'E'})
	buf.Write([]byte{esc, 'Q'}) // end of job
	return buf.Bytes()
}

func blankRow(page *image.Gray, y int) bool {
	row := page.Pix[(y-page.Rect.Min.Y)*page.Stride:]
	for x := 0; x < page.Rect.Dx(); x++ {
		if row[x] < 0x80 {
			return false
		}
	}
	return true
}
