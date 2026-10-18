package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/mcpserver"
)

// mcpHandler serves the MCP endpoint, /api/v1/mcp (README §6.6): tools for
// assistants, acting as the API token they present. Every tool asks the
// handlers here, in process, so it is held to the same rules as any request.
func (s *Server) mcpHandler() http.Handler {
	if !s.cfg.MCP.Enabled {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSONError(w, http.StatusNotFound, "MCP is turned off on this server ([mcp] enabled in its configuration)")
		})
	}
	return mcpserver.Handler(mcpserver.Options{Version: Version, Identify: s.mcpCaller})
}

// mcpCaller is the API token an MCP request presents, and a way to ask the
// API as it. A session cookie is not enough: MCP clients do not carry one,
// and a browser should never be the one calling.
func (s *Server) mcpCaller(w http.ResponseWriter, r *http.Request) (mcpserver.Caller, mcpserver.Backend, bool) {
	t := CurrentToken(r.Context())
	if t == nil {
		// Where to sign in, for an assistant that can (README §6.6).
		w.Header().Set("WWW-Authenticate", `Bearer realm="BAM!", resource_metadata="`+s.resourceMetadataURL()+`"`)
		writeJSONError(w, http.StatusUnauthorized, "MCP needs an API token, sent as Authorization: Bearer bam_…")
		return mcpserver.Caller{}, nil, false
	}
	authorization := r.Header.Get("Authorization")
	api := func(ctx context.Context, method, path string, body []byte, contentType string) mcpserver.Response {
		req, err := http.NewRequestWithContext(ctx, method, path, bytes.NewReader(body))
		if err != nil {
			msg, _ := json.Marshal(ErrorResponse{Error: err.Error()})
			return mcpserver.Response{Status: http.StatusBadRequest, Body: msg}
		}
		req.RequestURI = path
		req.RemoteAddr = r.RemoteAddr
		req.Host = r.Host
		req.Header.Set("Authorization", authorization)
		if contentType != "" && body != nil {
			req.Header.Set("Content-Type", contentType)
		}
		rec := &recorder{header: http.Header{}}
		s.httpServer.Handler.ServeHTTP(rec, req)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		return mcpserver.Response{Status: rec.status, Header: rec.header, Body: rec.body.Bytes()}
	}
	caller := mcpserver.Caller{Kind: "token", ID: t.ID, Name: t.Name, Permissions: UserPermissions(r.Context())}
	if u := CurrentUser(r.Context()); u != nil {
		caller = mcpserver.Caller{Kind: "person", ID: u.ID, Name: u.Name, Token: t.Name, Permissions: UserPermissions(r.Context())}
		if caller.Name == "" {
			caller.Name = "Unnamed user"
		}
	}
	return caller, api, true
}

// recorder keeps a response made in process.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(b)
}
