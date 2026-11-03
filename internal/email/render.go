package email

import (
	"bytes"
	"embed"
	htmltemplate "html/template"
	"regexp"
	texttemplate "text/template"
)

//go:embed templates
var templateFiles embed.FS

var (
	htmlLayout = htmltemplate.Must(htmltemplate.ParseFS(templateFiles, "templates/layout.html"))
	textLayout = texttemplate.Must(texttemplate.ParseFS(templateFiles, "templates/layout.txt"))
)

// Content is what every email says, laid out the same way: a title, its
// paragraphs, a link to act on, and a footer saying why it was sent.
type Content struct {
	Title      string
	Paragraphs []string
	// Link and LinkLabel are a button in HTML and a line in text.
	Link      string
	LinkLabel string
	// Accent colors the bar above the title, as "#rrggbb".
	Accent string
	Footer string
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Render lays c out as HTML and as plain text. Everything in c is text:
// the HTML escapes it, so a name cannot add markup.
func Render(c Content) (html, text string, err error) {
	if !hexColor.MatchString(c.Accent) {
		c.Accent = "#6b7280"
	}
	if c.LinkLabel == "" {
		c.LinkLabel = "Open in BAM!"
	}
	var h, t bytes.Buffer
	if err := htmlLayout.ExecuteTemplate(&h, "layout.html", c); err != nil {
		return "", "", err
	}
	if err := textLayout.ExecuteTemplate(&t, "layout.txt", c); err != nil {
		return "", "", err
	}
	return h.String(), t.String(), nil
}
