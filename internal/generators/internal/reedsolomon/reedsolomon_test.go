package reedsolomon

import (
	"bytes"
	"testing"
)

// TestEncodeKnownBlock checks the worked example from ISO/IEC 18004 Annex I:
// "01234567" as a version 1-M QR Code.
func TestEncodeKnownBlock(t *testing.T) {
	data := []byte{0x10, 0x20, 0x0C, 0x56, 0x61, 0x80, 0xEC, 0x11, 0xEC, 0x11, 0xEC, 0x11, 0xEC, 0x11, 0xEC, 0x11}
	want := []byte{0xA5, 0x24, 0xD4, 0xC1, 0xED, 0x36, 0xC7, 0x87, 0x2C, 0x55}

	if got := Encode(data, len(want)); !bytes.Equal(got, want) {
		t.Errorf("Encode() = % X, want % X", got, want)
	}
}

// TestCodewordIsDivisibleByGenerator checks the defining property of the code:
// data followed by its error correction has no remainder.
func TestCodewordIsDivisibleByGenerator(t *testing.T) {
	data := []byte("BAM! boring asset manager")
	for _, n := range []int{2, 7, 10, 17, 30} {
		message := append(append([]byte{}, data...), Encode(data, n)...)
		// Evaluating the message polynomial at each generator root must give zero.
		for i := 0; i < n; i++ {
			var acc byte
			for _, c := range message {
				acc = multiply(acc, expTable[i]) ^ c
			}
			if acc != 0 {
				t.Errorf("n=%d: message is not divisible by (x - α^%d)", n, i)
			}
		}
	}
}

func TestMultiplyAgreesWithCarrylessMultiplication(t *testing.T) {
	slow := func(a, b byte) byte {
		var product uint16
		for i := 0; i < 8; i++ {
			if b&(1<<uint(i)) != 0 {
				product ^= uint16(a) << uint(i)
			}
		}
		for bit := 15; bit >= 8; bit-- {
			if product&(1<<uint(bit)) != 0 {
				product ^= 0x11D << uint(bit-8)
			}
		}
		return byte(product)
	}
	for a := 0; a < 256; a++ {
		for b := 0; b < 256; b++ {
			if got, want := multiply(byte(a), byte(b)), slow(byte(a), byte(b)); got != want {
				t.Fatalf("multiply(%d, %d) = %d, want %d", a, b, got, want)
			}
		}
	}
}
