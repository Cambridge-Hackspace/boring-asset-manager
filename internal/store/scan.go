package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/google/uuid"
)

// Scan templates (README §6.4) are reports of kind scan. A category names
// the one its assets show when scanned; subcategories inherit it unless
// they name their own, and assets in no such category show the default.

const (
	ReportKindReport = "report"
	ReportKindScan   = "scan"
)

var (
	// ErrNotScanTemplate is returned for naming a report that is not a
	// scan template where one is needed.
	ErrNotScanTemplate = errors.New("that is not a scan template")
	// ErrReportInUse is returned for deleting the default scan template,
	// or one that categories name.
	ErrReportInUse = errors.New("scan template in use")
)

// scanCandidate is a category naming a scan template, found from one of an
// asset's categories at a distance: 0 for the category itself, 1 for its
// parent, and so on.
type scanCandidate struct {
	template, source string
	distance         int
}

// chooseScan picks the scan template for an asset in these categories:
// the one named by the closest category up from any of them; among those
// equally close, by the category with (b) the longest ancestry, then (c)
// the fewest descendants, then (d) the oldest, then by ID. Ties broken by
// (b) to (d) between different templates are conflicts, with the
// categories involved. With no candidate, it is the default template.
func (t categoryTraits) chooseScan(categoryIDs []string, defaultID string) models.ScanChoice {
	var found []scanCandidate
	for _, c := range categoryIDs {
		for id, d := c, 0; id != "" && d < 100; id, d = t.parents[id], d+1 {
			if tmpl := t.scan[id]; tmpl != "" {
				found = append(found, scanCandidate{template: tmpl, source: id, distance: d})
				break
			}
		}
	}
	if len(found) == 0 {
		return models.ScanChoice{TemplateID: defaultID}
	}
	closest := found[0].distance
	for _, f := range found {
		closest = min(closest, f.distance)
	}
	var tied []scanCandidate
	seen := map[string]bool{}
	templates := map[string]bool{}
	for _, f := range found {
		if f.distance == closest && !seen[f.source] {
			seen[f.source] = true
			tied = append(tied, f)
			templates[f.template] = true
		}
	}
	depth, descendants := t.depth, t.descendants
	sort.Slice(tied, func(i, j int) bool {
		a, b := tied[i].source, tied[j].source
		switch {
		case depth[a] != depth[b]:
			return depth[a] > depth[b]
		case descendants[a] != descendants[b]:
			return descendants[a] < descendants[b]
		case !t.created[a].Equal(t.created[b]):
			return t.created[a].Before(t.created[b])
		}
		return a < b
	})
	choice := models.ScanChoice{TemplateID: tied[0].template, SourceCategoryID: tied[0].source}
	if len(templates) > 1 {
		choice.Conflict = true
		for _, c := range tied {
			choice.Contenders = append(choice.Contenders, c.source)
		}
		sort.Strings(choice.Contenders)
	}
	return choice
}

// shape counts each category's ancestors and descendants.
func shape(parents map[string]string) (depth, descendants map[string]int) {
	depth, descendants = map[string]int{}, map[string]int{}
	for id := range parents {
		for p, d := parents[id], 0; p != "" && d < 100; p, d = parents[p], d+1 {
			depth[id]++
			descendants[p]++
		}
	}
	return depth, descendants
}

// defaultScanTemplate returns the default scan template's ID, or "" if
// there is none.
func (s *Store) defaultScanTemplate() (string, error) {
	var id string
	err := s.QueryRow("SELECT id FROM reports WHERE kind = ? AND is_default = ?", ReportKindScan, true).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading the default scan template: %w", err)
	}
	return id, nil
}

// contextScan returns the scan template for an asset in these categories,
// reading the default the first time.
func (s *Store) contextScan(ctx *assetContext, categoryIDs []string) (models.ScanChoice, error) {
	traits, err := s.contextTraits(ctx)
	if err != nil {
		return models.ScanChoice{}, err
	}
	if ctx.defaultScan == nil {
		id, err := s.defaultScanTemplate()
		if err != nil {
			return models.ScanChoice{}, err
		}
		ctx.defaultScan = &id
	}
	return traits.chooseScan(categoryIDs, *ctx.defaultScan), nil
}

// CreateScanTemplate saves a new scan template with a spec its caller has
// checked.
func (s *Store) CreateScanTemplate(name string, spec []byte) (*models.Report, error) {
	name, err := normalizeReportName(name)
	if err != nil {
		return nil, err
	}
	id, now := uuid.NewString(), time.Now().UTC()
	if _, err := s.Exec("INSERT INTO reports (id, name, kind, spec, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)", id, name, ReportKindScan, string(spec), now, now); err != nil {
		return nil, fmt.Errorf("saving scan template: %w", err)
	}
	return s.GetReport(id)
}

// SetDefaultScanTemplate makes a scan template the default.
func (s *Store) SetDefaultScanTemplate(id string) error {
	r, err := s.GetReport(id)
	if err != nil {
		return err
	}
	if r.Kind != ReportKindScan {
		return ErrNotScanTemplate
	}
	if _, err := s.Exec("UPDATE reports SET is_default = CASE WHEN id = ? THEN TRUE ELSE FALSE END WHERE kind = ?", id, ReportKindScan); err != nil {
		return fmt.Errorf("setting the default scan template: %w", err)
	}
	return nil
}

// scanTemplateInUse says why a report cannot be deleted, if it is a scan
// template that is the default or that categories name.
func (s *Store) scanTemplateInUse(r *models.Report) error {
	if r.Kind != ReportKindScan {
		return nil
	}
	if r.Default {
		return fmt.Errorf("%w: it is the default; make another the default first", ErrReportInUse)
	}
	var n int
	if err := s.QueryRow("SELECT COUNT(*) FROM categories WHERE scan_template_id = ?", r.ID).Scan(&n); err != nil {
		return fmt.Errorf("counting categories: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("%w: %d categories name it", ErrReportInUse, n)
	}
	return nil
}

// SetCategoryScanTemplate names the scan template a category's assets
// show, or with nil, none, so they show their parent's.
func (s *Store) SetCategoryScanTemplate(categoryID string, templateID *string) (*models.Category, error) {
	if _, err := s.GetCategoryByID(categoryID); err != nil {
		return nil, err
	}
	if templateID != nil {
		r, err := s.GetReport(*templateID)
		if err != nil {
			return nil, err
		}
		if r.Kind != ReportKindScan {
			return nil, ErrNotScanTemplate
		}
	}
	if _, err := s.Exec("UPDATE categories SET scan_template_id = ?, updated_at = ? WHERE id = ?", templateID, time.Now().UTC(), categoryID); err != nil {
		return nil, fmt.Errorf("setting the scan template: %w", err)
	}
	return s.GetCategoryByID(categoryID)
}

// ScanConflicts lists the categories involved in active assets' scan
// template conflicts, with how many assets each is involved for. With
// categoryID set, it answers as if that category named templateID (nil for
// none): what saving it would do.
func (s *Store) ScanConflicts(categoryID string, templateID *string) (map[string]int, error) {
	traits, err := s.categoryTraits(storeQuerier{s})
	if err != nil {
		return nil, err
	}
	if categoryID != "" {
		if templateID == nil {
			delete(traits.scan, categoryID)
		} else {
			traits.scan[categoryID] = *templateID
		}
	}
	rows, err := s.Query(`SELECT ac.asset_id, ac.category_id FROM asset_categories ac
		JOIN assets a ON a.id = ac.asset_id WHERE a.archived_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("reading asset categories: %w", err)
	}
	defer rows.Close()
	cats := map[string][]string{}
	for rows.Next() {
		var asset, cat string
		if err := rows.Scan(&asset, &cat); err != nil {
			return nil, fmt.Errorf("scanning asset category: %w", err)
		}
		cats[asset] = append(cats[asset], cat)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	involved := map[string]int{}
	for _, c := range cats {
		for _, id := range traits.chooseScan(c, "").Contenders {
			involved[id]++
		}
	}
	return involved, nil
}
