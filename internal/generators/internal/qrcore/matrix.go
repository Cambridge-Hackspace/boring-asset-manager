// Package qrcore holds the parts of ISO/IEC 18004 shared by QR Code and Micro QR
// Code: the module matrix, codeword placement, data masks, format information
// and data segment encoding.
package qrcore

import "github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators/internal/bitbuf"

// Matrix is a square grid of modules that also records which modules belong
// to function patterns, so data placement and masking can skip them.
// Coordinates are (x, y) = (column, row) with the origin at the top-left.
type Matrix struct {
	Size     int
	dark     []bool
	function []bool
}

// NewMatrix returns a light matrix with no function modules.
func NewMatrix(size int) *Matrix {
	return &Matrix{
		Size:     size,
		dark:     make([]bool, size*size),
		function: make([]bool, size*size),
	}
}

// Clone returns an independent copy.
func (m *Matrix) Clone() *Matrix {
	return &Matrix{
		Size:     m.Size,
		dark:     append([]bool(nil), m.dark...),
		function: append([]bool(nil), m.function...),
	}
}

// Dark reports whether the module at (x, y) is dark.
func (m *Matrix) Dark(x, y int) bool {
	return m.dark[y*m.Size+x]
}

// SetFunction marks (x, y) as a function module with the given color.
func (m *Matrix) SetFunction(x, y int, dark bool) {
	m.dark[y*m.Size+x] = dark
	m.function[y*m.Size+x] = true
}

// Reserve marks (x, y) as a light function module unless it already belongs to
// a function pattern. It holds space for format and version information,
// which is drawn after masking.
func (m *Matrix) Reserve(x, y int) {
	if !m.function[y*m.Size+x] {
		m.SetFunction(x, y, false)
	}
}

// Modules returns a row-major copy of the module colors.
func (m *Matrix) Modules() []bool {
	return append([]bool(nil), m.dark...)
}

// DrawFinder draws a finder pattern centered on (cx, cy) together with its
// one-module light separator, clipped to the matrix.
func (m *Matrix) DrawFinder(cx, cy int) {
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			x, y := cx+dx, cy+dy
			if x < 0 || y < 0 || x >= m.Size || y >= m.Size {
				continue
			}
			ring := max(abs(dx), abs(dy))
			m.SetFunction(x, y, ring != 2 && ring != 4)
		}
	}
}

// DrawAlignment draws a 5×5 alignment pattern centered on (cx, cy).
func (m *Matrix) DrawAlignment(cx, cy int) {
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			m.SetFunction(cx+dx, cy+dy, max(abs(dx), abs(dy)) != 1)
		}
	}
}

// PlaceBits fills the non-function modules with bits in the standard zigzag:
// two-module-wide columns from right to left, alternately upwards and
// downwards, starting upwards at the bottom-right corner. skipColumn names a
// vertical timing column the zigzag steps over (6 for QR Code), or -1.
// Modules left over once bits run out stay light (the remainder bits).
func (m *Matrix) PlaceBits(bits *bitbuf.Buffer, skipColumn int) {
	i := 0
	upward := true
	for right := m.Size - 1; right >= 1; right -= 2 {
		if right == skipColumn {
			right--
		}
		for v := 0; v < m.Size; v++ {
			y := v
			if upward {
				y = m.Size - 1 - v
			}
			for dx := 0; dx < 2; dx++ {
				x := right - dx
				if m.function[y*m.Size+x] || i >= bits.Len() {
					continue
				}
				m.dark[y*m.Size+x] = bits.Bit(i)
				i++
			}
		}
		upward = !upward
	}
}

// ApplyMask inverts every non-function module for which pattern holds.
func (m *Matrix) ApplyMask(pattern MaskPattern) {
	for y := 0; y < m.Size; y++ {
		for x := 0; x < m.Size; x++ {
			if !m.function[y*m.Size+x] && pattern(y, x) {
				m.dark[y*m.Size+x] = !m.dark[y*m.Size+x]
			}
		}
	}
}

// MaskPattern reports whether the module at row i, column j is inverted.
type MaskPattern func(i, j int) bool

// MaskPatterns are the eight QR Code data mask conditions (ISO/IEC 18004
// Table 10), indexed by QR Code mask reference. Micro QR Code uses four of them.
var MaskPatterns = [8]MaskPattern{
	func(i, j int) bool { return (i+j)%2 == 0 },
	func(i, j int) bool { return i%2 == 0 },
	func(i, j int) bool { return j%3 == 0 },
	func(i, j int) bool { return (i+j)%3 == 0 },
	func(i, j int) bool { return (i/2+j/3)%2 == 0 },
	func(i, j int) bool { return (i*j)%2+(i*j)%3 == 0 },
	func(i, j int) bool { return ((i*j)%2+(i*j)%3)%2 == 0 },
	func(i, j int) bool { return ((i+j)%2+(i*j)%3)%2 == 0 },
}

// FormatBits appends the BCH(15,5) error correction bits to the five format
// data bits and applies the symbology's XOR mask.
func FormatBits(data, xorMask uint32) uint32 {
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	return (data<<10 | rem&0x3FF) ^ xorMask
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
