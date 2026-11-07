package markdown

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	gmrenderer "github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Mentions (docs/comments.md#mentions): <@id> names a person by their
// user ID, so that a mention follows them when they are renamed. It is
// rendered as @ and their name as it is when read, and nothing in code
// spans or blocks is a mention.

// mentionPattern is a mention as written: <@ and a user's ID, a UUID.
var mentionPattern = regexp.MustCompile(`^<@([0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12})>`)

// Mention writes the mention of a person, by their user ID.
func Mention(id string) string { return "<@" + id + ">" }

// KindMention is the kind of a Mention node.
var KindMention = ast.NewNodeKind("Mention")

// mentionNode is a mention in a parsed document: the person's ID, and the
// name it is rendered with.
type mentionNode struct {
	ast.BaseInline
	ID, Name string
}

func (n *mentionNode) Kind() ast.NodeKind { return KindMention }

func (n *mentionNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"ID": n.ID, "Name": n.Name}, nil)
}

// namesKey holds, in a parse's context, how to name the people mentioned.
var namesKey = parser.NewContextKey()

// mentionParser reads <@id>. It is tried before autolinks and raw HTML,
// which <@ never begins.
type mentionParser struct{}

func (mentionParser) Trigger() []byte { return []byte{'<'} }

func (mentionParser) Parse(_ ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	m := mentionPattern.FindSubmatch(line)
	if m == nil {
		return nil
	}
	block.Advance(len(m[0]))
	n := &mentionNode{ID: strings.ToLower(string(m[1]))}
	if names, ok := pc.Get(namesKey).(func(string) string); ok && names != nil {
		n.Name = names(n.ID)
	}
	return n
}

// mentionRenderer writes a mention as a span holding @ and the name,
// escaped; the sanitizer lets the span through with this class alone.
type mentionRenderer struct{}

func (mentionRenderer) RegisterFuncs(reg gmrenderer.NodeRendererFuncRegisterer) {
	reg.Register(KindMention, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		name := node.(*mentionNode).Name
		if strings.TrimSpace(name) == "" {
			name = Someone
		}
		_, _ = w.WriteString(`<span class="mention">@`)
		_, _ = w.Write(util.EscapeHTML([]byte(name)))
		_, _ = w.WriteString(`</span>`)
		return ast.WalkSkipChildren, nil
	})
}

// Someone is how a mention reads to those who may not see who it names,
// and for a person who is gone.
const Someone = "someone"

// Mentions lists the user IDs a text mentions, each once, in the order
// first mentioned. Mentions in code are not mentions.
func Mentions(source string) []string {
	doc := renderer.Parser().Parse(text.NewReader([]byte(source)))
	seen := map[string]bool{}
	var out []string
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if m, ok := n.(*mentionNode); ok && entering && !seen[m.ID] {
			seen[m.ID] = true
			out = append(out, m.ID)
		}
		return ast.WalkContinue, nil
	})
	return out
}

// PlainMentions writes the mentions in a text as @ and a name, for plain
// text such as an email: names gives each person's name, or "" for
// Someone. A mention in code keeps its text.
func PlainMentions(source string, names func(string) string) string {
	src := []byte(source)
	if !mentionAnywhere.Match(src) {
		return source
	}
	doc := renderer.Parser().Parse(text.NewReader(src))
	// Code holds <@id> as text, so the code's ranges are skipped.
	code := codeRanges(doc, src)
	var b bytes.Buffer
	last := 0
	for _, loc := range mentionAnywhere.FindAllSubmatchIndex(src, -1) {
		if inRanges(code, loc[0]) {
			continue
		}
		b.Write(src[last:loc[0]])
		name := ""
		if names != nil {
			name = names(strings.ToLower(string(src[loc[2]:loc[3]])))
		}
		if strings.TrimSpace(name) == "" {
			name = Someone
		}
		b.WriteString("@" + name)
		last = loc[1]
	}
	b.Write(src[last:])
	return b.String()
}

// mentionAnywhere is mentionPattern anywhere in a text.
var mentionAnywhere = regexp.MustCompile(mentionPattern.String()[1:])

// codeRanges are the byte ranges of a document's code spans and blocks.
func codeRanges(doc ast.Node, src []byte) [][2]int {
	var out [][2]int
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindCodeSpan:
			start, stop := -1, -1
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					if start < 0 {
						start = t.Segment.Start
					}
					stop = t.Segment.Stop
				}
			}
			if start >= 0 {
				out = append(out, [2]int{start, stop})
			}
			return ast.WalkSkipChildren, nil
		case ast.KindCodeBlock, ast.KindFencedCodeBlock:
			lines := n.Lines()
			if lines.Len() > 0 {
				out = append(out, [2]int{lines.At(0).Start, lines.At(lines.Len() - 1).Stop})
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return out
}

func inRanges(ranges [][2]int, at int) bool {
	for _, r := range ranges {
		if at >= r[0] && at < r[1] {
			return true
		}
	}
	return false
}
