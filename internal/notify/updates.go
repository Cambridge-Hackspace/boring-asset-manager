package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/release"
)

// The update check (docs/installing.md#knowing-when-a-release-is-out) asks,
// with the worker's turn and at most once a day, whether a newer full
// release of BAM! is out, and announces each once as
// instance.update_available. Servers sharing a database take turns: the
// check is claimed in the database before it runs.

// UpdateCheck is what the update check needs.
type UpdateCheck struct {
	// URL answers with the latest release, as GitHub's releases API does
	// at .../releases/latest, or with a list of releases.
	URL string
	// Version is the release this server runs. A build that is not a
	// release never checks.
	Version string
	// Client asks; nil uses one with a 30-second timeout.
	Client *http.Client
	// Every is how often to check (default a day).
	Every time.Duration
}

// NewUpdateCheck is the update check the configuration asks for, or nil
// when it is off, as it is unless turned on.
func NewUpdateCheck(cfg config.UpdatesConfig, version string) *UpdateCheck {
	if !cfg.Check {
		return nil
	}
	return &UpdateCheck{URL: cfg.URL, Version: version}
}

// updateClient asks for releases, unless a check gives its own.
var updateClient = &http.Client{Timeout: 30 * time.Second}

// CheckForUpdates runs the update check when it is on, due, and claimed
// by this server, answering the release it announced, if any. A check
// that fails answers an error, to be logged, and records nothing; it is
// tried again once the day is out.
func (w *Worker) CheckForUpdates(ctx context.Context) (string, error) {
	w.defaults()
	c := w.Updates
	if c == nil || !release.Is(c.Version) {
		return "", nil
	}
	every := c.Every
	if every == 0 {
		every = 24 * time.Hour
	}
	claimed, err := w.Store.ClaimUpdateCheck(w.Now(), every)
	if err != nil || !claimed {
		return "", err
	}
	latest, err := c.latest(ctx)
	if err != nil {
		host := c.URL
		if u, perr := url.Parse(c.URL); perr == nil {
			host = u.Host
		}
		return "", fmt.Errorf("asking %s for the latest release: %w", host, err)
	}
	if latest == nil || !release.Newer(latest.TagName, c.Version) {
		return "", nil
	}
	notes, updating := release.Pages(c.URL, latest.TagName)
	if u, err := url.Parse(latest.HTMLURL); notes == "" && err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
		notes = latest.HTMLURL
	}
	announced, err := w.Store.AnnounceRelease(models.EventInstance{From: c.Version, To: latest.TagName, NotesURL: notes, UpdatingURL: updating})
	if err != nil || !announced {
		return "", err
	}
	return latest.TagName, nil
}

// releaseInfo is what the update check reads of a release, as GitHub's
// API describes it.
type releaseInfo struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

// latest asks for the newest full release: the one answered, as GitHub's
// releases/latest does, or the newest of a list. Drafts, pre-releases and
// tags that are not a full release's are passed over; nil when none is
// left.
func (c *UpdateCheck) latest(ctx context.Context) (*releaseInfo, error) {
	client := c.Client
	if client == nil {
		client = updateClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "BAM!/"+c.Version)
	resp, err := client.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		// GitHub's rate limit answers 403 or 429, saying so in the body.
		excerpt := strings.TrimSpace(string(body[:min(len(body), 200)]))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, excerpt)
	}
	var list []releaseInfo
	body = bytes.TrimSpace(body)
	if len(body) > 0 && body[0] == '[' {
		err = json.Unmarshal(body, &list)
	} else {
		var one releaseInfo
		err = json.Unmarshal(body, &one)
		list = []releaseInfo{one}
	}
	if err != nil {
		return nil, fmt.Errorf("reading the answer: %w", err)
	}
	var newest *releaseInfo
	for i := range list {
		r := &list[i]
		if r.Draft || r.Prerelease || !release.IsFull(r.TagName) {
			continue
		}
		if newest == nil || release.Newer(r.TagName, newest.TagName) {
			newest = r
		}
	}
	return newest, nil
}
