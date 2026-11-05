package zebra

import (
	"bytes"
	"fmt"
	"image"
	"strconv"
	"strings"
)

// EPL2Options are the per-printer choices that change an EPL2 job.
type EPL2Options struct {
	// Density is the darkness, 0-15 (D); nil keeps the printer's setting.
	Density *int
}

// Check reports what is wrong with the options.
func (o EPL2Options) Check() error {
	if o.Density != nil && (*o.Density < 0 || *o.Density > 15) {
		return fmt.Errorf("density must be 0-15")
	}
	return nil
}

// EncodeEPL2 builds the EPL2 that prints pages, each prepared for the model
// (see Prepare) and dark wherever a pixel is below 50% gray. A run of the
// same page is printed as copies of one label.
func EncodeEPL2(mo Model, m Media, pages []*image.Gray, o EPL2Options) ([]byte, error) {
	if err := o.Check(); err != nil {
		return nil, fmt.Errorf("epl2: %w", err)
	}
	if err := checkPages(mo, pages); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	// A line feed first ends anything the printer was part way through.
	buf.WriteString("\n")
	err := runs(pages, func(page *image.Gray, copies int) error {
		width, length := labelSize(mo, m, page)
		gap := 0
		if m.DieCut() {
			gap = mo.Dots(m.GapMM)
		}
		// N clears the image buffer; q and Q are the label's width, and its
		// length and gap, which 0 makes continuous; ZT prints from the top.
		fmt.Fprintf(&buf, "N\nq%d\nQ%d,%d\nZT\n", width, length, gap)
		if o.Density != nil {
			fmt.Fprintf(&buf, "D%d\n", *o.Density)
		}
		// GW's data clears a bit to print a dot. Centered across the label.
		widthBytes, all := rows(page)
		fmt.Fprintf(&buf, "GW%d,0,%d,%d,", max(0, (width-page.Rect.Dx())/2), widthBytes, len(all))
		for _, row := range all {
			for _, b := range row {
				buf.WriteByte(^b)
			}
		}
		fmt.Fprintf(&buf, "\nP%d\n", copies)
		return nil
	})
	return buf.Bytes(), err
}

// EPL2Job is an EPL2 job as read back, for tests.
type EPL2Job struct {
	Labels []EPL2Label
}

// EPL2Label is one label: its commands, besides its graphic, and what it
// prints.
type EPL2Label struct {
	Commands []string
	Width    int
	Length   int
	Gap      int
	Copies   int
	// Image is the label as printed, black where a dot prints.
	Image *image.Gray
}

// DecodeEPL2 reads back a job EncodeEPL2 made: each label's image, from
// its graphic.
func DecodeEPL2(stream []byte) (*EPL2Job, error) {
	job := &EPL2Job{}
	var label *EPL2Label
	s := stream
	for len(s) > 0 {
		nl := bytes.IndexByte(s, '\n')
		if nl < 0 {
			return nil, fmt.Errorf("epl2: %.20q does not end in a line feed", s)
		}
		line := string(s[:nl])
		switch {
		case line == "":
			s = s[nl+1:]
		case line == "N":
			label = &EPL2Label{Copies: 1}
			s = s[nl+1:]
		case label == nil:
			return nil, fmt.Errorf("epl2: %q before N", line)
		case strings.HasPrefix(line, "GW"):
			// GW's data is binary, and may hold line feeds: read it by its
			// size.
			f := strings.SplitN(string(s[2:min(len(s), 64)]), ",", 5)
			if len(f) < 5 {
				return nil, fmt.Errorf("epl2: a short GW")
			}
			x, _ := strconv.Atoi(f[0])
			widthBytes, _ := strconv.Atoi(f[2])
			height, _ := strconv.Atoi(f[3])
			head := 2 + len(f[0]) + len(f[1]) + len(f[2]) + len(f[3]) + 4
			n := widthBytes * height
			if len(s) < head+n+1 || s[head+n] != '\n' {
				return nil, fmt.Errorf("epl2: GW's data is not %d bytes and a line feed", n)
			}
			data := s[head : head+n]
			img := image.NewGray(image.Rect(0, 0, label.Width, max(label.Length, height)))
			for i := range img.Pix {
				img.Pix[i] = 0xFF
			}
			for k, b := range data {
				for bit := range 8 {
					px, py := x+(k%widthBytes)*8+bit, k/widthBytes
					if b&(0x80>>bit) == 0 && px < img.Rect.Dx() {
						img.Pix[py*img.Stride+px] = 0
					}
				}
			}
			label.Image = img
			s = s[head+n+1:]
		case strings.HasPrefix(line, "q"):
			label.Width, _ = strconv.Atoi(line[1:])
			s = s[nl+1:]
		case strings.HasPrefix(line, "Q"):
			if _, err := fmt.Sscanf(line[1:], "%d,%d", &label.Length, &label.Gap); err != nil {
				return nil, fmt.Errorf("epl2: %s: %w", line, err)
			}
			s = s[nl+1:]
		case strings.HasPrefix(line, "P"):
			label.Copies, _ = strconv.Atoi(line[1:])
			job.Labels = append(job.Labels, *label)
			label = nil
			s = s[nl+1:]
		default:
			label.Commands = append(label.Commands, line)
			s = s[nl+1:]
		}
	}
	if label != nil {
		return nil, fmt.Errorf("epl2: a label has no P")
	}
	return job, nil
}
