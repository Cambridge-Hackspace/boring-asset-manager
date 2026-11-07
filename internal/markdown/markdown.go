// Package markdown renders the Markdown people write, such as comments
// (docs/comments.md), to HTML that is safe to show in the web app: GitHub's
// flavor, with headings, lists, tables, code, links, images and task lists,
// then passed through an allowlist sanitizer, so that the page's Content
// Security Policy is not the only defense.
package markdown

import (
	"bytes"
	stdhtml "html"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmrenderer "github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// renderer is goldmark with GitHub's extensions. HTML written in the
// Markdown is never passed through: goldmark leaves it out. Line breaks
// within a paragraph are kept, as in comments elsewhere.
var renderer = goldmark.New(
	goldmark.WithExtensions(
		extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute)),
		extension.Strikethrough,
		extension.Linkify,
		extension.TaskList,
	),
	goldmark.WithParserOptions(
		parser.WithASTTransformers(util.Prioritized(remoteImages{}, 100)),
		parser.WithInlineParsers(util.Prioritized(mentionParser{}, 250)),
	),
	goldmark.WithRendererOptions(html.WithHardWraps(), gmrenderer.WithNodeRenderers(util.Prioritized(mentionRenderer{}, 100))),
)

// policy is what the HTML may hold: the elements GitHub's Markdown makes,
// links to http, https and mailto addresses or within BAM!, images from
// within BAM!, the disabled checkboxes of task lists, and mentions' spans.
// Everything else, scripts, event handlers, styles, frames and SVG among
// it, is removed.
var policy = func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("p", "br", "hr", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "pre", "code",
		"em", "strong", "del", "ul", "ol", "li", "table", "thead", "tbody", "tr", "th", "td")
	p.AllowAttrs("href", "title").OnElements("a")
	p.AllowAttrs("src", "alt", "title").OnElements("img")
	p.AllowURLSchemes("http", "https", "mailto")
	p.AllowRelativeURLs(true)
	p.RequireNoReferrerOnFullyQualifiedLinks(true)
	p.RequireNoFollowOnFullyQualifiedLinks(true)
	p.AllowAttrs("start").Matching(regexp.MustCompile(`^[0-9]{1,9}$`)).OnElements("ol")
	p.AllowAttrs("align").Matching(regexp.MustCompile(`^(left|center|right)$`)).OnElements("th", "td")
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^language-[A-Za-z0-9_+.#-]{1,40}$`)).OnElements("code")
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^mention$`)).OnElements("span")
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	p.AllowAttrs("checked", "disabled").Matching(regexp.MustCompile(`^(|checked|disabled)$`)).OnElements("input")
	return p
}()

// Render renders Markdown to sanitized HTML, each mention reading as
// Someone.
func Render(source string) string { return RenderWith(source, nil) }

// RenderWith renders Markdown to sanitized HTML, naming each person
// mentioned by names, given their user ID; "" reads as Someone.
func RenderWith(source string, names func(id string) string) string {
	var buf bytes.Buffer
	pc := parser.NewContext()
	pc.Set(namesKey, names)
	if err := renderer.Convert([]byte(source), &buf, parser.WithContext(pc)); err != nil {
		// goldmark fails only when writing fails, which a buffer does not;
		// the text alone, escaped, is still worth showing.
		return policy.Sanitize("<p>" + stdhtml.EscapeString(source) + "</p>")
	}
	return tasks.Replace(policy.Sanitize(buf.String()))
}

// tasks turns a task list's checkboxes, as the sanitizer writes them, into
// marks that say whether each is done. They are never ticked from the
// comment, so they are not controls, which a reader would try to press,
// and too small to press besides. Only these exact strings, which the
// sanitized HTML holds for nothing else, are replaced, by fixed ones.
var tasks = strings.NewReplacer(
	`<input checked="" disabled="" type="checkbox">`, `<span class="task-mark" role="img" aria-label="Done">☑</span>`,
	`<input disabled="" type="checkbox">`, `<span class="task-mark" role="img" aria-label="Not done">☐</span>`,
)

// Local reports whether a URL is within BAM!: a path from its root, and not
// one that a browser reads as another host, as //host and /\host are.
func Local(url string) bool {
	return strings.HasPrefix(url, "/") && !strings.HasPrefix(url, "//") && !strings.HasPrefix(url, `/\`)
}

// remoteImages turns images from other sites into links to them. Showing
// one would tell that site the address of everyone who reads the comment,
// and the web app's Content Security Policy would not load it anyway;
// images within BAM!, such as attachments, are shown.
type remoteImages struct{}

func (remoteImages) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	var images []*ast.Image
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if img, ok := n.(*ast.Image); ok && entering && !Local(string(img.Destination)) {
			images = append(images, img)
		}
		return ast.WalkContinue, nil
	})
	for _, img := range images {
		link := ast.NewLink()
		link.Destination, link.Title = img.Destination, img.Title
		alt := strings.TrimSpace(string(img.Text(source)))
		if alt == "" {
			alt = string(img.Destination)
		}
		link.AppendChild(link, ast.NewString([]byte("Image: "+alt)))
		img.Parent().ReplaceChild(img.Parent(), img, link)
	}
}
