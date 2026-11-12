package industrial

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/zebra"
)

// crlf ends every EZPL command.
const crlf = "\r\n"

// EZPLOptions are the per-printer choices that change an EZPL job.
type EZPLOptions struct {
	// Darkness is 0-19 (^H); nil keeps the printer's setting.
	Darkness *int
	// Speed is the print speed in inches per second, 2-7 (^S); nil keeps
	// the printer's setting.
	Speed *int
	// MediaType is "direct" for direct thermal labels or "transfer" for a
	// ribbon (^A); empty keeps the printer's setting.
	MediaType string
	// Sensor finds die-cut labels by their "gap" or by a black "mark",
	// whose width is the roll's gap (^Q); empty is gap.
	Sensor string
}

// EZPLMediaTypes are the media types, and the commands that choose them.
var EZPLMediaTypes = map[string]string{"direct": "^AD", "transfer": "^AT"}

// maxEZPLCopies is the most copies ^P asks for.
const maxEZPLCopies = 32767

// Check reports what is wrong with the options.
func (o EZPLOptions) Check() error {
	if o.Darkness != nil && (*o.Darkness < 0 || *o.Darkness > 19) {
		return fmt.Errorf("darkness must be 0-19")
	}
	if o.Speed != nil && (*o.Speed < 2 || *o.Speed > 7) {
		return fmt.Errorf("speed must be 2-7")
	}
	if _, ok := EZPLMediaTypes[o.MediaType]; o.MediaType != "" && !ok {
		return fmt.Errorf("media_type must be direct or transfer")
	}
	if o.Sensor != "" && o.Sensor != "gap" && o.Sensor != "mark" {
		return fmt.Errorf("media_sensor must be gap or mark")
	}
	return nil
}

// EncodeEZPL builds the EZPL that prints pages, each prepared for the
// model (see Prepare) and dark wherever a pixel is below 50% gray. A run
// of the same page is printed as copies of one label.
func EncodeEZPL(mo Model, m zebra.Media, pages []*image.Gray, o EZPLOptions) ([]byte, error) {
	if err := o.Check(); err != nil {
		return nil, fmt.Errorf("ezpl: %w", err)
	}
	if err := checkPages(mo, pages); err != nil {
		return nil, err
	}
	// EZPL gives sizes in whole millimeters.
	mm := func(dots int) int { return int(math.Round(float64(dots) / float64(mo.DPI) * 25.4)) }
	var buf bytes.Buffer
	err := runs(pages, maxEZPLCopies, func(page *image.Gray, copies int) error {
		width, length := labelSize(mo, m, page)
		// ^Q is the label's length, then the gap, the black mark's width
		// and where the label starts from it, or 0,0 for continuous media.
		switch {
		case !m.DieCut():
			fmt.Fprintf(&buf, "^Q%d,0,0"+crlf, mm(length))
		case o.Sensor == "mark":
			fmt.Fprintf(&buf, "^Q%d,%d,0+"+crlf, mm(length), max(1, int(math.Round(m.GapMM))))
		default:
			fmt.Fprintf(&buf, "^Q%d,%d"+crlf, mm(length), max(1, int(math.Round(m.GapMM))))
		}
		fmt.Fprintf(&buf, "^W%d"+crlf, mm(width))
		if o.Darkness != nil {
			fmt.Fprintf(&buf, "^H%02d"+crlf, *o.Darkness)
		}
		if o.Speed != nil {
			fmt.Fprintf(&buf, "^S%d"+crlf, *o.Speed)
		}
		if o.MediaType != "" {
			buf.WriteString(EZPLMediaTypes[o.MediaType] + crlf)
		}
		fmt.Fprintf(&buf, "^P%d"+crlf+"^L"+crlf, copies)
		// One pattern, centered across the label: its width in bytes, its
		// height in dots, then the rows from the top.
		widthBytes := (page.Rect.Dx() + 7) / 8
		fmt.Fprintf(&buf, "Q%d,0,%d,%d"+crlf, max(0, (width-page.Rect.Dx())/2), widthBytes, page.Rect.Dy())
		for _, row := range rows(page, 0, widthBytes, page.Rect.Dy()) {
			buf.Write(row)
		}
		buf.WriteString(crlf + "E" + crlf)
		return nil
	})
	return buf.Bytes(), err
}

// EZPLJob is an EZPL job as read back, for tests.
type EZPLJob struct {
	Labels []EZPLLabel
}

// EZPLLabel is one label: its setup commands besides ^P, and what it
// prints.
type EZPLLabel struct {
	Setup  []string
	Copies int
	// Image is the pattern as printed, black where a dot prints, from the
	// label's left edge.
	Image *image.Gray
}

// DecodeEZPL reads back a job EncodeEZPL made: each label's setup, and its
// image, from its pattern.
func DecodeEZPL(stream []byte) (*EZPLJob, error) {
	job := &EZPLJob{}
	s := stream
	label := &EZPLLabel{Copies: 1}
	formatting := false
	for len(s) > 0 {
		end := bytes.Index(s, []byte(crlf))
		if end < 0 {
			return nil, fmt.Errorf("ezpl: %.20q does not end in CR LF", s)
		}
		line := string(s[:end])
		s = s[end+2:]
		switch {
		case line == "^L":
			formatting = true
		case line == "E":
			if !formatting || label.Image == nil {
				return nil, fmt.Errorf("ezpl: E with no pattern")
			}
			job.Labels = append(job.Labels, *label)
			label, formatting = &EZPLLabel{Copies: 1}, false
		case formatting && strings.HasPrefix(line, "Q"):
			f := strings.Split(line[1:], ",")
			if len(f) != 4 {
				return nil, fmt.Errorf("ezpl: %q is not a pattern", line)
			}
			var v [4]int
			for i := range f {
				v[i], _ = strconv.Atoi(f[i])
			}
			n := v[2] * v[3]
			if len(s) < n+2 || string(s[n:n+2]) != crlf {
				return nil, fmt.Errorf("ezpl: the pattern is not %d bytes and CR LF", n)
			}
			all := make([][]byte, v[3])
			for r := range all {
				all[r] = s[r*v[2] : (r+1)*v[2]]
			}
			label.Image = blank(v[0]+v[2]*8, v[1]+v[3])
			draw(label.Image, v[0], v[1], all)
			s = s[n+2:]
		case formatting:
			return nil, fmt.Errorf("ezpl: %q in a label format", line)
		case strings.HasPrefix(line, "^P"):
			label.Copies, _ = strconv.Atoi(line[2:])
		default:
			label.Setup = append(label.Setup, line)
		}
	}
	if formatting {
		return nil, fmt.Errorf("ezpl: a label format has no E")
	}
	return job, nil
}
