package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/wordnet"
)

// Seeding related terms from WordNet (docs/data-model.md#seeding-from-wordnet):
// the database is downloaded once, checked against its SHA-256, and kept
// in the instance's storage; a seed imports the nouns the instance's
// asset and category names use, as one import. Nothing is fetched to
// search.

// WordNetStatus is what the Search terms page shows of WordNet: where it
// comes from, whether it is kept here yet, and its license.
type WordNetStatus struct {
	Name             string `json:"name"`
	SourceURL        string `json:"source_url"`
	DefaultSourceURL string `json:"default_source_url"`
	SHA256           string `json:"sha256"`
	Size             int64  `json:"size"`
	// Cached is whether the database is kept in storage already, so a
	// seed does not download it.
	Cached bool   `json:"cached"`
	Notice string `json:"notice"`
}

// maxSeedSenses bounds the meanings a seed takes of each word.
const maxSeedSenses = 100

// wordnetUploadLimit bounds a copy of WordNet uploaded by hand, above the
// published file's 16 MB.
const wordnetUploadLimit = 32 << 20

// wordnetSource is the database this server seeds from: WordNet 3.1, or a
// test's.
func (s *Server) wordnetSource() wordnet.Source {
	if s.thesaurus.URL != "" {
		return s.thesaurus
	}
	return wordnet.WordNet31
}

// wordnetKey is where the database is kept in storage. It is outside the
// kinds a sweep looks through (filestore.Kinds), as no row names it.
func (s *Server) wordnetKey() string {
	src := s.wordnetSource()
	return "thesauri/" + strings.ToLower(src.Name) + "-" + src.Version + ".tar.gz"
}

// fetch downloads what url holds, with tests' fetcher where one is set.
func (s *Server) fetch(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	if s.fetchThesaurus != nil {
		return s.fetchThesaurus(ctx, rawURL)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("User-Agent", "BAM/"+Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("it answered %s", resp.Status)
	}
	return cancelOnClose{resp.Body, cancel}, nil
}

// cancelOnClose ends a download's context once its body is closed.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// wordnetSourceURL is where the database is downloaded from: the admin's
// setting, or Princeton's.
func (s *Server) wordnetSourceURL() (string, error) {
	return s.store.WordNetSource(s.wordnetSource().URL)
}

func (s *Server) wordnetCached(ctx context.Context) (bool, error) {
	rc, err := s.blobs.Get(ctx, s.wordnetKey())
	if errors.Is(err, filestore.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	rc.Close()
	return true, nil
}

// handleWordNetStatus is GET /api/v1/terms/wordnet.
func (s *Server) handleWordNetStatus(w http.ResponseWriter, r *http.Request) {
	src := s.wordnetSource()
	from, err := s.wordnetSourceURL()
	if err != nil {
		writeServerError(w, err, "read where WordNet comes from")
		return
	}
	cached, err := s.wordnetCached(r.Context())
	if err != nil {
		writeServerError(w, err, "look for WordNet in storage")
		return
	}
	writeJSON(w, http.StatusOK, WordNetStatus{Name: src.Title(), SourceURL: from, DefaultSourceURL: src.URL, SHA256: src.SHA256, Size: src.Size, Cached: cached, Notice: wordnet.Notice})
}

// handleSetWordNetSource is PUT /api/v1/terms/wordnet with
// {"source_url": "..."}: where WordNet is downloaded from, such as a
// mirror on the instance's own network; empty is Princeton's. Whatever is
// downloaded must still be the same file, by its SHA-256.
func (s *Server) handleSetWordNetSource(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SourceURL string `json:"source_url"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	raw := strings.TrimSpace(in.SourceURL)
	if raw == s.wordnetSource().URL {
		raw = ""
	}
	if raw != "" {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			writeJSONError(w, http.StatusBadRequest, "The address must be a full http:// or https:// URL")
			return
		}
	}
	if err := s.store.SetWordNetSource(raw); err != nil {
		writeServerError(w, err, "set where WordNet comes from")
		return
	}
	s.handleWordNetStatus(w, r)
}

// handleUploadWordNet is PUT /api/v1/terms/wordnet/file, with the
// database's .tar.gz as the body: for an instance that cannot reach any
// copy. It must be the same file, by its SHA-256.
func (s *Server) handleUploadWordNet(w http.ResponseWriter, r *http.Request) {
	src := s.wordnetSource()
	r.Body = http.MaxBytesReader(w, r.Body, src.Size+1)
	data, err := src.Check(r.Body)
	if errors.Is(err, wordnet.ErrMismatch) {
		writeJSONError(w, http.StatusUnprocessableEntity, fmt.Sprintf("That file isn't %s as published (%s): its SHA-256 doesn't match", src.Title(), src.URL))
		return
	}
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		writeJSONError(w, http.StatusUnprocessableEntity, fmt.Sprintf("That file isn't %s as published (%s): it is larger", src.Title(), src.URL))
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "The file could not be read")
		return
	}
	if err := s.blobs.Put(r.Context(), s.wordnetKey(), bytes.NewReader(data), int64(len(data)), "application/gzip"); err != nil {
		writeServerError(w, err, "keep WordNet in storage")
		return
	}
	s.handleWordNetStatus(w, r)
}

// wordnetArchive is the database, from storage, or downloaded and kept
// there first. downloaded says which.
func (s *Server) wordnetArchive(ctx context.Context) (data []byte, downloaded bool, status int, err error) {
	src := s.wordnetSource()
	rc, err := s.blobs.Get(ctx, s.wordnetKey())
	if err == nil {
		defer rc.Close()
		// What storage holds is checked again: a store can be written to
		// from outside bam.
		if data, err = src.Check(rc); err == nil {
			return data, false, 0, nil
		}
	} else if !errors.Is(err, filestore.ErrNotFound) {
		return nil, false, http.StatusInternalServerError, fmt.Errorf("reading WordNet from storage: %w", err)
	}
	from, err := s.wordnetSourceURL()
	if err != nil {
		return nil, false, http.StatusInternalServerError, err
	}
	body, err := s.fetch(ctx, from)
	if err != nil {
		return nil, false, http.StatusBadGateway, fmt.Errorf("WordNet couldn't be downloaded from %s: %v", from, err)
	}
	defer body.Close()
	data, err = src.Check(body)
	if errors.Is(err, wordnet.ErrMismatch) {
		return nil, false, http.StatusBadGateway, fmt.Errorf("what %s holds isn't %s as published: its SHA-256 doesn't match", from, src.Title())
	}
	if err != nil {
		return nil, false, http.StatusBadGateway, fmt.Errorf("WordNet couldn't be downloaded from %s: %v", from, err)
	}
	if err := s.blobs.Put(ctx, s.wordnetKey(), bytes.NewReader(data), int64(len(data)), "application/gzip"); err != nil {
		return nil, false, http.StatusInternalServerError, fmt.Errorf("keeping WordNet in storage: %w", err)
	}
	return data, true, 0, nil
}

// SeedRequest is POST /api/v1/terms/wordnet/seed's body.
type SeedRequest struct {
	// Senses is how many meanings of each word to take, most used first;
	// 0 takes them all.
	Senses int `json:"senses"`
}

// handleSeedWordNet is POST /api/v1/terms/wordnet/seed: the nouns the
// instance's names use, as one import named for WordNet.
func (s *Server) handleSeedWordNet(w http.ResponseWriter, r *http.Request) {
	var in SeedRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Senses < 0 || in.Senses > maxSeedSenses {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Meanings per word is from 0, for all of them, to %d", maxSeedSenses))
		return
	}
	data, downloaded, status, err := s.wordnetArchive(r.Context())
	if err != nil {
		if status == http.StatusInternalServerError {
			writeServerError(w, err, "get WordNet")
		} else {
			writeJSONError(w, status, capitalize(err.Error()))
		}
		return
	}
	db, err := wordnet.Open(data)
	if err != nil {
		writeServerError(w, err, "read WordNet")
		return
	}
	names, err := s.store.CatalogNames()
	if err != nil {
		writeServerError(w, err, "read the names to look up")
		return
	}
	found, err := db.Seed(names, in.Senses)
	if err != nil {
		writeServerError(w, err, "read WordNet")
		return
	}
	if len(found) == 0 {
		writeJSONError(w, http.StatusUnprocessableEntity, "None of the words in asset and category names are nouns in "+s.wordnetSource().Title())
		return
	}
	if len(found) > store.MaxTermImport {
		found = found[:store.MaxTermImport]
	}
	items := make([]store.TermImportItem, len(found))
	for i, f := range found {
		items[i] = store.TermImportItem{TermInput: store.TermInput{Label: f.Label, AltLabels: f.AltLabels, Note: f.Note}, Broader: f.Broader, Related: []string{}}
	}
	as := s.actingStore(r)
	imp, err := as.ImportTerms(s.wordnetSource().Title(), items)
	if err != nil {
		writeTermError(w, err, "import the terms")
		return
	}
	if imp.Terms == 0 && imp.Links == 0 {
		// Seeded before, and nothing has been named since that it lacks:
		// an empty import is only clutter.
		if err := as.DeleteTermImport(imp.ID); err != nil {
			writeServerError(w, err, "remove the empty import")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"import": nil, "downloaded": downloaded, "message": "Nothing new: every term WordNet has for these names is here already"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"import": imp, "downloaded": downloaded})
}
