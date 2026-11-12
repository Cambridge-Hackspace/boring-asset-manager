package industrial

import (
	"bytes"
	"fmt"
	"image"
	"strconv"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/zebra"
)

// esc begins every SBPL command.
const esc = "\x1b"

// SBPLOptions are the per-printer choices that change an SBPL job.
type SBPLOptions struct {
	// Darkness is 1-10 (#F); nil keeps the printer's setting.
	Darkness *int
	// Speed is the print speed in inches per second, 2-10 (CS); nil keeps
	// the printer's setting.
	Speed *int
	// Sensor is the label sensor (IG): "gap", "mark", or "none" for
	// continuous media; empty keeps the printer's setting.
	Sensor string
	// Framed wraps the job in STX and ETX.
	Framed bool
}

// SBPLSensors are the label sensors, and IG's values for them.
var SBPLSensors = map[string]int{"mark": 0, "gap": 1, "none": 2}

// SBPL limits: the copies of one label (Q); the rows of one graphic, which
// are sent in bands of whole 8-dot blocks (SATO's heads take from 400 to
// 999 blocks, so a band of 100 suits every one); and the length A1 gives in
// four digits, beyond which it takes five.
const (
	maxSBPLCopies  = 999999
	sbplBandBlocks = 100
	sbplShortA1    = 9999
)

// MaxSBPLLengthDots is the longest label SBPL prints at each resolution
// (A1 and V).
var MaxSBPLLengthDots = map[int]int{203: 20000, 300: 18000, 600: 9600}

// Check reports what is wrong with the options.
func (o SBPLOptions) Check() error {
	if o.Darkness != nil && (*o.Darkness < 1 || *o.Darkness > 10) {
		return fmt.Errorf("darkness must be 1-10")
	}
	if o.Speed != nil && (*o.Speed < 2 || *o.Speed > 10) {
		return fmt.Errorf("speed must be 2-10")
	}
	if _, ok := SBPLSensors[o.Sensor]; o.Sensor != "" && !ok {
		return fmt.Errorf("media_sensor must be gap, mark or none")
	}
	return nil
}

// EncodeSBPL builds the SBPL that prints pages, each prepared for the
// model (see Prepare) and dark wherever a pixel is below 50% gray. A run
// of the same page is printed as copies of one label.
func EncodeSBPL(mo Model, m zebra.Media, pages []*image.Gray, o SBPLOptions) ([]byte, error) {
	if err := o.Check(); err != nil {
		return nil, fmt.Errorf("sbpl: %w", err)
	}
	if err := checkPages(mo, pages); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if o.Framed {
		buf.WriteString("\x02")
	}
	// The printer keeps these settings, so they go in a block of their own
	// before any label.
	if o.Sensor != "" || o.Speed != nil || o.Darkness != nil {
		buf.WriteString(esc + "A")
		if o.Sensor != "" {
			fmt.Fprintf(&buf, esc+"IG%d", SBPLSensors[o.Sensor])
		}
		if o.Speed != nil {
			fmt.Fprintf(&buf, esc+"CS%d", *o.Speed)
		}
		if o.Darkness != nil {
			fmt.Fprintf(&buf, esc+"#F%d", *o.Darkness)
		}
		buf.WriteString(esc + "Z")
	}
	err := runs(pages, maxSBPLCopies, func(page *image.Gray, copies int) error {
		width, length := labelSize(mo, m, page)
		if most := MaxSBPLLengthDots[mo.DPI]; length > most {
			return fmt.Errorf("sbpl: a label %d dots long is longer than the %s prints (%d)", length, mo.Name, most)
		}
		buf.WriteString(esc + "A")
		// The label's length and width in dots, in the longer form when
		// the length takes five digits.
		if length > sbplShortA1 {
			fmt.Fprintf(&buf, esc+"A1V%05dH%04d", length, width)
		} else {
			fmt.Fprintf(&buf, esc+"A1%04d%04d", length, width)
		}
		// Centered across the label, in bands of whole blocks of 8 rows;
		// positions count from 1. The last band is filled out with white.
		x := max(0, (width-page.Rect.Dx())/2)
		widthBytes := (page.Rect.Dx() + 7) / 8
		blocks := (page.Rect.Dy() + 7) / 8
		all := rows(page, 0, widthBytes, blocks*8)
		for b := 0; b < blocks; b += sbplBandBlocks {
			n := min(sbplBandBlocks, blocks-b)
			fmt.Fprintf(&buf, esc+"V%d"+esc+"H%d"+esc+"GB%03d%03d", b*8+1, x+1, widthBytes, n)
			for _, row := range all[b*8 : (b+n)*8] {
				buf.Write(row)
			}
		}
		fmt.Fprintf(&buf, esc+"Q%d"+esc+"Z", copies)
		return nil
	})
	if o.Framed {
		buf.WriteString("\x03")
	}
	return buf.Bytes(), err
}

// SBPLJob is an SBPL job as read back, for tests.
type SBPLJob struct {
	// Framed is set when the job is wrapped in STX and ETX.
	Framed bool
	// Setup are the commands of blocks that print nothing, such as IG1.
	Setup  []string
	Labels []SBPLLabel
}

// SBPLLabel is one label: its size, and what it prints.
type SBPLLabel struct {
	Width, Length int
	Copies        int
	// Graphics counts the graphics the label is sent in.
	Graphics int
	// Image is the label as printed, black where a dot prints.
	Image *image.Gray
}

// DecodeSBPL reads back a job EncodeSBPL made: its settings, and each
// label's image, from its graphics.
func DecodeSBPL(stream []byte) (*SBPLJob, error) {
	job := &SBPLJob{}
	s := stream
	if len(s) > 1 && s[0] == 0x02 && s[len(s)-1] == 0x03 {
		job.Framed, s = true, s[1:len(s)-1]
	}
	// num reads the digits at the start of s.
	num := func(n int) (int, error) {
		if n > len(s) {
			return 0, fmt.Errorf("sbpl: a short number")
		}
		v, err := strconv.Atoi(string(s[:n]))
		s = s[n:]
		return v, err
	}
	digits := func() int {
		n := 0
		for n < len(s) && s[n] >= '0' && s[n] <= '9' {
			n++
		}
		return n
	}
	var label *SBPLLabel
	var setup []string
	x, y := 1, 1
	for len(s) > 0 {
		if s[0] != 0x1b {
			return nil, fmt.Errorf("sbpl: %.10q is not a command", s)
		}
		s = s[1:]
		var err error
		switch {
		case bytes.HasPrefix(s, []byte("A1V")):
			s = s[3:]
			label.Length, err = num(5)
			if err == nil && len(s) > 0 && s[0] == 'H' {
				s = s[1:]
				label.Width, err = num(4)
			}
		case bytes.HasPrefix(s, []byte("A1")):
			s = s[2:]
			if label.Length, err = num(4); err == nil {
				label.Width, err = num(4)
			}
		case s[0] == 'A':
			s = s[1:]
			label, setup = &SBPLLabel{Copies: 1}, nil
		case s[0] == 'Z':
			s = s[1:]
			if label == nil {
				return nil, fmt.Errorf("sbpl: Z before A")
			}
			if label.Graphics == 0 {
				job.Setup = append(job.Setup, setup...)
			} else {
				job.Labels = append(job.Labels, *label)
			}
			label = nil
		case label == nil:
			return nil, fmt.Errorf("sbpl: %.10q outside A and Z", s)
		case s[0] == 'V':
			s = s[1:]
			y, err = num(digits())
		case s[0] == 'H':
			s = s[1:]
			x, err = num(digits())
		case s[0] == 'Q':
			s = s[1:]
			label.Copies, err = num(digits())
		case bytes.HasPrefix(s, []byte("GB")):
			s = s[2:]
			var widthBytes, blocks int
			if widthBytes, err = num(3); err == nil {
				blocks, err = num(3)
			}
			n := widthBytes * blocks * 8
			if err != nil || len(s) < n {
				return nil, fmt.Errorf("sbpl: a short graphic")
			}
			if label.Image == nil {
				label.Image = blank(label.Width, label.Length)
			}
			band := make([][]byte, blocks*8)
			for r := range band {
				band[r] = s[r*widthBytes : (r+1)*widthBytes]
			}
			draw(label.Image, x-1, y-1, band)
			label.Graphics++
			s = s[n:]
		default:
			// A setting: letters, then its digits.
			n := 0
			for n < len(s) && s[n] != 0x1b {
				n++
			}
			setup = append(setup, string(s[:n]))
			s = s[n:]
		}
		if err != nil {
			return nil, fmt.Errorf("sbpl: %w", err)
		}
	}
	if label != nil {
		return nil, fmt.Errorf("sbpl: a block has no Z")
	}
	return job, nil
}
