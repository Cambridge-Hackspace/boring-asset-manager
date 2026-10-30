package archive

import (
	"strings"
	"unicode"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
)

// Icons' dependencies (#144): which definitions an isolex source makes, and
// which others it names, read from the source itself, so a package brings
// the library files an icon needs. isolex's own dependency graph is the
// studio's, not its WebAssembly module's, so this reads just enough of KDL
// to find them: what may name another definition (isolex SPEC §2-§4) is
// `extends`, as a node or a property, `insert`, `badge`, `axes`, and
// `font=` on text. It may find more than the compiler would use, never
// less.

// iconDefinitions are the kinds of top-level node that define a name.
var iconDefinitions = map[string]bool{"base": true, "icon": true, "fragment": true, "axis": true, "font": true}

// IconNames are what a source defines and the names it uses.
type IconNames struct {
	Defines []string
	Uses    []string
}

// ScanIcons reads the definitions a source makes and the names it uses.
func ScanIcons(source string) IconNames {
	var out IconNames
	seenDef, seenUse := map[string]bool{}, map[string]bool{}
	walkIcons(kdlTokens(source), func(t kdlToken, define bool) {
		switch {
		case t.text == "":
		case define && !seenDef[t.text]:
			seenDef[t.text] = true
			out.Defines = append(out.Defines, t.text)
		case !define && !seenUse[t.text]:
			seenUse[t.text] = true
			out.Uses = append(out.Uses, t.text)
		}
	})
	return out
}

// RenameIcons renames definitions in a source (#145), where it makes them
// and where it names them, old name to new: an icon imported as a copy,
// and what uses it.
func RenameIcons(source string, renames map[string]string) string {
	r := []rune(source)
	var b strings.Builder
	at := 0
	walkIcons(kdlTokens(source), func(t kdlToken, _ bool) {
		if to, ok := renames[t.text]; ok && t.start >= at {
			b.WriteString(string(r[at:t.start]))
			b.WriteString(isolex.Quote(to))
			at = t.end
		}
	})
	b.WriteString(string(r[at:]))
	return b.String()
}

// walkIcons calls each with every string that defines a name, or names
// one, in order.
func walkIcons(toks []kdlToken, each func(t kdlToken, define bool)) {
	depth := 0
	for i := 0; i < len(toks); {
		// a node: its name, then its arguments and properties, to the end
		// of the line, a semicolon or its children
		t := toks[i]
		switch t.kind {
		case tokOpen:
			depth++
			i++
			continue
		case tokClose:
			if depth > 0 {
				depth--
			}
			i++
			continue
		case tokEnd:
			i++
			continue
		}
		name := t.text
		i++
		first := true
		for i < len(toks) && toks[i].kind != tokEnd && toks[i].kind != tokOpen && toks[i].kind != tokClose {
			v := toks[i]
			if v.kind == tokWord && i+2 < len(toks) && toks[i+1].kind == tokEquals {
				// a property
				if (v.text == "extends" || v.text == "font") && toks[i+2].kind == tokString {
					each(toks[i+2], false)
				}
				i += 3
				continue
			}
			if v.kind == tokString {
				switch {
				case depth == 0 && first && iconDefinitions[name]:
					each(v, true)
				case depth > 0 && (name == "extends" || name == "axes"):
					each(v, false)
				case depth > 0 && first && (name == "insert" || name == "badge"):
					each(v, false)
				}
				first = false
			}
			i++
		}
	}
}

type tokKind int

const (
	tokWord tokKind = iota
	tokString
	tokEquals
	tokOpen
	tokClose
	tokEnd
)

type kdlToken struct {
	kind tokKind
	text string
	// start and end are where a string is in the source, in runes.
	start, end int
}

// kdlTokens splits KDL 2.0 into words, strings, =, braces and the ends of
// nodes, leaving out comments. A type annotation, such as (text), is left
// out too.
func kdlTokens(s string) []kdlToken {
	var out []kdlToken
	r := []rune(s)
	for i := 0; i < len(r); {
		c := r[i]
		switch {
		case c == '\n' || c == ';':
			out = append(out, kdlToken{kind: tokEnd})
			i++
		case c == '\\':
			// a line continuation: the node goes on past the newline
			i++
			for i < len(r) && r[i] != '\n' {
				i++
			}
			i++
		case unicode.IsSpace(c):
			i++
		case c == '/' && i+1 < len(r) && r[i+1] == '/':
			for i < len(r) && r[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(r) && r[i+1] == '*':
			nest := 0
			for i < len(r) {
				if r[i] == '/' && i+1 < len(r) && r[i+1] == '*' {
					nest++
					i += 2
				} else if r[i] == '*' && i+1 < len(r) && r[i+1] == '/' {
					nest--
					i += 2
					if nest == 0 {
						break
					}
				} else {
					i++
				}
			}
		case c == '/' && i+1 < len(r) && r[i+1] == '-':
			// slashdash: what follows is commented out, but reading it
			// finds no less
			i += 2
		case c == '(':
			for i < len(r) && r[i] != ')' {
				i++
			}
			i++
		case c == '{':
			out = append(out, kdlToken{kind: tokOpen})
			i++
		case c == '}':
			out = append(out, kdlToken{kind: tokClose})
			i++
		case c == '=':
			out = append(out, kdlToken{kind: tokEquals})
			i++
		case c == '"':
			text, next := quoted(r, i)
			out = append(out, kdlToken{kind: tokString, text: text, start: i, end: min(next, len(r))})
			i = next
		case c == '#' && i+1 < len(r) && (r[i+1] == '"' || r[i+1] == '#'):
			text, next := raw(r, i)
			out = append(out, kdlToken{kind: tokString, text: text, start: i, end: min(next, len(r))})
			i = next
		default:
			start := i
			for i < len(r) && !unicode.IsSpace(r[i]) && !strings.ContainsRune("{}();=\"\\", r[i]) &&
				!(r[i] == '/' && i+1 < len(r) && (r[i+1] == '/' || r[i+1] == '*')) {
				i++
			}
			if i == start {
				i++
				continue
			}
			out = append(out, kdlToken{kind: tokWord, text: string(r[start:i])})
		}
	}
	return out
}

// quoted reads a quoted string, or a multi-line one, from r[i], its
// escapes undone as far as names need.
func quoted(r []rune, i int) (string, int) {
	if i+2 < len(r) && r[i+1] == '"' && r[i+2] == '"' {
		end := strings.Index(string(r[i+3:]), `"""`)
		if end < 0 {
			return string(r[i+3:]), len(r)
		}
		body := []rune(string(r[i+3:])[:end])
		return string(body), i + 3 + len(body) + 3
	}
	var b strings.Builder
	i++
	for i < len(r) && r[i] != '"' {
		if r[i] == '\\' && i+1 < len(r) {
			i++
			switch r[i] {
			case 'n':
				b.WriteRune('\n')
			case 't':
				b.WriteRune('\t')
			default:
				b.WriteRune(r[i])
			}
			i++
			continue
		}
		b.WriteRune(r[i])
		i++
	}
	return b.String(), i + 1
}

// raw reads a raw string, #"…"#, with as many # as it opens with.
func raw(r []rune, i int) (string, int) {
	hashes := 0
	for i < len(r) && r[i] == '#' {
		hashes++
		i++
	}
	if i >= len(r) || r[i] != '"' {
		return "", i
	}
	multi := i+2 < len(r) && r[i+1] == '"' && r[i+2] == '"'
	open := 1
	if multi {
		open = 3
	}
	closer := strings.Repeat(`"`, open) + strings.Repeat("#", hashes)
	rest := string(r[i+open:])
	end := strings.Index(rest, closer)
	if end < 0 {
		return rest, len(r)
	}
	body := rest[:end]
	return body, i + open + len([]rune(body)) + len([]rune(closer))
}
