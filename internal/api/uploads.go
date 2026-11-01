package api

import (
	"context"
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Archives are uploaded through the web in parts, each its own request, so
// that one of any size goes over a connection that drops, and is resumed
// where it stopped (docs/backups.md, #141). An upload is staged apart from the
// files until it is used (filestore.Staging), and only the person who
// began it may add to it, see it or use it.

// MaxUploadSize is the largest upload: S3's largest object.
const MaxUploadSize = 5 << 40

// UploadIdle is how long an upload may go without being added to or used
// before it is removed.
const UploadIdle = 24 * time.Hour

// requestTimeout is how long the server gives a request to arrive, and a
// response to be written, but on routes that move archives (transfer).
var requestTimeout = 15 * time.Second

// transferIdle is how long a transfer's bytes may stop moving before its
// request is given up: it replaces the server's 15 seconds for the routes
// that move archives, however long they take in all.
var transferIdle = 2 * time.Minute

// movingBody extends a request's read deadline each time it reads.
type movingBody struct {
	io.ReadCloser
	rc *http.ResponseController
}

func (b movingBody) Read(p []byte) (int, error) {
	_ = b.rc.SetReadDeadline(time.Now().Add(transferIdle))
	return b.ReadCloser.Read(p)
}

// movingWriter extends a response's write deadline each time it writes.
type movingWriter struct {
	http.ResponseWriter
	rc *http.ResponseController
}

func (w movingWriter) Write(p []byte) (int, error) {
	_ = w.rc.SetWriteDeadline(time.Now().Add(transferIdle))
	return w.ResponseWriter.Write(p)
}

func (w movingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// transfer lets a request move as many bytes as it needs: its deadlines
// last as long as bytes keep moving, and end when they stop.
func transfer(w http.ResponseWriter, r *http.Request) (http.ResponseWriter, *http.Request) {
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(transferIdle))
	_ = rc.SetWriteDeadline(time.Now().Add(transferIdle))
	r.Body = movingBody{r.Body, rc}
	return movingWriter{w, rc}, r
}

// uploadLocks keeps one part of an upload arriving at a time.
type uploadLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (l *uploadLocks) lock(id string) func() {
	l.mu.Lock()
	if l.locks == nil {
		l.locks = map[string]*sync.Mutex{}
	}
	m, ok := l.locks[id]
	if !ok {
		m = &sync.Mutex{}
		l.locks[id] = m
	}
	l.mu.Unlock()
	m.Lock()
	return m.Unlock
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// uploadResponse is an upload as the API shows it.
func uploadResponse(u *models.Upload) map[string]any {
	return map[string]any{
		"id": u.ID, "filename": u.Filename, "size": u.Size, "sha256": u.SHA256,
		"part_size": u.PartSize, "received": u.Received, "complete": u.Complete,
		"created_at": u.CreatedAt, "updated_at": u.UpdatedAt,
	}
}

// handleCreateUpload begins an upload: POST /api/v1/uploads with its file's
// name, size and SHA-256.
func (s *Server) handleCreateUpload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
		SHA256   string `json:"sha256"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	switch {
	case req.Size <= 0 || req.Size > MaxUploadSize:
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("size must be from 1 to %d bytes", int64(MaxUploadSize)))
		return
	case !sha256Hex.MatchString(req.SHA256):
		writeJSONError(w, http.StatusBadRequest, "sha256 must be the file's SHA-256, as 64 lowercase hex digits")
		return
	case req.Filename == "" || len(req.Filename) > 255:
		writeJSONError(w, http.StatusBadRequest, "filename must be from 1 to 255 bytes")
		return
	}
	state, err := sha256.New().(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not begin the upload")
		return
	}
	u := &models.Upload{
		ID: uuid.NewString(), UserID: CurrentUser(r.Context()).ID, Filename: req.Filename,
		Size: req.Size, SHA256: req.SHA256, PartSize: filestore.PartSize(req.Size), HashState: state,
	}
	if u.StagingToken, err = s.staging.Begin(r.Context(), u.ID, u.Size); err != nil {
		if errors.Is(err, filestore.ErrNoSpace) {
			writeJSONError(w, http.StatusInsufficientStorage, "The server has no room for this upload: "+err.Error())
			return
		}
		log.Printf("beginning an upload: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "Could not begin the upload")
		return
	}
	if err := s.store.CreateUpload(u); err != nil {
		_ = s.staging.Remove(r.Context(), u.ID, u.StagingToken)
		writeJSONError(w, http.StatusInternalServerError, "Could not begin the upload")
		return
	}
	writeJSON(w, http.StatusCreated, uploadResponse(u))
}

// ownUpload is the upload the path names, which must be the person's.
func (s *Server) ownUpload(w http.ResponseWriter, r *http.Request) *models.Upload {
	u, err := s.store.GetUpload(r.PathValue("id"), CurrentUser(r.Context()).ID)
	if errors.Is(err, store.ErrUploadNotFound) {
		writeJSONError(w, http.StatusNotFound, "Upload not found")
		return nil
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not read the upload")
		return nil
	}
	return u
}

// handleGetUpload says how much of an upload has arrived, for resuming it:
// in the body, and for HEAD in Upload-Offset and Upload-Length.
func (s *Server) handleGetUpload(w http.ResponseWriter, r *http.Request) {
	u := s.ownUpload(w, r)
	if u == nil {
		return
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(u.Received, 10))
	w.Header().Set("Upload-Length", strconv.FormatInt(u.Size, 10))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, uploadResponse(u))
}

var contentRange = regexp.MustCompile(`^bytes (\d+)-(\d+)/(\d+)$`)

// handlePutUploadPart takes one part: PUT /api/v1/uploads/{id} with
// Content-Range, starting where the upload has got to. Once the last is
// in, the whole file is checked against its SHA-256.
func (s *Server) handlePutUploadPart(w http.ResponseWriter, r *http.Request) {
	w, r = transfer(w, r)
	u := s.ownUpload(w, r)
	if u == nil {
		return
	}
	m := contentRange.FindStringSubmatch(r.Header.Get("Content-Range"))
	if m == nil {
		writeJSONError(w, http.StatusBadRequest, "A part needs Content-Range: bytes <first>-<last>/<size>")
		return
	}
	first, _ := strconv.ParseInt(m[1], 10, 64)
	last, _ := strconv.ParseInt(m[2], 10, 64)
	total, _ := strconv.ParseInt(m[3], 10, 64)
	length := last - first + 1
	want := min(u.PartSize, u.Size-first)
	switch {
	case total != u.Size:
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("The upload is %d bytes, not %d", u.Size, total))
		return
	case u.Complete:
		writeJSON(w, http.StatusConflict, map[string]any{"error": "The upload is complete", "received": u.Received})
		return
	case first != u.Received:
		writeJSON(w, http.StatusConflict, map[string]any{"error": fmt.Sprintf("The upload has %d bytes; the next part starts there", u.Received), "received": u.Received})
		return
	case last >= u.Size || length != want:
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("The part starting at %d is %d bytes", first, want))
		return
	case r.ContentLength != length:
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("The body must be the part's %d bytes", length))
		return
	}

	unlock := s.uploadLocks.lock(u.ID)
	defer unlock()
	// Read again under the lock: another part may have arrived meanwhile.
	if u = s.ownUpload(w, r); u == nil {
		return
	}
	if first != u.Received || u.Complete {
		writeJSON(w, http.StatusConflict, map[string]any{"error": fmt.Sprintf("The upload has %d bytes; the next part starts there", u.Received), "received": u.Received})
		return
	}
	h := sha256.New()
	if err := h.(encoding.BinaryUnmarshaler).UnmarshalBinary(u.HashState); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not resume the upload's hash")
		return
	}
	part := int(first/u.PartSize) + 1
	body := &countingBody{r: io.TeeReader(io.LimitReader(r.Body, length), h)}
	tag, err := s.staging.Part(r.Context(), u.ID, u.StagingToken, part, first, body, length)
	if err != nil || body.n != length {
		// The part is sent again from its start.
		log.Printf("upload %s, part %d: %v", u.ID, part, err)
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The part did not arrive whole; send it again", "received": u.Received})
		return
	}
	state, err := h.(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not record the part")
		return
	}
	if ok, err := s.store.AdvanceUpload(u.ID, first, first+length, state, part, tag); err != nil || !ok {
		writeJSONError(w, http.StatusInternalServerError, "Could not record the part")
		return
	}
	u.Received = first + length
	if u.Received == u.Size {
		if err := s.completeUpload(r.Context(), u, h); err != nil {
			writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, uploadResponse(u))
}

// completeUpload checks a whole upload against its SHA-256 and joins its
// parts. One that does not match is removed.
func (s *Server) completeUpload(ctx context.Context, u *models.Upload, h hash.Hash) error {
	if hex.EncodeToString(h.Sum(nil)) != u.SHA256 {
		s.removeUpload(ctx, u)
		return errors.New("The file that arrived is not the one begun: its SHA-256 differs. It has been removed; upload it again")
	}
	tags, err := s.store.UploadTags(u.ID)
	if err == nil {
		err = s.staging.Complete(ctx, u.ID, u.StagingToken, tags)
	}
	if err == nil {
		err = s.store.CompleteUpload(u.ID)
	}
	if err != nil {
		log.Printf("completing upload %s: %v", u.ID, err)
		return errors.New("Could not complete the upload")
	}
	u.Complete = true
	return nil
}

type countingBody struct {
	r io.Reader
	n int64
}

func (c *countingBody) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// handleDeleteUpload removes an upload, finished or not.
func (s *Server) handleDeleteUpload(w http.ResponseWriter, r *http.Request) {
	u := s.ownUpload(w, r)
	if u == nil {
		return
	}
	s.removeUpload(r.Context(), u)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeUpload(ctx context.Context, u *models.Upload) {
	if err := s.staging.Remove(ctx, u.ID, u.StagingToken); err != nil {
		log.Printf("removing upload %s: %v", u.ID, err)
	}
	if err := s.store.DeleteUpload(u.ID); err != nil {
		log.Printf("forgetting upload %s: %v", u.ID, err)
	}
}

// SweepUploads removes the uploads not added to or used since UploadIdle
// before now, and returns how many.
func (s *Server) SweepUploads(ctx context.Context, now time.Time) (int, error) {
	stale, err := s.store.StaleUploads(now.Add(-UploadIdle))
	if err != nil {
		return 0, err
	}
	for i := range stale {
		s.removeUpload(ctx, &stale[i])
	}
	return len(stale), nil
}

// RunUploads removes stale uploads every hour until ctx ends.
func (s *Server) RunUploads(ctx context.Context) {
	for {
		if n, err := s.SweepUploads(ctx, time.Now()); err != nil && ctx.Err() == nil {
			log.Printf("removing unused uploads: %v", err)
		} else if n > 0 {
			log.Printf("removed %d uploads unused for a day", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Hour):
		}
	}
}
