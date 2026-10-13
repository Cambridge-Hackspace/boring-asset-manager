package bitbuf

import (
	"bytes"
	"testing"
)

func TestAppendPacksMostSignificantBitFirst(t *testing.T) {
	var b Buffer
	b.Append(0b0100, 4)
	b.Append(0x0B, 8)
	b.AppendBytes([]byte{0xA5})

	if b.Len() != 20 {
		t.Fatalf("Len() = %d, want 20", b.Len())
	}
	if !b.Bit(1) || b.Bit(0) {
		t.Errorf("first nibble written in the wrong order")
	}
	if got, want := b.Bytes(), []byte{0x40, 0xBA, 0x50}; !bytes.Equal(got, want) {
		t.Errorf("Bytes() = % X, want % X", got, want)
	}

	var prefix Buffer
	prefix.AppendPrefix(&b, 12)
	if got, want := prefix.Bytes(), []byte{0x40, 0xB0}; !bytes.Equal(got, want) {
		t.Errorf("AppendPrefix(12) = % X, want % X", got, want)
	}
}
