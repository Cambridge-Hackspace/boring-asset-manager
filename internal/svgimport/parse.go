package svgimport

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// parseTransform reads a transform list: matrix, translate, scale, rotate,
// skewX and skewY, applied right to left as SVG has them.
func parseTransform(v string) (matrix, error) {
	m := identity
	rest := strings.TrimSpace(v)
	var fns []matrix
	for rest != "" {
		open := strings.IndexByte(rest, '(')
		close := strings.IndexByte(rest, ')')
		if open < 0 || close < open {
			return identity, fmt.Errorf("malformed transform %q", v)
		}
		name := strings.TrimSpace(rest[:open])
		var args []float64
		for _, f := range strings.FieldsFunc(rest[open+1:close], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' }) {
			n, err := strconv.ParseFloat(f, 64)
			if err != nil {
				return identity, fmt.Errorf("malformed transform %q", v)
			}
			args = append(args, n)
		}
		arg := func(i int, def float64) float64 {
			if i < len(args) {
				return args[i]
			}
			return def
		}
		var t matrix
		switch {
		case name == "matrix" && len(args) == 6:
			t = matrix{args[0], args[1], args[2], args[3], args[4], args[5]}
		case name == "translate" && len(args) >= 1:
			t = matrix{1, 0, 0, 1, args[0], arg(1, 0)}
		case name == "scale" && len(args) >= 1:
			t = matrix{args[0], 0, 0, arg(1, args[0]), 0, 0}
		case name == "rotate" && len(args) >= 1:
			a := args[0] * math.Pi / 180
			sin, cos := math.Sin(a), math.Cos(a)
			cx, cy := arg(1, 0), arg(2, 0)
			// About (cx, cy): translate there, turn, translate back.
			t = matrix{cos, sin, -sin, cos, cx - cx*cos + cy*sin, cy - cx*sin - cy*cos}
		case name == "skewX" && len(args) == 1:
			t = matrix{1, 0, math.Tan(args[0] * math.Pi / 180), 1, 0, 0}
		case name == "skewY" && len(args) == 1:
			t = matrix{1, math.Tan(args[0] * math.Pi / 180), 0, 1, 0, 0}
		default:
			return identity, fmt.Errorf("malformed transform %q", v)
		}
		fns = append(fns, t)
		rest = strings.TrimLeft(rest[close+1:], " ,\t\r\n")
	}
	// The last listed applies first.
	for i := len(fns) - 1; i >= 0; i-- {
		m = m.then(fns[i])
	}
	return m, nil
}

// parseColor reads a color: #rgb, #rrggbb (with alpha, #rgba and
// #rrggbbaa), rgb() and rgba(), or a name; red, green, blue and alpha from
// 0 to 1.
func parseColor(v string) (r, g, b, a float64, ok bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "currentcolor" {
		return 0, 0, 0, 1, true
	}
	if hex, found := strings.CutPrefix(v, "#"); found {
		switch len(hex) {
		case 3, 4:
			// Each digit doubled: #f80 is #ff8800.
			long := make([]byte, 0, 2*len(hex))
			for i := 0; i < len(hex); i++ {
				long = append(long, hex[i], hex[i])
			}
			hex = string(long)
		case 6, 8:
		default:
			return 0, 0, 0, 0, false
		}
		n, err := strconv.ParseUint(hex, 16, 64)
		if err != nil {
			return 0, 0, 0, 0, false
		}
		a = 1
		if len(hex) == 8 {
			a = float64(n&0xFF) / 255
			n >>= 8
		}
		return float64(n>>16&0xFF) / 255, float64(n>>8&0xFF) / 255, float64(n&0xFF) / 255, a, true
	}
	if args, found := strings.CutPrefix(v, "rgb"); found {
		args = strings.TrimPrefix(args, "a")
		args = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(args), "("), ")")
		parts := strings.FieldsFunc(args, func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
		if len(parts) < 3 {
			return 0, 0, 0, 0, false
		}
		var c [4]float64
		c[3] = 1
		for i, p := range parts[:min(4, len(parts))] {
			pct := strings.HasSuffix(p, "%")
			n, err := strconv.ParseFloat(strings.TrimSuffix(p, "%"), 64)
			if err != nil {
				return 0, 0, 0, 0, false
			}
			switch {
			case pct:
				n /= 100
			case i < 3:
				n /= 255
			}
			c[i] = math.Max(0, math.Min(1, n))
		}
		return c[0], c[1], c[2], c[3], true
	}
	if rgb, found := named[v]; found {
		return float64(rgb>>16&0xFF) / 255, float64(rgb>>8&0xFF) / 255, float64(rgb&0xFF) / 255, 1, true
	}
	return 0, 0, 0, 0, false
}

// named are the common CSS color names.
var named = map[string]uint32{
	"black": 0x000000, "white": 0xffffff, "red": 0xff0000, "green": 0x008000, "blue": 0x0000ff,
	"yellow": 0xffff00, "orange": 0xffa500, "purple": 0x800080, "gray": 0x808080, "grey": 0x808080,
	"silver": 0xc0c0c0, "maroon": 0x800000, "navy": 0x000080, "olive": 0x808000, "teal": 0x008080,
	"lime": 0x00ff00, "aqua": 0x00ffff, "cyan": 0x00ffff, "fuchsia": 0xff00ff, "magenta": 0xff00ff,
	"darkgray": 0xa9a9a9, "darkgrey": 0xa9a9a9, "lightgray": 0xd3d3d3, "lightgrey": 0xd3d3d3,
	"dimgray": 0x696969, "dimgrey": 0x696969, "gold": 0xffd700, "pink": 0xffc0cb, "brown": 0xa52a2a,
	"darkblue": 0x00008b, "darkred": 0x8b0000, "darkgreen": 0x006400, "whitesmoke": 0xf5f5f5,
}
