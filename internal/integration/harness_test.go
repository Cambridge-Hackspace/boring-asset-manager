// Package integration drives a real BAM server over HTTP, on SQLite or on the
// PostgreSQL or MariaDB server named by BAM_TEST_DIALECT and BAM_TEST_DSN
// (README §8.3), to check behavior that spans the API, the store and the
// database together.
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/api"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

const password = "Pass1234!"

// env is a running server on a fresh database.
type env struct {
	t   *testing.T
	st  *store.Store
	srv *httptest.Server
}

func newEnv(t *testing.T) *env {
	t.Helper()
	var st *store.Store
	if dialect := os.Getenv(store.TestDialectEnv); dialect != "" && dialect != "sqlite" {
		s, cleanup, err := store.OpenEphemeral(dialect, os.Getenv(store.TestDSNEnv))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(cleanup)
		st = s
	} else {
		s, err := store.Open(&config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "bam.db")})
		if err != nil {
			t.Fatal(err)
		}
		st = s
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Storage.LocalPath = t.TempDir()
	srv := httptest.NewServer(api.NewServer(cfg, st).Handler())
	t.Cleanup(srv.Close)
	return &env{t: t, st: st, srv: srv}
}

// admin creates an administrator and signs them in.
func (e *env) admin(email string) *client {
	e.t.Helper()
	if _, err := e.st.CreateAdminUser(email, password); err != nil {
		e.t.Fatal(err)
	}
	return e.signIn(email)
}

// signIn returns a client holding a session for the user.
func (e *env) signIn(email string) *client {
	e.t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &client{t: e.t, base: e.srv.URL + "/api/v1", http: &http.Client{Jar: jar}}
	c.mustJSON(http.MethodPost, "/auth/login", map[string]string{"email": email, "password": password}, nil, http.StatusOK)
	return c
}

// client is a signed-in user talking to the server.
type client struct {
	t    *testing.T
	base string
	http *http.Client
}

// response is a status and body.
type response struct {
	status int
	body   []byte
}

func (r response) String() string { return fmt.Sprintf("%d %s", r.status, r.body) }

// send makes a request, with a JSON body unless body is a *multipart form.
func (c *client) send(method, path string, body any) response {
	var (
		reader      io.Reader
		contentType string
	)
	switch b := body.(type) {
	case nil:
	case *form:
		reader, contentType = &b.buf, b.contentType
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			c.t.Fatal(err)
		}
		reader, contentType = bytes.NewReader(raw), "application/json"
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		c.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := c.http.Do(req)
	if err != nil {
		// Safe from any goroutine: report, and let the caller see the failure.
		c.t.Errorf("%s %s: %v", method, path, err)
		return response{}
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return response{status: res.StatusCode, body: data}
}

// mustJSON sends a request, checks the status and decodes the response.
func (c *client) mustJSON(method, path string, body, out any, status int) response {
	c.t.Helper()
	r := c.send(method, path, body)
	if r.status != status {
		c.t.Fatalf("%s %s: got %s, want %d", method, path, r, status)
	}
	if out != nil {
		if err := json.Unmarshal(r.body, out); err != nil {
			c.t.Fatalf("%s %s: decoding %s: %v", method, path, r.body, err)
		}
	}
	return r
}

// form is a multipart body.
type form struct {
	buf         bytes.Buffer
	contentType string
}

// csvForm builds an import request.
func csvForm(file string, mapping []string) *form {
	f := &form{}
	mw := multipart.NewWriter(&f.buf)
	fw, _ := mw.CreateFormFile("file", "assets.csv")
	_, _ = fw.Write([]byte(file))
	if mapping != nil {
		raw, _ := json.Marshal(mapping)
		_ = mw.WriteField("mapping", string(raw))
	}
	_ = mw.Close()
	f.contentType = mw.FormDataContentType()
	return f
}

// fileForm builds an upload of one file.
func fileForm(name string, data []byte) *form {
	f := &form{}
	mw := multipart.NewWriter(&f.buf)
	fw, _ := mw.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = mw.Close()
	f.contentType = mw.FormDataContentType()
	return f
}

// Shapes of API responses the scenarios read.
type (
	idOnly   struct{ ID string }
	category struct {
		Category struct {
			ID                string
			IsRunningLow      bool `json:"is_running_low"`
			LowStockThreshold *int `json:"low_stock_threshold"`
		}
		ActiveCount int `json:"active_count"`
	}
	assetResponse struct {
		Asset struct {
			Asset       idOnly
			FieldValues map[string]string `json:"field_values"`
			Effective   []effectiveField  `json:"effective_fields"`
		}
	}
	effectiveField struct {
		Field struct {
			ID  string
			Key string
		}
		IsRequired bool   `json:"is_required"`
		Source     string `json:"source_category_id"`
	}
)

// newCategory creates a category and returns its ID.
func (c *client) newCategory(body map[string]any) string {
	c.t.Helper()
	var out category
	c.mustJSON(http.MethodPost, "/categories", body, &out, http.StatusCreated)
	return out.Category.ID
}

// newAsset creates an asset and returns its ID.
func (c *client) newAsset(body map[string]any) string {
	c.t.Helper()
	var out assetResponse
	c.mustJSON(http.MethodPost, "/assets", body, &out, http.StatusCreated)
	return out.Asset.Asset.ID
}

// getCategory reads a category with its active count.
func (c *client) getCategory(id string) category {
	c.t.Helper()
	var out category
	c.mustJSON(http.MethodGet, "/categories/"+id, nil, &out, http.StatusOK)
	return out
}
