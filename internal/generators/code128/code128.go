// Package code128 encodes Code 128 linear barcodes as defined by ISO/IEC 15417.
//
// Printable ASCII (32–126) is encoded with code set B, switching to code set C
// for runs of digits long enough to shorten the symbol.
package code128

import (
	"errors"
	"fmt"
)

// QuietZone is the standard light margin on each side, in modules.
const QuietZone = 10

// ErrUnsupportedCharacter is returned for bytes outside printable ASCII.
var ErrUnsupportedCharacter = errors.New("code128: payload contains characters outside printable ASCII (32-126)")

const (
	startB = 104
	startC = 105
	codeB  = 100 // switch to code set B (from C)
	codeC  = 99  // switch to code set C (from B)
	stop   = 106
)

// Code is an encoded symbol.
type Code struct {
	// Values are the symbol character values, from start character to stop.
	Values []int
	// Modules are the module colors from left to right, without quiet zones;
	// true is a bar.
	Modules []bool
}

// Encode encodes payload, which must be non-empty printable ASCII.
func Encode(payload string) (*Code, error) {
	if payload == "" {
		return nil, errors.New("code128: payload is empty")
	}
	for i := 0; i < len(payload); i++ {
		if c := payload[i]; c < 32 || c > 126 {
			return nil, fmt.Errorf("%w: byte 0x%02X at offset %d", ErrUnsupportedCharacter, c, i)
		}
	}

	values := symbolValues(payload)
	checksum := values[0]
	for i := 1; i < len(values); i++ {
		checksum += i * values[i]
	}
	values = append(values, checksum%103, stop)

	var modules []bool
	for _, v := range values {
		bar := true
		for _, w := range patterns[v] {
			for k := 0; k < int(w-'0'); k++ {
				modules = append(modules, bar)
			}
			bar = !bar
		}
	}
	return &Code{Values: values, Modules: modules}, nil
}

// symbolValues returns the start character and data symbol values. It follows
// the code set selection rules of ISO/IEC 15417 Annex E restricted to sets B
// and C: start in C for four or more leading digits (or a payload of exactly
// two), and switch from B to C for a run of four digits at the end or six
// elsewhere, encoding an odd leading digit in B first.
func symbolValues(payload string) []int {
	digitRun := func(i int) int {
		n := 0
		for i+n < len(payload) && payload[i+n] >= '0' && payload[i+n] <= '9' {
			n++
		}
		return n
	}

	inC := false
	values := []int{startB}
	if run := digitRun(0); run >= 4 || (run == 2 && len(payload) == 2) {
		inC = true
		values[0] = startC
	}

	for i := 0; i < len(payload); {
		run := digitRun(i)
		if inC {
			if run >= 2 {
				values = append(values, int(payload[i]-'0')*10+int(payload[i+1]-'0'))
				i += 2
				continue
			}
			values = append(values, codeB)
			inC = false
			continue
		}
		if run >= 6 || (run >= 4 && i+run == len(payload)) {
			if run%2 == 1 {
				values = append(values, int(payload[i])-32)
				i++
			}
			values = append(values, codeC)
			inC = true
			continue
		}
		values = append(values, int(payload[i])-32)
		i++
	}
	return values
}

// patterns are the bar and space widths of each symbol character, starting
// with a bar (ISO/IEC 15417 Table 1).
var patterns = [107]string{
	"212222", "222122", "222221", "121223", "121322", "131222", "122213", "122312", "132212", "221213",
	"221312", "231212", "112232", "122132", "122231", "113222", "123122", "123221", "223211", "221132",
	"221231", "213212", "223112", "312131", "311222", "321122", "321221", "312212", "322112", "322211",
	"212123", "212321", "232121", "111323", "131123", "131321", "112313", "132113", "132311", "211313",
	"231113", "231311", "112133", "112331", "132131", "113123", "113321", "133121", "313121", "211331",
	"231131", "213113", "213311", "213131", "311123", "311321", "331121", "312113", "312311", "332111",
	"314111", "221411", "431111", "111224", "111422", "121124", "121421", "141122", "141221", "112214",
	"112412", "122114", "122411", "142112", "142211", "241211", "221114", "413111", "241112", "134111",
	"111242", "121142", "121241", "114212", "124112", "124211", "411212", "421112", "421211", "212141",
	"214121", "412121", "111143", "111341", "131141", "114113", "114311", "411113", "411311", "113141",
	"114131", "311141", "411131", "211412", "211214", "211232", "2331112",
}
