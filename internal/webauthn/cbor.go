package webauthn

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

// What authenticators send is CBOR (RFC 8949), in the definite-length form
// CTAP2 requires. Only what WebAuthn uses is read: integers, byte and text
// strings, arrays, maps keyed by integers or text, tags (read through), and
// true, false and null. Floats and indefinite lengths are refused.

// cborMaxDepth is how deeply arrays and maps may nest.
const cborMaxDepth = 16

var errCBOR = errors.New("malformed CBOR")

// decodeCBOR reads one item from the start of data, and what follows it.
// Integers are int64, byte strings []byte, text strings string, arrays
// []any and maps map[any]any, keyed by int64 or string.
func decodeCBOR(data []byte) (any, []byte, error) {
	return decodeCBORItem(data, 0)
}

func decodeCBORItem(data []byte, depth int) (any, []byte, error) {
	if depth > cborMaxDepth {
		return nil, nil, fmt.Errorf("%w: nested too deeply", errCBOR)
	}
	if len(data) == 0 {
		return nil, nil, fmt.Errorf("%w: ends early", errCBOR)
	}
	major, info := data[0]>>5, data[0]&31
	data = data[1:]
	if major == 7 {
		switch info {
		case 20:
			return false, data, nil
		case 21:
			return true, data, nil
		case 22, 23:
			return nil, data, nil
		}
		return nil, nil, fmt.Errorf("%w: unsupported simple value or float", errCBOR)
	}
	var arg uint64
	switch {
	case info < 24:
		arg = uint64(info)
	case info <= 27:
		n := 1 << (info - 24)
		if len(data) < n {
			return nil, nil, fmt.Errorf("%w: ends early", errCBOR)
		}
		switch n {
		case 1:
			arg = uint64(data[0])
		case 2:
			arg = uint64(binary.BigEndian.Uint16(data))
		case 4:
			arg = uint64(binary.BigEndian.Uint32(data))
		case 8:
			arg = binary.BigEndian.Uint64(data)
		}
		data = data[n:]
	default:
		return nil, nil, fmt.Errorf("%w: indefinite or reserved length", errCBOR)
	}
	switch major {
	case 0:
		if arg > math.MaxInt64 {
			return nil, nil, fmt.Errorf("%w: integer too large", errCBOR)
		}
		return int64(arg), data, nil
	case 1:
		if arg > math.MaxInt64 {
			return nil, nil, fmt.Errorf("%w: integer too large", errCBOR)
		}
		return -1 - int64(arg), data, nil
	case 2, 3:
		if arg > uint64(len(data)) {
			return nil, nil, fmt.Errorf("%w: ends early", errCBOR)
		}
		b := data[:arg]
		if major == 3 {
			if !utf8.Valid(b) {
				return nil, nil, fmt.Errorf("%w: text is not UTF-8", errCBOR)
			}
			return string(b), data[arg:], nil
		}
		return append([]byte(nil), b...), data[arg:], nil
	case 4:
		// Each item takes a byte at least.
		if arg > uint64(len(data)) {
			return nil, nil, fmt.Errorf("%w: ends early", errCBOR)
		}
		out := make([]any, 0, arg)
		for i := uint64(0); i < arg; i++ {
			v, rest, err := decodeCBORItem(data, depth+1)
			if err != nil {
				return nil, nil, err
			}
			out, data = append(out, v), rest
		}
		return out, data, nil
	case 5:
		if arg > uint64(len(data))/2 {
			return nil, nil, fmt.Errorf("%w: ends early", errCBOR)
		}
		out := make(map[any]any, arg)
		for i := uint64(0); i < arg; i++ {
			k, rest, err := decodeCBORItem(data, depth+1)
			if err != nil {
				return nil, nil, err
			}
			switch k.(type) {
			case int64, string:
			default:
				return nil, nil, fmt.Errorf("%w: map key is not an integer or text", errCBOR)
			}
			if _, dup := out[k]; dup {
				return nil, nil, fmt.Errorf("%w: map key repeated", errCBOR)
			}
			v, rest, err := decodeCBORItem(rest, depth+1)
			if err != nil {
				return nil, nil, err
			}
			out[k], data = v, rest
		}
		return out, data, nil
	default: // 6, a tag: what it tags is read as itself.
		return decodeCBORItem(data, depth+1)
	}
}

// cborPair is one entry of a map being written, which keeps its order.
type cborPair struct {
	Key, Value any
}

// encodeCBOR writes v as CBOR, for the software authenticator: int, int64,
// []byte, string, []any and []cborPair, as a map in the order given.
func encodeCBOR(v any) []byte {
	head := func(major byte, n uint64) []byte {
		switch {
		case n < 24:
			return []byte{major<<5 | byte(n)}
		case n <= math.MaxUint8:
			return []byte{major<<5 | 24, byte(n)}
		case n <= math.MaxUint16:
			return binary.BigEndian.AppendUint16([]byte{major<<5 | 25}, uint16(n))
		case n <= math.MaxUint32:
			return binary.BigEndian.AppendUint32([]byte{major<<5 | 26}, uint32(n))
		}
		return binary.BigEndian.AppendUint64([]byte{major<<5 | 27}, n)
	}
	switch x := v.(type) {
	case int:
		return encodeCBOR(int64(x))
	case int64:
		if x < 0 {
			return head(1, uint64(-1-x))
		}
		return head(0, uint64(x))
	case []byte:
		return append(head(2, uint64(len(x))), x...)
	case string:
		return append(head(3, uint64(len(x))), x...)
	case []any:
		out := head(4, uint64(len(x)))
		for _, item := range x {
			out = append(out, encodeCBOR(item)...)
		}
		return out
	case []cborPair:
		out := head(5, uint64(len(x)))
		for _, p := range x {
			out = append(append(out, encodeCBOR(p.Key)...), encodeCBOR(p.Value)...)
		}
		return out
	}
	panic(fmt.Sprintf("encodeCBOR: %T", v))
}
