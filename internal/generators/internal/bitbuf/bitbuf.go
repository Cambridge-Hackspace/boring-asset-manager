// Package bitbuf accumulates a bit stream, most significant bit first, as used
// by the QR Code family of symbologies.
package bitbuf

// Buffer is an append-only sequence of bits.
type Buffer struct {
	bits []bool
}

// Append writes the low n bits of value, most significant first.
func (b *Buffer) Append(value uint32, n int) {
	for i := n - 1; i >= 0; i-- {
		b.bits = append(b.bits, (value>>uint(i))&1 == 1)
	}
}

// AppendBytes writes every bit of data, most significant first.
func (b *Buffer) AppendBytes(data []byte) {
	for _, d := range data {
		b.Append(uint32(d), 8)
	}
}

// AppendPrefix writes the first n bits of src.
func (b *Buffer) AppendPrefix(src *Buffer, n int) {
	b.bits = append(b.bits, src.bits[:n]...)
}

// Len returns the number of bits written.
func (b *Buffer) Len() int {
	return len(b.bits)
}

// Bit reports whether bit i is set.
func (b *Buffer) Bit(i int) bool {
	return b.bits[i]
}

// Bytes packs the stream into bytes. A trailing partial byte is padded with
// zero bits on the right.
func (b *Buffer) Bytes() []byte {
	out := make([]byte, (len(b.bits)+7)/8)
	for i, set := range b.bits {
		if set {
			out[i/8] |= 0x80 >> uint(i%8)
		}
	}
	return out
}
