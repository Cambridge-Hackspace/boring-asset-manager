package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/google/uuid"
)

// Comments (docs/comments.md): notes in Markdown on assets, categories,
// loans and flows' filings, each kind in a table of its own, so that each
// goes with its owner's kind of information in backups. Every version of a
// comment's text is kept in the events of what it is on: a loan's and a
// filing's in their asset's.

// MaxCommentLength is the most characters a comment may have.
const MaxCommentLength = 20000

var (
	ErrCommentNotFound = errors.New("comment not found")
	ErrCommentEmpty    = errors.New("a comment needs some text")
	ErrCommentTooLong  = fmt.Errorf("a comment is limited to %d characters", MaxCommentLength)
	// ErrCommentDeleted is an edit or a deletion of a comment already
	// deleted.
	ErrCommentDeleted = errors.New("the comment has been deleted")
	ErrLoanNotFound   = errors.New("loan not found")
	ErrFilingNotFound = errors.New("the flow did not file that asset")
)

// CommentTarget is what a comment is on: one of the models.CommentOn…
// kinds, and its ID, or for a filing the asset's ID with the flow's.
type CommentTarget struct {
	On, ID, FlowID string
}

// commentTable is where comments on one kind of thing are kept, and the
// columns naming what each is on.
type commentTable struct {
	on, table string
	owner     []string
}

var commentTables = []commentTable{
	{models.CommentOnAsset, "asset_comments", []string{"asset_id"}},
	{models.CommentOnCategory, "category_comments", []string{"category_id"}},
	{models.CommentOnLoan, "loan_comments", []string{"loan_id"}},
	{models.CommentOnFiling, "flow_filing_comments", []string{"flow_id", "asset_id"}},
}

func commentTableOf(on string) (commentTable, error) {
	for _, t := range commentTables {
		if t.on == on {
			return t, nil
		}
	}
	return commentTable{}, fmt.Errorf("comments are not kept on %q", on)
}

// keys are the values of a target's owner columns, in their order.
func (t CommentTarget) keys() []any {
	if t.On == models.CommentOnFiling {
		return []any{t.FlowID, t.ID}
	}
	return []any{t.ID}
}

// where matches the comments on a target, in a table aliased c.
func (ct commentTable) where() string {
	parts := make([]string, len(ct.owner))
	for i, col := range ct.owner {
		parts[i] = "c." + col + " = ?"
	}
	return strings.Join(parts, " AND ")
}

// commentSelect reads comments from a table aliased c, with the names of
// who wrote and deleted them, as commentScan reads them.
func commentSelect(ct commentTable) string {
	return "SELECT c.id, " + strings.Join(prefixed("c.", ct.owner), ", ") + `, c.body, c.author_id, c.created_at, c.edited_at, c.deleted_at, c.deleted_by,
		au.name, au.email, aut.name, du.name, du.email, dut.name
		FROM ` + ct.table + ` c
		LEFT JOIN users au ON au.id = c.author_id LEFT JOIN api_tokens aut ON aut.id = c.author_id
		LEFT JOIN users du ON du.id = c.deleted_by LEFT JOIN api_tokens dut ON dut.id = c.deleted_by`
}

func prefixed(prefix string, cols []string) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = prefix + c
	}
	return out
}

// scanComment reads a row of commentSelect.
func scanComment(ct commentTable, row interface{ Scan(...any) error }) (models.Comment, error) {
	var (
		c                                   = models.Comment{On: ct.on}
		owner                               = make([]string, len(ct.owner))
		author, deletedBy                   sql.NullString
		edited, deleted                     sql.NullTime
		aName, aEmail, aToken, dName, dMail sql.NullString
		dToken                              sql.NullString
	)
	dest := []any{&c.ID}
	for i := range owner {
		dest = append(dest, &owner[i])
	}
	dest = append(dest, &c.Body, &author, &c.CreatedAt, &edited, &deleted, &deletedBy, &aName, &aEmail, &aToken, &dName, &dMail, &dToken)
	if err := row.Scan(dest...); err != nil {
		return c, err
	}
	switch ct.on {
	case models.CommentOnFiling:
		c.FlowID, c.OnID, c.AssetID = owner[0], owner[1], owner[1]
	case models.CommentOnAsset:
		c.OnID, c.AssetID = owner[0], owner[0]
	default:
		c.OnID = owner[0]
	}
	c.AuthorID, c.AuthorName = author.String, personOrToken(aName, aEmail, aToken)
	c.DeletedBy, c.DeletedByName = deletedBy.String, personOrToken(dName, dMail, dToken)
	c.CreatedAt = c.CreatedAt.UTC()
	if edited.Valid {
		t := edited.Time.UTC()
		c.EditedAt = &t
	}
	if deleted.Valid {
		t := deleted.Time.UTC()
		c.DeletedAt = &t
	}
	return c, nil
}

// personOrToken names a user, by their name or else their email, or an
// API token.
func personOrToken(name, email, token sql.NullString) string {
	switch {
	case name.Valid && name.String != "":
		return name.String
	case email.Valid:
		return email.String
	}
	return token.String
}

// checkCommentTarget reports whether what a comment would be on exists.
func (s *Store) checkCommentTarget(tx *sql.Tx, t CommentTarget) error {
	var query string
	var notFound error
	switch t.On {
	case models.CommentOnAsset:
		query, notFound = "SELECT 1 FROM assets WHERE id = ?", ErrAssetNotFound
	case models.CommentOnCategory:
		query, notFound = "SELECT 1 FROM categories WHERE id = ?", ErrCategoryNotFound
	case models.CommentOnLoan:
		query, notFound = "SELECT 1 FROM asset_loans WHERE id = ?", ErrLoanNotFound
	case models.CommentOnFiling:
		query, notFound = "SELECT 1 FROM flow_filings WHERE flow_id = ? AND asset_id = ?", ErrFilingNotFound
	default:
		return fmt.Errorf("comments are not kept on %q", t.On)
	}
	var one int
	err := s.TxQueryRow(tx, query, t.keys()...).Scan(&one)
	if err == sql.ErrNoRows {
		return notFound
	}
	if err != nil {
		return fmt.Errorf("reading what the comment is on: %w", err)
	}
	return nil
}

// checkCommentBody refuses a comment with no text, or too much.
func checkCommentBody(body string) error {
	if strings.TrimSpace(body) == "" {
		return ErrCommentEmpty
	}
	if utf8.RuneCountInString(body) > MaxCommentLength {
		return ErrCommentTooLong
	}
	return nil
}

// AddComment writes a comment on a target, as the acting user or token,
// and records it in the history of what it is on, and the people it
// mentions, to be told.
func (s *Store) AddComment(t CommentTarget, body string) (*models.Comment, error) {
	if err := checkCommentBody(body); err != nil {
		return nil, err
	}
	ct, err := commentTableOf(t.On)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.checkCommentTarget(tx, t); err != nil {
		return nil, err
	}
	id := uuid.NewString()
	cols := append(append([]string{"id"}, ct.owner...), "body", "author_id", "created_at")
	args := append(append([]any{id}, t.keys()...), body, s.actorID(), dbTime(time.Now()))
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ")
	if _, err := s.TxExec(tx, "INSERT INTO "+ct.table+" ("+strings.Join(cols, ", ")+") VALUES ("+marks+")", args...); err != nil {
		return nil, fmt.Errorf("adding the comment: %w", err)
	}
	mentions, err := s.recordMentionsTx(tx, id, body)
	if err != nil {
		return nil, err
	}
	if err := s.recordCommentTx(tx, models.EventCommentAdded, t, models.EventComment{ID: id, Text: body, Mentions: mentions}, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing the comment: %w", err)
	}
	return s.GetComment(id)
}

// recordCommentTx records an event about a comment in the history of what
// it is on: a loan's and a filing's in their asset's.
func (s *Store) recordCommentTx(tx *sql.Tx, kind string, t CommentTarget, ec models.EventComment, changes []models.EventChange) error {
	ec.On = t.On
	payload := models.EventPayload{Trigger: models.TriggerManual, Comment: &ec, Changes: changes}
	switch t.On {
	case models.CommentOnCategory:
		return s.recordEvent(tx, kind, t.ID, payload)
	case models.CommentOnLoan:
		var asset string
		if err := s.TxQueryRow(tx, "SELECT asset_id FROM asset_loans WHERE id = ?", t.ID).Scan(&asset); err != nil {
			return fmt.Errorf("reading the loan for its history: %w", err)
		}
		ec.LoanID = t.ID
		return s.recordAssetEvent(tx, kind, asset, payload)
	case models.CommentOnFiling:
		flow := models.EventFlow{}
		if err := s.TxQueryRow(tx, "SELECT slug, name FROM flows WHERE id = ?", t.FlowID).Scan(&flow.Slug, &flow.Name); err != nil {
			return fmt.Errorf("reading the flow for its history: %w", err)
		}
		ec.Flow = &flow
		return s.recordAssetEvent(tx, kind, t.ID, payload)
	}
	return s.recordAssetEvent(tx, kind, t.ID, payload)
}

// GetComment reads a comment, whatever it is on.
func (s *Store) GetComment(id string) (*models.Comment, error) {
	return s.getComment(nil, id)
}

func (s *Store) getComment(tx *sql.Tx, id string) (*models.Comment, error) {
	for _, ct := range commentTables {
		query := commentSelect(ct) + " WHERE c.id = ?"
		var row *sql.Row
		if tx != nil {
			row = s.TxQueryRow(tx, query, id)
		} else {
			row = s.QueryRow(query, id)
		}
		c, err := scanComment(ct, row)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading the comment: %w", err)
		}
		if ct.on == models.CommentOnLoan {
			if err := s.loanAssetInto(tx, &c); err != nil {
				return nil, err
			}
		}
		return &c, nil
	}
	return nil, ErrCommentNotFound
}

// loanAssetInto fills in the asset a loan's comment is about.
func (s *Store) loanAssetInto(tx *sql.Tx, c *models.Comment) error {
	var row *sql.Row
	if tx != nil {
		row = s.TxQueryRow(tx, "SELECT asset_id FROM asset_loans WHERE id = ?", c.OnID)
	} else {
		row = s.QueryRow("SELECT asset_id FROM asset_loans WHERE id = ?", c.OnID)
	}
	if err := row.Scan(&c.AssetID); err != nil {
		return fmt.Errorf("reading the comment's loan: %w", err)
	}
	return nil
}

// TargetOf is what a comment is on.
func TargetOf(c *models.Comment) CommentTarget {
	return CommentTarget{On: c.On, ID: c.OnID, FlowID: c.FlowID}
}

// EditComment replaces a comment's text, marking it edited and keeping
// what it said in the history of what it is on. The same text again
// changes nothing.
func (s *Store) EditComment(id, body string) (*models.Comment, error) {
	if err := checkCommentBody(body); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	c, err := s.getComment(tx, id)
	if err != nil {
		return nil, err
	}
	if c.DeletedAt != nil {
		return nil, ErrCommentDeleted
	}
	if c.Body == body {
		return c, nil
	}
	ct, _ := commentTableOf(c.On)
	if _, err := s.TxExec(tx, "UPDATE "+ct.table+" SET body = ?, edited_at = ? WHERE id = ?", body, dbTime(time.Now()), id); err != nil {
		return nil, fmt.Errorf("editing the comment: %w", err)
	}
	// Only those the edit mentions first are told; whoever the comment
	// mentioned before was told then.
	if _, err := s.recordMentionsTx(tx, id, body); err != nil {
		return nil, err
	}
	change := models.EventChange{What: "comment", Key: id, From: c.Body, To: body}
	if err := s.recordCommentTx(tx, models.EventCommentEdited, TargetOf(c), models.EventComment{ID: id}, []models.EventChange{change}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing the edit: %w", err)
	}
	return s.GetComment(id)
}

// DeleteComment deletes a comment's text, as the acting user or token,
// leaving its place marked as deleted by them, and keeping what it said in
// the history of what it is on.
func (s *Store) DeleteComment(id string) (*models.Comment, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	c, err := s.getComment(tx, id)
	if err != nil {
		return nil, err
	}
	if c.DeletedAt != nil {
		return nil, ErrCommentDeleted
	}
	ct, _ := commentTableOf(c.On)
	if _, err := s.TxExec(tx, "UPDATE "+ct.table+" SET body = ?, deleted_at = ?, deleted_by = ? WHERE id = ?", "", dbTime(time.Now()), s.actorID(), id); err != nil {
		return nil, fmt.Errorf("deleting the comment: %w", err)
	}
	change := models.EventChange{What: "comment", Key: id, From: c.Body}
	if err := s.recordCommentTx(tx, models.EventCommentDeleted, TargetOf(c), models.EventComment{ID: id}, []models.EventChange{change}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing the deletion: %w", err)
	}
	return s.GetComment(id)
}

// ListComments lists the comments on a target, oldest first. With
// ancestors, an asset's are followed by those on its categories and their
// ancestors, and a category's by those on its ancestors: each category's
// together, nearest first, each marked with the category it is on.
func (s *Store) ListComments(t CommentTarget, ancestors bool) ([]models.Comment, error) {
	ct, err := commentTableOf(t.On)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.checkCommentTarget(tx, t); err != nil {
		return nil, err
	}
	out, err := s.commentsWhere(tx, ct, ct.where(), t.keys())
	if err != nil {
		return nil, err
	}
	if t.On == models.CommentOnLoan {
		for i := range out {
			if err := s.loanAssetInto(tx, &out[i]); err != nil {
				return nil, err
			}
		}
	}
	if !ancestors || (t.On != models.CommentOnAsset && t.On != models.CommentOnCategory) {
		return out, nil
	}
	cats, err := s.commentAncestors(tx, t)
	if err != nil {
		return nil, err
	}
	if len(cats) == 0 {
		return out, nil
	}
	cc, _ := commentTableOf(models.CommentOnCategory)
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(cats)), ", ")
	args := make([]any, len(cats))
	for i, c := range cats {
		args[i] = c.ID
	}
	inherited, err := s.commentsWhere(tx, cc, "c.category_id IN ("+marks+")", args)
	if err != nil {
		return nil, err
	}
	byCat := map[string][]models.Comment{}
	for _, c := range inherited {
		byCat[c.OnID] = append(byCat[c.OnID], c)
	}
	for _, cat := range cats {
		place := cat
		for _, c := range byCat[cat.ID] {
			c.Category = &place
			out = append(out, c)
		}
	}
	return out, nil
}

// commentsWhere reads the comments of a table matching where, oldest
// first.
func (s *Store) commentsWhere(tx *sql.Tx, ct commentTable, where string, args []any) ([]models.Comment, error) {
	rows, err := s.TxQuery(tx, commentSelect(ct)+" WHERE "+where+" ORDER BY c.created_at, c.id", args...)
	if err != nil {
		return nil, fmt.Errorf("listing comments: %w", err)
	}
	defer rows.Close()
	out := []models.Comment{}
	for rows.Next() {
		c, err := scanComment(ct, rows)
		if err != nil {
			return nil, fmt.Errorf("reading a comment: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// commentAncestors lists the categories whose comments an asset's or a
// category's list carries, nearest first: an asset's categories, then
// their parents, and so on; a category's parent, then its parent. Those
// as near as each other come by name.
func (s *Store) commentAncestors(tx *sql.Tx, t CommentTarget) ([]models.CommentPlace, error) {
	rows, err := s.TxQuery(tx, "SELECT id, name, parent_id FROM categories")
	if err != nil {
		return nil, fmt.Errorf("reading categories: %w", err)
	}
	names, parents := map[string]string{}, map[string]string{}
	for rows.Next() {
		var id, name string
		var parent sql.NullString
		if err := rows.Scan(&id, &name, &parent); err != nil {
			rows.Close()
			return nil, err
		}
		names[id] = name
		if parent.Valid {
			parents[id] = parent.String
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var level []string
	seen := map[string]bool{}
	if t.On == models.CommentOnAsset {
		rows, err := s.TxQuery(tx, "SELECT category_id FROM asset_categories WHERE asset_id = ?", t.ID)
		if err != nil {
			return nil, fmt.Errorf("reading the asset's categories: %w", err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			level = append(level, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	} else {
		seen[t.ID] = true
		if p, ok := parents[t.ID]; ok {
			level = []string{p}
		}
	}
	var out []models.CommentPlace
	for len(level) > 0 {
		sort.Slice(level, func(i, j int) bool {
			if names[level[i]] != names[level[j]] {
				return names[level[i]] < names[level[j]]
			}
			return level[i] < level[j]
		})
		var next []string
		for _, id := range level {
			if seen[id] {
				continue
			}
			seen[id] = true
			if _, ok := names[id]; !ok {
				continue
			}
			out = append(out, models.CommentPlace{ID: id, Name: names[id]})
			if p, ok := parents[id]; ok {
				next = append(next, p)
			}
		}
		level = next
	}
	return out, nil
}

// CommentHistory lists the events of a comment, oldest first: when it was
// written, each edit with what it said before, and its deletion.
func (s *Store) CommentHistory(id string) ([]models.Event, error) {
	c, err := s.GetComment(id)
	if err != nil {
		return nil, err
	}
	column, owner := "asset_id", c.AssetID
	if c.On == models.CommentOnCategory {
		column, owner = "category_id", c.OnID
	}
	rows, err := s.Query("SELECT "+eventColumns+" FROM events WHERE "+column+" = ? AND kind IN (?, ?, ?) ORDER BY created_at, id",
		owner, models.EventCommentAdded, models.EventCommentEdited, models.EventCommentDeleted)
	if err != nil {
		return nil, fmt.Errorf("reading the comment's history: %w", err)
	}
	defer rows.Close()
	out := []models.Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		if e.Payload.Comment != nil && e.Payload.Comment.ID == id {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

// MaxCommentResults is the most comments a search finds.
const MaxCommentResults = 100

// SearchComments finds the comments, not deleted, whose text holds term,
// ignoring case, newest first, each with Place naming what it is on: for
// a loan's or a filing's, its asset. It matches in Go, as MatchesSearch
// does, the same way on every database. Who may see each is the caller's
// to decide.
func (s *Store) SearchComments(term string) ([]models.Comment, error) {
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return []models.Comment{}, nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	var out []models.Comment
	for _, ct := range commentTables {
		found, err := s.commentsWhere(tx, ct, "c.deleted_at IS NULL", nil)
		if err != nil {
			return nil, err
		}
		for _, c := range found {
			if strings.Contains(strings.ToLower(c.Body), term) {
				out = append(out, c)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > MaxCommentResults {
		out = out[:MaxCommentResults]
	}
	for i := range out {
		if err := s.placeComment(tx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// placeComment fills in what a comment is on, by name: the category, or
// the asset its loan or filing is about.
func (s *Store) placeComment(tx *sql.Tx, c *models.Comment) error {
	if c.On == models.CommentOnLoan {
		if err := s.loanAssetInto(tx, c); err != nil {
			return err
		}
	}
	p := &models.CommentPlace{}
	var err error
	if c.On == models.CommentOnCategory {
		p.ID = c.OnID
		err = s.TxQueryRow(tx, "SELECT name FROM categories WHERE id = ?", c.OnID).Scan(&p.Name)
	} else {
		p.ID = c.AssetID
		err = s.TxQueryRow(tx, "SELECT name FROM assets WHERE id = ?", c.AssetID).Scan(&p.Name)
	}
	if err != nil {
		return fmt.Errorf("reading what a comment is on: %w", err)
	}
	c.Place = p
	return nil
}

// CommentedAssets are the assets whose comments, or whose loans' or
// filings' comments, not deleted, hold term, ignoring case, so that a
// search of assets finds them by their comments.
func (s *Store) CommentedAssets(term string) (map[string]bool, error) {
	term = strings.ToLower(strings.TrimSpace(term))
	out := map[string]bool{}
	if term == "" {
		return out, nil
	}
	rows, err := s.Query(`SELECT asset_id, body FROM asset_comments WHERE deleted_at IS NULL
		UNION ALL SELECT l.asset_id, c.body FROM loan_comments c JOIN asset_loans l ON l.id = c.loan_id WHERE c.deleted_at IS NULL
		UNION ALL SELECT asset_id, body FROM flow_filing_comments WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("searching comments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, body string
		if err := rows.Scan(&id, &body); err != nil {
			return nil, err
		}
		if strings.Contains(strings.ToLower(body), term) {
			out[id] = true
		}
	}
	return out, rows.Err()
}

// LoanAsset is the asset a loan lends.
func (s *Store) LoanAsset(loanID string) (string, error) {
	var asset string
	err := s.QueryRow("SELECT asset_id FROM asset_loans WHERE id = ?", loanID).Scan(&asset)
	if err == sql.ErrNoRows {
		return "", ErrLoanNotFound
	}
	if err != nil {
		return "", fmt.Errorf("reading the loan: %w", err)
	}
	return asset, nil
}

// ListFilings lists the flows that filed an asset, most recently first,
// with how many comments each filing has.
func (s *Store) ListFilings(assetID string) ([]models.Filing, error) {
	rows, err := s.Query(`SELECT ff.flow_id, f.slug, f.name, ff.asset_id, ff.filed_at,
		(SELECT COUNT(*) FROM flow_filing_comments c WHERE c.flow_id = ff.flow_id AND c.asset_id = ff.asset_id AND c.deleted_at IS NULL)
		FROM flow_filings ff JOIN flows f ON f.id = ff.flow_id
		WHERE ff.asset_id = ? ORDER BY ff.filed_at DESC, f.name`, assetID)
	if err != nil {
		return nil, fmt.Errorf("listing the asset's filings: %w", err)
	}
	defer rows.Close()
	out := []models.Filing{}
	for rows.Next() {
		var f models.Filing
		if err := rows.Scan(&f.FlowID, &f.FlowSlug, &f.FlowName, &f.AssetID, &f.FiledAt, &f.Comments); err != nil {
			return nil, err
		}
		f.FiledAt = f.FiledAt.UTC()
		out = append(out, f)
	}
	return out, rows.Err()
}
