package qrcore

import (
	"bytes"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators/internal/bitbuf"
)

func TestFormatBitsMatchesPublishedSequences(t *testing.T) {
	cases := []struct {
		name    string
		data    uint32
		xorMask uint32
		want    uint32
	}{
		// ISO/IEC 18004 Annex C: QR Code level M (00), mask 0 is all-zero data.
		{"qr M mask 0", 0b00000, 0x5412, 0b101010000010010},
		// Level L (01), mask 4.
		{"qr L mask 4", 0b01100, 0x5412, 0b110011000101111},
		// Level H (10), mask 7.
		{"qr H mask 7", 0b10111, 0x5412, 0b000100000111011},
		// Micro QR Code symbol number 0, mask 0.
		{"micro 0 mask 0", 0b00000, 0x4445, 0b100010001000101},
	}
	for _, c := range cases {
		if got := FormatBits(c.data, c.xorMask); got != c.want {
			t.Errorf("%s: FormatBits = %015b, want %015b", c.name, got, c.want)
		}
	}
}

func TestDetectMode(t *testing.T) {
	cases := map[string]Mode{
		"0123456789":   Numeric,
		"":             Numeric,
		"HELLO WORLD":  Alphanumeric,
		"A1$%*+-./:":   Alphanumeric,
		"hello":        Byte,
		"HELLO\nWORLD": Byte,
		"café":         Byte,
	}
	for payload, want := range cases {
		if got := DetectMode(payload); got != want {
			t.Errorf("DetectMode(%q) = %d, want %d", payload, got, want)
		}
	}
}

func TestWriteDataMatchesBitLengthAndExamples(t *testing.T) {
	cases := []struct {
		mode    Mode
		payload string
		want    []byte // packed, right-padded with zeros
		bits    int
	}{
		// ISO/IEC 18004 7.4.3: "01234567" -> 000 0001100 0101011001 1000011.
		{Numeric, "01234567", []byte{0x03, 0x15, 0x98, 0x60}, 27},
		// ISO/IEC 18004 7.4.4: "AC-42" -> 00111001110 11100111001 000010.
		{Alphanumeric, "AC-42", []byte{0x39, 0xDC, 0xE4, 0x20}, 28},
		{Byte, "Hi", []byte{'H', 'i'}, 16},
	}
	for _, c := range cases {
		var buf bitbuf.Buffer
		WriteData(&buf, c.mode, c.payload)
		if buf.Len() != c.bits || DataBits(c.mode, c.payload) != c.bits {
			t.Errorf("%q: wrote %d bits, DataBits says %d, want %d", c.payload, buf.Len(), DataBits(c.mode, c.payload), c.bits)
		}
		if got := buf.Bytes(); !bytes.Equal(got, c.want) {
			t.Errorf("%q: encoded % X, want % X", c.payload, got, c.want)
		}
	}
}

func TestPad(t *testing.T) {
	cases := []struct {
		name       string
		dataBits   int
		capacity   int
		terminator int
		want       []byte
	}{
		// Already byte-aligned after the terminator: pad codewords follow at once.
		{"aligned", 20, 64, 4, []byte{0, 0, 0, 0xEC, 0x11, 0xEC, 0x11, 0xEC}},
		{"unaligned", 13, 40, 4, []byte{0, 0, 0, 0xEC, 0x11}},
		// Data fills the symbol: no room for a terminator.
		{"full", 40, 40, 4, []byte{0, 0, 0, 0, 0}},
		// Micro QR Code M3-L: 84 bits, the last codeword four bits long.
		{"half codeword pads", 50, 84, 7, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0xEC, 0x11, 0x00}},
		{"half codeword tail", 79, 84, 7, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x00}},
	}
	for _, c := range cases {
		var buf bitbuf.Buffer
		buf.Append(0, c.dataBits)
		Pad(&buf, c.capacity, c.terminator)
		if buf.Len() != c.capacity {
			t.Errorf("%s: padded to %d bits, want %d", c.name, buf.Len(), c.capacity)
		}
		if got := buf.Bytes(); !bytes.Equal(got, c.want) {
			t.Errorf("%s: padded to % X, want % X", c.name, got, c.want)
		}
	}
}

func TestPlaceBitsSkipsFunctionModulesAndTimingColumn(t *testing.T) {
	m := NewMatrix(9)
	for y := 0; y < 9; y++ {
		m.SetFunction(6, y, false) // stand-in timing column
	}
	var bits bitbuf.Buffer
	for i := 0; i < 9*8; i++ {
		bits.Append(1, 1)
	}
	m.PlaceBits(&bits, 6)

	for y := 0; y < 9; y++ {
		for x := 0; x < 9; x++ {
			if want := x != 6; m.Dark(x, y) != want {
				t.Fatalf("module (%d,%d) dark = %v, want %v", x, y, m.Dark(x, y), want)
			}
		}
	}
}

func TestPlaceBitsZigzagOrder(t *testing.T) {
	// Placement order in a 4×4 matrix: the right column pair upwards from the
	// bottom, right module first, then the left pair downwards from the top.
	order := [4][4]int{
		{9, 8, 7, 6},
		{11, 10, 5, 4},
		{13, 12, 3, 2},
		{15, 14, 1, 0},
	}
	for target := 0; target < 16; target++ {
		m := NewMatrix(4)
		var bits bitbuf.Buffer
		for i := 0; i < 16; i++ {
			if i == target {
				bits.Append(1, 1)
			} else {
				bits.Append(0, 1)
			}
		}
		m.PlaceBits(&bits, -1)
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				if want := order[y][x] == target; m.Dark(x, y) != want {
					t.Fatalf("bit %d: module (%d,%d) dark = %v, want %v", target, x, y, m.Dark(x, y), want)
				}
			}
		}
	}
}
