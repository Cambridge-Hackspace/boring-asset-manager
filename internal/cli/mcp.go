package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP relays the Model Context Protocol between in and out (stdin and stdout)
// and a BAM! server's MCP endpoint, as the API token given, for assistants
// that start local programs rather than reach URLs: bam mcp (README §6.6).
// It ends when in does.
func MCP(ctx context.Context, server, token string, in io.ReadCloser, out io.WriteCloser) error {
	if strings.TrimSpace(server) == "" {
		return errors.New("--url is required: the address of the BAM! server, such as https://bam.example.org")
	}
	if token == "" {
		return errors.New("no API token: put one in the environment variable --token-env names (BAM_TOKEN unless given)")
	}
	base := strings.TrimRight(server, "/")
	client := &http.Client{Transport: bearer{token: token, next: http.DefaultTransport}}
	if err := checkMCP(ctx, client, base); err != nil {
		return err
	}

	remote, err := (&mcp.StreamableClientTransport{Endpoint: base + "/api/v1/mcp", HTTPClient: client, DisableStandaloneSSE: true}).Connect(ctx)
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", base, err)
	}
	defer remote.Close()
	local, err := (&mcp.IOTransport{Reader: in, Writer: out}).Connect(ctx)
	if err != nil {
		return err
	}
	defer local.Close()

	done := make(chan error, 2)
	relay := func(from, to mcp.Connection) {
		for {
			msg, err := from.Read(ctx)
			if err != nil {
				done <- err
				return
			}
			if err := to.Write(ctx, msg); err != nil {
				done <- err
				return
			}
		}
	}
	go relay(local, remote)
	go relay(remote, local)
	if err := <-done; err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
		return err
	}
	return nil
}

// checkMCP asks the server whether it serves MCP, to say so plainly rather
// than fail on the first message.
func checkMCP(ctx context.Context, client *http.Client, base string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/info", nil)
	if err != nil {
		return fmt.Errorf("--url %q: %w", base, err)
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("reaching %s: %w", base, err)
	}
	defer res.Body.Close()
	var info struct {
		MCP *bool `json:"mcp"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&info) != nil {
		return fmt.Errorf("%s does not answer as a BAM! server (GET /api/v1/info: %s)", base, res.Status)
	}
	if info.MCP != nil && !*info.MCP {
		return fmt.Errorf("%s has MCP turned off ([mcp] enabled = false in its configuration)", base)
	}
	return nil
}

// bearer sends the API token with every request.
type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}
