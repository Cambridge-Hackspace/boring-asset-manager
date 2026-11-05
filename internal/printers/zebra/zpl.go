package zebra

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"image"
	"strconv"
	"strings"
)

// ZPLOptions are the per-printer choices that change a ZPL job. Each left
// unset keeps the printer's own setting.
type ZPLOptions struct {
	// MediaType is "direct" for direct thermal labels or "transfer" for a
	// ribbon (^MT).
	MediaType string
	// Darkness is 0-30 (~SD).
	Darkness *int
	// Speed is the print speed in inches per second, 1-14 (^PR).
	Speed *int
	// PrintMode is "tear_off", "peel_off" or "cutter" (^MM).
	PrintMode string
	// Calibrate measures the labels before printing (~JC).
	Calibrate bool
	// Uncompressed sends graphics as plain hexadecimal, for an emulator
	// that mishandles Zebra's compression.
	Uncompressed bool
}

// MediaTypes, PrintModes, and the commands they send.
var (
	MediaTypes = map[string]string{"direct": "^MTD", "transfer": "^MTT"}
	PrintModes = map[string]string{"tear_off": "^MMT", "peel_off": "^MMP", "cutter": "^MMC"}
)

// MaxGraphicBytes is the most one ^GF takes: a label larger is sent in
// bands, one under the other.
const MaxGraphicBytes = 99999

// Check reports what is wrong with the options.
func (o ZPLOptions) Check() error {
	if _, ok := MediaTypes[o.MediaType]; o.MediaType != "" && !ok {
		return fmt.Errorf("media_type must be direct or transfer")
	}
	if _, ok := PrintModes[o.PrintMode]; o.PrintMode != "" && !ok {
		return fmt.Errorf("print_mode must be tear_off, peel_off or cutter")
	}
	if o.Darkness != nil && (*o.Darkness < 0 || *o.Darkness > 30) {
		return fmt.Errorf("darkness must be 0-30")
	}
	if o.Speed != nil && (*o.Speed < 1 || *o.Speed > 14) {
		return fmt.Errorf("speed must be 1-14")
	}
	return nil
}

// EncodeZPL builds the ZPL that prints pages, each prepared for the model
// (see Prepare) and dark wherever a pixel is below 50% gray. A run of the
// same page is printed as copies of one label.
func EncodeZPL(mo Model, m Media, pages []*image.Gray, o ZPLOptions) ([]byte, error) {
	if err := o.Check(); err != nil {
		return nil, fmt.Errorf("zpl: %w", err)
	}
	if err := checkPages(mo, pages); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	// Commands that act at once, before any label.
	if o.Calibrate {
		buf.WriteString("~JC\n")
	}
	if o.Darkness != nil {
		fmt.Fprintf(&buf, "~SD%02d\n", *o.Darkness)
	}
	err := runs(pages, func(page *image.Gray, copies int) error {
		width, length := labelSize(mo, m, page)
		buf.WriteString("^XA\n")
		// Die-cut labels are found by their gaps; continuous media is cut
		// to the label's length.
		if m.DieCut() {
			buf.WriteString("^MNY\n")
		} else {
			buf.WriteString("^MNN\n")
		}
		if o.MediaType != "" {
			buf.WriteString(MediaTypes[o.MediaType] + "\n")
		}
		if o.PrintMode != "" {
			buf.WriteString(PrintModes[o.PrintMode] + "\n")
		}
		if o.Speed != nil {
			fmt.Fprintf(&buf, "^PR%d\n", *o.Speed)
		}
		fmt.Fprintf(&buf, "^PW%d\n^LL%d\n^LH0,0\n", width, length)

		// Centered across the label, in bands of at most MaxGraphicBytes.
		x := max(0, (width-page.Rect.Dx())/2)
		widthBytes, all := rows(page)
		perBand := max(1, MaxGraphicBytes/widthBytes)
		for y := 0; y < len(all); y += perBand {
			band := all[y:min(y+perBand, len(all))]
			total := widthBytes * len(band)
			fmt.Fprintf(&buf, "^FO%d,%d^GFA,%d,%d,%d,", x, y, total, total, widthBytes)
			if o.Uncompressed {
				for _, row := range band {
					buf.WriteString(strings.ToUpper(hex.EncodeToString(row)))
				}
			} else {
				buf.WriteString(compressZPL(band))
			}
			buf.WriteString("^FS\n")
		}
		fmt.Fprintf(&buf, "^PQ%d\n^XZ\n", copies)
		return nil
	})
	return buf.Bytes(), err
}

// compressZPL writes rows in the ASCII compression of ZPL's ^GF: a run of
// a hexadecimal digit is a count then the digit, a comma fills the rest of
// a row with 0 and an exclamation mark with F, and a colon repeats the row
// before.
func compressZPL(rows [][]byte) string {
	var out strings.Builder
	prev := ""
	for i, row := range rows {
		h := strings.ToUpper(hex.EncodeToString(row))
		if i > 0 && h == prev {
			out.WriteByte(':')
			continue
		}
		prev = h
		body, fill := h, byte(0)
		if t := strings.TrimRight(h, "0"); len(h)-len(t) >= 2 {
			body, fill = t, ','
		} else if t := strings.TrimRight(h, "F"); len(h)-len(t) >= 2 {
			body, fill = t, '!'
		}
		for j := 0; j < len(body); {
			n := 1
			for j+n < len(body) && body[j+n] == body[j] {
				n++
			}
			if n >= 3 {
				writeRun(&out, n, body[j])
			} else {
				out.WriteString(body[j : j+n])
			}
			j += n
		}
		if fill != 0 {
			out.WriteByte(fill)
		}
	}
	return out.String()
}

// writeRun writes n of a digit as counts: G-Y are 1-19 and g-z are 20-400
// by twenties, so one count reaches 419.
func writeRun(out *strings.Builder, n int, digit byte) {
	for n > 0 {
		k := min(n, 419)
		if hi := k / 20; hi > 0 {
			out.WriteByte('g' + byte(hi-1))
		}
		if lo := k % 20; lo > 0 {
			out.WriteByte('G' + byte(lo-1))
		}
		out.WriteByte(digit)
		n -= k
	}
}

// decompressZPL reads ^GF's ASCII data, compressed or not, into rows of
// widthBytes, total bytes in all.
func decompressZPL(data string, total, widthBytes int) ([]byte, error) {
	if widthBytes <= 0 || total%widthBytes != 0 {
		return nil, fmt.Errorf("zpl: %d bytes is not rows of %d", total, widthBytes)
	}
	rowHex := 2 * widthBytes
	var out []byte
	row := make([]byte, 0, rowHex)
	var prev []byte
	flush := func() error {
		b := make([]byte, widthBytes)
		if _, err := hex.Decode(b, row); err != nil {
			return fmt.Errorf("zpl: %w", err)
		}
		out = append(out, b...)
		prev, row = append([]byte(nil), row...), row[:0]
		return nil
	}
	count := 0
	for i := 0; i < len(data); i++ {
		c := data[i]
		switch {
		case c >= 'G' && c <= 'Y':
			count += int(c-'G') + 1
		case c >= 'g' && c <= 'z':
			count += (int(c-'g') + 1) * 20
		case c == ',' || c == '!':
			fill := byte('0')
			if c == '!' {
				fill = 'F'
			}
			for len(row) < rowHex {
				row = append(row, fill)
			}
			if err := flush(); err != nil {
				return nil, err
			}
		case c == ':':
			if prev == nil || len(row) != 0 {
				return nil, fmt.Errorf("zpl: a colon not at the start of a row after another")
			}
			row = append(row, prev...)
			if err := flush(); err != nil {
				return nil, err
			}
		case strings.IndexByte("0123456789ABCDEFabcdef", c) >= 0:
			for range max(count, 1) {
				row = append(row, c)
				if len(row) == rowHex {
					if err := flush(); err != nil {
						return nil, err
					}
				}
			}
			count = 0
		case c == '\r' || c == '\n' || c == ' ':
		default:
			return nil, fmt.Errorf("zpl: %q in graphic data", c)
		}
	}
	if len(row) != 0 || len(out) != total {
		return nil, fmt.Errorf("zpl: graphic data gave %d bytes, not %d", len(out), total)
	}
	return out, nil
}

// ZPLJob is a ZPL job as read back, for tests.
type ZPLJob struct {
	// Setup are the commands outside labels, such as ~SD15.
	Setup  []string
	Labels []ZPLLabel
}

// ZPLLabel is one label format: its commands, besides its graphics, and
// what it prints.
type ZPLLabel struct {
	Commands []string
	Width    int
	Length   int
	Copies   int
	// Image is the label as printed, black where a dot prints.
	Image *image.Gray
}

// DecodeZPL reads back a job EncodeZPL made: its setup, and each label's
// image, from its graphics.
func DecodeZPL(stream []byte) (*ZPLJob, error) {
	job := &ZPLJob{}
	s := string(stream)
	var label *ZPLLabel
	type graphic struct {
		x, y, widthBytes int
		data             []byte
	}
	var graphics []graphic
	for len(s) > 0 {
		i := strings.IndexAny(s, "^~")
		if i < 0 {
			break
		}
		s = s[i:]
		end := strings.IndexAny(s[1:], "^~")
		cmd := s
		if end >= 0 {
			cmd = s[:end+1]
		}
		s = s[len(cmd):]
		cmd = strings.TrimSpace(cmd)
		name, args := cmd[:min(3, len(cmd))], cmd[min(3, len(cmd)):]
		switch {
		case name == "^XA":
			label, graphics = &ZPLLabel{Copies: 1}, nil
		case label == nil:
			job.Setup = append(job.Setup, cmd)
		case name == "^XZ":
			img := image.NewGray(image.Rect(0, 0, label.Width, label.Length))
			for i := range img.Pix {
				img.Pix[i] = 0xFF
			}
			for _, g := range graphics {
				for k, b := range g.data {
					for bit := range 8 {
						x, y := g.x+(k%g.widthBytes)*8+bit, g.y+k/g.widthBytes
						if b&(0x80>>bit) != 0 && x < label.Width && y < label.Length {
							img.Pix[y*img.Stride+x] = 0
						}
					}
				}
			}
			label.Image = img
			job.Labels = append(job.Labels, *label)
			label = nil
		case name == "^PW":
			label.Width, _ = strconv.Atoi(args)
		case name == "^LL":
			label.Length, _ = strconv.Atoi(args)
		case name == "^PQ":
			label.Copies, _ = strconv.Atoi(args)
		case name == "^FO":
			var x, y int
			if _, err := fmt.Sscanf(args, "%d,%d", &x, &y); err != nil {
				return nil, fmt.Errorf("zpl: %s: %w", cmd, err)
			}
			graphics = append(graphics, graphic{x: x, y: y})
		case name == "^GF":
			f := strings.SplitN(args, ",", 5)
			if len(f) != 5 || f[0] != "A" || len(graphics) == 0 {
				return nil, fmt.Errorf("zpl: %.40s is not an ASCII graphic after ^FO", cmd)
			}
			total, _ := strconv.Atoi(f[2])
			widthBytes, _ := strconv.Atoi(f[3])
			data, err := decompressZPL(f[4], total, widthBytes)
			if err != nil {
				return nil, err
			}
			g := &graphics[len(graphics)-1]
			g.widthBytes, g.data = widthBytes, data
		case name == "^FS", name == "^LH":
		default:
			label.Commands = append(label.Commands, cmd)
		}
	}
	if label != nil {
		return nil, fmt.Errorf("zpl: a label has no ^XZ")
	}
	return job, nil
}
