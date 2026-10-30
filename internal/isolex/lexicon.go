package isolex

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Vocabulary is what bam declares in the `lexicon {}` block it writes
// for every build (isolex SPEC §1): nobody writes that block by hand, so
// the palette's tokens are the lexicon's vocabulary and the compiler
// holds every icon to them.
type Vocabulary struct {
	// Tokens are the theme tokens icons may paint with: the palette's.
	// None means the compiler checks no tokens.
	Tokens []string
	// StrokeWidths are the widths strokes may be drawn at. None means
	// any width.
	StrokeWidths []float64
	// Reserved tokens are painted only by icons that grant them.
	Reserved []string
	// Limits overrides the compiler's limits (isolex SPEC §9), by name
	// (shape-limit, repeat-limit, insert-depth, icon-limit,
	// expression-depth). 0 means no limit at all.
	Limits map[string]int64
}

// LimitNames are the limits a lexicon block may set.
var LimitNames = []string{"shape-limit", "repeat-limit", "insert-depth", "icon-limit", "expression-depth"}

// LexiconFileName names the generated block in diagnostics; it sorts
// before every other file, so it is compiled first.
const LexiconFileName = "_lexicon.ilx"

// LexiconBlock writes v as a `lexicon {}` block, or "" when it declares
// nothing. The output is deterministic: limits in name order.
func LexiconBlock(v Vocabulary) (string, error) {
	var b strings.Builder
	line := func(name string, values []string) {
		if len(values) > 0 {
			fmt.Fprintf(&b, "    %s %s\n", name, strings.Join(values, " "))
		}
	}
	line("tokens", quoteAll(v.Tokens))
	widths := make([]string, len(v.StrokeWidths))
	for i, w := range v.StrokeWidths {
		if !(w > 0) {
			return "", fmt.Errorf("stroke width %v is not a positive number", w)
		}
		widths[i] = strconv.FormatFloat(w, 'f', -1, 64)
	}
	line("stroke-widths", widths)
	line("reserved", quoteAll(v.Reserved))
	names := make([]string, 0, len(v.Limits))
	for name := range v.Limits {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !isLimit(name) {
			return "", fmt.Errorf("unknown limit %q (one of %s)", name, strings.Join(LimitNames, ", "))
		}
		n := v.Limits[name]
		switch {
		case n == 0:
			line(name, []string{"#null"})
		case n > 0:
			line(name, []string{strconv.FormatInt(n, 10)})
		default:
			return "", fmt.Errorf("%s: %d is not a positive whole number, or 0 for no limit", name, n)
		}
	}
	if b.Len() == 0 {
		return "", nil
	}
	return "// Written by bam from the palette and the icon settings.\nlexicon {\n" + b.String() + "}\n", nil
}

// PaletteColor is a token's colors, as a `palette` block gives them.
type PaletteColor struct {
	Token string
	Colors
}

// paletteColor is a color a `palette` block can hold (isolex SPEC §2).
var paletteColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// PaletteBlock writes a palette as a `palette {}` block, in its order, so
// that a folder of the lexicon's files carries its colors (isolex SPEC
// §2), or "" for none. A block has no room for transparency: a token
// with a color written #rrggbbaa is left out, saying so in a comment.
func PaletteBlock(palette []PaletteColor) string {
	if len(palette) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("palette {\n")
	for _, c := range palette {
		if !paletteColor.MatchString(c.Light) || !paletteColor.MatchString(c.Dark) {
			fmt.Fprintf(&b, "    // %s is light %s and dark %s, which a palette block cannot hold\n", c.Token, c.Light, c.Dark)
			continue
		}
		fmt.Fprintf(&b, "    %s light=%s dark=%s\n", c.Token, Quote(c.Light), Quote(c.Dark))
	}
	b.WriteString("}\n")
	return b.String()
}

func isLimit(name string) bool {
	for _, n := range LimitNames {
		if n == name {
			return true
		}
	}
	return false
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = Quote(s)
	}
	return out
}

// Quote writes s as a KDL 2.0 quoted string.
func Quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u{%x}`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
