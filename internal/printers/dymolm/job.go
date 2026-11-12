package dymolm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
)

// Commands of Appendix B of the LabelWriter 450 Series Technical Reference
// Manual, and the 500TS's framing from DYMO's Linux driver.
const (
	esc = 0x1B
	syn = 0x16 // one line: its bytes follow, or none when bytes per line is 0

	cmdDotTab   = 'B'
	cmdTapeType = 'C'
	cmdBytes    = 'D'
	cmdCut      = 'E'
	cmdFrame    = 'Y'

	// chainMark is the byte of the line DYMO's driver prints between
	// labels it does not cut apart, to show where to cut.
	chainMark = 0x99

	// frameBytes is how much of the stream each of the 500TS's blocks
	// holds; DYMO's driver sends a block once it has more than this.
	frameBytes = 4096
)

// TapeType is the tape's colors, which set the head's heat (Appendix B,
// "Set Tape Type").
type TapeType string

// tapeTypes are the manual's tape types, in its order: each one's number is
// its index.
var tapeTypes = []TapeType{
	"black_on_white", "black_on_blue", "black_on_red", "black_on_silver", "black_on_yellow",
	"black_on_gold", "black_on_green", "black_on_fluorescent_green", "black_on_fluorescent_red",
	"white_on_clear", "white_on_black", "blue_on_white", "red_on_white",
}

// TapeBlackOnWhite is black on white or clear tape, the default.
const TapeBlackOnWhite TapeType = "black_on_white"

// TapeTypes lists the tape types in the manual's order.
func TapeTypes() []TapeType { return append([]TapeType(nil), tapeTypes...) }

func (t TapeType) number() (byte, bool) {
	for i, v := range tapeTypes {
		if v == t {
			return byte(i), true
		}
	}
	return 0, false
}

// Cut says when a model with an automatic cutter cuts the tape off.
type Cut string

const (
	// CutEach cuts each label off as it finishes.
	CutEach Cut = "each_label"
	// CutEnd cuts only after the job's last label, and prints a cut mark
	// between the others.
	CutEnd Cut = "last_label"
)

// JobOptions are the per-printer choices that change the command stream.
type JobOptions struct {
	Tape TapeType
	// Cut is for models with an automatic cutter; the others print a cut
	// mark between labels.
	Cut Cut
	// MarginLines is the tape fed before and after each label.
	MarginLines int
}

// DefaultOptions are DYMO's: black on white, each label cut off, and its
// driver's leader.
func DefaultOptions(mo Model) JobOptions {
	return JobOptions{Tape: TapeBlackOnWhite, Cut: CutEach, MarginLines: mo.LeaderLines}
}

// MaxMarginMM is the longest margin.
const MaxMarginMM = 127

// Check reports whether the model takes the options.
func (o JobOptions) Check(mo Model) error {
	if _, ok := o.Tape.number(); !ok {
		return fmt.Errorf("unknown tape type %q", o.Tape)
	}
	if o.Cut != CutEach && o.Cut != CutEnd {
		return fmt.Errorf("cut_after must be %s or %s", CutEach, CutEnd)
	}
	if o.Cut == CutEnd && !mo.AutoCut {
		return fmt.Errorf("the %s has no automatic cutter", mo.Name)
	}
	// The tape between the head and the cutter is fed before every label
	// whatever the margin, so a margin is at least that.
	if o.MarginLines < mo.CutterLines || o.MarginLines > mo.Dots(MaxMarginMM) {
		return fmt.Errorf("the %s takes margins of %.1f-%d mm", mo.Name, mo.MM(mo.CutterLines), MaxMarginMM)
	}
	return nil
}

// headBytes is the length of a full line.
func (mo Model) headBytes() int { return mo.HeadDots / 8 }

// encoder writes the stream, sending the dot tab and bytes per line only
// when they change, as the manual's printers hold them.
type encoder struct {
	buf                 bytes.Buffer
	dotTab, bytesInLine int
}

func (e *encoder) set(cmd byte, v int, last *int) {
	if v != *last {
		e.buf.Write([]byte{esc, cmd, byte(v)})
		*last = v
	}
}

// feed sends n blank lines: bytes per line 0, then a SYN for each.
func (e *encoder) feed(n int) {
	if n <= 0 {
		return
	}
	e.buf.Write([]byte{esc, cmdBytes, 0})
	e.buf.Write(bytes.Repeat([]byte{syn}, n))
	e.bytesInLine = -1
}

// line sends one line of the head, without its blank bytes at either end.
func (e *encoder) line(line []byte, lead, trail int) {
	e.set(cmdDotTab, lead, &e.dotTab)
	e.set(cmdBytes, len(line)-lead-trail, &e.bytesInLine)
	e.buf.WriteByte(syn)
	e.buf.Write(line[lead : len(line)-trail])
}

// mark prints the cut mark: a full line of it, from the head's first dot.
func (e *encoder) mark(headBytes int) {
	e.dotTab, e.bytesInLine = -1, -1
	e.line(bytes.Repeat([]byte{chainMark}, headBytes), 0, 0)
	e.dotTab, e.bytesInLine = -1, -1
}

// EncodeJob builds the command stream that prints pages, each a label
// already prepared for the layout (see Layout.Prepare): PrintableDots wide,
// one row per line, and dark wherever a pixel is below 50% gray.
//
// The stream is laid out as DYMO's Linux driver lays it out. A line of NUL
// bytes and the tape type begin it. Each label then has the margin fed
// before it (less the tape already between the head and the cutter), its
// lines, and the margin fed after it, lengthened to the model's shortest
// label. Between labels the tape is fed to the cutter and cut, or a cut
// mark is printed. After the last, the tape is fed to the cutter and cut,
// on models that cut.
func EncodeJob(l Layout, pages []*image.Gray, opts JobOptions) ([]byte, error) {
	mo := l.Model
	if len(pages) == 0 {
		return nil, fmt.Errorf("dymolm: a job needs at least one page")
	}
	if err := opts.Check(mo); err != nil {
		return nil, fmt.Errorf("dymolm: %w", err)
	}
	for i, page := range pages {
		b := page.Bounds()
		if b.Dx() != l.PrintableDots {
			return nil, fmt.Errorf("dymolm: page %d is %d dots wide, %s prints %d", i, b.Dx(), l.Media.Name, l.PrintableDots)
		}
		if b.Dy() < 1 || b.Dy() > mo.MaxLines() {
			return nil, fmt.Errorf("dymolm: page %d is %d lines long, the %s prints 1-%d", i, b.Dy(), mo.Name, mo.MaxLines())
		}
	}
	tapeType, _ := opts.Tape.number()

	e := &encoder{}
	e.buf.Write(make([]byte, mo.headBytes()))
	e.buf.Write([]byte{esc, cmdTapeType, tapeType})
	line := make([]byte, mo.headBytes())
	for i, page := range pages {
		e.dotTab, e.bytesInLine = -1, -1
		before := opts.MarginLines
		if i == 0 {
			before -= mo.CutterLines
		} else if mo.AutoCut && opts.Cut == CutEach {
			e.feed(mo.CutterLines)
			e.buf.Write([]byte{esc, cmdCut})
			before -= mo.CutterLines
		} else {
			e.mark(mo.headBytes())
		}
		e.feed(before)

		b := page.Bounds()
		blank := 0
		for y := b.Min.Y; y < b.Max.Y; y++ {
			l.packLine(line, page, y)
			lead, trail := blanks(line)
			if lead == len(line) {
				blank++
				continue
			}
			e.feed(blank)
			blank = 0
			e.line(line, lead, trail)
		}
		e.feed(blank + opts.MarginLines + max(0, mo.MinLines-b.Dy()))
	}
	e.feed(mo.CutterLines)
	if mo.AutoCut {
		e.buf.Write([]byte{esc, cmdCut})
	}
	if !mo.Framed {
		return e.buf.Bytes(), nil
	}
	return frame(e.buf.Bytes()), nil
}

// frame splits a stream into the 500TS's blocks: ESC Y 1 and a big-endian
// length before each, and ESC Y 0 with a length of 0 after the last.
func frame(stream []byte) []byte {
	var buf bytes.Buffer
	for len(stream) > 0 {
		n := min(len(stream), frameBytes)
		buf.Write([]byte{esc, cmdFrame, 1})
		_ = binary.Write(&buf, binary.BigEndian, uint32(n))
		buf.Write(stream[:n])
		stream = stream[n:]
	}
	buf.Write([]byte{esc, cmdFrame, 0, 0, 0, 0, 0})
	return buf.Bytes()
}

// packLine fills line with one row of the page, on the head's dots from
// FirstDot, most significant bit first.
func (l Layout) packLine(line []byte, page *image.Gray, y int) {
	for i := range line {
		line[i] = 0
	}
	row := page.Pix[(y-page.Rect.Min.Y)*page.Stride:]
	for x := 0; x < l.PrintableDots; x++ {
		if row[x] < 0x80 {
			dot := l.FirstDot + x
			line[dot/8] |= 0x80 >> uint(dot%8)
		}
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

// Job is a command stream recovered by DecodeJob.
type Job struct {
	TapeType byte
	// Lines holds every line the tape is fed, head-wide: blank ones are
	// zero, and cut marks full of 0x99.
	Lines [][]byte
	// Cuts holds, for each cut, how many lines had been fed before it.
	Cuts []int
}

// DecodeJob parses a command stream produced by EncodeJob for the model. It
// exists for tests and diagnostics, such as a fake printer.
func (mo Model) DecodeJob(stream []byte) (Job, error) {
	if mo.Framed {
		var err error
		if stream, err = unframe(stream); err != nil {
			return Job{}, err
		}
	}
	hb := mo.headBytes()
	if len(stream) < hb || !bytes.Equal(stream[:hb], make([]byte, hb)) {
		return Job{}, fmt.Errorf("dymolm: stream does not start with %d NUL bytes", hb)
	}
	s := stream[hb:]
	if len(s) < 3 || s[0] != esc || s[1] != cmdTapeType || int(s[2]) >= len(tapeTypes) {
		return Job{}, fmt.Errorf("dymolm: missing tape type")
	}
	job := Job{TapeType: s[2]}
	s = s[3:]
	dotTab, bytesInLine := -1, -1
	for len(s) > 0 {
		switch {
		case s[0] == syn:
			if bytesInLine < 0 {
				return Job{}, fmt.Errorf("dymolm: line before bytes per line")
			}
			line := make([]byte, hb)
			if bytesInLine > 0 {
				if dotTab < 0 || dotTab+bytesInLine > hb {
					return Job{}, fmt.Errorf("dymolm: dot tab %d and %d bytes per line overrun the head", dotTab, bytesInLine)
				}
				if len(s) < 1+bytesInLine {
					return Job{}, fmt.Errorf("dymolm: stream truncated")
				}
				copy(line[dotTab:], s[1:1+bytesInLine])
			}
			job.Lines = append(job.Lines, line)
			s = s[1+bytesInLine:]
		case s[0] == esc && len(s) >= 2 && s[1] == cmdCut:
			job.Cuts = append(job.Cuts, len(job.Lines))
			s = s[2:]
		case s[0] == esc && len(s) >= 3 && s[1] == cmdDotTab:
			dotTab = int(s[2])
			s = s[3:]
		case s[0] == esc && len(s) >= 3 && s[1] == cmdBytes:
			bytesInLine = int(s[2])
			s = s[3:]
		default:
			return Job{}, fmt.Errorf("dymolm: unknown command at % X", s[:min(len(s), 3)])
		}
	}
	if mo.AutoCut && (len(job.Cuts) == 0 || job.Cuts[len(job.Cuts)-1] != len(job.Lines)) {
		return Job{}, fmt.Errorf("dymolm: the job does not end with a cut")
	}
	if !mo.AutoCut && len(job.Cuts) > 0 {
		return Job{}, fmt.Errorf("dymolm: the %s has no automatic cutter", mo.Name)
	}
	return job, nil
}

func unframe(stream []byte) ([]byte, error) {
	var out []byte
	for {
		if len(stream) < 7 || stream[0] != esc || stream[1] != cmdFrame {
			return nil, fmt.Errorf("dymolm: missing block header")
		}
		n := int(binary.BigEndian.Uint32(stream[3:7]))
		if stream[2] == 0 {
			if n != 0 || len(stream) != 7 {
				return nil, fmt.Errorf("dymolm: bytes after the last block")
			}
			return out, nil
		}
		if stream[2] != 1 || n == 0 || len(stream) < 7+n {
			return nil, fmt.Errorf("dymolm: damaged block")
		}
		out = append(out, stream[7:7+n]...)
		stream = stream[7+n:]
	}
}

// PageImage turns lines of a decoded job back into a PrintableDots-wide
// image, undoing packLine.
func (l Layout) PageImage(lines [][]byte) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, l.PrintableDots, len(lines)))
	for y, line := range lines {
		for x := 0; x < l.PrintableDots; x++ {
			dot := l.FirstDot + x
			if line[dot/8]&(0x80>>uint(dot%8)) == 0 {
				img.Pix[y*img.Stride+x] = 0xFF
			}
		}
	}
	return img
}
