package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/archive"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Backups from the web, and replacing an instance with one (README §2.3,
// #142). A backup from the web never holds authentication; replacing one
// never touches it, so no one is signed out. Checking and replacing take as
// long as the archive does, so each runs as a job, which the page asks
// after.

// backupJob is a check or a replace under way, or done.
type backupJob struct {
	ID     string    `json:"id"`
	Kind   string    `json:"kind"`
	Done   bool      `json:"done"`
	Error  string    `json:"error,omitempty"`
	Result any       `json:"result,omitempty"`
	Start  time.Time `json:"started_at"`
	userID string
}

// backupJobs keeps the jobs of the last day, and lets one replace run at a
// time.
type backupJobs struct {
	mu        sync.Mutex
	jobs      map[string]*backupJob
	replacing sync.Mutex
}

func (j *backupJobs) start(kind, userID string, run func() (any, error)) *backupJob {
	job := &backupJob{ID: uuid.NewString(), Kind: kind, Start: time.Now().UTC(), userID: userID}
	j.mu.Lock()
	if j.jobs == nil {
		j.jobs = map[string]*backupJob{}
	}
	for id, old := range j.jobs {
		if old.Done && time.Since(old.Start) > UploadIdle {
			delete(j.jobs, id)
		}
	}
	j.jobs[job.ID] = job
	j.mu.Unlock()
	go func() {
		result, err := run()
		j.mu.Lock()
		defer j.mu.Unlock()
		job.Done, job.Result = true, result
		if err != nil {
			job.Error = err.Error()
		}
	}()
	return job
}

func (j *backupJobs) get(id, userID string) (backupJob, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	job, ok := j.jobs[id]
	if !ok || job.userID != userID {
		return backupJob{}, false
	}
	return *job, true
}

// instanceHost is what is typed to confirm a replace: the host of the
// instance's base URL, which says which instance it is.
func (s *Server) instanceHost() string {
	if u, err := url.Parse(s.cfg.Server.BaseURL); err == nil && u.Host != "" {
		return u.Host
	}
	return s.cfg.Server.BaseURL
}

// handleDownloadBackup streams a backup of the instance, without
// authentication, as it is written: GET /api/v1/backup.
func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	w, r = transfer(w, r)
	name := fmt.Sprintf("bam-%s-%s.bam.tar.gz", sanitizeForHeader(s.instanceHost()), time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Cache-Control", "no-store")
	started := false
	open := func(int) (io.WriteCloser, error) {
		started = true
		return nopWriteCloser{w}, nil
	}
	_, err := archive.Write(r.Context(), s.store, s.blobs, open, archive.Options{BamVersion: Version})
	if err != nil {
		log.Printf("writing a backup: %v", err)
		if !started {
			writeJSONError(w, http.StatusInternalServerError, "Could not write the backup")
			return
		}
		// Part written: the browser must see the download fail, not end.
		panic(http.ErrAbortHandler)
	}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// stagedUpload is a complete upload of the person's, to read.
func (s *Server) stagedUpload(r *http.Request, id string) (*models.Upload, int, string) {
	u, err := s.store.GetUpload(id, CurrentUser(r.Context()).ID)
	if errors.Is(err, store.ErrUploadNotFound) {
		return nil, http.StatusNotFound, "Upload not found"
	}
	if err != nil {
		return nil, http.StatusInternalServerError, "Could not read the upload"
	}
	if !u.Complete {
		return nil, http.StatusConflict, "The upload is not complete"
	}
	_ = s.store.TouchUpload(u.ID)
	return u, 0, ""
}

func (s *Server) openUpload(id string) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) { return s.staging.Open(context.Background(), id) }
}

// handleUploadContent streams a complete upload back to its owner, such as
// the backup a replace took first: GET /api/v1/uploads/{id}/content.
func (s *Server) handleUploadContent(w http.ResponseWriter, r *http.Request) {
	w, r = transfer(w, r)
	u, code, msg := s.stagedUpload(r, r.PathValue("id"))
	if u == nil {
		writeJSONError(w, code, msg)
		return
	}
	rc, err := s.staging.Open(r.Context(), u.ID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not read the upload")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Length", strconv.FormatInt(u.Size, 10))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, sanitizeForHeader(u.Filename)))
	if _, err := io.Copy(w, rc); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// checkResult is what checking an uploaded archive found.
type checkResult struct {
	Header        archive.Header `json:"header"`
	Tables        map[string]int `json:"tables,omitempty"`
	Files         int            `json:"files"`
	FileBytes     int64          `json:"file_bytes"`
	Missing       int            `json:"missing_files"`
	Parts         int            `json:"parts"`
	SchemaCurrent bool           `json:"schema_current"`
	InstanceHost  string         `json:"instance_host"`
	Warnings      []string       `json:"warnings"`
}

var authNotThroughTheWeb = "This archive holds authentication (passwords, sessions and tokens), which is never imported through the web: everything else can be, and no one is signed out."

// handleCheckBackup checks an uploaded archive in full, as a job:
// POST /api/v1/backup/check with {"upload_id"}.
func (s *Server) handleCheckBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UploadID string `json:"upload_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	u, code, msg := s.stagedUpload(r, req.UploadID)
	if u == nil {
		writeJSONError(w, code, msg)
		return
	}
	job := s.backupJobs.start("check", CurrentUser(r.Context()).ID, func() (any, error) {
		rc, err := s.openUpload(u.ID)()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		c, err := archive.Check(rc)
		if err != nil {
			return nil, err
		}
		current, err := s.store.SchemaVersion()
		if err != nil {
			return nil, err
		}
		res := checkResult{
			Header: c.Header, Tables: c.Rows, Files: c.Files, FileBytes: c.FileBytes, Missing: len(c.Manifest.Missing),
			Parts: c.Manifest.Set.Parts, SchemaCurrent: c.Header.SchemaVersion == current, InstanceHost: s.instanceHost(), Warnings: []string{},
		}
		if c.Header.Secrets {
			res.Warnings = append(res.Warnings, authNotThroughTheWeb)
		}
		return res, nil
	})
	writeJSON(w, http.StatusAccepted, job)
}

// replaceResult is what a replace did.
type replaceResult struct {
	Summary *archive.RestoreSummary `json:"summary"`
	// BeforeUploadID is the backup taken of what was replaced, first: to
	// download, or to replace with again.
	BeforeUploadID string `json:"before_upload_id,omitempty"`
}

// handleReplaceBackup replaces the kinds chosen with an uploaded archive's,
// as a job: POST /api/v1/backup/replace with {"upload_id", "kinds",
// "confirm"}, confirm being the instance's host. A backup of what is
// replaced is taken first, and the replace refused if that fails. A later
// part of a set adds its files, and needs neither.
func (s *Server) handleReplaceBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UploadID string         `json:"upload_id"`
		Kinds    []archive.Kind `json:"kinds"`
		Confirm  string         `json:"confirm"`
		Part     int            `json:"part"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	u, code, msg := s.stagedUpload(r, req.UploadID)
	if u == nil {
		writeJSONError(w, code, msg)
		return
	}
	later := req.Part > 1
	if !later {
		if req.Confirm != s.instanceHost() {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Type %s to confirm replacing this instance", s.instanceHost()))
			return
		}
		if len(req.Kinds) == 0 {
			writeJSONError(w, http.StatusBadRequest, "Choose at least one kind of information to replace")
			return
		}
		for _, k := range req.Kinds {
			if k == archive.KindAuth {
				writeJSONError(w, http.StatusBadRequest, "Authentication is never replaced through the web; bam restore --replace does that, from the command line")
				return
			}
			if !kindKnown(k) {
				writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("%q is not a kind of information", k))
				return
			}
		}
	}
	if !s.backupJobs.replacing.TryLock() {
		writeJSONError(w, http.StatusConflict, "Another replace is under way; wait for it to finish")
		return
	}
	user := CurrentUser(r.Context())
	job := s.backupJobs.start("replace", user.ID, func() (any, error) {
		defer s.backupJobs.replacing.Unlock()
		ctx := context.Background()
		res := &replaceResult{}
		if !later {
			before, err := s.stageBackup(ctx, user.ID, req.Kinds)
			if err != nil {
				return nil, fmt.Errorf("the backup of what it replaces could not be taken, so nothing was replaced: %w", err)
			}
			res.BeforeUploadID = before
		}
		sum, err := archive.Restore(ctx, s.store, s.blobs, s.openUpload(u.ID), archive.RestoreOptions{Kinds: req.Kinds, Replace: true, Keep: user.ID})
		res.Summary = sum
		if sum != nil && sum.Header.Secrets && !later {
			sum.Warnings = append(sum.Warnings, authNotThroughTheWeb)
		}
		// The icons are built again from what is there now.
		select {
		case s.iconsDirty <- struct{}{}:
		default:
		}
		return res, err
	})
	writeJSON(w, http.StatusAccepted, job)
}

func kindKnown(k archive.Kind) bool {
	for _, known := range archive.Kinds {
		if k == known {
			return true
		}
	}
	return false
}

// stageBackup writes a backup of the kinds given, without authentication,
// into the staging, as an upload of the person's, kept a day like any.
func (s *Server) stageBackup(ctx context.Context, userID string, kinds []archive.Kind) (string, error) {
	id := uuid.NewString()
	pr, pw := io.Pipe()
	h := sha256.New()
	go func() {
		_, err := archive.Write(ctx, s.store, s.blobs, func(int) (io.WriteCloser, error) { return nopWriteCloser{io.MultiWriter(pw, h)}, nil },
			archive.Options{BamVersion: Version, Kinds: kinds})
		pw.CloseWithError(err)
	}()
	token, size, err := filestore.StageFrom(ctx, s.staging, id, pr)
	if err != nil {
		pr.CloseWithError(err)
		return "", err
	}
	up := &models.Upload{
		ID: id, UserID: userID, Filename: fmt.Sprintf("before-replace-%s.bam.tar.gz", time.Now().UTC().Format("20060102-150405")),
		Size: size, SHA256: hex.EncodeToString(h.Sum(nil)), PartSize: filestore.StreamPartSize, Received: size,
		HashState: []byte{}, StagingToken: token, Complete: true,
	}
	if err := s.store.CreateUpload(up); err != nil {
		_ = s.staging.Remove(ctx, id, token)
		return "", err
	}
	return id, nil
}

// handleBackupJob says how a check or replace is going: GET
// /api/v1/backup/jobs/{id}, the person's own.
func (s *Server) handleBackupJob(w http.ResponseWriter, r *http.Request) {
	job, ok := s.backupJobs.get(r.PathValue("id"), CurrentUser(r.Context()).ID)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "Job not found")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, job)
}
