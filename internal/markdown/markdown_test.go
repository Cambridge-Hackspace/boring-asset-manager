package markdown

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func TestMarkdownRenders(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"heading", "# Motor", "<h1>Motor</h1>"},
		{"emphasis", "**bold** and _italic_ and ~~gone~~", "<strong>bold</strong> and <em>italic</em> and <del>gone</del>"},
		{"list", "- one\n- two", "<ul>\n<li>one</li>\n<li>two</li>\n</ul>"},
		{"ordered list", "3. three\n4. four", `<ol start="3">`},
		{"table", "| a | b |\n|:-|-:|\n| 1 | 2 |", `<th align="left">a</th>`},
		{"table body", "| a | b |\n|:-|-:|\n| 1 | 2 |", `<td align="right">2</td>`},
		{"code", "```go\nfmt.Println(\"<hi>\")\n```", `<pre><code class="language-go">fmt.Println(&#34;&lt;hi&gt;&#34;)`},
		{"inline code", "use `rm -rf`", "<code>rm -rf</code>"},
		{"link", "[the docs](https://example.com/docs)", `<a href="https://example.com/docs" rel="nofollow noreferrer">the docs</a>`},
		{"link within BAM!", "[the drill](/assets/abc)", `<a href="/assets/abc">the drill</a>`},
		{"mail link", "[mail](mailto:a@example.com)", `<a href="mailto:a@example.com">mail</a>`},
		{"bare link", "see https://example.com", `<a href="https://example.com" rel="nofollow noreferrer">https://example.com</a>`},
		{"image within BAM!", "![belt](/api/v1/assets/a/attachments/b)", `<img src="/api/v1/assets/a/attachments/b" alt="belt">`},
		{"task list", "- [x] done\n- [ ] to do", `<span class="task-mark" role="img" aria-label="Done">☑</span> done`},
		{"open task", "- [ ] to do", `<span class="task-mark" role="img" aria-label="Not done">☐</span> to do`},
		{"line break", "one\ntwo", "one<br>\ntwo"},
		{"quote", "> said", "<blockquote>\n<p>said</p>\n</blockquote>"},
		{"entities", "a &amp; b &lt;i&gt;", "a &amp; b &lt;i&gt;"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := Render(c.in); !strings.Contains(got, c.want) {
				t.Errorf("Render(%q) = %q, want it to hold %q", c.in, got, c.want)
			}
		})
	}
}

// TestRemoteImagesAreLinks: an image from another site is a link to it,
// which shows no one's address to that site.
func TestRemoteImagesAreLinks(t *testing.T) {
	for _, in := range []string{
		"![a belt](https://example.com/belt.png)",
		"![a belt](//example.com/belt.png)",
		`![a belt](/\example.com/belt.png)`,
		"![a belt](http://example.com/belt.png)",
	} {
		got := Render(in)
		if strings.Contains(got, "<img") {
			t.Errorf("Render(%q) = %q, showing an image from another site", in, got)
		}
		if !strings.Contains(got, "Image: a belt</a>") {
			t.Errorf("Render(%q) = %q, want a link to the image", in, got)
		}
	}
}

// TestMarkdownIsSanitized: whatever is written, nothing that runs, styles
// the page or frames another reaches the HTML.
func TestMarkdownIsSanitized(t *testing.T) {
	payloads := []string{
		`<input type="checkbox" onclick="alert(1)">`,
		`<span class="task-mark" role="img" aria-label="Done" onmouseover="alert(1)">x</span>`,
		`<input checked="" disabled="" type="checkbox"><script>alert(1)</script>`,
		`<script>alert(1)</script>`,
		"<SCRIPT SRC=//evil.example/x.js></SCRIPT>",
		`<img src=x onerror=alert(1)>`,
		`<p onclick="alert(1)">hi</p>`,
		`<a href="javascript:alert(1)">x</a>`,
		`[x](javascript:alert(1))`,
		`[x](JaVaScRiPt:alert(1))`,
		`[x](java&#x09;script:alert(1))`,
		`[x](&#106;avascript:alert(1))`,
		`[x](vbscript:msgbox(1))`,
		`[x](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)`,
		`![x](data:image/svg+xml;base64,PHN2ZyBvbmxvYWQ9YWxlcnQoMSk+)`,
		`![x](javascript:alert(1))`,
		`<a href="data:text/html,<script>alert(1)</script>">x</a>`,
		`<div style="background:url(javascript:alert(1))">x</div>`,
		`<style>body{display:none}</style>`,
		`<iframe src="https://evil.example"></iframe>`,
		`<object data="x.swf"></object><embed src="x.swf">`,
		`<svg onload=alert(1)><circle r=1 /></svg>`,
		`<svg><script>alert(1)</script></svg>`,
		`<math><mtext><table><mglyph><style><img src=x onerror=alert(1)>`,
		`<form action="https://evil.example"><input name=x></form>`,
		`<meta http-equiv="refresh" content="0;url=https://evil.example">`,
		`<base href="https://evil.example/">`,
		`<link rel=stylesheet href=https://evil.example/x.css>`,
		"&lt;script&gt;alert(1)&lt;/script&gt;",
		`&#60;script&#62;alert(1)&#60;/script&#62;`,
		`[x](https://example.com "\" onmouseover=\"alert(1)")`,
		`![x](/a.png "\" onerror=\"alert(1)")`,
		"<details open ontoggle=alert(1)>",
		"`<script>alert(1)</script>`",
		"```\n<script>alert(1)</script>\n```",
		`<input type="text" autofocus onfocus="alert(1)">`,
		"- [x] <img src=x onerror=alert(1)>",
	}
	for _, in := range payloads {
		got := Render(in)
		if bad := unsafeHTML(t, got); bad != "" {
			t.Errorf("Render(%q) = %q: %s", in, got, bad)
		}
	}
}

// allowed are the elements Render may make, with their attributes.
var allowed = map[string]map[string]bool{
	"p": {}, "br": {}, "hr": {}, "h1": {}, "h2": {}, "h3": {}, "h4": {}, "h5": {}, "h6": {}, "blockquote": {}, "pre": {},
	"code": {"class": true}, "em": {}, "strong": {}, "del": {}, "ul": {}, "ol": {"start": true}, "li": {},
	"table": {}, "thead": {}, "tbody": {}, "tr": {}, "th": {"align": true}, "td": {"align": true},
	"a": {"href": true, "title": true, "rel": true}, "img": {"src": true, "alt": true, "title": true},
	"span": {"class": true, "role": true, "aria-label": true},
}

// taskMark are the only values a span's attributes may have: a task list's
// mark.
var taskMark = map[string]bool{"class=task-mark": true, "role=img": true, "aria-label=Done": true, "aria-label=Not done": true}

// unsafeHTML parses HTML as a browser would, and says what in it Render
// should never make: an element or attribute it does not allow, a link or
// image to anything but http, https, mailto or a path within BAM!, or a
// span that is not a task list's mark.
func unsafeHTML(t *testing.T, s string) string {
	t.Helper()
	nodes, err := html.ParseFragment(strings.NewReader(s), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		t.Fatal(err)
	}
	var check func(n *html.Node) string
	check = func(n *html.Node) string {
		if n.Type == html.ElementNode {
			attrs, ok := allowed[n.Data]
			if !ok {
				return "element " + n.Data
			}
			for _, a := range n.Attr {
				if !attrs[a.Key] {
					return "attribute " + a.Key + " on " + n.Data
				}
				if a.Key == "href" || a.Key == "src" {
					v := strings.ToLower(strings.TrimSpace(a.Val))
					if !(strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") || strings.HasPrefix(v, "mailto:") || Local(v)) {
						return a.Key + " " + a.Val
					}
					if a.Key == "src" && !Local(v) {
						return "an image from another site"
					}
				}
				if n.Data == "span" && !taskMark[a.Key+"="+a.Val] {
					return "a span with " + a.Key + "=" + a.Val
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if bad := check(c); bad != "" {
				return bad
			}
		}
		return ""
	}
	for _, n := range nodes {
		if bad := check(n); bad != "" {
			return bad
		}
	}
	return ""
}

func TestLocal(t *testing.T) {
	for url, want := range map[string]bool{
		"/api/v1/assets/a/attachments/b": true,
		"/assets/a":                      true,
		"//evil.example/x":               false,
		`/\evil.example/x`:               false,
		"https://example.com":            false,
		"x.png":                          false,
		"":                               false,
	} {
		if got := Local(url); got != want {
			t.Errorf("Local(%q) = %v, want %v", url, got, want)
		}
	}
}
