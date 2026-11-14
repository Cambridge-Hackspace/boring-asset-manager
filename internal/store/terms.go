package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/terms"
)

// Related terms (#386, docs/data-model.md#related-terms): the graph search
// expands through. Each term and link records who added it, the assistant
// it came through, if any, and the import it came in, if any.

var (
	ErrTermNotFound       = errors.New("term not found")
	ErrTermImportNotFound = errors.New("import not found")
)

// TermProblem is a term or link that cannot be saved as given; its message
// says why, for the person who gave it.
type TermProblem struct{ Message string }

func (e *TermProblem) Error() string { return e.Message }

func termProblem(format string, args ...any) error {
	return &TermProblem{Message: fmt.Sprintf(format, args...)}
}

// TermLabelTaken is a preferred label another term has already, once
// folded.
type TermLabelTaken struct{ Label, TermID string }

func (e *TermLabelTaken) Error() string {
	return fmt.Sprintf("another term is already called %q", e.Label)
}

// Limits on a term, so that one cannot crowd out the rest.
const (
	MaxTermLabel     = 255
	MaxTermAltLabels = 50
	MaxTermNote      = 4000
	// MaxTermImport is how many terms one import may hold.
	MaxTermImport = 50_000
)

// TermInput is a term as given: its preferred label, its other names, and
// a note on what it means.
type TermInput struct {
	Label     string   `json:"label"`
	AltLabels []string `json:"alt_labels"`
	Note      string   `json:"note"`
}

// clean trims the term, drops other names that repeat it or each other,
// and checks it fits.
func (in TermInput) clean() (TermInput, error) {
	out := TermInput{Label: strings.Join(strings.Fields(in.Label), " "), Note: strings.TrimSpace(in.Note), AltLabels: []string{}}
	if out.Label == "" {
		return out, termProblem("a term needs a label")
	}
	if utf8.RuneCountInString(out.Label) > MaxTermLabel {
		return out, termProblem("a term's label is at most %d characters", MaxTermLabel)
	}
	if utf8.RuneCountInString(out.Note) > MaxTermNote {
		return out, termProblem("a term's note is at most %d characters", MaxTermNote)
	}
	seen := map[string]bool{terms.Fold(out.Label): true}
	for _, a := range in.AltLabels {
		a = strings.Join(strings.Fields(a), " ")
		if a == "" || seen[terms.Fold(a)] {
			continue
		}
		if utf8.RuneCountInString(a) > MaxTermLabel {
			return out, termProblem("another name for a term is at most %d characters", MaxTermLabel)
		}
		seen[terms.Fold(a)] = true
		out.AltLabels = append(out.AltLabels, a)
	}
	if len(out.AltLabels) > MaxTermAltLabels {
		return out, termProblem("a term has at most %d other names", MaxTermAltLabels)
	}
	return out, nil
}

// termSource is who is adding something now, as the columns record it.
func (s *Store) termSource() (by any, token bool, via string) {
	by, token = s.actorColumns()
	return by, token, s.via
}

const termColumns = "t.id, t.label, t.alt_labels, t.note, t.added_by, t.added_by_token, t.via, t.import_id, t.created_at, t.updated_at, " + sourceName
const linkColumns = "t.term_id, t.other_id, t.relation, t.added_by, t.added_by_token, t.via, t.import_id, t.created_at, " + sourceName

// sourceName names who added a row, a person or a token, as they are now.
const sourceName = "COALESCE(NULLIF(u.name, ''), u.email, k.name, '')"
const sourceJoins = " LEFT JOIN users u ON u.id = t.added_by LEFT JOIN api_tokens k ON k.id = t.added_by"

func scanTerm(r rowScanner) (models.Term, error) {
	var t models.Term
	var alts string
	var by, imp sql.NullString
	if err := r.Scan(&t.ID, &t.Label, &alts, &t.Note, &by, &t.AddedByToken, &t.Via, &imp, &t.CreatedAt, &t.UpdatedAt, &t.AddedByName); err != nil {
		return t, err
	}
	t.AddedBy, t.ImportID = by.String, imp.String
	t.AltLabels = []string{}
	if err := json.Unmarshal([]byte(alts), &t.AltLabels); err != nil {
		return t, fmt.Errorf("reading the other names of term %s: %w", t.ID, err)
	}
	t.CreatedAt, t.UpdatedAt = t.CreatedAt.UTC(), t.UpdatedAt.UTC()
	t.Source = t.Kind()
	return t, nil
}

func scanLink(r rowScanner) (models.TermLink, error) {
	var l models.TermLink
	var by, imp sql.NullString
	if err := r.Scan(&l.TermID, &l.OtherID, &l.Relation, &by, &l.AddedByToken, &l.Via, &imp, &l.CreatedAt, &l.AddedByName); err != nil {
		return l, err
	}
	l.AddedBy, l.ImportID = by.String, imp.String
	l.CreatedAt = l.CreatedAt.UTC()
	l.Source = l.Kind()
	return l, nil
}

// ListTerms is every term, by label, and every link between them.
func (s *Store) ListTerms() ([]models.Term, []models.TermLink, error) {
	rows, err := s.Query("SELECT " + termColumns + " FROM terms t" + sourceJoins)
	if err != nil {
		return nil, nil, fmt.Errorf("listing terms: %w", err)
	}
	defer rows.Close()
	list := []models.Term{}
	for rows.Next() {
		t, err := scanTerm(rows)
		if err != nil {
			return nil, nil, fmt.Errorf("listing terms: %w", err)
		}
		list = append(list, t)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("listing terms: %w", err)
	}
	sort.Slice(list, func(i, j int) bool {
		if a, b := terms.Fold(list[i].Label), terms.Fold(list[j].Label); a != b {
			return a < b
		}
		return list[i].ID < list[j].ID
	})
	links, err := s.listTermLinks()
	return list, links, err
}

func (s *Store) listTermLinks() ([]models.TermLink, error) {
	rows, err := s.Query("SELECT " + linkColumns + " FROM term_links t" + sourceJoins + " ORDER BY t.term_id, t.relation, t.other_id")
	if err != nil {
		return nil, fmt.Errorf("listing term links: %w", err)
	}
	defer rows.Close()
	out := []models.TermLink{}
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, fmt.Errorf("listing term links: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// TermGraph is the related terms, ready to expand searches with.
func (s *Store) TermGraph() (*terms.Graph, error) {
	list, links, err := s.ListTerms()
	if err != nil {
		return nil, err
	}
	return terms.New(list, links), nil
}

// GetTerm is one term.
func (s *Store) GetTerm(id string) (*models.Term, error) {
	t, err := scanTerm(s.QueryRow("SELECT "+termColumns+" FROM terms t"+sourceJoins+" WHERE t.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTermNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading term: %w", err)
	}
	return &t, nil
}

// labelTakenTx reports the term other than except already called label,
// once folded, if any.
func (s *Store) labelTakenTx(tx *sql.Tx, label, except string) (string, error) {
	rows, err := s.TxQuery(tx, "SELECT id, label FROM terms")
	if err != nil {
		return "", fmt.Errorf("reading terms: %w", err)
	}
	defer rows.Close()
	want := terms.Fold(label)
	for rows.Next() {
		var id, l string
		if err := rows.Scan(&id, &l); err != nil {
			return "", fmt.Errorf("reading terms: %w", err)
		}
		if id != except && terms.Fold(l) == want {
			return id, nil
		}
	}
	return "", rows.Err()
}

// CreateTerm adds a term.
func (s *Store) CreateTerm(in TermInput) (*models.Term, error) {
	in, err := in.clean()
	if err != nil {
		return nil, err
	}
	id := uuid.NewString()
	err = s.withTx(func(tx *sql.Tx) error { return s.insertTermTx(tx, id, in, nil) })
	if err != nil {
		return nil, err
	}
	return s.GetTerm(id)
}

func (s *Store) insertTermTx(tx *sql.Tx, id string, in TermInput, importID any) error {
	if taken, err := s.labelTakenTx(tx, in.Label, ""); err != nil {
		return err
	} else if taken != "" {
		return &TermLabelTaken{Label: in.Label, TermID: taken}
	}
	alts, err := json.Marshal(in.AltLabels)
	if err != nil {
		return err
	}
	by, token, via := s.termSource()
	now := dbTime(time.Now())
	if _, err := s.TxExec(tx, "INSERT INTO terms (id, label, alt_labels, note, added_by, added_by_token, via, import_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		id, in.Label, string(alts), in.Note, by, token, via, importID, now, now); err != nil {
		return fmt.Errorf("adding term: %w", err)
	}
	return nil
}

// UpdateTerm changes a term's labels and note. Who added it, and the
// import it came in, stay as they were.
func (s *Store) UpdateTerm(id string, in TermInput) (*models.Term, error) {
	in, err := in.clean()
	if err != nil {
		return nil, err
	}
	err = s.withTx(func(tx *sql.Tx) error {
		if taken, err := s.labelTakenTx(tx, in.Label, id); err != nil {
			return err
		} else if taken != "" {
			return &TermLabelTaken{Label: in.Label, TermID: taken}
		}
		alts, err := json.Marshal(in.AltLabels)
		if err != nil {
			return err
		}
		res, err := s.TxExec(tx, "UPDATE terms SET label = ?, alt_labels = ?, note = ?, updated_at = ? WHERE id = ?", in.Label, string(alts), in.Note, dbTime(time.Now()), id)
		if err != nil {
			return fmt.Errorf("changing term: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrTermNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetTerm(id)
}

// DeleteTerm removes a term and its links.
func (s *Store) DeleteTerm(id string) error {
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := s.TxExec(tx, "DELETE FROM term_links WHERE term_id = ? OR other_id = ?", id, id); err != nil {
			return fmt.Errorf("removing term links: %w", err)
		}
		res, err := s.TxExec(tx, "DELETE FROM terms WHERE id = ?", id)
		if err != nil {
			return fmt.Errorf("removing term: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrTermNotFound
		}
		return nil
	})
}

// termLinkStored is how a link between id and other by relation is stored:
// narrower is broader the other way, and related runs from the ID that
// sorts first.
func termLinkStored(id, relation, other string) (string, string, string, error) {
	switch relation {
	case models.TermBroader:
		return id, other, relation, nil
	case models.TermNarrower:
		return other, id, models.TermBroader, nil
	case models.TermRelated:
		if other < id {
			id, other = other, id
		}
		return id, other, relation, nil
	}
	return "", "", "", termProblem("a link is broader, narrower or related, not %q", relation)
}

// LinkTerms links two terms: id broader than other, narrower than it, or
// related to it. Linking them again changes nothing. A term is not linked
// to itself, and nothing is made narrower than itself.
func (s *Store) LinkTerms(id, relation, other string) error {
	return s.withTx(func(tx *sql.Tx) error {
		_, err := s.linkTermsTx(tx, id, relation, other, nil)
		return err
	})
}

// linkTermsTx links two terms, and reports whether it made a new link.
func (s *Store) linkTermsTx(tx *sql.Tx, id, relation, other string, importID any) (bool, error) {
	from, to, rel, err := termLinkStored(id, relation, other)
	if err != nil {
		return false, err
	}
	if from == to {
		return false, termProblem("a term isn't linked to itself")
	}
	for _, t := range []string{from, to} {
		var n int
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM terms WHERE id = ?", t).Scan(&n); err != nil {
			return false, fmt.Errorf("reading term: %w", err)
		}
		if n == 0 {
			return false, ErrTermNotFound
		}
	}
	var n int
	if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM term_links WHERE term_id = ? AND other_id = ? AND relation = ?", from, to, rel).Scan(&n); err != nil {
		return false, fmt.Errorf("reading term links: %w", err)
	}
	if n > 0 {
		return false, nil
	}
	if rel == models.TermBroader {
		// from becomes narrower than to: refused if to is already narrower
		// than from, however far down.
		loop, err := s.broaderReachesTx(tx, to, from)
		if err != nil {
			return false, err
		}
		if loop {
			return false, termProblem("that would make a term narrower than itself")
		}
	}
	by, token, via := s.termSource()
	if _, err := s.TxExec(tx, "INSERT INTO term_links (term_id, other_id, relation, added_by, added_by_token, via, import_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		from, to, rel, by, token, via, importID, dbTime(time.Now())); err != nil {
		return false, fmt.Errorf("linking terms: %w", err)
	}
	return true, nil
}

// broaderReachesTx reports whether following broader links up from start
// reaches target.
func (s *Store) broaderReachesTx(tx *sql.Tx, start, target string) (bool, error) {
	rows, err := s.TxQuery(tx, "SELECT term_id, other_id FROM term_links WHERE relation = ?", models.TermBroader)
	if err != nil {
		return false, fmt.Errorf("reading term links: %w", err)
	}
	up := map[string][]string{}
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			rows.Close()
			return false, fmt.Errorf("reading term links: %w", err)
		}
		up[a] = append(up[a], b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}
	seen := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		t := queue[0]
		queue = queue[1:]
		if t == target {
			return true, nil
		}
		for _, b := range up[t] {
			if !seen[b] {
				seen[b] = true
				queue = append(queue, b)
			}
		}
	}
	return false, nil
}

// UnlinkTerms removes a link between two terms, as LinkTerms names it.
func (s *Store) UnlinkTerms(id, relation, other string) error {
	from, to, rel, err := termLinkStored(id, relation, other)
	if err != nil {
		return err
	}
	res, err := s.Exec("DELETE FROM term_links WHERE term_id = ? AND other_id = ? AND relation = ?", from, to, rel)
	if err != nil {
		return fmt.Errorf("unlinking terms: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTermNotFound
	}
	return nil
}

// TermImportItem is a term in an import: its labels and note, and the
// terms broader than it and related to it, by label. A label no term has
// yet becomes a term of the import.
type TermImportItem struct {
	TermInput
	Broader []string `json:"broader"`
	Related []string `json:"related"`
}

// ImportTerms adds terms and links as one import, all or none. A term
// whose label another has already is not added again: its links are added
// to that one. Links that are there already, or would make a term
// narrower than itself, are left out.
func (s *Store) ImportTerms(name string, items []TermImportItem) (*models.TermImport, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, termProblem("an import needs a name, saying what it is")
	}
	if utf8.RuneCountInString(name) > MaxTermLabel {
		return nil, termProblem("an import's name is at most %d characters", MaxTermLabel)
	}
	if len(items) == 0 {
		return nil, termProblem("an import needs at least one term")
	}
	if len(items) > MaxTermImport {
		return nil, termProblem("an import holds at most %d terms", MaxTermImport)
	}
	cleaned := make([]TermImportItem, len(items))
	for i, it := range items {
		in, err := it.TermInput.clean()
		if err != nil {
			return nil, termProblem("term %d: %s", i+1, err.Error())
		}
		cleaned[i] = TermImportItem{TermInput: in, Broader: it.Broader, Related: it.Related}
	}
	imp := &models.TermImport{ID: uuid.NewString(), Name: name, CreatedAt: dbTime(time.Now())}
	err := s.withTx(func(tx *sql.Tx) error {
		by, token, via := s.termSource()
		if _, err := s.TxExec(tx, "INSERT INTO term_imports (id, name, added_by, added_by_token, via, created_at) VALUES (?, ?, ?, ?, ?, ?)",
			imp.ID, imp.Name, by, token, via, imp.CreatedAt); err != nil {
			return fmt.Errorf("adding import: %w", err)
		}
		// Every term there is, by folded label.
		ids := map[string]string{}
		rows, err := s.TxQuery(tx, "SELECT id, label FROM terms")
		if err != nil {
			return fmt.Errorf("reading terms: %w", err)
		}
		for rows.Next() {
			var id, l string
			if err := rows.Scan(&id, &l); err != nil {
				rows.Close()
				return fmt.Errorf("reading terms: %w", err)
			}
			ids[terms.Fold(l)] = id
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		termFor := func(in TermInput) (string, error) {
			if id, ok := ids[terms.Fold(in.Label)]; ok {
				return id, nil
			}
			id := uuid.NewString()
			if err := s.insertTermTx(tx, id, in, imp.ID); err != nil {
				return "", err
			}
			ids[terms.Fold(in.Label)] = id
			imp.Terms++
			return id, nil
		}
		for _, it := range cleaned {
			if _, err := termFor(it.TermInput); err != nil {
				return err
			}
		}
		link := func(id, relation, label string) error {
			in, err := TermInput{Label: label}.clean()
			if err != nil {
				return err
			}
			other, err := termFor(in)
			if err != nil {
				return err
			}
			made, err := s.linkTermsTx(tx, id, relation, other, imp.ID)
			var tp *TermProblem
			if errors.As(err, &tp) {
				return nil
			}
			if made {
				imp.Links++
			}
			return err
		}
		for _, it := range cleaned {
			id := ids[terms.Fold(it.Label)]
			for _, b := range it.Broader {
				if err := link(id, models.TermBroader, b); err != nil {
					return err
				}
			}
			for _, r := range it.Related {
				if err := link(id, models.TermRelated, r); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetTermImport(imp.ID)
}

const importColumns = "t.id, t.name, t.added_by, t.added_by_token, t.via, t.created_at, " + sourceName +
	", (SELECT COUNT(*) FROM terms x WHERE x.import_id = t.id), (SELECT COUNT(*) FROM term_links x WHERE x.import_id = t.id)"

func scanImport(r rowScanner) (models.TermImport, error) {
	var m models.TermImport
	var by sql.NullString
	if err := r.Scan(&m.ID, &m.Name, &by, &m.AddedByToken, &m.Via, &m.CreatedAt, &m.AddedByName, &m.Terms, &m.Links); err != nil {
		return m, err
	}
	m.AddedBy, m.CreatedAt = by.String, m.CreatedAt.UTC()
	return m, nil
}

// ListTermImports is every import, newest first, with how many of its
// terms and links remain.
func (s *Store) ListTermImports() ([]models.TermImport, error) {
	rows, err := s.Query("SELECT " + importColumns + " FROM term_imports t" + sourceJoins + " ORDER BY t.created_at DESC, t.id")
	if err != nil {
		return nil, fmt.Errorf("listing imports: %w", err)
	}
	defer rows.Close()
	out := []models.TermImport{}
	for rows.Next() {
		m, err := scanImport(rows)
		if err != nil {
			return nil, fmt.Errorf("listing imports: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetTermImport is one import.
func (s *Store) GetTermImport(id string) (*models.TermImport, error) {
	m, err := scanImport(s.QueryRow("SELECT "+importColumns+" FROM term_imports t"+sourceJoins+" WHERE t.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTermImportNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading import: %w", err)
	}
	return &m, nil
}

// DeleteTermImport removes an import: the terms and links it added, and
// any link to its terms, however added.
func (s *Store) DeleteTermImport(id string) error {
	return s.withTx(func(tx *sql.Tx) error {
		for _, q := range []string{
			"DELETE FROM term_links WHERE import_id = ?",
			"DELETE FROM term_links WHERE term_id IN (SELECT id FROM terms WHERE import_id = ?)",
			"DELETE FROM term_links WHERE other_id IN (SELECT id FROM terms WHERE import_id = ?)",
			"DELETE FROM terms WHERE import_id = ?",
		} {
			if _, err := s.TxExec(tx, q, id); err != nil {
				return fmt.Errorf("removing import: %w", err)
			}
		}
		res, err := s.TxExec(tx, "DELETE FROM term_imports WHERE id = ?", id)
		if err != nil {
			return fmt.Errorf("removing import: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrTermImportNotFound
		}
		return nil
	})
}
