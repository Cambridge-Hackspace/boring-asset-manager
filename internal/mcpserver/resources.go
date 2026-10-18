package mcpserver

import (
	"context"
	_ "embed"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/help"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed guide.md
var guide string

// GuideURI is the orientation the server's instructions point to.
const GuideURI = "bam://guide"

// addResources adds the guide and the app's help, the same topics people
// read at /help, to a server.
func addResources(s *mcp.Server) {
	text := func(uri, body string) mcp.ResourceHandler {
		return func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "text/markdown", Text: body}}}, nil
		}
	}
	s.AddResource(&mcp.Resource{
		URI: GuideURI, Name: "guide", Title: "Guide to BAM!", MIMEType: "text/markdown",
		Description: "What BAM!'s ideas mean: assets, categories and inherited fields, scopes, places, codes, lending, labels and the print queue. Read it first.",
	}, text(GuideURI, guide))
	for _, t := range help.Topics() {
		uri := "bam://help/" + t.Slug
		s.AddResource(&mcp.Resource{
			URI: uri, Name: "help-" + t.Slug, Title: "Help: " + t.Title, MIMEType: "text/markdown",
			Description: "The app's own help on " + t.Title + ", as people read it at /help/" + t.Slug + ".",
		}, text(uri, "# "+t.Title+"\n"+t.Body))
	}
}
