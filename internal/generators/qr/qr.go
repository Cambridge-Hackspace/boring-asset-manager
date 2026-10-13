// Package qr encodes QR Code (Model 2) symbols as defined by ISO/IEC 18004.
//
// A payload is encoded as a single segment in the most compact of numeric,
// alphanumeric or byte mode. Byte mode writes the payload's bytes as they are,
// without an ECI header, so UTF-8 text reaches phone scanners unchanged.
package qr

import (
	"errors"
	"fmt"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators/internal/bitbuf"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators/internal/qrcore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators/internal/reedsolomon"
)

// Level is an error correction level.
type Level int

const (
	L Level = iota // recovers about 7% of codewords
	M              // about 15%
	Q              // about 25%
	H              // about 30%
)

func (l Level) String() string {
	return [...]string{"L", "M", "Q", "H"}[l]
}

// QuietZone is the standard light margin around a symbol, in modules.
const QuietZone = 4

const (
	minVersion = 1
	maxVersion = 40
)

var (
	// ErrTooLong is returned when a payload does not fit in a version 40 symbol
	// at the requested error correction level.
	ErrTooLong = errors.New("qr: payload exceeds symbol capacity")
	// ErrInvalidLevel is returned for an unknown error correction level.
	ErrInvalidLevel = errors.New("qr: invalid error correction level")
)

// Code is an encoded symbol.
type Code struct {
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

// Encode encodes payload in the smallest version that fits at level, choosing
// the data mask by the standard's penalty rules.
func Encode(payload string, level Level) (*Code, error) {
	return encode(payload, level, 0, -1)
}

// encode allows the version (0 for the smallest that fits) and mask (-1 for
// automatic selection) to be pinned, which golden tests rely on.
func encode(payload string, level Level, version, mask int) (*Code, error) {
	if level < L || level > H {
		return nil, ErrInvalidLevel
	}

	mode := qrcore.DetectMode(payload)
	if version == 0 {
		for v := minVersion; v <= maxVersion; v++ {
			if fits(mode, payload, v, level) {
				version = v
				break
			}
		}
		if version == 0 {
			return nil, fmt.Errorf("%w (%d bytes at level %s)", ErrTooLong, len(payload), level)
		}
	} else if version < minVersion || version > maxVersion || !fits(mode, payload, version, level) {
		return nil, fmt.Errorf("%w: version %d-%s", ErrTooLong, version, level)
	}

	var data bitbuf.Buffer
	data.Append(modeIndicator[mode], 4)
	data.Append(uint32(len(payload)), charCountBits(mode, version))
	qrcore.WriteData(&data, mode, payload)
	qrcore.Pad(&data, dataCodewords(version, level)*8, 4)

	m := layout(version)
	m.PlaceBits(interleave(data.Bytes(), version, level), 6)

	if mask < 0 {
		mask = bestMask(m)
	}
	m.ApplyMask(qrcore.MaskPatterns[mask])
	drawFormat(m, level, mask)
	drawVersion(m, version)

	return &Code{
		Version: version,
		Level:   level,
		Mask:    mask,
		Size:    m.Size,
		Modules: m.Modules(),
	}, nil
}

var modeIndicator = map[qrcore.Mode]uint32{
	qrcore.Numeric:      0b0001,
	qrcore.Alphanumeric: 0b0010,
	qrcore.Byte:         0b0100,
}

func charCountBits(mode qrcore.Mode, version int) int {
	tier := 0
	switch {
	case version >= 27:
		tier = 2
	case version >= 10:
		tier = 1
	}
	switch mode {
	case qrcore.Numeric:
		return [3]int{10, 12, 14}[tier]
	case qrcore.Alphanumeric:
		return [3]int{9, 11, 13}[tier]
	default:
		return [3]int{8, 16, 16}[tier]
	}
}

func fits(mode qrcore.Mode, payload string, version int, level Level) bool {
	countBits := charCountBits(mode, version)
	if len(payload) >= 1<<uint(countBits) {
		return false
	}
	bits := 4 + countBits + qrcore.DataBits(mode, payload)
	return bits <= dataCodewords(version, level)*8
}

func symbolSize(version int) int {
	return 17 + 4*version
}

// layout draws the function patterns of a symbol and reserves the format and
// version information areas, which are left light until after masking.
func layout(version int) *qrcore.Matrix {
	size := symbolSize(version)
	m := qrcore.NewMatrix(size)

	m.DrawFinder(3, 3)
	m.DrawFinder(size-4, 3)
	m.DrawFinder(3, size-4)

	for i := 8; i < size-8; i++ {
		m.SetFunction(i, 6, i%2 == 0)
		m.SetFunction(6, i, i%2 == 0)
	}

	positions := alignmentPositions(version)
	last := len(positions) - 1
	for i, cy := range positions {
		for j, cx := range positions {
			// Skip the three corners occupied by finder patterns.
			if (i == 0 && j == 0) || (i == 0 && j == last) || (i == last && j == 0) {
				continue
			}
			m.DrawAlignment(cx, cy)
		}
	}

	// Format information, including the always-dark module above the
	// bottom-left finder.
	for i := 0; i <= 8; i++ {
		m.Reserve(8, i)
		m.Reserve(i, 8)
	}
	for i := 0; i < 8; i++ {
		m.Reserve(size-1-i, 8)
		m.Reserve(8, size-1-i)
	}

	if version >= 7 {
		for i := 0; i < 6; i++ {
			for j := 0; j < 3; j++ {
				m.Reserve(size-11+j, i)
				m.Reserve(i, size-11+j)
			}
		}
	}
	return m
}

// alignmentPositions returns the row/column centers of the alignment
// patterns (ISO/IEC 18004 Annex E).
func alignmentPositions(version int) []int {
	if version == 1 {
		return nil
	}
	count := version/7 + 2
	step := (version*8 + count*3 + 5) / (count*4 - 4) * 2
	positions := make([]int, count)
	positions[0] = 6
	for i, pos := count-1, symbolSize(version)-7; i >= 1; i, pos = i-1, pos-step {
		positions[i] = pos
	}
	return positions
}

// interleave splits the data codewords into error correction blocks, appends
// each block's error correction codewords, and interleaves the result into
// the final bit stream.
func interleave(data []byte, version int, level Level) *bitbuf.Buffer {
	numBlocks := blocksPerLevel[level][version]
	ecLen := ecCodewordsPerBlock[level][version]
	raw := rawCodewords(version)
	numShort := numBlocks - raw%numBlocks
	shortDataLen := raw/numBlocks - ecLen

	dataBlocks := make([][]byte, numBlocks)
	ecBlocks := make([][]byte, numBlocks)
	offset := 0
	for i := range dataBlocks {
		n := shortDataLen
		if i >= numShort {
			n++
		}
		dataBlocks[i] = data[offset : offset+n]
		ecBlocks[i] = reedsolomon.Encode(dataBlocks[i], ecLen)
		offset += n
	}

	var out bitbuf.Buffer
	for i := 0; i <= shortDataLen; i++ {
		for _, block := range dataBlocks {
			if i < len(block) {
				out.Append(uint32(block[i]), 8)
			}
		}
	}
	for i := 0; i < ecLen; i++ {
		for _, block := range ecBlocks {
			out.Append(uint32(block[i]), 8)
		}
	}
	return &out
}

// formatLevelBits are the two error correction bits of the format information.
var formatLevelBits = [4]uint32{L: 0b01, M: 0b00, Q: 0b11, H: 0b10}

func drawFormat(m *qrcore.Matrix, level Level, mask int) {
	bits := qrcore.FormatBits(formatLevelBits[level]<<3|uint32(mask), 0x5412)
	bit := func(i int) bool { return (bits>>uint(i))&1 == 1 }
	size := m.Size

	// Around the top-left finder, stepping over the timing patterns.
	for i := 0; i <= 5; i++ {
		m.SetFunction(8, i, bit(i))
	}
	m.SetFunction(8, 7, bit(6))
	m.SetFunction(8, 8, bit(7))
	m.SetFunction(7, 8, bit(8))
	for i := 9; i < 15; i++ {
		m.SetFunction(14-i, 8, bit(i))
	}

	// The second copy, split between the other two finders.
	for i := 0; i < 8; i++ {
		m.SetFunction(size-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		m.SetFunction(8, size-15+i, bit(i))
	}
	m.SetFunction(8, size-8, true)
}

func drawVersion(m *qrcore.Matrix, version int) {
	if version < 7 {
		return
	}
	rem := uint32(version)
	for i := 0; i < 12; i++ {
		rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
	}
	bits := uint32(version)<<12 | rem&0xFFF
	for i := 0; i < 18; i++ {
		dark := (bits>>uint(i))&1 == 1
		a, b := m.Size-11+i%3, i/3
		m.SetFunction(a, b, dark)
		m.SetFunction(b, a, dark)
	}
}
