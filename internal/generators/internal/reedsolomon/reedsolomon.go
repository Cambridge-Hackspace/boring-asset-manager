// Package reedsolomon computes Reed–Solomon error correction codewords over
// GF(256) with the primitive polynomial x^8 + x^4 + x^3 + x^2 + 1 (0x11D), as
// specified for QR Code and Micro QR Code (ISO/IEC 18004).
package reedsolomon

var (
	expTable [512]byte
	logTable [256]byte
)

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		expTable[i] = byte(x)
		logTable[x] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	// Doubling the table lets multiply skip the modulo-255 reduction.
	for i := 255; i < len(expTable); i++ {
		expTable[i] = expTable[i-255]
	}
}

func multiply(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return expTable[int(logTable[a])+int(logTable[b])]
}

// generator returns the coefficients of (x - α^0)(x - α^1)…(x - α^(n-1)),
// highest degree first, without the leading 1.
func generator(n int) []byte {
	poly := []byte{1}
	for i := 0; i < n; i++ {
		next := make([]byte, len(poly)+1)
		for j, c := range poly {
			next[j] ^= c
			next[j+1] ^= multiply(c, expTable[i])
		}
		poly = next
	}
	return poly[1:]
}

// Encode returns the n error correction codewords for data.
func Encode(data []byte, n int) []byte {
	gen := generator(n)
	remainder := make([]byte, n)
	for _, d := range data {
		factor := d ^ remainder[0]
		copy(remainder, remainder[1:])
		remainder[n-1] = 0
		for i, g := range gen {
			remainder[i] ^= multiply(g, factor)
		}
	}
	return remainder
}
