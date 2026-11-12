package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

// Displays (docs/printing.md#displays): what each display shows, the image a
// display that pulls fetches, the tokens it fetches it with, and refreshes
// asked for.

var (
	// ErrDisplayNotFound is returned for a display that has been shown
	// nothing, or a printer that is not a display.
	ErrDisplayNotFound = errors.New("the display has been shown nothing yet")
	// ErrNotADisplay is returned for a display's operation on a printer
	// whose driver drives printers.
	ErrNotADisplay = errors.New("that printer is not a display")
	// ErrDisplayTokenNotFound is returned for a display token that is
	// unknown or revoked.
	ErrDisplayTokenNotFound = errors.New("display token not found")
	// ErrInvalidDisplayToken is wrapped by refusals of a display token's
	// name.
	ErrInvalidDisplayToken = errors.New("invalid display token")
)

// DisplayTokenPrefix starts every display token's secret, so that it is
// told from an API token's.
const DisplayTokenPrefix = "bamd_"

// The instance's display settings: how long a refresh waits for the rest
// of a burst of changes, and how long a display that pulls waits before
// asking again, when its own options do not say; both in seconds.
const (
	DisplayRefreshWindowSetting = "displays.refresh_window_seconds"
	DisplayPullRefreshSetting   = "displays.pull_refresh_seconds"
)

// paperlesspaper's cloud, for displays pushed through it: its API, empty
// for the default, and the API key, sealed with the configuration's secret
// key for PaperlessPaperKeyPurpose.
const (
	PaperlessPaperBaseSetting = "displays.paperlesspaper_base_url"
	PaperlessPaperKeySetting  = "displays.paperlesspaper_api_key"
	PaperlessPaperKeyPurpose  = "displays.paperlesspaper_api_key"
)

// SoluM's AIMS, for labels pushed through it: its API, empty for the
// default, the company and store its labels are in, who signs in, and the
// password, sealed with the configuration's secret key for
// SolumPasswordPurpose.
const (
	SolumBaseSetting     = "displays.solum_base_url"
	SolumCompanySetting  = "displays.solum_company"
	SolumStoreSetting    = "displays.solum_store"
	SolumUsernameSetting = "displays.solum_username"
	SolumPasswordSetting = "displays.solum_password"
	SolumPasswordPurpose = "displays.solum_password"
)

// Their defaults and limits: half a minute, up to an hour, for the window,
// and a quarter of an hour, as TRMNL's own server answers, for the pull.
const (
	DefaultDisplayRefreshWindow = 30
	MaxDisplayRefreshWindow     = 60 * 60
	DefaultDisplayPullRefresh   = 15 * 60
)

// DisplaySettings reads the instance's display settings, with the defaults
// for those unset or out of range.
func (s *Store) DisplaySettings() (window, pull int, err error) {
	window, pull = DefaultDisplayRefreshWindow, DefaultDisplayPullRefresh
	w, err := s.Setting(DisplayRefreshWindowSetting, "")
	if err != nil {
		return window, pull, err
	}
	p, err := s.Setting(DisplayPullRefreshSetting, "")
	if err != nil {
		return window, pull, err
	}
	if n, err := strconv.Atoi(w); err == nil && n >= 0 && n <= MaxDisplayRefreshWindow {
		window = n
	}
	if n, err := strconv.Atoi(p); err == nil && n >= printers.MinDisplayRefresh && n <= printers.MaxDisplayRefresh {
		pull = n
	}
	return window, pull, nil
}

// displayRefreshDue is when a refresh asked for at now is due: after the
// instance's window, for the rest of a burst to join it.
func (s *Store) displayRefreshDue(now time.Time) (time.Time, error) {
	window, _, err := s.DisplaySettings()
	return now.Add(time.Duration(window) * time.Second), err
}

const displayColumns = `d.printer_id, d.template_id, COALESCE(t.name, ''), d.asset_id, COALESCE(a.name, ''), d.shown_at,
	d.shown_by, d.shown_by_token, d.etag, d.fetched_at, d.refresh_at`

const displayJoins = ` FROM displays d LEFT JOIN templates t ON t.id = d.template_id LEFT JOIN assets a ON a.id = d.asset_id`

func scanDisplay(row interface{ Scan(...any) error }) (models.Display, error) {
	var d models.Display
	var template, asset, by, etag sql.NullString
	var shown, fetched, refresh sql.NullTime
	if err := row.Scan(&d.PrinterID, &template, &d.TemplateName, &asset, &d.AssetName, &shown, &by, &d.ShownByToken, &etag, &fetched, &refresh); err != nil {
		return d, err
	}
	if template.Valid {
		d.TemplateID = &template.String
	}
	if asset.Valid {
		d.AssetID = &asset.String
	}
	d.ShownBy, d.ETag = by.String, etag.String
	for _, t := range []struct {
		from sql.NullTime
		to   **time.Time
	}{{shown, &d.ShownAt}, {fetched, &d.FetchedAt}, {refresh, &d.RefreshAt}} {
		if t.from.Valid {
			v := t.from.Time.UTC()
			*t.to = &v
		}
	}
	return d, nil
}

// GetDisplay reads what a display shows: ErrDisplayNotFound when it has
// been shown nothing.
func (s *Store) GetDisplay(printerID string) (*models.Display, error) {
	d, err := scanDisplay(s.QueryRow("SELECT "+displayColumns+displayJoins+" WHERE d.printer_id = ?", printerID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDisplayNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading the display: %w", err)
	}
	return &d, nil
}

// ListDisplays reads what every display that has been shown anything
// shows.
func (s *Store) ListDisplays() ([]models.Display, error) {
	rows, err := s.Query("SELECT " + displayColumns + displayJoins + " ORDER BY d.printer_id")
	if err != nil {
		return nil, fmt.Errorf("listing displays: %w", err)
	}
	defer rows.Close()
	out := []models.Display{}
	for rows.Next() {
		d, err := scanDisplay(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning a display: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// upsertDisplay sets columns of a display's row, making it if it has none.
func (s *Store) upsertDisplay(printerID string, columns []string, values []any) error {
	set := make([]string, len(columns))
	for i, c := range columns {
		set[i] = c + " = ?"
	}
	for range 2 {
		res, err := s.Exec("UPDATE displays SET "+strings.Join(set, ", ")+" WHERE printer_id = ?", append(slices.Clone(values), printerID)...)
		if err != nil {
			return fmt.Errorf("saving what the display shows: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			return nil
		}
		marks := strings.Repeat(", ?", len(columns))
		_, err = s.Exec("INSERT INTO displays (printer_id, "+strings.Join(columns, ", ")+") VALUES (?"+marks+")", append([]any{printerID}, values...)...)
		if err == nil {
			return nil
		}
		// Another server made it first: set it there.
		if !IsUniqueViolation(err) {
			return fmt.Errorf("saving what the display shows: %w", err)
		}
	}
	return errors.New("saving what the display shows: it kept changing")
}

// ShowOnDisplay records that a display was shown a template's label for
// an asset, by the acting user or token, whom its refreshes act as.
// templateID and assetID are nil for a label of neither, such as a test.
func (s *Store) ShowOnDisplay(printerID string, templateID, assetID *string) error {
	by, byToken := s.actorColumns()
	return s.upsertDisplay(printerID, []string{"template_id", "asset_id", "shown_by", "shown_by_token", "shown_at"},
		[]any{templateID, assetID, by, byToken, dbTime(time.Now())})
}

// SetDisplayImage keeps the image a display that pulls fetches, and
// returns its tag, which changes when it does.
func (s *Store) SetDisplayImage(printerID string, img []byte, contentType string) (string, error) {
	sum := sha256.Sum256(img)
	etag := hex.EncodeToString(sum[:16])
	err := s.upsertDisplay(printerID, []string{"image", "content_type", "etag"}, []any{base64.StdEncoding.EncodeToString(img), contentType, etag})
	return etag, err
}

// DisplayImage reads the image a display that pulls fetches, its media
// type and tag: ErrDisplayNotFound when it has none yet.
func (s *Store) DisplayImage(printerID string) ([]byte, string, string, error) {
	var encoded, contentType, etag sql.NullString
	err := s.QueryRow("SELECT image, content_type, etag FROM displays WHERE printer_id = ?", printerID).Scan(&encoded, &contentType, &etag)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !encoded.Valid) {
		return nil, "", "", ErrDisplayNotFound
	}
	if err != nil {
		return nil, "", "", fmt.Errorf("reading the display's image: %w", err)
	}
	img, err := base64.StdEncoding.DecodeString(encoded.String)
	if err != nil {
		return nil, "", "", fmt.Errorf("reading the display's image: %w", err)
	}
	return img, contentType.String, etag.String, nil
}

// RecordDisplayFetch records that a display fetched its image at now.
func (s *Store) RecordDisplayFetch(printerID string, now time.Time) error {
	if _, err := s.Exec("UPDATE displays SET fetched_at = ? WHERE printer_id = ?", dbTime(now), printerID); err != nil {
		return fmt.Errorf("recording the display's fetch: %w", err)
	}
	return nil
}

// displaysShowing lists the displays that show an asset, or something in
// it: an asset whose location runs through it.
func (s *Store) displaysShowing(assetID string) ([]string, error) {
	rows, err := s.Query("SELECT printer_id, asset_id FROM displays WHERE asset_id IS NOT NULL")
	if err != nil {
		return nil, fmt.Errorf("finding the displays showing an asset: %w", err)
	}
	shown := map[string][]string{}
	for rows.Next() {
		var printer, asset string
		if err := rows.Scan(&printer, &asset); err != nil {
			rows.Close()
			return nil, err
		}
		shown[asset] = append(shown[asset], printer)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []string
	for asset, displays := range shown {
		in := asset == assetID
		if !in {
			a, err := s.GetAssetByID(asset)
			if errors.Is(err, ErrAssetNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			in = slices.ContainsFunc(a.Location, func(l models.LinkedAsset) bool { return l.ID == assetID })
		}
		if in {
			out = append(out, displays...)
		}
	}
	slices.Sort(out)
	return out, nil
}

// refreshDisplaysTx asks, within tx, for displays to be refreshed at due,
// unless a refresh is already due for them: a burst of changes is one
// refresh, at the time the first asked for.
func (s *Store) refreshDisplaysTx(tx *sql.Tx, printerIDs []string, due time.Time) error {
	for _, id := range printerIDs {
		if _, err := s.TxExec(tx, "UPDATE displays SET refresh_at = ? WHERE printer_id = ? AND refresh_at IS NULL", dbTime(due), id); err != nil {
			return fmt.Errorf("asking for a display's refresh: %w", err)
		}
	}
	return nil
}

// RefreshDisplay asks for a display to be refreshed at at, or sooner if
// one is already due sooner: ErrDisplayNotFound when it shows nothing to
// refresh.
func (s *Store) RefreshDisplay(printerID string, at time.Time) error {
	at = dbTime(at)
	res, err := s.Exec("UPDATE displays SET refresh_at = ? WHERE printer_id = ? AND asset_id IS NOT NULL AND template_id IS NOT NULL AND (refresh_at IS NULL OR refresh_at > ?)", at, printerID, at)
	if err != nil {
		return fmt.Errorf("asking for a display's refresh: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	d, err := s.GetDisplay(printerID)
	if err != nil {
		return err
	}
	if d.AssetID == nil || d.TemplateID == nil {
		return ErrDisplayNotFound
	}
	// One is due sooner already.
	return nil
}

// DueDisplayRefreshes lists the displays whose refresh is due at now.
func (s *Store) DueDisplayRefreshes(now time.Time) ([]models.Display, error) {
	rows, err := s.Query("SELECT "+displayColumns+displayJoins+" WHERE d.refresh_at IS NOT NULL AND d.refresh_at <= ? ORDER BY d.refresh_at", dbTime(now))
	if err != nil {
		return nil, fmt.Errorf("listing displays to refresh: %w", err)
	}
	defer rows.Close()
	var out []models.Display
	for rows.Next() {
		d, err := scanDisplay(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ClaimDisplayRefresh claims the refresh due at at for a display, so that
// of servers sharing the database one makes it: false when another has.
func (s *Store) ClaimDisplayRefresh(printerID string, at time.Time) (bool, error) {
	res, err := s.Exec("UPDATE displays SET refresh_at = NULL WHERE printer_id = ? AND refresh_at = ?", printerID, dbTime(at))
	if err != nil {
		return false, fmt.Errorf("claiming a display's refresh: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// displayPrinter reads a printer that is a display.
func (s *Store) displayPrinter(printerID string) (*models.Printer, error) {
	p, err := s.GetPrinter(printerID)
	if err != nil {
		return nil, err
	}
	if !printers.IsDisplay(p.Driver) {
		return nil, ErrNotADisplay
	}
	return p, nil
}

// DisplayByMAC finds the display of a driver that pulls and names itself
// by its MAC address, such as a TRMNL: ErrPrinterNotFound when none does.
func (s *Store) DisplayByMAC(driver, mac string) (*models.Printer, error) {
	conn, err := printers.NormalizeConnection(printers.Connection{Type: printers.ConnPull, Address: mac})
	if err != nil || conn.Address == "" {
		return nil, ErrPrinterNotFound
	}
	var id string
	err = s.QueryRow("SELECT id FROM printers WHERE driver = ? AND connection_type = ? AND address = ? ORDER BY created_at LIMIT 1", driver, printers.ConnPull, conn.Address).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPrinterNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("finding the display: %w", err)
	}
	return s.GetPrinter(id)
}

const displayTokenColumns = "id, printer_id, name, created_by, created_at, last_used_at"

func scanDisplayToken(row interface{ Scan(...any) error }) (models.DisplayToken, error) {
	var t models.DisplayToken
	var by sql.NullString
	var used sql.NullTime
	if err := row.Scan(&t.ID, &t.PrinterID, &t.Name, &by, &t.CreatedAt, &used); err != nil {
		return t, err
	}
	t.CreatedBy = by.String
	t.CreatedAt = t.CreatedAt.UTC()
	if used.Valid {
		v := used.Time.UTC()
		t.LastUsedAt = &v
	}
	return t, nil
}

// CreateDisplayToken makes a token for a display to fetch its image with,
// by the acting user or token, and returns it with its secret, which is
// not kept: only its hash is.
func (s *Store) CreateDisplayToken(printerID, name string) (*models.DisplayToken, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", fmt.Errorf("%w: a name is required", ErrInvalidDisplayToken)
	}
	if err := textcheck.Check("token name", name, textcheck.MaxName); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalidDisplayToken, err)
	}
	if _, err := s.displayPrinter(printerID); err != nil {
		return nil, "", err
	}
	random, err := auth.GenerateSessionToken()
	if err != nil {
		return nil, "", err
	}
	secret := DisplayTokenPrefix + random
	t := models.DisplayToken{ID: uuid.NewString(), PrinterID: printerID, Name: name, CreatedAt: dbTime(time.Now())}
	by := s.actorID()
	if id, ok := by.(string); ok {
		t.CreatedBy = id
	}
	if _, err := s.Exec("INSERT INTO display_tokens (id, printer_id, name, token_hash, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		t.ID, t.PrinterID, t.Name, sessionKey(secret), by, t.CreatedAt); err != nil {
		return nil, "", fmt.Errorf("making the display token: %w", err)
	}
	return &t, secret, nil
}

// ListDisplayTokens lists a display's tokens, newest first.
func (s *Store) ListDisplayTokens(printerID string) ([]models.DisplayToken, error) {
	rows, err := s.Query("SELECT "+displayTokenColumns+" FROM display_tokens WHERE printer_id = ? ORDER BY created_at DESC, id", printerID)
	if err != nil {
		return nil, fmt.Errorf("listing display tokens: %w", err)
	}
	defer rows.Close()
	out := []models.DisplayToken{}
	for rows.Next() {
		t, err := scanDisplayToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeDisplayToken deletes one of a display's tokens, which then fetches
// nothing.
func (s *Store) RevokeDisplayToken(printerID, tokenID string) error {
	res, err := s.Exec("DELETE FROM display_tokens WHERE id = ? AND printer_id = ?", tokenID, printerID)
	if err != nil {
		return fmt.Errorf("revoking the display token: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrDisplayTokenNotFound
	}
	return nil
}

// AuthenticateDisplayToken finds the display token with this secret, and
// records its use, at most once a minute: ErrDisplayTokenNotFound when
// none has it.
func (s *Store) AuthenticateDisplayToken(secret string) (*models.DisplayToken, error) {
	if !strings.HasPrefix(secret, DisplayTokenPrefix) {
		return nil, ErrDisplayTokenNotFound
	}
	t, err := scanDisplayToken(s.QueryRow("SELECT "+displayTokenColumns+" FROM display_tokens WHERE token_hash = ?", sessionKey(secret)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDisplayTokenNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading the display token: %w", err)
	}
	now := time.Now().UTC()
	if t.LastUsedAt == nil || now.Sub(*t.LastUsedAt) >= tokenUseInterval {
		if _, err := s.Exec("UPDATE display_tokens SET last_used_at = ? WHERE id = ?", dbTime(now), t.ID); err != nil {
			return nil, fmt.Errorf("recording the display token's use: %w", err)
		}
		t.LastUsedAt = &now
	}
	return &t, nil
}
