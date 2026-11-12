package niimbot

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
)

// JobOptions are the choices a job is encoded with.
type JobOptions struct {
	// Density is the print darkness, in the model's range.
	Density int
	// Task is the print task to encode the job for.
	Task Task
	// LabelType is the roll's, LabelGaps or LabelContinuous.
	LabelType byte
}

// Check reports whether the options suit the model.
func (o JobOptions) Check(mo Model) error {
	if o.Density < mo.MinDensity || o.Density > mo.MaxDensity {
		return fmt.Errorf("density must be %d-%d on the %s", mo.MinDensity, mo.MaxDensity, mo.Name)
	}
	switch o.Task {
	case TaskD110, TaskB1, TaskB21V1:
	default:
		return fmt.Errorf("print task must be one of %s, %s and %s", TaskB1, TaskB21V1, TaskD110)
	}
	return nil
}

// maxRepeat is the most rows one row packet repeats.
const maxRepeat = 255

// one is the data of the commands that take a single 1.
var one = []byte{1}

func u16(v int) []byte { return binary.BigEndian.AppendUint16(nil, uint16(v)) }

// EncodeJob encodes pages, each prepared for the model (Prepare), as one
// job: the packets sent in order, those that wait for the printer included
// (Converse sends them).
//
// Every job connects, asks for the roll's RFID tag, sets the density and
// label type, and starts the print; then sends each page; then ends the
// print. How it starts the print and each page, and where it waits for the
// printer, is the print task's.
func EncodeJob(mo Model, pages []*image.Gray, opts JobOptions) ([]byte, error) {
	if err := opts.Check(mo); err != nil {
		return nil, err
	}
	if len(pages) == 0 || len(pages) > 0xFFFF {
		return nil, fmt.Errorf("a job has 1-%d pages", 0xFFFF)
	}
	var buf bytes.Buffer
	put := func(cmd byte, data ...byte) { buf.Write(Packet{Cmd: cmd, Data: data}.Bytes()) }

	put(CmdConnect, one...)
	put(CmdRFIDInfo, one...)
	put(CmdSetDensity, byte(opts.Density))
	put(CmdSetLabelType, opts.LabelType)
	if opts.Task == TaskB1 {
		put(CmdPrintStart, append(u16(len(pages)), 0, 0, 0, 0, 0)...)
	} else {
		put(CmdPrintStart, one...)
	}
	for _, page := range pages {
		w, h := page.Bounds().Dx(), page.Bounds().Dy()
		if w != mo.HeadDots {
			return nil, fmt.Errorf("a page is %d dots wide, not the %s head's %d", w, mo.Name, mo.HeadDots)
		}
		if h < 1 || h > 0xFFFF {
			return nil, fmt.Errorf("a page is 1-%d rows long", 0xFFFF)
		}
		size := append(u16(h), u16(w)...)
		switch opts.Task {
		case TaskD110:
			put(CmdPrintClear, one...)
			put(CmdPageStart, one...)
			put(CmdSetPageSize, size...)
			put(CmdPrintQuantity, u16(1)...)
		case TaskB1:
			put(CmdPageStart, one...)
			put(CmdSetPageSize, append(size, u16(1)...)...)
		case TaskB21V1:
			put(CmdPageStart, one...)
			put(CmdSetPageSize, size...)
		}
		writeRows(&buf, mo, page, opts.Task == TaskB21V1)
		put(CmdPageEnd, one...)
		if opts.Task == TaskD110 {
			put(CmdPrintStatus, one...)
		}
	}
	if opts.Task == TaskB1 {
		put(CmdPrintStatus, one...)
	}
	put(CmdPrintEnd, one...)
	return buf.Bytes(), nil
}

// writeRows writes a page's rows, a dot to a bit with the first leftmost:
// each run of the same row in one packet, blank rows as such, and the rest
// with how many dots they print (pixelCounts).
func writeRows(buf *bytes.Buffer, mo Model, page *image.Gray, total bool) {
	w, h := page.Bounds().Dx(), page.Bounds().Dy()
	row := func(y int) []byte {
		out := make([]byte, (w+7)/8)
		for x := 0; x < w; x++ {
			if page.Pix[y*page.Stride+x] < 0x80 {
				out[x/8] |= 0x80 >> (x % 8)
			}
		}
		return out
	}
	for y := 0; y < h; {
		data := row(y)
		n := 1
		for y+n < h && n < maxRepeat && bytes.Equal(row(y+n), data) {
			n++
		}
		if bytes.Count(data, []byte{0}) == len(data) {
			buf.Write(Packet{Cmd: CmdEmptyRows, Data: append(u16(y), byte(n))}.Bytes())
		} else {
			d := append(u16(y), pixelCounts(data, mo.HeadDots, total)...)
			d = append(d, byte(n))
			buf.Write(Packet{Cmd: CmdBitmapRows, Data: append(d, data...)}.Bytes())
		}
		y += n
	}
}

// pixelCounts is the three bytes a bitmap row gives for the dots it
// prints. Split, each counts the dots in its third of the head, at most
// 255; in total, the first is 0 and the others the whole count, low byte
// first. A row wider than three whole thirds of the head's bytes is
// counted in total, and so is every row of the B21's first task.
func pixelCounts(data []byte, headDots int, total bool) []byte {
	third := headDots / 8 / 3
	if total || third == 0 || len(data) > third*3 {
		n := 0
		for _, b := range data {
			n += popcount(b)
		}
		return []byte{0, byte(n), byte(n >> 8)}
	}
	var parts [3]int
	for i, b := range data {
		parts[i/third] += popcount(b)
	}
	out := make([]byte, 3)
	for i, n := range parts {
		out[i] = byte(min(n, 255))
	}
	return out
}

func popcount(b byte) int {
	n := 0
	for ; b != 0; b &= b - 1 {
		n++
	}
	return n
}
