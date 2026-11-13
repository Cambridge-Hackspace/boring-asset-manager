package brady

import (
	"encoding/binary"
	"errors"
)

// LZ4's block format, which the printer takes each page's BMP in: a run of
// sequences, each some literal bytes and then a match, a copy of bytes
// already written. It follows LZ4's own description of the format
// (github.com/lz4/lz4, doc/lz4_Block_format.md).
const (
	minMatch = 4
	// The last 5 bytes are always literals, and the last match starts at
	// least 12 bytes before the end.
	lastLiterals = 5
	mfLimit      = 12
	maxOffset    = 65535
	hashBits     = 16
)

// compressBlock compresses src into one LZ4 block, finding matches
// greedily through a hash of each 4 bytes. A BMP is mostly white rows,
// which it shrinks to almost nothing.
func compressBlock(src []byte) []byte {
	dst := make([]byte, 0, len(src)/4+16)
	var table [1 << hashBits]int32 // a position plus 1, by hash
	anchor := 0
	for i := 0; i+mfLimit < len(src); {
		seq := binary.LittleEndian.Uint32(src[i:])
		h := (seq * 2654435761) >> (32 - hashBits)
		cand := int(table[h]) - 1
		table[h] = int32(i + 1)
		if cand < 0 || i-cand > maxOffset || binary.LittleEndian.Uint32(src[cand:]) != seq {
			i++
			continue
		}
		n := minMatch
		for i+n < len(src)-lastLiterals && src[cand+n] == src[i+n] {
			n++
		}
		dst = appendSequence(dst, src[anchor:i], i-cand, n)
		i += n
		anchor = i
	}
	return appendSequence(dst, src[anchor:], 0, 0)
}

// appendSequence appends literals, then a match of n bytes offset back,
// or, with n 0, only the literals that end a block.
func appendSequence(dst, literals []byte, offset, n int) []byte {
	token := len(dst)
	dst = append(dst, 0)
	dst[token] = byte(min(len(literals), 15)) << 4
	dst = appendLength(dst, len(literals))
	dst = append(dst, literals...)
	if n == 0 {
		return dst
	}
	dst = append(dst, byte(offset), byte(offset>>8))
	dst[token] |= byte(min(n-minMatch, 15))
	return appendLength(dst, n-minMatch)
}

// appendLength appends what of a length its token's 4 bits do not hold.
func appendLength(dst []byte, n int) []byte {
	if n < 15 {
		return dst
	}
	for n -= 15; n >= 255; n -= 255 {
		dst = append(dst, 255)
	}
	return append(dst, byte(n))
}

var errCorrupt = errors.New("brady: corrupt LZ4 block")

// decompressBlock reverses compressBlock, or any LZ4 block.
func decompressBlock(src []byte) ([]byte, error) {
	var dst []byte
	length := func(i, n int) (int, int, error) {
		if n < 15 {
			return i, n, nil
		}
		for {
			if i >= len(src) {
				return i, 0, errCorrupt
			}
			b := src[i]
			i++
			n += int(b)
			if b != 255 {
				return i, n, nil
			}
		}
	}
	for i := 0; i < len(src); {
		token := src[i]
		i++
		var lit, n int
		var err error
		i, lit, err = length(i, int(token>>4))
		if err != nil || i+lit > len(src) {
			return nil, errCorrupt
		}
		dst = append(dst, src[i:i+lit]...)
		i += lit
		if i == len(src) {
			return dst, nil
		}
		if i+2 > len(src) {
			return nil, errCorrupt
		}
		offset := int(src[i]) | int(src[i+1])<<8
		i += 2
		i, n, err = length(i, int(token&15))
		if err != nil || offset == 0 || offset > len(dst) {
			return nil, errCorrupt
		}
		// A match may overlap what it writes, so copy byte by byte.
		from := len(dst) - offset
		for k := 0; k < n+minMatch; k++ {
			dst = append(dst, dst[from+k])
		}
	}
	return nil, errCorrupt
}
