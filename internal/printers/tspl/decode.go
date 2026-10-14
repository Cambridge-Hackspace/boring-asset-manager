package tspl

import (
	"bytes"
	"fmt"
	"image"
	"strconv"
	"strings"
)

// Job is a TSPL program recovered by DecodeJob.
type Job struct {
	// Setup holds the setup commands in order, such as "SIZE 101.6 mm,152.4 mm".
	Setup  []string
	Labels []Label
}

// Label is one bitmap and how many copies of it were printed.
type Label struct {
	X      int
	Image  *image.Gray
	Copies int
}

// DecodeJob parses a program produced by EncodeJob. It exists for tests and
// diagnostics, such as a fake printer that checks what it received.
func DecodeJob(program []byte) (Job, error) {
	var job Job
	s := program
	var pending *Label
	for len(s) > 0 {
		if bytes.HasPrefix(s, []byte("BITMAP ")) {
			// BITMAP x,y,width,height,mode, then width × height bytes.
			header := s[len("BITMAP "):]
			fields := make([]int, 0, 5)
			for len(fields) < 5 {
				comma := bytes.IndexByte(header, ',')
				if comma < 0 {
					return job, fmt.Errorf("tspl: truncated BITMAP header")
				}
				n, err := strconv.Atoi(string(header[:comma]))
				if err != nil {
					return job, fmt.Errorf("tspl: BITMAP header: %v", err)
				}
				fields = append(fields, n)
				header = header[comma+1:]
			}
			x, y, widthBytes, height, mode := fields[0], fields[1], fields[2], fields[3], fields[4]
			if y != 0 || mode != 0 || pending == nil || pending.Image != nil {
				return job, fmt.Errorf("tspl: unexpected BITMAP %v", fields)
			}
			n := widthBytes * height
			if len(header) < n+2 || string(header[n:n+2]) != "\r\n" {
				return job, fmt.Errorf("tspl: truncated BITMAP data")
			}
			img := image.NewGray(image.Rect(0, 0, widthBytes*8, height))
			for i, b := range header[:n] {
				for bit := 0; bit < 8; bit++ {
					if b&(0x80>>uint(bit)) != 0 {
						img.Pix[(i/widthBytes)*img.Stride+(i%widthBytes)*8+bit] = 0xFF
					}
				}
			}
			pending.X, pending.Image = x, img
			s = header[n+2:]
			continue
		}
		end := bytes.Index(s, []byte("\r\n"))
		if end < 0 {
			return job, fmt.Errorf("tspl: command without CRLF: %q", s)
		}
		cmd := string(s[:end])
		s = s[end+2:]
		switch {
		case cmd == "CLS":
			if pending != nil {
				return job, fmt.Errorf("tspl: CLS before the previous label printed")
			}
			pending = &Label{}
		case strings.HasPrefix(cmd, "PRINT 1,"):
			copies, err := strconv.Atoi(strings.TrimPrefix(cmd, "PRINT 1,"))
			if err != nil || pending == nil || pending.Image == nil {
				return job, fmt.Errorf("tspl: unexpected %q", cmd)
			}
			pending.Copies = copies
			job.Labels = append(job.Labels, *pending)
			pending = nil
		default:
			if len(job.Labels) > 0 || pending != nil {
				return job, fmt.Errorf("tspl: setup command %q after the first label", cmd)
			}
			job.Setup = append(job.Setup, cmd)
		}
	}
	if pending != nil || len(job.Labels) == 0 {
		return job, fmt.Errorf("tspl: program does not end with a printed label")
	}
	return job, nil
}
