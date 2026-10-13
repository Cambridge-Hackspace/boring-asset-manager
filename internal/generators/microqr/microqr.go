// Package microqr encodes Micro QR Code symbols as defined by ISO/IEC 18004.
//
// Payloads are encoded in numeric or alphanumeric mode as a single segment.
// Versions M2 to M4 are used. M1 offers error detection but no correction, so
// it is never chosen: a scuffed label should still scan.
package microqr

import (
	"errors"
	"fmt"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators/internal/bitbuf"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators/internal/qrcore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators/internal/reedsolomon"
)

// Level is an error correction level. Q is available in M4 only.
type Level int

const (
	L Level = iota // recovers about 7% of codewords
	M              // about 15%
	Q              // about 25%
)

func (l Level) String() string {
	return [...]string{"L", "M", "Q"}[l]
}

// QuietZone is the standard light margin around a symbol, in modules.
const QuietZone = 2

const (
	minVersion = 2
	maxVersion = 4
)

var (
	// ErrTooLong is returned when a payload does not fit in an M4 symbol at the
	// requested error correction level.
	ErrTooLong = errors.New("microqr: payload exceeds symbol capacity")
	// ErrUnsupportedCharacter is returned for payloads outside the
	// alphanumeric character set (0-9, A-Z, space and $%*+-./:).
	ErrUnsupportedCharacter = errors.New("microqr: payload contains characters outside the alphanumeric set")
	// ErrInvalidLevel is returned for an unknown error correction level.
	ErrInvalidLevel = errors.New("microqr: invalid error correction level")
)

// Code is an encoded symbol.
type Code struct {
	// Version is 2, 3 or 4 for M2, M3 or M4.
	Version int
	Level   Level
	Mask    int
	Size    int
	// Modules holds Size×Size module colors in row-major order; true is dark.
	Modules []bool
}

// Dark reports whether the module at column x, row y is dark.
func (c *Code) Dark(x, y int) bool {
	return c.Modules[y*c.Size+x]
}

// Designator returns the version name, such as "M3".
func (c *Code) Designator() string {
	return fmt.Sprintf("M%d", c.Version)
}

// spec describes one version and error correction level combination.
type spec struct {
	symbolNumber  uint32 // format information symbol number
	dataBits      int
	totalCodeword int
}

var specs = map[int]map[Level]spec{
	2: {L: {1, 40, 10}, M: {2, 32, 10}},
	3: {L: {3, 84, 17}, M: {4, 68, 17}},
	4: {L: {5, 128, 24}, M: {6, 112, 24}, Q: {7, 80, 24}},
}

// Mode indicator and character count lengths, indexed by version.
var (
	modeIndicatorBits = [5]int{2: 1, 3: 2, 4: 3}
	terminatorBits    = [5]int{2: 5, 3: 7, 4: 9}
	countBits         = map[qrcore.Mode][5]int{
		qrcore.Numeric:      {2: 4, 3: 5, 4: 6},
		qrcore.Alphanumeric: {2: 3, 3: 4, 4: 5},
	}
	modeIndicator = map[qrcore.Mode]uint32{
		qrcore.Numeric:      0,
		qrcore.Alphanumeric: 1,
	}
)

// Micro QR Code masks 0–3 are QR Code mask patterns 1, 4, 6 and 7.
var maskPatterns = [4]qrcore.MaskPattern{
	qrcore.MaskPatterns[1],
	qrcore.MaskPatterns[4],
	qrcore.MaskPatterns[6],
	qrcore.MaskPatterns[7],
}

// Encode encodes payload in the smallest version that fits at level,
// choosing the data mask by the Micro QR Code evaluation rule.
func Encode(payload string, level Level) (*Code, error) {
	return encode(payload, level, 0, -1)
}

// encode allows the version (0 for the smallest that fits) and mask (-1 for
// automatic selection) to be pinned, which golden tests rely on.
func encode(payload string, level Level, version, mask int) (*Code, error) {
	if level < L || level > Q {
		return nil, ErrInvalidLevel
	}
	mode := qrcore.DetectMode(payload)
	if mode == qrcore.Byte {
		return nil, ErrUnsupportedCharacter
	}

	if version == 0 {
		for v := minVersion; v <= maxVersion; v++ {
			if fits(mode, payload, v, level) {
				version = v
				break
			}
		}
		if version == 0 {
			return nil, fmt.Errorf("%w (%d characters at level %s)", ErrTooLong, len(payload), level)
		}
	} else if !fits(mode, payload, version, level) {
		return nil, fmt.Errorf("%w: M%d-%s", ErrTooLong, version, level)
	}
	s := specs[version][level]

	var data bitbuf.Buffer
	data.Append(modeIndicator[mode], modeIndicatorBits[version])
	data.Append(uint32(len(payload)), countBits[mode][version])
	qrcore.WriteData(&data, mode, payload)
	qrcore.Pad(&data, s.dataBits, terminatorBits[version])

	// M3 ends its data with a four-bit codeword. Error correction treats it as
	// the high nibble of a byte; only its four bits are placed in the symbol.
	codewords := data.Bytes()
	var stream bitbuf.Buffer
	stream.AppendPrefix(&data, s.dataBits)
	stream.AppendBytes(reedsolomon.Encode(codewords, s.totalCodeword-len(codewords)))

	m := layout(version)
	m.PlaceBits(&stream, -1)

	if mask < 0 {
		mask = bestMask(m)
	}
	m.ApplyMask(maskPatterns[mask])
	drawFormat(m, s.symbolNumber, mask)

	return &Code{
		Version: version,
		Level:   level,
		Mask:    mask,
		Size:    m.Size,
		Modules: m.Modules(),
	}, nil
}

func fits(mode qrcore.Mode, payload string, version int, level Level) bool {
	s, ok := specs[version][level]
	if !ok {
		return false
	}
	cb := countBits[mode][version]
	if len(payload) >= 1<<uint(cb) {
		return false
	}
	return modeIndicatorBits[version]+cb+qrcore.DataBits(mode, payload) <= s.dataBits
}

// layout draws the finder pattern and timing patterns and reserves the format
// information area.
func layout(version int) *qrcore.Matrix {
	size := 2*version + 9
	m := qrcore.NewMatrix(size)
	m.DrawFinder(3, 3)
	for i := 8; i < size; i++ {
		m.SetFunction(i, 0, i%2 == 0)
		m.SetFunction(0, i, i%2 == 0)
	}
	for i := 1; i <= 8; i++ {
		m.Reserve(8, i)
		m.Reserve(i, 8)
	}
	return m
}

// bestMask returns the mask scoring highest by ISO/IEC 18004 7.8.3.2, which
// favors dark modules along the right and bottom edges. Ties go to the
// lowest mask number.
func bestMask(m *qrcore.Matrix) int {
	best, bestScore := 0, -1
	last := m.Size - 1
	for mask, pattern := range maskPatterns {
		candidate := m.Clone()
		candidate.ApplyMask(pattern)
		right, bottom := 0, 0
		for i := 1; i < m.Size; i++ {
			if candidate.Dark(last, i) {
				right++
			}
			if candidate.Dark(i, last) {
				bottom++
			}
		}
		score := bottom*16 + right
		if right <= bottom {
			score = right*16 + bottom
		}
		if score > bestScore {
			best, bestScore = mask, score
		}
	}
	return best
}

func drawFormat(m *qrcore.Matrix, symbolNumber uint32, mask int) {
	bits := qrcore.FormatBits(symbolNumber<<2|uint32(mask), 0x4445)
	for i := 0; i < 8; i++ {
		m.SetFunction(8, i+1, (bits>>uint(i))&1 == 1)
		m.SetFunction(i+1, 8, (bits>>uint(14-i))&1 == 1)
	}
}
