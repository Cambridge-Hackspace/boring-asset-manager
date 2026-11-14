package query

import "strings"

// The grammar (docs/data-model.md#queries):
//
//	query      = [ or ] [ ORDER BY order { "," order } ]
//	or         = and { OR and }
//	and        = not { AND not }
//	not        = NOT not | primary
//	primary    = "(" or ")" | comparison | term
//	comparison = key op value
//	           | key [ NOT ] IN "(" value { "," value } ")"
//	           | key IS [ NOT ] EMPTY
//	term       = word | string           (plain search, as the search box)
//	order      = key [ ASC | DESC ]
//	op         = "=" | "!=" | "~" | "!~" | "<" | ">" | "<=" | ">="
//
// Keywords are unquoted words in any case. A word or string followed by an
// operator, IN or IS is a key; otherwise it is a term.

// Node is a part of a query's condition.
type Node interface{ nodeSpan() span }

// And matches what both sides match; Or what either does; Not what X
// does not.
type And struct{ L, R Node }
type Or struct{ L, R Node }
type Not struct {
	X Node
	s span
}

// Term is plain words, matched as the search box matches them.
type Term struct {
	Text string
	s    span
}

// Key names what a comparison looks at: name, field.diameter, and so on,
// lower-cased.
type Key struct {
	Name string
	s    span
}

// Value is a value as written: Quoted when it was in quotes, so that
// EMPTY or me in quotes are only words.
type Value struct {
	Text   string
	Quoted bool
	s      span
}

// Compare is key op value; In is key IN (values), Negate for NOT IN;
// Empty is key IS EMPTY, Negate for IS NOT EMPTY.
type Compare struct {
	Key   Key
	Op    string
	Value Value
	s     span
}
type In struct {
	Key    Key
	Values []Value
	Negate bool
	s      span
}
type Empty struct {
	Key    Key
	Negate bool
	s      span
}

func (n And) nodeSpan() span     { return span{n.L.nodeSpan().start, n.R.nodeSpan().end} }
func (n Or) nodeSpan() span      { return span{n.L.nodeSpan().start, n.R.nodeSpan().end} }
func (n Not) nodeSpan() span     { return n.s }
func (n Term) nodeSpan() span    { return n.s }
func (n Compare) nodeSpan() span { return n.s }
func (n In) nodeSpan() span      { return n.s }
func (n Empty) nodeSpan() span   { return n.s }

// Order is one key of an ORDER BY.
type Order struct {
	Key  Key
	Desc bool
}

// Query is a parsed query: Where is nil when it matches everything.
type Query struct {
	Where Node
	Order []Order
}

type parser struct {
	toks []token
	i    int
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token {
	t := p.toks[p.i]
	if t.kind != tokEOF {
		p.i++
	}
	return t
}

// Parse reads a query. Its errors are *Error.
func Parse(input string) (*Query, error) {
	toks, err := lex(input)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	q := &Query{}
	if !p.atOrder() && p.peek().kind != tokEOF {
		if q.Where, err = p.or(); err != nil {
			return nil, err
		}
	}
	if p.atOrder() {
		p.next()
		p.next()
		for {
			t := p.next()
			if t.kind != tokWord && t.kind != tokString {
				return nil, errAt(t.span, "ORDER BY needs what to sort by, such as name or field.bought")
			}
			o := Order{Key: Key{Name: strings.ToLower(t.text), s: t.span}}
			if p.peek().keyword("asc") {
				p.next()
			} else if p.peek().keyword("desc") {
				p.next()
				o.Desc = true
			}
			q.Order = append(q.Order, o)
			if p.peek().kind != tokComma {
				break
			}
			p.next()
		}
	}
	if t := p.peek(); t.kind != tokEOF {
		switch {
		case t.kind == tokRParen:
			return nil, errAt(t.span, "this ) closes nothing")
		case t.keyword("order"):
			return nil, errAt(t.span, "ORDER needs BY after it")
		default:
			return nil, errAt(t.span, "expected AND, OR or ORDER BY before %s", describe(t))
		}
	}
	return q, nil
}

func (p *parser) atOrder() bool {
	return p.peek().keyword("order") && p.toks[p.i+1].keyword("by")
}

func (p *parser) or() (Node, error) {
	l, err := p.and()
	if err != nil {
		return nil, err
	}
	for p.peek().keyword("or") {
		p.next()
		r, err := p.and()
		if err != nil {
			return nil, err
		}
		l = Or{L: l, R: r}
	}
	return l, nil
}

func (p *parser) and() (Node, error) {
	l, err := p.not()
	if err != nil {
		return nil, err
	}
	for p.peek().keyword("and") {
		p.next()
		r, err := p.not()
		if err != nil {
			return nil, err
		}
		l = And{L: l, R: r}
	}
	return l, nil
}

func (p *parser) not() (Node, error) {
	if t := p.peek(); t.keyword("not") {
		p.next()
		x, err := p.not()
		if err != nil {
			return nil, err
		}
		return Not{X: x, s: span{t.span.start, x.nodeSpan().end}}, nil
	}
	return p.primary()
}

// reserved are the keywords that cannot start a term unquoted.
var reserved = []string{"and", "or", "not", "in", "is", "empty", "order", "by", "asc", "desc"}

func isReserved(t token) bool {
	for _, kw := range reserved {
		if t.keyword(kw) {
			return true
		}
	}
	return false
}

func (p *parser) primary() (Node, error) {
	t := p.next()
	switch t.kind {
	case tokLParen:
		x, err := p.or()
		if err != nil {
			return nil, err
		}
		if c := p.next(); c.kind != tokRParen {
			return nil, errAt(span{t.span.start, c.span.end}, "this ( is never closed: end it with )")
		}
		return x, nil
	case tokWord, tokString:
		// A keyword is a key where a comparison follows it, as in
		// `in ~ "shelf a"`.
		n := p.peek()
		compared := n.kind == tokOp || n.keyword("is") || n.keyword("in") || (n.keyword("not") && p.toks[p.i+1].keyword("in"))
		if t.kind == tokWord && isReserved(t) && !compared {
			return nil, errAt(t.span, "expected something to search for before %s", strings.ToUpper(t.text))
		}
	default:
		return nil, errAt(t.span, "expected something to search for, not %s", describe(t))
	}
	key := Key{Name: strings.ToLower(t.text), s: t.span}
	switch n := p.peek(); {
	case n.kind == tokOp:
		p.next()
		if n.text == "~~" {
			return nil, errAt(n.span, "~~, matching through related words, isn't supported yet: use ~")
		}
		v, err := p.value("a value after " + n.text)
		if err != nil {
			return nil, err
		}
		return Compare{Key: key, Op: n.text, Value: v, s: span{t.span.start, v.s.end}}, nil
	case n.keyword("in"), n.keyword("not") && p.toks[p.i+1].keyword("in"):
		negate := n.keyword("not")
		if negate {
			p.next()
		}
		p.next()
		open := p.next()
		if open.kind != tokLParen {
			return nil, errAt(open.span, "IN needs a list in parentheses, such as (ok, out_of_service)")
		}
		var vals []Value
		for {
			v, err := p.value("a value in the list")
			if err != nil {
				return nil, err
			}
			vals = append(vals, v)
			c := p.next()
			if c.kind == tokRParen {
				return In{Key: key, Values: vals, Negate: negate, s: span{t.span.start, c.span.end}}, nil
			}
			if c.kind != tokComma {
				return nil, errAt(c.span, "expected , or ) in the list, not %s", describe(c))
			}
		}
	case n.keyword("is"):
		p.next()
		negate := false
		if p.peek().keyword("not") {
			p.next()
			negate = true
		}
		e := p.next()
		if !e.keyword("empty") {
			return nil, errAt(e.span, "IS needs EMPTY or NOT EMPTY after it")
		}
		return Empty{Key: key, Negate: negate, s: span{t.span.start, e.span.end}}, nil
	}
	return Term{Text: t.text, s: t.span}, nil
}

func (p *parser) value(what string) (Value, error) {
	t := p.next()
	if t.kind == tokString || (t.kind == tokWord && !isReserved(t)) {
		return Value{Text: t.text, Quoted: t.kind == tokString, s: t.span}, nil
	}
	return Value{}, errAt(t.span, "expected %s, not %s", what, describe(t))
}

// describe names a token for an error message.
func describe(t token) string {
	switch t.kind {
	case tokEOF:
		return "the end"
	case tokString:
		return `"` + t.text + `"`
	case tokWord:
		if isReserved(t) {
			return strings.ToUpper(t.text)
		}
	}
	return t.text
}
