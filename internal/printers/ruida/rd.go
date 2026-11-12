// Package ruida speaks to Ruida laser controllers (docs/printing.md#ruida),
// as community projects describe their undocumented protocol: it writes
// jobs in Ruida's own format, the one RDWorks and LightBurn save as .rd,
// uploads them to the controller's memory for a person to start from its
// panel, and reads whether the controller is running one.
package ruida

import (
	"bytes"
	"fmt"
	"image"
	"math"
)

// Ruida's format is a stream of commands: a command byte, with its top bit
// set, then its values, each byte with its top bit clear. Coordinates are
// micrometers in five 7-bit bytes, most significant first; speeds are
// micrometers a second, alike; power is a percentage of 0x3FFF in two 7-bit
// bytes. Everything sent is swizzled (Swizzle) byte by byte.

// Commands, as MeerK40t's rdjob.py (MIT) names them.
var (
	cmdMoveAbs          = []byte{0x88}       // x, y: move with the laser off
	cmdCutAbs           = []byte{0xA8}       // x, y: move with it on
	cmdMinPower1        = []byte{0xC6, 0x01} // power
	cmdMaxPower1        = []byte{0xC6, 0x02} // power
	cmdLaserOnDelay     = []byte{0xC6, 0x12} // time
	cmdLaserOffDelay    = []byte{0xC6, 0x13} // time
	cmdMinPower1Part    = []byte{0xC6, 0x31} // part, power
	cmdMaxPower1Part    = []byte{0xC6, 0x32} // part, power
	cmdSpeedLaser1      = []byte{0xC9, 0x02} // speed
	cmdSpeedLaser1Part  = []byte{0xC9, 0x04} // part, speed
	cmdLayerEnd         = []byte{0xCA, 0x01, 0x00}
	cmdLaserDevice0     = []byte{0xCA, 0x01, 0x10}
	cmdLaser2Offset0    = []byte{0xCA, 0x01, 0x30}
	cmdLayerNumberPart  = []byte{0xCA, 0x02} // part
	cmdLaserTubeStart   = []byte{0xCA, 0x03} // switch
	cmdLayerColorPart   = []byte{0xCA, 0x06} // part, color
	cmdMaxLayerPart     = []byte{0xCA, 0x22} // part
	cmdWorkModePart     = []byte{0xCA, 0x41} // part, mode
	cmdEndOfFile        = []byte{0xD7}
	cmdRefPointMachine  = []byte{0xD8, 0x10}
	cmdStopProcess      = []byte{0xD8, 0x01}
	cmdPauseProcess     = []byte{0xD8, 0x02}
	cmdRestoreProcess   = []byte{0xD8, 0x03}
	cmdGetSetting       = []byte{0xDA, 0x00} // memory
	cmdSetFileSum       = []byte{0xE5, 0x05} // value
	cmdSetAbsolute      = []byte{0xE6, 0x01}
	cmdBlockEnd         = []byte{0xE7, 0x00}
	cmdProcessTopLeft   = []byte{0xE7, 0x03} // x, y
	cmdProcessRepeat    = []byte{0xE7, 0x04} // seven 14-bit values
	cmdArrayDirection   = []byte{0xE7, 0x05} // direction
	cmdFeedRepeat       = []byte{0xE7, 0x06} // two values
	cmdProcessBotRight  = []byte{0xE7, 0x07} // x, y
	cmdFeedAutoPause    = []byte{0xE7, 0x38} // switch
	cmdDocumentMin      = []byte{0xE7, 0x50} // x, y
	cmdDocumentMax      = []byte{0xE7, 0x51} // x, y
	cmdPartMin          = []byte{0xE7, 0x52} // part, x, y
	cmdPartMax          = []byte{0xE7, 0x53} // part, x, y
	cmdPartMinEx        = []byte{0xE7, 0x61} // part, x, y
	cmdPartMaxEx        = []byte{0xE7, 0x62} // part, x, y
	cmdRefPointSet      = []byte{0xF0}
	cmdBlockCutting     = []byte{0xF1, 0x02} // switch
	cmdFileTransferName = []byte{0xE8, 0x02, 0xE7, 0x01}
)

// StartProcess runs a job at once, D8 00. BAM! never sends it
// (docs/printing.md#ruida): a job is started from the controller's panel.
var StartProcess = []byte{0xD8, 0x00}

// Memory the controller is asked for.
const (
	// MachineRunning is the status bit set while a job runs.
	MachineRunning = 0x00000001
)

// MemMachineStatus is the address of the controller's status bits:
// MachineRunning among them while a job runs.
var MemMachineStatus = [2]byte{0x04, 0x00}

// Replies: ACK takes a packet, NAK asks for it again (its checksum did not
// match), ERR refuses it, and ENQ is a keep-alive.
const (
	ACK = 0xCC
	ERR = 0xCD
	ENQ = 0xCE
	NAK = 0xCF
)

// DefaultMagic is the swizzle key of most controllers, such as the 644XG
// and 644XS; the 634XG takes 0x11, and the RDL9635 0x38.
const DefaultMagic = 0x88

// Swizzle scrambles a byte as the controller expects: its top and bottom
// bits swapped, XORed with the magic, plus one.
func Swizzle(b, magic byte) byte {
	b = b&0x7E | b>>7 | b<<7
	return (b ^ magic) + 1
}

// Unswizzle undoes Swizzle.
func Unswizzle(b, magic byte) byte {
	b = (b - 1) ^ magic
	return b&0x7E | b>>7 | b<<7
}

// SwizzleAll swizzles each byte.
func SwizzleAll(data []byte, magic byte) []byte {
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = Swizzle(b, magic)
	}
	return out
}

// UnswizzleAll unswizzles each byte.
func UnswizzleAll(data []byte, magic byte) []byte {
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = Unswizzle(b, magic)
	}
	return out
}

// Checksum is a UDP packet's: the low 16 bits of the sum of its swizzled
// bytes.
func Checksum(swizzled []byte) uint16 {
	var sum uint16
	for _, b := range swizzled {
		sum += uint16(b)
	}
	return sum
}

// Packet is a UDP packet: the checksum, big-endian, then the payload,
// swizzled.
func Packet(payload []byte, magic byte) []byte {
	sw := SwizzleAll(payload, magic)
	sum := Checksum(sw)
	return append([]byte{byte(sum >> 8), byte(sum)}, sw...)
}

// value35 is a value in five 7-bit bytes.
func value35(v int64) []byte {
	return []byte{byte(v>>28) & 0x7F, byte(v>>21) & 0x7F, byte(v>>14) & 0x7F, byte(v>>7) & 0x7F, byte(v) & 0x7F}
}

// Value35 reads a value from five 7-bit bytes.
func Value35(b []byte) int64 {
	var v int64
	for _, c := range b[:5] {
		v = v<<7 | int64(c&0x7F)
	}
	return v
}

// value14 is a value in two 7-bit bytes.
func value14(v int) []byte {
	return []byte{byte(v>>7) & 0x7F, byte(v) & 0x7F}
}

// power is a percentage as the controller takes it.
func power(percent float64) []byte {
	return value14(int(math.Round(min(max(percent, 0), 100) * 0x3FFF / 100)))
}

// speed is millimeters a second as micrometers a second.
func speed(mmPerSecond float64) []byte {
	return value35(int64(math.Round(mmPerSecond * 1000)))
}

// Setting is how the laser burns: power as a percentage, speed in
// millimeters a second, and how many passes.
type Setting struct {
	Power  float64
	Speed  float64
	Passes int
}

// Raster is a 1-bit image to engrave, black burned, covering a rectangle
// of the bed in millimeters.
type Raster struct {
	X, Y, Width, Height float64
	Dots                *image.Gray
}

// Point is a point on the bed, in millimeters.
type Point struct{ X, Y float64 }

// Path is a path to cut, closed back to its first point when Closed.
type Path struct {
	Points []Point
	Closed bool
}

// Bed is a machine's work area, and where its origin is: points are given
// from the work area's top left corner, as the person faces the machine,
// and FlipX and FlipY turn them around for a machine whose X runs right to
// left or whose Y runs from the front up.
type Bed struct {
	WidthMM, HeightMM float64
	FlipX, FlipY      bool
}

// Program is a job: a raster engraved, then paths cut, on a machine's bed.
type Program struct {
	Bed
	Raster  *Raster
	Engrave Setting
	Paths   []Path
	Cut     Setting
}

// fileWriter writes a file, checking every point is on the bed.
type fileWriter struct {
	buf bytes.Buffer
	bed Bed
	err error
}

func (w *fileWriter) cmd(parts ...[]byte) {
	for _, p := range parts {
		w.buf.Write(p)
	}
}

// at is a point as the controller takes it, in micrometers.
func (w *fileWriter) at(x, y float64) []byte {
	const tolerance = 0.01
	if x < -tolerance || y < -tolerance || x > w.bed.WidthMM+tolerance || y > w.bed.HeightMM+tolerance || math.IsNaN(x) || math.IsNaN(y) {
		if w.err == nil {
			w.err = fmt.Errorf("ruida: the point %.3f, %.3f mm is outside the %g × %g mm work area", x, y, w.bed.WidthMM, w.bed.HeightMM)
		}
	}
	x, y = min(max(x, 0), w.bed.WidthMM), min(max(y, 0), w.bed.HeightMM)
	if w.bed.FlipX {
		x = w.bed.WidthMM - x
	}
	if w.bed.FlipY {
		y = w.bed.HeightMM - y
	}
	return append(value35(int64(math.Round(x*1000))), value35(int64(math.Round(y*1000)))...)
}

// part is a layer of the job: one setting, and the moves made with it.
type part struct {
	setting Setting
	moves   [][2]Point // from, to: travel to from, burn to to
	minX    float64
	minY    float64
	maxX    float64
	maxY    float64
}

func (p *part) add(from, to Point) {
	if len(p.moves) == 0 {
		p.minX, p.minY, p.maxX, p.maxY = from.X, from.Y, from.X, from.Y
	}
	for _, q := range []Point{from, to} {
		p.minX, p.minY, p.maxX, p.maxY = min(p.minX, q.X), min(p.minY, q.Y), max(p.maxX, q.X), max(p.maxY, q.Y)
	}
	p.moves = append(p.moves, [2]Point{from, to})
}

// File writes a program in Ruida's format: a header with the job's bounds
// and its parts' settings, the engraving as one part, each run of black
// dots a cut, row by row, back and forth, once a pass, then the outlines
// as another, once a pass, and the file's sum. It never holds StartProcess:
// it is to be started from the panel.
func File(p Program) ([]byte, error) {
	if !(p.WidthMM > 0 && p.HeightMM > 0) {
		return nil, fmt.Errorf("ruida: no work area")
	}
	var parts []*part
	if r := p.Raster; r != nil && r.Dots != nil {
		if err := checkSetting("engraving", p.Engrave); err != nil {
			return nil, err
		}
		pt := &part{setting: p.Engrave}
		for range p.Engrave.Passes {
			rasterMoves(pt, r)
		}
		if len(pt.moves) > 0 {
			parts = append(parts, pt)
		}
	}
	if len(p.Paths) > 0 {
		if err := checkSetting("cutting", p.Cut); err != nil {
			return nil, err
		}
		pt := &part{setting: p.Cut}
		for range p.Cut.Passes {
			for _, path := range p.Paths {
				pathMoves(pt, path)
			}
		}
		if len(pt.moves) > 0 {
			parts = append(parts, pt)
		}
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("ruida: nothing to engrave or cut")
	}
	w := &fileWriter{bed: p.Bed}
	minX, minY, maxX, maxY := parts[0].minX, parts[0].minY, parts[0].maxX, parts[0].maxY
	for _, pt := range parts[1:] {
		minX, minY, maxX, maxY = min(minX, pt.minX), min(minY, pt.minY), max(maxX, pt.maxX), max(maxY, pt.maxY)
	}
	// The bounds, as the controller has them: a corner each, whichever
	// way its axes run.
	lo, hi := w.at(minX, minY), w.at(maxX, maxY)
	w.cmd(cmdRefPointMachine)
	w.cmd(cmdSetAbsolute)
	w.cmd(cmdRefPointSet)
	w.cmd(cmdBlockCutting, []byte{0})
	w.cmd(cmdFeedRepeat, value35(0), value35(0))
	w.cmd(cmdFeedAutoPause, []byte{0})
	w.cmd(cmdProcessTopLeft, lo)
	w.cmd(cmdProcessBotRight, hi)
	w.cmd(cmdDocumentMin, lo)
	w.cmd(cmdDocumentMax, hi)
	w.cmd(cmdProcessRepeat, value14(1), value14(1), value14(0), value14(0), value14(0), value14(0), value14(0))
	w.cmd(cmdArrayDirection, []byte{0})
	for i, pt := range parts {
		n := []byte{byte(i)}
		plo, phi := w.at(pt.minX, pt.minY), w.at(pt.maxX, pt.maxY)
		w.cmd(cmdSpeedLaser1Part, n, speed(pt.setting.Speed))
		w.cmd(cmdMinPower1Part, n, power(pt.setting.Power))
		w.cmd(cmdMaxPower1Part, n, power(pt.setting.Power))
		w.cmd(cmdLayerColorPart, n, value35(0))
		w.cmd(cmdWorkModePart, n, []byte{0})
		w.cmd(cmdPartMin, n, plo)
		w.cmd(cmdPartMax, n, phi)
		w.cmd(cmdPartMinEx, n, plo)
		w.cmd(cmdPartMaxEx, n, phi)
	}
	w.cmd(cmdMaxLayerPart, []byte{byte(len(parts) - 1)})
	for i, pt := range parts {
		if i > 0 {
			w.layerEnd()
		}
		w.cmd(cmdLayerNumberPart, []byte{byte(i)})
		w.cmd(cmdLaserDevice0)
		w.cmd(cmdSpeedLaser1, speed(pt.setting.Speed))
		w.cmd(cmdLaserOnDelay, value35(0))
		w.cmd(cmdLaserOffDelay, value35(0))
		w.cmd(cmdMinPower1, power(pt.setting.Power))
		w.cmd(cmdMaxPower1, power(pt.setting.Power))
		w.cmd(cmdLaserTubeStart, []byte{1})
		var at Point
		for j, m := range pt.moves {
			if j == 0 || m[0] != at {
				w.cmd(cmdMoveAbs, w.at(m[0].X, m[0].Y))
			}
			w.cmd(cmdCutAbs, w.at(m[1].X, m[1].Y))
			at = m[1]
		}
	}
	w.layerEnd()
	if w.err != nil {
		return nil, w.err
	}
	// The sum of the file's bytes, the end of file's among them.
	var sum int64 = 0xD7
	for _, b := range w.buf.Bytes() {
		sum += int64(b)
	}
	w.cmd(cmdSetFileSum, value35(sum))
	w.cmd(cmdEndOfFile)
	return w.buf.Bytes(), nil
}

func (w *fileWriter) layerEnd() {
	w.cmd(cmdBlockEnd)
	w.cmd(cmdLayerEnd)
	w.cmd(cmdLaser2Offset0)
}

func checkSetting(what string, s Setting) error {
	if !(s.Power > 0 && s.Power <= 100) || !(s.Speed > 0) || s.Passes < 1 {
		return fmt.Errorf("ruida: the %s setting %+v is not one to burn with", what, s)
	}
	return nil
}

// rasterMoves engraves an image once: each row with something black on
// it, from left to right and then right to left, each run of black dots a
// cut.
func rasterMoves(pt *part, r *Raster) {
	b := r.Dots.Bounds()
	cols, rows := b.Dx(), b.Dy()
	if cols == 0 || rows == 0 {
		return
	}
	dx, dy := r.Width/float64(cols), r.Height/float64(rows)
	black := func(x, y int) bool { return r.Dots.Pix[y*r.Dots.Stride+x] < 0x80 }
	reverse := false
	for y := range rows {
		var runs [][2]int
		for x := 0; x < cols; {
			if !black(x, y) {
				x++
				continue
			}
			start := x
			for x < cols && black(x, y) {
				x++
			}
			runs = append(runs, [2]int{start, x})
		}
		if len(runs) == 0 {
			continue
		}
		rowY := r.Y + (float64(y)+0.5)*dy
		edge := func(col int) float64 { return r.X + float64(col)*dx }
		if !reverse {
			for _, run := range runs {
				pt.add(Point{edge(run[0]), rowY}, Point{edge(run[1]), rowY})
			}
		} else {
			for i := len(runs) - 1; i >= 0; i-- {
				pt.add(Point{edge(runs[i][1]), rowY}, Point{edge(runs[i][0]), rowY})
			}
		}
		reverse = !reverse
	}
}

// pathMoves cuts a path once.
func pathMoves(pt *part, p Path) {
	if len(p.Points) < 2 {
		return
	}
	points := p.Points
	if p.Closed {
		points = append(points[:len(points):len(points)], points[0])
	}
	for i := 1; i < len(points); i++ {
		pt.add(points[i-1], points[i])
	}
}

// Upload is the file as sent to be kept in the controller's memory under
// a name: the name first, then the file. The controller keeps names of up
// to eight characters.
func Upload(name string, file []byte) []byte {
	out := append(bytes.Clone(cmdFileTransferName), name...)
	out = append(out, 0)
	return append(out, file...)
}

// FileTransferName is the command that names a file sent after it.
func FileTransferName() []byte { return bytes.Clone(cmdFileTransferName) }

// GetSetting asks for a word of the controller's memory; it answers DA 01,
// the address, and the word as five 7-bit bytes.
func GetSetting(mem [2]byte) []byte {
	return append(bytes.Clone(cmdGetSetting), mem[0], mem[1])
}

// Control commands.
func StopProcess() []byte    { return bytes.Clone(cmdStopProcess) }
func PauseProcess() []byte   { return bytes.Clone(cmdPauseProcess) }
func RestoreProcess() []byte { return bytes.Clone(cmdRestoreProcess) }

// SetFileSum is the command that gives a file's sum.
func SetFileSum() []byte { return bytes.Clone(cmdSetFileSum) }

// Chunks cuts a payload into packets of at most size bytes, each ending
// before a command byte where one comes soon enough: a command is not split
// across packets unless it is longer than a packet.
func Chunks(payload []byte, size int) [][]byte {
	var out [][]byte
	for len(payload) > size {
		cut := size
		for i := size; i > size/2; i-- {
			if payload[i]&0x80 != 0 {
				cut = i
				break
			}
		}
		out = append(out, payload[:cut])
		payload = payload[cut:]
	}
	if len(payload) > 0 {
		out = append(out, payload)
	}
	return out
}
