package dymolw

import (
	"encoding/binary"
	"fmt"
	"image"
)

// Job is a command stream recovered by DecodeJob.
type Job struct {
	// Density and Roll are the command letters sent, such as 'e' and '0';
	// Roll is 0 when none was sent.
	Density, Roll byte
	// JobID is the version 2 job tag.
	JobID uint32
	// Sync is the length of the version 1 resynchronizing ESC run.
	Sync  int
	Pages []Page
}

// Page is one label of a decoded job.
type Page struct {
	// Length is the label length command's value.
	Length uint16
	// Index is the version 2 label index.
	Index uint16
	// Image holds the lines sent, white where no dot was set, as wide as the
	// widthDots passed to DecodeJob.
	Image *image.Gray
}

// DecodeJob parses a command stream produced by EncodeJob for a page
// widthDots wide. It exists for tests and diagnostics, such as a fake printer
// that checks what it received.
func DecodeJob(p Protocol, stream []byte, widthDots int) (Job, error) {
	d := decoder{s: stream, width: widthDots}
	switch p {
	case ProtocolV1:
		return d.v1()
	case ProtocolV2:
		return d.v2()
	}
	return Job{}, fmt.Errorf("dymolw: unknown protocol %d", p)
}

type decoder struct {
	s     []byte
	width int
	job   Job
	rows  [][]byte
}

func (d *decoder) take(n int) ([]byte, error) {
	if len(d.s) < n {
		return nil, fmt.Errorf("dymolw: stream truncated")
	}
	out := d.s[:n]
	d.s = d.s[n:]
	return out, nil
}

// endPage turns the rows collected since the last page break into an image.
func (d *decoder) endPage() error {
	if len(d.job.Pages) == 0 {
		return fmt.Errorf("dymolw: form feed before any page")
	}
	img := image.NewGray(image.Rect(0, 0, d.width, len(d.rows)))
	for y, row := range d.rows {
		for x := 0; x < d.width; x++ {
			if x/8 >= len(row) || row[x/8]&(0x80>>uint(x%8)) == 0 {
				img.Pix[y*img.Stride+x] = 0xFF
			}
		}
	}
	d.job.Pages[len(d.job.Pages)-1].Image = img
	d.rows = nil
	return nil
}

func (d *decoder) v1() (Job, error) {
	// The resynchronizing run is every ESC but the one that starts the
	// first command.
	run := 0
	for run < len(d.s) && d.s[run] == esc {
		run++
	}
	if run-1 < minSync {
		return Job{}, fmt.Errorf("dymolw: stream starts with %d ESC bytes, not at least %d", max(run-1, 0), minSync)
	}
	d.job.Sync = run - 1
	d.s = d.s[run-1:]

	// A label starts once the label length is set and ends at a short form
	// feed (another follows) or the final form feed.
	dotTab, bytesPerLine := 0, -1
	inPage := false
	var length uint16
	line := func(data []byte) {
		row := make([]byte, dotTab+len(data))
		copy(row[dotTab:], data)
		d.rows = append(d.rows, row)
	}
	for len(d.s) > 0 {
		switch d.s[0] {
		case syn:
			d.s = d.s[1:]
			if bytesPerLine < 0 {
				return Job{}, fmt.Errorf("dymolw: raster line before bytes per line was set")
			}
			data, err := d.take(bytesPerLine)
			if err != nil {
				return Job{}, err
			}
			if !inPage {
				return Job{}, fmt.Errorf("dymolw: raster line outside a page")
			}
			line(data)
			continue
		case etb:
			d.s = d.s[1:]
			if bytesPerLine < 0 {
				return Job{}, fmt.Errorf("dymolw: raster line before bytes per line was set")
			}
			var bits []bool
			for len(bits) < bytesPerLine*8 {
				code, err := d.take(1)
				if err != nil {
					return Job{}, err
				}
				for k := 0; k <= int(code[0]&0x7F); k++ {
					bits = append(bits, code[0]&0x80 != 0)
				}
			}
			if len(bits) != bytesPerLine*8 || !inPage {
				return Job{}, fmt.Errorf("dymolw: compressed line of %d dots", len(bits))
			}
			data := make([]byte, bytesPerLine)
			for i, b := range bits {
				if b {
					data[i/8] |= 0x80 >> uint(i%8)
				}
			}
			line(data)
			continue
		case esc:
		default:
			return Job{}, fmt.Errorf("dymolw: unknown command byte 0x%02X", d.s[0])
		}
		if len(d.s) < 2 {
			return Job{}, fmt.Errorf("dymolw: stream truncated")
		}
		cmd := d.s[1]
		d.s = d.s[2:]
		switch cmd {
		case 'B', 'D':
			arg, err := d.take(1)
			if err != nil {
				return Job{}, err
			}
			if cmd == 'B' {
				dotTab = int(arg[0])
			} else {
				bytesPerLine = int(arg[0])
			}
		case 'h':
		case 'c', 'd', 'e', 'g':
			d.job.Density = cmd
		case 'q':
			arg, err := d.take(1)
			if err != nil {
				return Job{}, err
			}
			d.job.Roll = arg[0]
		case 'L':
			arg, err := d.take(2)
			if err != nil {
				return Job{}, err
			}
			if inPage {
				return Job{}, fmt.Errorf("dymolw: label length set twice")
			}
			length = binary.BigEndian.Uint16(arg)
			d.job.Pages = append(d.job.Pages, Page{Length: length})
			inPage = true
		case 'f':
			arg, err := d.take(2)
			if err != nil {
				return Job{}, err
			}
			if arg[0] != 1 || !inPage {
				return Job{}, fmt.Errorf("dymolw: bad line skip")
			}
			for k := 0; k < int(arg[1]); k++ {
				d.rows = append(d.rows, nil)
			}
		case 'G':
			if !inPage {
				return Job{}, fmt.Errorf("dymolw: short form feed outside a page")
			}
			if err := d.endPage(); err != nil {
				return Job{}, err
			}
			d.job.Pages = append(d.job.Pages, Page{Length: length})
		case 'E':
			if !inPage || len(d.s) > 0 {
				return Job{}, fmt.Errorf("dymolw: form feed must end the job")
			}
			if err := d.endPage(); err != nil {
				return Job{}, err
			}
			return d.job, nil
		default:
			return Job{}, fmt.Errorf("dymolw: unknown command ESC %q", cmd)
		}
	}
	return Job{}, fmt.Errorf("dymolw: stream does not end with a form feed")
}

func (d *decoder) v2() (Job, error) {
	inPage := false
	ended := false
	for len(d.s) > 0 {
		if d.s[0] != esc || len(d.s) < 2 {
			return Job{}, fmt.Errorf("dymolw: unknown command byte 0x%02X", d.s[0])
		}
		cmd := d.s[1]
		d.s = d.s[2:]
		if ended && cmd != 'Q' {
			return Job{}, fmt.Errorf("dymolw: command after the final form feed")
		}
		switch cmd {
		case 's':
			arg, err := d.take(4)
			if err != nil {
				return Job{}, err
			}
			d.job.JobID = binary.LittleEndian.Uint32(arg)
		case 'c', 'd', 'e', 'g':
			d.job.Density = cmd
		case 'h':
		case 'M':
			if _, err := d.take(8); err != nil {
				return Job{}, err
			}
		case 'L':
			arg, err := d.take(2)
			if err != nil {
				return Job{}, err
			}
			if inPage {
				return Job{}, fmt.Errorf("dymolw: label length inside a page")
			}
			d.job.Pages = append(d.job.Pages, Page{Length: binary.LittleEndian.Uint16(arg)})
			inPage = true
		case 'n':
			arg, err := d.take(2)
			if err != nil {
				return Job{}, err
			}
			if !inPage {
				return Job{}, fmt.Errorf("dymolw: label index outside a page")
			}
			d.job.Pages[len(d.job.Pages)-1].Index = binary.LittleEndian.Uint16(arg)
		case 'D':
			hdr, err := d.take(10)
			if err != nil {
				return Job{}, err
			}
			if hdr[0] != 1 || hdr[1] != 2 || !inPage {
				return Job{}, fmt.Errorf("dymolw: unexpected print data header % X", hdr)
			}
			lines := int(binary.LittleEndian.Uint32(hdr[2:6]))
			dots := int(binary.LittleEndian.Uint32(hdr[6:10]))
			if dots%8 != 0 {
				return Job{}, fmt.Errorf("dymolw: line of %d dots is not whole bytes", dots)
			}
			for k := 0; k < lines; k++ {
				row, err := d.take(dots / 8)
				if err != nil {
					return Job{}, err
				}
				d.rows = append(d.rows, row)
			}
		case 'G':
			if !inPage {
				return Job{}, fmt.Errorf("dymolw: short form feed outside a page")
			}
			if err := d.endPage(); err != nil {
				return Job{}, err
			}
			inPage = false
		case 'E':
			if inPage || len(d.job.Pages) == 0 {
				return Job{}, fmt.Errorf("dymolw: form feed before the last page ended")
			}
			ended = true
		case 'Q':
			if !ended || len(d.s) > 0 {
				return Job{}, fmt.Errorf("dymolw: end of job must follow the final form feed and end the stream")
			}
			return d.job, nil
		default:
			return Job{}, fmt.Errorf("dymolw: unknown command ESC %q", cmd)
		}
	}
	return Job{}, fmt.Errorf("dymolw: stream does not end with an end-of-job command")
}
