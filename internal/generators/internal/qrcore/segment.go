package qrcore

import (
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators/internal/bitbuf"
)

// Mode is a data encoding mode.
type Mode int

const (
	Numeric Mode = iota
	Alphanumeric
	Byte
)

// AlphanumericCharset lists the alphanumeric mode characters in value order.
const AlphanumericCharset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ $%*+-./:"

// DetectMode returns the most compact mode able to encode all of payload as
// a single segment.
func DetectMode(payload string) Mode {
	numeric, alphanumeric := true, true
	for i := 0; i < len(payload); i++ {
		c := payload[i]
		if c < '0' || c > '9' {
			numeric = false
		}
		if strings.IndexByte(AlphanumericCharset, c) < 0 {
			alphanumeric = false
		}
	}
	switch {
	case numeric:
		return Numeric
	case alphanumeric:
		return Alphanumeric
	default:
		return Byte
	}
}

// DataBits returns the length of payload's encoded data, excluding the mode
// indicator and character count.
func DataBits(mode Mode, payload string) int {
	n := len(payload)
	switch mode {
	case Numeric:
		return n/3*10 + [3]int{0, 4, 7}[n%3]
	case Alphanumeric:
		return n/2*11 + n%2*6
	default:
		return n * 8
	}
}

// WriteData appends payload's encoded data, excluding the mode indicator and
// character count. The payload must be valid for the mode.
func WriteData(buf *bitbuf.Buffer, mode Mode, payload string) {
	switch mode {
	case Numeric:
		for i := 0; i < len(payload); i += 3 {
			group := payload[i:min(i+3, len(payload))]
			value := 0
			for j := 0; j < len(group); j++ {
				value = value*10 + int(group[j]-'0')
			}
			buf.Append(uint32(value), [4]int{0, 4, 7, 10}[len(group)])
		}
	case Alphanumeric:
		for i := 0; i < len(payload); i += 2 {
			first := strings.IndexByte(AlphanumericCharset, payload[i])
			if i+1 == len(payload) {
				buf.Append(uint32(first), 6)
				break
			}
			second := strings.IndexByte(AlphanumericCharset, payload[i+1])
			buf.Append(uint32(first*45+second), 11)
		}
	default:
		buf.AppendBytes([]byte(payload))
	}
}

var padCodewords = [2]uint32{0xEC, 0x11}

// Pad completes a data bit stream to capacityBits: the terminator (up to
// terminatorBits zeros), zero bits to the next codeword boundary, then
// alternating pad codewords. When capacityBits is not a multiple of eight
// (Micro QR Code M1 and M3), the final codeword is four bits and is padded
// with zeros.
func Pad(buf *bitbuf.Buffer, capacityBits, terminatorBits int) {
	buf.Append(0, min(terminatorBits, capacityBits-buf.Len()))

	fullCodewordBits := capacityBits - capacityBits%8
	if buf.Len() < fullCodewordBits {
		if rem := buf.Len() % 8; rem != 0 {
			buf.Append(0, 8-rem)
		}
		for i := 0; buf.Len() < fullCodewordBits; i++ {
			buf.Append(padCodewords[i%2], 8)
		}
	}
	buf.Append(0, capacityBits-buf.Len())
}
