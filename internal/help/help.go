// Package help holds the user help (README §6.5): Markdown topics the web
// app bundles and shows at /help, and the MCP server offers assistants, so
// both read the same words.
package help

import (
	"embed"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed topics/*.md
var files embed.FS

// Topic is one help topic.
type Topic struct {
	Slug  string
	Title string
	// Body is the topic's Markdown, without its title.
	Body string
}

// Topics are every topic, by slug.
func Topics() []Topic {
	names, _ := fs.Glob(files, "topics/*.md")
	sort.Strings(names)
	var out []Topic
	for _, name := range names {
		data, _ := files.ReadFile(name)
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		first, rest, _ := strings.Cut(text, "\n")
		out = append(out, Topic{
			Slug:  strings.TrimSuffix(path.Base(name), ".md"),
			Title: strings.TrimSpace(strings.TrimPrefix(first, "#")),
			Body:  rest,
		})
	}
	return out
}
