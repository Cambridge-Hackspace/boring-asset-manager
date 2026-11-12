package industrial

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"image"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/zebra"
)

// DPL's control characters: STX begins a system-level command, and CR ends
// every command and record.
const (
	stx = "\x02"
	cr  = "\r"
)

// DPLOptions are the per-printer choices that change a DPL job.
type DPLOptions struct {
	// Heat is the darkness, 0-30 (H); nil keeps the printer's setting.
	Heat *int
	// Speed is the print speed in inches per second, 1-12 (P); nil keeps
	// the printer's setting.
	Speed *int
	// Sensor finds die-cut labels by their "gap" (<STX>e) or by a black
	// "mark" (<STX>r); empty is gap.
	Sensor string
	// Module is the memory module the label's image is loaded to, a letter;
	// empty is D, the printer's DRAM.
	Module string
}

// DPLSensors are the label sensors, and the commands that choose them.
var DPLSensors = map[string]string{"gap": "e", "mark": "r"}

// DPLImageName is the name the label's image is loaded under, each label
// replacing the last.
const DPLImageName = "BAM"

// maxDPLCopies is the most copies one format asks for: Q9999 prints
// without end on most models.
const maxDPLCopies = 9998

// Check reports what is wrong with the options.
func (o DPLOptions) Check() error {
	if o.Heat != nil && (*o.Heat < 0 || *o.Heat > 30) {
		return fmt.Errorf("heat must be 0-30")
	}
	if o.Speed != nil && (*o.Speed < 1 || *o.Speed > 12) {
		return fmt.Errorf("speed must be 1-12")
	}
	if _, ok := DPLSensors[o.Sensor]; o.Sensor != "" && !ok {
		return fmt.Errorf("media_sensor must be gap or mark")
	}
	if len(o.Module) > 1 || o.Module != "" && (o.Module[0] < 'A' || o.Module[0] > 'Z') {
		return fmt.Errorf("image_module must be one capital letter")
	}
	return nil
}

func (o DPLOptions) module() string {
	if o.Module == "" {
		return "D"
	}
	return o.Module
}

// dplSpeed is a speed in inches per second as DPL's letter: A is 1 and each
// letter after it half an inch faster, to W at 12.
func dplSpeed(ips int) byte { return 'A' + byte(2*(ips-1)) }

// EncodeDPL builds the DPL that prints pages, each prepared for the model
// (see Prepare) and dark wherever a pixel is below 50% gray. A run of the
// same page is printed as copies of one label.
//
// Each label is loaded as an image in Datamax's 7-bit ASCII format, then
// printed by a label format that places it. An image is printed from its
// first row up, so rows are sent from the bottom of the label, which DPL
// counts from.
func EncodeDPL(mo Model, m zebra.Media, pages []*image.Gray, o DPLOptions) ([]byte, error) {
	if err := o.Check(); err != nil {
		return nil, fmt.Errorf("dpl: %w", err)
	}
	if err := checkPages(mo, pages); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	err := runs(pages, maxDPLCopies, func(page *image.Gray, copies int) error {
		width, length := labelSize(mo, m, page)
		if m.DieCut() {
			// <STX>c0000 ends continuous media, so the sensor finds labels.
			sensor := DPLSensors["gap"]
			if o.Sensor != "" {
				sensor = DPLSensors[o.Sensor]
			}
			buf.WriteString(stx + "c0000" + cr + stx + sensor + cr)
		} else {
			// In inches, as the length is in hundredths of one.
			fmt.Fprintf(&buf, stx+"n"+cr+stx+"c%04d"+cr, int(float64(length)/float64(mo.DPI)*100+0.5))
		}

		// The image spans the label, the page centered across it and at its
		// top, so it is placed at the label's origin.
		widthBytes := (width + 7) / 8
		all := rows(page, max(0, (width-page.Rect.Dx())/2), widthBytes, max(length, page.Rect.Dy()))
		buf.WriteString(stx + "I" + o.module() + "AF" + DPLImageName + cr)
		for i := len(all) - 1; i >= 0; {
			fmt.Fprintf(&buf, "80%02X%s"+cr, widthBytes, strings.ToUpper(hex.EncodeToString(all[i])))
			// A repeat record copies the row before, up to 255 times.
			same := 0
			for i-same-1 >= 0 && same < 0xFF && bytes.Equal(all[i-same-1], all[i]) {
				same++
			}
			if same > 0 {
				fmt.Fprintf(&buf, "0000FF%02X"+cr, same)
			}
			i -= same + 1
		}
		buf.WriteString("FFFF" + cr)

		buf.WriteString(stx + "L" + cr + "D11" + cr)
		if o.Heat != nil {
			fmt.Fprintf(&buf, "H%02d"+cr, *o.Heat)
		}
		if o.Speed != nil {
			fmt.Fprintf(&buf, "P%c"+cr, dplSpeed(*o.Speed))
		}
		// Rotation 1, an image, at 1× and 1×, at row and column 0.
		buf.WriteString("1Y1100000000000" + DPLImageName + cr)
		fmt.Fprintf(&buf, "Q%04d"+cr+"E"+cr, copies)
		return nil
	})
	return buf.Bytes(), err
}

// DPLJob is a DPL job as read back, for tests.
type DPLJob struct {
	Labels []DPLLabel
}

// DPLLabel is one label: its system commands before it, the commands of its
// format besides placing the image, and what it prints.
type DPLLabel struct {
	System   []string
	Commands []string
	Copies   int
	// Image is the label as printed, black where a dot prints, its first
	// row the top of the label.
	Image *image.Gray
}

// DecodeDPL reads back a job EncodeDPL made: each label's image, from the
// image its format places.
func DecodeDPL(stream []byte) (*DPLJob, error) {
	job := &DPLJob{}
	lines := strings.Split(string(stream), cr)
	if lines[len(lines)-1] != "" {
		return nil, fmt.Errorf("dpl: the job does not end in a carriage return")
	}
	lines = lines[:len(lines)-1]
	var system []string
	images := map[string][][]byte{}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, stx+"I"):
			// <STX>I, the module, A for 7-bit ASCII, F for Datamax's format,
			// and the name.
			if len(line) < 5 || line[3:5] != "AF" {
				return nil, fmt.Errorf("dpl: %q is not a 7-bit image", line)
			}
			name := line[5:]
			var rowsUp [][]byte
			for i++; ; i++ {
				if i == len(lines) {
					return nil, fmt.Errorf("dpl: image %s has no FFFF", name)
				}
				rec := lines[i]
				if rec == "FFFF" {
					break
				}
				switch {
				case strings.HasPrefix(rec, "0000FF") && len(rec) == 8:
					n, err := strconv.ParseUint(rec[6:], 16, 8)
					if err != nil || len(rowsUp) == 0 {
						return nil, fmt.Errorf("dpl: a bad repeat record %q", rec)
					}
					for range n {
						rowsUp = append(rowsUp, rowsUp[len(rowsUp)-1])
					}
				case strings.HasPrefix(rec, "80") && len(rec) >= 4:
					n, err := strconv.ParseUint(rec[2:4], 16, 8)
					data, herr := hex.DecodeString(rec[4:])
					if err != nil || herr != nil || len(data) != int(n) {
						return nil, fmt.Errorf("dpl: a bad data record %.20q", rec)
					}
					rowsUp = append(rowsUp, data)
				default:
					return nil, fmt.Errorf("dpl: %.20q in an image", rec)
				}
			}
			images[name] = rowsUp
		case line == stx+"L":
			label := DPLLabel{System: system, Copies: 1}
			system = nil
			for i++; ; i++ {
				if i == len(lines) {
					return nil, fmt.Errorf("dpl: a label format has no E")
				}
				rec := lines[i]
				if rec == "E" {
					break
				}
				switch {
				case len(rec) > 15 && rec[1] == 'Y':
					rowsUp, ok := images[rec[15:]]
					if !ok || len(rowsUp) == 0 {
						return nil, fmt.Errorf("dpl: no image %q", rec[15:])
					}
					if rec[:15] != "1Y1100000000000" {
						return nil, fmt.Errorf("dpl: %q is not at the origin", rec)
					}
					img := blank(len(rowsUp[0])*8, len(rowsUp))
					top := make([][]byte, len(rowsUp))
					for r := range rowsUp {
						top[len(rowsUp)-1-r] = rowsUp[r]
					}
					draw(img, 0, 0, top)
					label.Image = img
				case strings.HasPrefix(rec, "Q"):
					label.Copies, _ = strconv.Atoi(rec[1:])
				default:
					label.Commands = append(label.Commands, rec)
				}
			}
			if label.Image == nil {
				return nil, fmt.Errorf("dpl: a label format places no image")
			}
			job.Labels = append(job.Labels, label)
		case strings.HasPrefix(line, stx):
			system = append(system, line[1:])
		default:
			return nil, fmt.Errorf("dpl: %.20q outside a format or image", line)
		}
	}
	if len(system) > 0 {
		return nil, fmt.Errorf("dpl: commands after the last label")
	}
	return job, nil
}
