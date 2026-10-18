package integration

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/cli"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPRelay drives the server's MCP endpoint through bam mcp's relay, as
// an assistant that starts local programs would: JSON-RPC on the relay's
// stdin and stdout, the server over HTTP.
func TestMCPRelay(t *testing.T) {
	e := newEnv(t)
	admin, err := e.st.CreateAdminUser("ada@hackspace.org", password)
	if err != nil {
		t.Fatal(err)
	}
	role, err := e.st.CreateRole(store.RoleInput{Name: "Assistants", Permissions: []string{"assets.read"}})
	if err != nil {
		t.Fatal(err)
	}
	_, secret, err := e.st.CreateToken("Relay", []string{role.ID}, nil, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	toRelay, clientOut := io.Pipe()
	clientIn, fromRelay := io.Pipe()
	relayed := make(chan error, 1)
	go func() { relayed <- cli.MCP(ctx, e.srv.URL, secret, toRelay, fromRelay) }()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "relay test", Version: "1"}, nil).
		Connect(ctx, &mcp.IOTransport{Reader: clientIn, Writer: clientOut}, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "whoami"})
	if err != nil || res.IsError {
		t.Fatalf("whoami: %v %+v", err, res)
	}
	if text := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, `"Relay"`) {
		t.Errorf("whoami: %s", text)
	}
	guide, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "bam://guide"})
	if err != nil || !strings.HasPrefix(guide.Contents[0].Text, "# Guide to BAM!") {
		t.Errorf("guide: %v", err)
	}

	// Closing the client's end ends the relay, without error.
	cs.Close()
	clientOut.Close()
	select {
	case err := <-relayed:
		if err != nil {
			t.Errorf("relay ended with %v", err)
		}
	case <-ctx.Done():
		t.Error("the relay did not end")
	}

	// Plain refusals before relaying anything.
	if err := cli.MCP(ctx, e.srv.URL, "", io.NopCloser(strings.NewReader("")), nopWriter{}); err == nil || !strings.Contains(err.Error(), "no API token") {
		t.Errorf("without a token: %v", err)
	}
	if err := cli.MCP(ctx, e.srv.URL+"/nothing-here", secret, io.NopCloser(strings.NewReader("")), nopWriter{}); err == nil || !strings.Contains(err.Error(), "BAM! server") {
		t.Errorf("not BAM!: %v", err)
	}
}

type nopWriter struct{}

func (nopWriter) Write(b []byte) (int, error) { return len(b), nil }
func (nopWriter) Close() error                { return nil }
