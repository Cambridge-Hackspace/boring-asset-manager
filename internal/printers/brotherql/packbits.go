package brotherql

import "errors"

// packBits compresses data with the TIFF PackBits scheme the raster command
// language uses: a header n in 0..127 copies the next n+1 bytes, and a header
// in -127..-1 repeats the next byte 1-n times.
func packBits(data []byte) []byte {
	var out []byte
	for i := 0; i < len(data); {
		// A run of at least two equal bytes.
		run := 1
		for i+run < len(data) && run < 128 && data[i+run] == data[i] {
			run++
		}
		if run >= 2 {
			out = append(out, byte(1-run), data[i])
			i += run
			continue
		}
		// Literals until the next run of three or more, at most 128 bytes.
		// Breaking a literal for a run of two would cost a header byte and
		// save none.
		start := i
		for i < len(data) && i-start < 128 {
			if i+2 < len(data) && data[i] == data[i+1] && data[i] == data[i+2] {
				break
			}
			i++
		}
		out = append(out, byte(i-start-1))
		out = append(out, data[start:i]...)
	}
	return out
}

var errCorruptPackBits = errors.New("brotherql: corrupt PackBits data")

func unpackBits(data []byte) ([]byte, error) {
	var out []byte
	for i := 0; i < len(data); {
		n := int(int8(data[i]))
		i++
		switch {
		case n >= 0:
			if i+n+1 > len(data) {
				return nil, errCorruptPackBits
			}
			out = append(out, data[i:i+n+1]...)
			i += n + 1
		case n != -128:
			if i >= len(data) {
				return nil, errCorruptPackBits
			}
			for k := 0; k < 1-n; k++ {
				out = append(out, data[i])
			}
			i++
		}
	}
	return out, nil
}
