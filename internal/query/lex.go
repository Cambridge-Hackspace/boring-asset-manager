// Package query is the asset search language (docs/data-model.md#queries):
// comparisons such as `field.diameter >= 1.75`, joined by AND, OR and NOT,
// with plain words among them, and an ORDER BY. It parses a query, checks
// it against the instance's fields, and matches assets against it in Go,
// as plain search does, so it builds no SQL and reads only what the API
// has already let the requester see.
package query

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Error is a query that cannot be run, with where in it the problem is:
// Start and End are offsets in UTF-16 code units, as a browser counts a
// string's characters, so a page can underline the part at fault.
type Error struct {
	Message    string
	Start, End int
}

func (e *Error) Error() string { return e.Message }

// span is a stretch of the query, in UTF-16 code units.
type span struct{ start, end int }

func errAt(s span, format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...), Start: s.start, End: s.end}
}

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokWord
	tokString
	tokOp
	tokLParen
	tokRParen
	tokComma
)

type token struct {
	kind tokenKind
	text string // a word as written, a string's contents, or an operator
	span span
}

// keyword reports whether t is the unquoted keyword kw, in any case.
func (t token) keyword(kw string) bool {
	return t.kind == tokWord && strings.EqualFold(t.text, kw)
}

// operators are the comparisons, longest first so that `<=` is not read
// as `<` then `=`. `~~` matches through related terms, and despite typing
// mistakes (#386).
var operators = []string{"!=", "!~", "<=", ">=", "~~", "=", "~", "<", ">"}

// wordBreak reports whether r ends a word: space, or a character with a
// meaning of its own.
func wordBreak(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune(`()=!~<>,"'`, r)
}

// lex splits a query into tokens. On an error it returns those before it.
func lex(input string) ([]token, error) {
	var toks []token
	pos := 0 // in UTF-16 code units
	units := func(s string) int {
		n := 0
		for _, r := range s {
			if r >= 0x10000 {
				n += 2
			} else {
				n++
			}
		}
		return n
	}
	for i := 0; i < len(input); {
		r, size := utf8.DecodeRuneInString(input[i:])
		switch {
		case unicode.IsSpace(r):
			i += size
			pos += units(string(r))
			continue
		case r == '(' || r == ')' || r == ',':
			kind := map[rune]tokenKind{'(': tokLParen, ')': tokRParen, ',': tokComma}[r]
			toks = append(toks, token{kind: kind, text: string(r), span: span{pos, pos + 1}})
			i++
			pos++
			continue
		case r == '"' || r == '\'':
			var b strings.Builder
			j := i + 1
			closed := false
			for j < len(input) {
				c, cs := utf8.DecodeRuneInString(input[j:])
				if c == '\\' && j+cs < len(input) {
					n, ns := utf8.DecodeRuneInString(input[j+cs:])
					b.WriteRune(n)
					j += cs + ns
					continue
				}
				j += cs
				if c == r {
					closed = true
					break
				}
				b.WriteRune(c)
			}
			end := pos + units(input[i:j])
			if !closed {
				return toks, errAt(span{pos, end}, "this quote is never closed: end it with %c", r)
			}
			toks = append(toks, token{kind: tokString, text: b.String(), span: span{pos, end}})
			pos = end
			i = j
			continue
		}
		if op := operatorAt(input[i:]); op != "" {
			toks = append(toks, token{kind: tokOp, text: op, span: span{pos, pos + len(op)}})
			i += len(op)
			pos += len(op)
			continue
		}
		if r == '!' {
			return toks, errAt(span{pos, pos + 1}, "! is only part of != and !~: write NOT to leave something out")
		}
		j := i
		for j < len(input) {
			c, cs := utf8.DecodeRuneInString(input[j:])
			if wordBreak(c) {
				break
			}
			j += cs
		}
		word := input[i:j]
		toks = append(toks, token{kind: tokWord, text: word, span: span{pos, pos + units(word)}})
		pos += units(word)
		i = j
	}
	return append(toks, token{kind: tokEOF, span: span{pos, pos}}), nil
}

func operatorAt(s string) string {
	for _, op := range operators {
		if strings.HasPrefix(s, op) {
			return op
		}
	}
	return ""
}

// IsQuery reports whether a search is a query rather than plain words: it
// holds a comparison, or ORDER BY, outside quotes. Plain words, however
// many, are found as one phrase, as search always found them.
func IsQuery(input string) bool {
	// A query with a mistake later on, such as an unclosed quote, is still
	// a query, so that the mistake is shown rather than searched for.
	toks, _ := lex(input)
	for i, t := range toks {
		if t.kind == tokOp || (t.keyword("order") && i+1 < len(toks) && toks[i+1].keyword("by")) {
			return true
		}
	}
	return false
}
