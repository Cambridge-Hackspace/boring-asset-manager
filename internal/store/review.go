package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/google/uuid"
)

// Review states (docs/review.md): an asset in a category that uses them is
// draft, confirmed or verified, and can be asked to be reviewed again a
// number of months after it was last confirmed or verified. Changes made
// through an assistant can land as drafts, for a person to accept or
// reject.

var (
	// ErrReviewOff is returned for a review move on an asset none of whose
	// categories uses review states.
	ErrReviewOff = errors.New("none of the asset's categories uses review states")
	// ErrReviewMove is returned for a move its state does not allow:
	// verifying a draft, or returning a draft to draft.
	ErrReviewMove = errors.New("the asset's review state does not allow that")
	// ErrReviewOwnConfirmation is returned for verifying what one confirmed
	// oneself, where its categories do not allow it.
	ErrReviewOwnConfirmation = errors.New("someone other than whoever confirmed it must verify it")
	// ErrReviewNoteRequired is returned for returning an asset to draft
	// without saying why.
	ErrReviewNoteRequired = errors.New("say why it goes back to draft")
	// ErrReviewByAssistant is returned for confirming, verifying, or
	// accepting changes through an assistant: it proposes, and a person
	// decides.
	ErrReviewByAssistant = errors.New("an assistant cannot confirm, verify or accept changes: a person does")
	// ErrProposalPending is returned for confirming or verifying an asset
	// with changes from an assistant waiting: they are accepted or rejected
	// first.
	ErrProposalPending = errors.New("an assistant's changes to it are waiting to be accepted or rejected")
	// ErrNoProposal is returned for accepting or rejecting changes when
	// none are waiting.
	ErrNoProposal = errors.New("no changes from an assistant are waiting")
	// ErrProposalNotUndoable is returned for rejecting changes whose
	// asset's earlier record could not be kept: they can only be accepted.
	ErrProposalNotUndoable = errors.New("these changes cannot be undone: accept them, then edit the asset")
)

// The instance's review settings.
const (
	// ReviewAssistantDraftsSetting is "off" when changes through an
	// assistant are taken as the person's own; on by default, landing them
	// as drafts wherever review states are on.
	ReviewAssistantDraftsSetting = "review.assistant_drafts"
	// ReviewDueDaysSetting is how many days before an asset's review date
	// it shows as due, and asset.review_due is recorded.
	ReviewDueDaysSetting = "review.due_days"
)

// The default, and the most, days before a review date it falls due, and
// the most months a category can ask for between reviews.
const (
	DefaultReviewDueDays = 14
	MaxReviewDueDays     = 365
	MaxReviewMonths      = 120
	// MaxReviewNote is the longest note a move takes, in characters.
	MaxReviewNote = 1000
)

// ReviewSettings are the instance's choices.
type ReviewSettings struct {
	AssistantDrafts bool `json:"assistant_drafts"`
	DueDays         int  `json:"due_days"`
}

// rowQuery runs a query for one row, in or outside a transaction.
type rowQuery func(query string, args ...any) *sql.Row

func (s *Store) txRow(tx *sql.Tx) rowQuery {
	return func(query string, args ...any) *sql.Row { return s.TxQueryRow(tx, query, args...) }
}

// settingFrom reads an instance setting through row, or def when unset.
func settingFrom(row rowQuery, key, def string) (string, error) {
	var v string
	err := row("SELECT value FROM settings WHERE setting_key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return def, nil
	}
	if err != nil {
		return "", fmt.Errorf("reading setting %s: %w", key, err)
	}
	return v, nil
}

func reviewSettingsFrom(row rowQuery) (ReviewSettings, error) {
	out := ReviewSettings{AssistantDrafts: true, DueDays: DefaultReviewDueDays}
	v, err := settingFrom(row, ReviewAssistantDraftsSetting, "on")
	if err != nil {
		return out, err
	}
	out.AssistantDrafts = v != "off"
	if v, err = settingFrom(row, ReviewDueDaysSetting, ""); err != nil {
		return out, err
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= MaxReviewDueDays {
		out.DueDays = n
	}
	return out, nil
}

// ReviewSettings reads the instance's review settings.
func (s *Store) ReviewSettings() (ReviewSettings, error) {
	return reviewSettingsFrom(s.QueryRow)
}

// SetReviewSettings saves the instance's review settings.
func (s *Store) SetReviewSettings(in ReviewSettings) error {
	if in.DueDays < 0 || in.DueDays > MaxReviewDueDays {
		return fmt.Errorf("%w: days before a review is due must be 0 to %d", ErrInvalidSetting, MaxReviewDueDays)
	}
	on := "off"
	if in.AssistantDrafts {
		on = "on"
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.setSettingTx(tx, ReviewAssistantDraftsSetting, on); err != nil {
		return err
	}
	if err := s.setSettingTx(tx, ReviewDueDaysSetting, strconv.Itoa(in.DueDays)); err != nil {
		return err
	}
	return tx.Commit()
}

// reviewTree is each category's parent and own review settings, from which
// what is in effect for a category, or an asset, is worked out.
type reviewTree struct {
	parents map[string]string
	own     map[string]models.CategoryReview
}

// reviewColumns are a category's review settings, in scanReview's order.
const reviewColumns = "review_enabled, review_months, review_on_edit, review_self_verify"

// reviewScan holds a category's review columns as read.
type reviewScan struct {
	enabled, selfVerify sql.NullBool
	months              sql.NullInt64
	onEdit              sql.NullString
}

func (r *reviewScan) dest() []any { return []any{&r.enabled, &r.months, &r.onEdit, &r.selfVerify} }

func (r *reviewScan) review() models.CategoryReview {
	var out models.CategoryReview
	if r.enabled.Valid {
		out.Enabled = &r.enabled.Bool
	}
	if r.months.Valid {
		m := int(r.months.Int64)
		out.Months = &m
	}
	if r.onEdit.Valid && r.onEdit.String != "" {
		out.OnEdit = &r.onEdit.String
	}
	if r.selfVerify.Valid {
		out.SelfVerify = &r.selfVerify.Bool
	}
	return out
}

func readReviewTree(q interface {
	Query(string, ...any) (*sql.Rows, error)
}) (reviewTree, error) {
	rows, err := q.Query("SELECT id, parent_id, " + reviewColumns + " FROM categories")
	if err != nil {
		return reviewTree{}, fmt.Errorf("reading categories' review settings: %w", err)
	}
	defer rows.Close()
	t := reviewTree{parents: map[string]string{}, own: map[string]models.CategoryReview{}}
	for rows.Next() {
		var (
			id     string
			parent sql.NullString
			rs     reviewScan
		)
		if err := rows.Scan(append([]any{&id, &parent}, rs.dest()...)...); err != nil {
			return reviewTree{}, fmt.Errorf("scanning a category's review settings: %w", err)
		}
		t.parents[id], t.own[id] = parent.String, rs.review()
	}
	return t, rows.Err()
}

// rules is what is in effect for a category: each setting its own, or else
// the nearest ancestor's that sets it, or else the default.
func (t reviewTree) rules(id string) models.ReviewRules {
	var (
		enabled, selfVerify *bool
		months              *int
		onEdit              *string
	)
	for c, depth := id, 0; c != "" && depth < 100; c, depth = t.parents[c], depth+1 {
		own := t.own[c]
		if enabled == nil {
			enabled = own.Enabled
		}
		if months == nil {
			months = own.Months
		}
		if onEdit == nil {
			onEdit = own.OnEdit
		}
		if selfVerify == nil {
			selfVerify = own.SelfVerify
		}
	}
	out := models.ReviewRules{OnEdit: models.ReviewOnEditDraft}
	if enabled != nil {
		out.Enabled = *enabled
	}
	if months != nil {
		out.Months = *months
	}
	if onEdit != nil {
		out.OnEdit = *onEdit
	}
	if selfVerify != nil {
		out.SelfVerify = *selfVerify
	}
	return out
}

// assetRules is what is in effect for an asset in these categories: on
// when any uses review states, and then, of those that do, the shortest
// time between reviews that any asks for, a return to draft on editing if
// any asks for it, and verifying one's own confirmation only if all allow
// it.
func (t reviewTree) assetRules(categoryIDs []string) models.ReviewRules {
	out := models.ReviewRules{OnEdit: models.ReviewOnEditKeep, SelfVerify: true}
	for _, id := range categoryIDs {
		r := t.rules(id)
		if !r.Enabled {
			continue
		}
		out.Enabled = true
		if r.Months > 0 && (out.Months == 0 || r.Months < out.Months) {
			out.Months = r.Months
		}
		if r.OnEdit != models.ReviewOnEditKeep {
			out.OnEdit = models.ReviewOnEditDraft
		}
		out.SelfVerify = out.SelfVerify && r.SelfVerify
	}
	if !out.Enabled {
		return models.ReviewRules{OnEdit: models.ReviewOnEditDraft}
	}
	return out
}

// withReview fills in each category's review settings, its own and in
// effect.
func withReview(categories []models.Category, t reviewTree) {
	for i := range categories {
		categories[i].Review = t.own[categories[i].ID]
		categories[i].EffectiveReview = t.rules(categories[i].ID)
	}
}

// fillCategoryReview reads the tree and fills in each category's review
// settings.
func (s *Store) fillCategoryReview(categories []models.Category) error {
	t, err := readReviewTree(storeQuerier{s})
	if err != nil {
		return err
	}
	withReview(categories, t)
	return nil
}

// SetCategoryReview replaces a category's own review settings, each nil to
// take its parent's, and works out again when its assets, and those below
// it, are next due for review.
func (s *Store) SetCategoryReview(id string, in models.CategoryReview) (*models.Category, error) {
	if in.Months != nil && (*in.Months < 0 || *in.Months > MaxReviewMonths) {
		return nil, fmt.Errorf("%w: months between reviews must be 0 to %d", ErrInvalidSetting, MaxReviewMonths)
	}
	if in.OnEdit != nil && *in.OnEdit != models.ReviewOnEditDraft && *in.OnEdit != models.ReviewOnEditKeep {
		return nil, fmt.Errorf("%w: on_edit must be draft or keep", ErrInvalidSetting)
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	res, err := s.TxExec(tx, "UPDATE categories SET review_enabled = ?, review_months = ?, review_on_edit = ?, review_self_verify = ?, updated_at = ? WHERE id = ?",
		in.Enabled, in.Months, in.OnEdit, in.SelfVerify, dbTime(time.Now()), id)
	if err != nil {
		return nil, fmt.Errorf("saving review settings: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrCategoryNotFound
	}
	if err := s.refreshAllDueTx(tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetCategoryByID(id)
}

// refreshAllDueTx works out again when every asset that has been
// confirmed or verified is next due for review, as after a category's
// settings or place in the tree change.
func (s *Store) refreshAllDueTx(tx *sql.Tx) error {
	q := txQuerier{s, tx}
	t, err := readReviewTree(q)
	if err != nil {
		return err
	}
	cats := map[string][]string{}
	rows, err := s.TxQuery(tx, `SELECT ac.asset_id, ac.category_id FROM asset_categories ac
		JOIN assets a ON a.id = ac.asset_id WHERE a.reviewed_at IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("reading assets' categories: %w", err)
	}
	for rows.Next() {
		var a, c string
		if err := rows.Scan(&a, &c); err != nil {
			rows.Close()
			return err
		}
		cats[a] = append(cats[a], c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	type due struct {
		id  string
		was *time.Time
		now *time.Time
	}
	var changed []due
	rows, err = s.TxQuery(tx, "SELECT id, review_state, reviewed_at, review_due FROM assets WHERE reviewed_at IS NOT NULL")
	if err != nil {
		return fmt.Errorf("reading reviews: %w", err)
	}
	for rows.Next() {
		var (
			id            string
			state         sql.NullString
			reviewed, was sql.NullTime
		)
		if err := rows.Scan(&id, &state, &reviewed, &was); err != nil {
			rows.Close()
			return err
		}
		d := due{id: id, now: dueFor(state.String, timePtr(reviewed), t.assetRules(cats[id]))}
		if was.Valid {
			d.was = &was.Time
		}
		if !sameTime(d.was, d.now) {
			changed = append(changed, d)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, d := range changed {
		if _, err := s.TxExec(tx, "UPDATE assets SET review_due = ? WHERE id = ?", dbTimePtr(d.now), d.id); err != nil {
			return fmt.Errorf("setting when a review is due: %w", err)
		}
	}
	return nil
}

func timePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time.UTC()
	return &v
}

func dbTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return dbTime(*t)
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// dueFor is when an asset in state, last reviewed at reviewed, is next due
// for review under rules: nil for a draft, or where no review is asked for.
func dueFor(state string, reviewed *time.Time, rules models.ReviewRules) *time.Time {
	if !rules.Enabled || rules.Months <= 0 || reviewed == nil || (state != models.ReviewConfirmed && state != models.ReviewVerified) {
		return nil
	}
	d := reviewed.AddDate(0, rules.Months, 0)
	return &d
}

// assetReviewRow is an asset's review columns as read.
type assetReviewRow struct {
	state, by, confirmedBy                   sql.NullString
	at, confirmedAt, reviewedAt, due, notify sql.NullTime
}

// assetReviewColumns are an asset's review columns, in dest's order.
const assetReviewColumns = "review_state, review_by, review_at, confirmed_by, confirmed_at, reviewed_at, review_due"

func (r *assetReviewRow) dest() []any {
	return []any{&r.state, &r.by, &r.at, &r.confirmedBy, &r.confirmedAt, &r.reviewedAt, &r.due}
}

// current is the state it is in, a draft until first set.
func (r *assetReviewRow) current() string {
	if r.state.Valid && models.ValidReviewState(r.state.String) {
		return r.state.String
	}
	return models.ReviewDraft
}

// reviewPeople names the users and API tokens reviews name, by ID.
type reviewPeople map[string]models.Person

func readReviewPeople(s *Store) (reviewPeople, error) {
	people := reviewPeople{}
	rows, err := s.Query("SELECT id, email, name, archived_at FROM users")
	if err != nil {
		return nil, fmt.Errorf("reading people: %w", err)
	}
	for rows.Next() {
		var (
			p        models.Person
			archived sql.NullTime
		)
		if err := rows.Scan(&p.ID, &p.Email, &p.Name, &archived); err != nil {
			rows.Close()
			return nil, err
		}
		p.Archived = archived.Valid
		people[p.ID] = p
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = s.Query("SELECT id, name FROM api_tokens")
	if err != nil {
		return nil, fmt.Errorf("reading tokens: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p models.Person
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, err
		}
		people[p.ID] = p
	}
	return people, rows.Err()
}

func (p reviewPeople) person(id sql.NullString) *models.Person {
	if !id.Valid || id.String == "" {
		return nil
	}
	if found, ok := p[id.String]; ok {
		return &found
	}
	return &models.Person{ID: id.String}
}

// reviewContext is what reading reviews needs once for a list.
type reviewContext struct {
	settings  *ReviewSettings
	people    reviewPeople
	proposals map[string]models.Proposal
}

// reviewDetails fills in an asset's review from its review columns, when
// one of its categories uses review states.
func (s *Store) reviewDetails(a *models.AssetWithDetails, row assetReviewRow, ctx *assetContext, traits categoryTraits, catIDs []string) error {
	rules := traits.review.assetRules(catIDs)
	if !rules.Enabled {
		return nil
	}
	rc := &ctx.review
	if rc.settings == nil {
		set, err := s.ReviewSettings()
		if err != nil {
			return err
		}
		rc.settings = &set
	}
	if rc.people == nil {
		people, err := readReviewPeople(s)
		if err != nil {
			return err
		}
		rc.people = people
	}
	if rc.proposals == nil || !ctx.list {
		where, args := "", []any{}
		if !ctx.list {
			where, args = " WHERE asset_id = ?", []any{a.Asset.ID}
		}
		proposals, err := s.readProposals(where, args, rc.people)
		if err != nil {
			return err
		}
		rc.proposals = proposals
	}
	r := &models.AssetReview{
		State:       row.current(),
		SetBy:       rc.people.person(row.by),
		SetAt:       timePtr(row.at),
		ConfirmedBy: rc.people.person(row.confirmedBy),
		ConfirmedAt: timePtr(row.confirmedAt),
		ReviewedAt:  timePtr(row.reviewedAt),
		DueAt:       timePtr(row.due),
		Rules:       rules,
	}
	if r.DueAt != nil {
		now := time.Now().UTC()
		r.Overdue = !now.Before(*r.DueAt)
		r.Due = !now.Before(r.DueAt.AddDate(0, 0, -rc.settings.DueDays))
	}
	if p, ok := rc.proposals[a.Asset.ID]; ok {
		r.Proposal = &p
	}
	a.Review = r
	return nil
}

// readProposals reads the changes waiting from assistants, by asset.
func (s *Store) readProposals(where string, args []any, people reviewPeople) (map[string]models.Proposal, error) {
	rows, err := s.Query("SELECT asset_id, prior_state, record IS NOT NULL, is_new, proposed_by, via, created_at, updated_at FROM asset_proposals"+where, args...)
	if err != nil {
		return nil, fmt.Errorf("reading proposals: %w", err)
	}
	defer rows.Close()
	out := map[string]models.Proposal{}
	for rows.Next() {
		var (
			id         string
			prior, by  sql.NullString
			p          models.Proposal
			hasRecord  bool
			created, u time.Time
		)
		if err := rows.Scan(&id, &prior, &hasRecord, &p.New, &by, &p.Via, &created, &u); err != nil {
			return nil, fmt.Errorf("scanning a proposal: %w", err)
		}
		p.PriorState, p.By, p.CreatedAt, p.UpdatedAt = prior.String, people.person(by), created.UTC(), u.UTC()
		if p.PriorState == "" {
			p.PriorState = models.ReviewDraft
		}
		p.Undoable = hasRecord || p.New
		out[id] = p
	}
	return out, rows.Err()
}

// reviewerID is who changes are attributed to: a user, an API token or an
// incoming hook, by ID; nil for BAM! itself.
func (s *Store) reviewerID() *string {
	for _, id := range []string{s.actor, s.actorToken, s.actorIncoming} {
		if id != "" {
			return &id
		}
	}
	return nil
}

// Confirming returns a view of the store whose new assets are filed as
// confirmed, where review states are on, rather than as drafts. The caller
// checks that whoever acts may confirm them.
func (s *Store) Confirming() *Store {
	c := *s
	c.confirm = true
	return &c
}

// quietReview returns a view of the store whose edits leave review states
// as they are, for putting back what an assistant changed.
func (s *Store) quietReview() *Store {
	c := *s
	c.noReview = true
	return &c
}

// heldRecords keeps, for one request through an assistant, each asset as
// it was before the request changed it, until its changes land as a
// draft.
type heldRecords struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newHeldRecords() *heldRecords { return &heldRecords{m: map[string][]byte{}} }

// assistantDraftsTx reports whether changes through this view land as
// drafts: it acts through an assistant, and the instance says so.
func (s *Store) assistantDraftsTx(tx *sql.Tx) (bool, error) {
	if s.via == "" {
		return false, nil
	}
	set, err := reviewSettingsFrom(s.txRow(tx))
	return set.AssistantDrafts, err
}

// holdForReviewTx keeps each asset as it is before an assistant changes
// it, so that rejecting its changes can put it back: called before a
// change that may land as a draft. One with changes waiting already keeps
// what it was before the first of them.
func (s *Store) holdForReviewTx(tx *sql.Tx, ids ...string) error {
	if s.via == "" || s.held == nil || s.noReview {
		return nil
	}
	on, err := s.assistantDraftsTx(tx)
	if err != nil || !on {
		return err
	}
	for _, id := range ids {
		var n int
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM asset_proposals WHERE asset_id = ?", id).Scan(&n); err != nil {
			return fmt.Errorf("reading proposals: %w", err)
		}
		if n > 0 {
			continue
		}
		rec, err := s.readRecordTx(tx, id)
		if errors.Is(err, ErrAssetNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		raw, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("encoding the asset's record: %w", err)
		}
		s.held.mu.Lock()
		s.held.m[id] = raw
		s.held.mu.Unlock()
	}
	return nil
}

// takeHeld is the asset as holdForReviewTx kept it, or nil.
func (s *Store) takeHeld(id string) []byte {
	if s.held == nil {
		return nil
	}
	s.held.mu.Lock()
	defer s.held.mu.Unlock()
	raw := s.held.m[id]
	delete(s.held.m, id)
	return raw
}

// reviewOfTx reads what reviewing an asset needs within tx: its review
// columns, the rules of its categories, and whether it is archived.
func (s *Store) reviewOfTx(tx *sql.Tx, id string) (assetReviewRow, models.ReviewRules, bool, error) {
	var (
		row      assetReviewRow
		archived sql.NullTime
	)
	err := s.TxQueryRow(tx, "SELECT "+assetReviewColumns+", archived_at FROM assets WHERE id = ?", id).Scan(append(row.dest(), &archived)...)
	if errors.Is(err, sql.ErrNoRows) {
		return row, models.ReviewRules{}, false, ErrAssetNotFound
	}
	if err != nil {
		return row, models.ReviewRules{}, false, fmt.Errorf("reading the asset's review: %w", err)
	}
	t, err := readReviewTree(txQuerier{s, tx})
	if err != nil {
		return row, models.ReviewRules{}, false, err
	}
	cats, err := s.assetCategoriesTx(tx, id)
	if err != nil {
		return row, models.ReviewRules{}, false, err
	}
	return row, t.assetRules(cats), archived.Valid, nil
}

func (s *Store) assetCategoriesTx(tx *sql.Tx, id string) ([]string, error) {
	rows, err := s.TxQuery(tx, "SELECT category_id FROM asset_categories WHERE asset_id = ?", id)
	if err != nil {
		return nil, fmt.Errorf("reading the asset's categories: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// moveTx puts an asset in state, recording who did and when, and when it
// is next due for review, and records the move in its history. confirmed
// marks a confirmation, and reviewed a confirmation or verification, which
// starts the time until the next review again.
func (s *Store) moveTx(tx *sql.Tx, id string, from, to, cause, note string, rules models.ReviewRules, reviewedAt *time.Time) error {
	now := dbTime(time.Now())
	by := s.reviewerID()
	var due *time.Time
	switch {
	case reviewedAt != nil:
		due = dueFor(to, reviewedAt, rules)
		q := "UPDATE assets SET review_state = ?, review_by = ?, review_at = ?, reviewed_at = ?, review_due = ?"
		args := []any{to, by, now, dbTime(*reviewedAt), dbTimePtr(due)}
		if to == models.ReviewConfirmed {
			q += ", confirmed_by = ?, confirmed_at = ?"
			args = append(args, by, now)
		}
		if _, err := s.TxExec(tx, q+" WHERE id = ?", append(args, id)...); err != nil {
			return fmt.Errorf("moving the asset's review: %w", err)
		}
	default:
		var reviewed sql.NullTime
		if err := s.TxQueryRow(tx, "SELECT reviewed_at FROM assets WHERE id = ?", id).Scan(&reviewed); err != nil {
			return fmt.Errorf("reading the asset's review: %w", err)
		}
		due = dueFor(to, timePtr(reviewed), rules)
		if _, err := s.TxExec(tx, "UPDATE assets SET review_state = ?, review_by = ?, review_at = ?, review_due = ? WHERE id = ?",
			to, by, now, dbTimePtr(due), id); err != nil {
			return fmt.Errorf("moving the asset's review: %w", err)
		}
	}
	return s.recordAssetEvent(tx, models.EventAssetReviewChanged, id, models.EventPayload{
		Trigger: models.TriggerManual,
		Note:    note,
		Review:  &models.EventReview{From: from, To: to, Cause: cause, DueAt: due},
	})
}

// ReviewAsset confirms, verifies, or returns an asset to draft
// (docs/review.md#moves). Confirming moves any state to confirmed, and
// verifying a confirmed or verified asset to verified, each starting the
// time until its next review again; returning moves a confirmed or
// verified asset back to draft, with a note saying why. Someone cannot
// verify what they confirmed themselves unless its categories allow it.
// The caller checks the permission each move needs.
func (s *Store) ReviewAsset(id, move, note string) (*models.AssetWithDetails, error) {
	if len([]rune(note)) > MaxReviewNote {
		return nil, fmt.Errorf("%w: the note is longer than %d characters", ErrInvalidSetting, MaxReviewNote)
	}
	if s.via != "" && move != models.ReviewMoveReturn {
		if on, err := s.assistantDrafts(); err != nil {
			return nil, err
		} else if on {
			return nil, ErrReviewByAssistant
		}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	row, rules, archived, err := s.reviewOfTx(tx, id)
	if err != nil {
		return nil, err
	}
	if archived {
		return nil, ErrAssetArchived
	}
	if !rules.Enabled {
		return nil, ErrReviewOff
	}
	from := row.current()
	if move != models.ReviewMoveReturn {
		var n int
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM asset_proposals WHERE asset_id = ?", id).Scan(&n); err != nil {
			return nil, fmt.Errorf("reading proposals: %w", err)
		}
		if n > 0 {
			return nil, ErrProposalPending
		}
	}
	now := time.Now().UTC()
	switch move {
	case models.ReviewMoveConfirm:
		err = s.moveTx(tx, id, from, models.ReviewConfirmed, models.ReviewByMove, note, rules, &now)
	case models.ReviewMoveVerify:
		if from == models.ReviewDraft {
			return nil, ErrReviewMove
		}
		if by := s.reviewerID(); by != nil && row.confirmedBy.Valid && row.confirmedBy.String == *by && !rules.SelfVerify {
			return nil, ErrReviewOwnConfirmation
		}
		err = s.moveTx(tx, id, from, models.ReviewVerified, models.ReviewByMove, note, rules, &now)
	case models.ReviewMoveReturn:
		if from == models.ReviewDraft {
			return nil, ErrReviewMove
		}
		if note == "" {
			return nil, ErrReviewNoteRequired
		}
		err = s.moveTx(tx, id, from, models.ReviewDraft, models.ReviewByMove, note, rules, nil)
	default:
		return nil, fmt.Errorf("%w: a move is confirm, verify or return", ErrInvalidSetting)
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetAssetByID(id)
}

// assistantDrafts reports, outside a transaction, whether changes through
// this view land as drafts.
func (s *Store) assistantDrafts() (bool, error) {
	if s.via == "" {
		return false, nil
	}
	set, err := s.ReviewSettings()
	return set.AssistantDrafts, err
}

// reviewCreatedTx gives a new asset its review state, when one of its
// categories uses review states: a draft, or confirmed when this view
// files assets as confirmed, unless it was made through an assistant
// whose changes land as drafts, when it is a draft proposed as new.
func (s *Store) reviewCreatedTx(tx *sql.Tx, id string) error {
	_, rules, _, err := s.reviewOfTx(tx, id)
	if err != nil || !rules.Enabled {
		return err
	}
	drafting, err := s.assistantDraftsTx(tx)
	if err != nil {
		return err
	}
	switch {
	case drafting:
		if err := s.proposeTx(tx, id, "", nil, true); err != nil {
			return err
		}
		return s.moveTx(tx, id, "", models.ReviewDraft, models.ReviewByAssistant, "", rules, nil)
	case s.confirm:
		now := time.Now().UTC()
		return s.moveTx(tx, id, "", models.ReviewConfirmed, models.ReviewByCreate, "", rules, &now)
	}
	if _, err := s.TxExec(tx, "UPDATE assets SET review_state = ? WHERE id = ?", models.ReviewDraft, id); err != nil {
		return fmt.Errorf("filing the asset as a draft: %w", err)
	}
	return nil
}

// reviewEditedTx applies what editing an asset does to its review, once
// the edit is recorded. Through an assistant whose changes land as
// drafts, it returns the asset to draft with the changes waiting to be
// accepted or rejected. Otherwise a person's edit accepts any changes
// waiting, and returns a confirmed or verified asset to draft when its
// categories say so. Either way, when it is next due for review is worked
// out again, as its categories may have changed.
func (s *Store) reviewEditedTx(tx *sql.Tx, id string) error {
	row, rules, _, err := s.reviewOfTx(tx, id)
	if err != nil {
		return err
	}
	var waiting int
	if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM asset_proposals WHERE asset_id = ?", id).Scan(&waiting); err != nil {
		return fmt.Errorf("reading proposals: %w", err)
	}
	if !rules.Enabled {
		if waiting > 0 {
			if _, err := s.TxExec(tx, "DELETE FROM asset_proposals WHERE asset_id = ?", id); err != nil {
				return fmt.Errorf("dropping the proposal: %w", err)
			}
		}
		return s.refreshDueTx(tx, id, row, rules)
	}
	from := row.current()
	drafting, err := s.assistantDraftsTx(tx)
	if err != nil {
		return err
	}
	if drafting {
		if waiting > 0 {
			if _, err := s.TxExec(tx, "UPDATE asset_proposals SET updated_at = ? WHERE asset_id = ?", dbTime(time.Now()), id); err != nil {
				return fmt.Errorf("updating the proposal: %w", err)
			}
		} else if err := s.proposeTx(tx, id, from, s.takeHeld(id), false); err != nil {
			return err
		}
		if from == models.ReviewDraft && row.state.Valid {
			return nil
		}
		return s.moveTx(tx, id, from, models.ReviewDraft, models.ReviewByAssistant, "", rules, nil)
	}
	if waiting > 0 {
		if _, err := s.TxExec(tx, "DELETE FROM asset_proposals WHERE asset_id = ?", id); err != nil {
			return fmt.Errorf("accepting the proposal: %w", err)
		}
		if err := s.recordAssetEvent(tx, models.EventAssetReviewChanged, id, models.EventPayload{
			Trigger: models.TriggerManual,
			Review:  &models.EventReview{From: from, To: from, Cause: models.ReviewByAccept, DueAt: timePtr(row.due)},
		}); err != nil {
			return err
		}
	}
	if from != models.ReviewDraft && rules.OnEdit == models.ReviewOnEditDraft {
		return s.moveTx(tx, id, from, models.ReviewDraft, models.ReviewByEdit, "", rules, nil)
	}
	return s.refreshDueTx(tx, id, row, rules)
}

// refreshDueTx works out again when an asset is next due for review.
func (s *Store) refreshDueTx(tx *sql.Tx, id string, row assetReviewRow, rules models.ReviewRules) error {
	due := dueFor(row.current(), timePtr(row.reviewedAt), rules)
	if sameTime(timePtr(row.due), due) {
		return nil
	}
	if _, err := s.TxExec(tx, "UPDATE assets SET review_due = ? WHERE id = ?", dbTimePtr(due), id); err != nil {
		return fmt.Errorf("setting when a review is due: %w", err)
	}
	return nil
}

// proposeTx records that an asset's changes through an assistant wait to
// be accepted or rejected: what it was before them, as JSON, or nil when
// that could not be kept, and whether the assistant made it.
func (s *Store) proposeTx(tx *sql.Tx, id, prior string, record []byte, isNew bool) error {
	var rec any
	if record != nil {
		rec = string(record)
	}
	var priorState any
	if prior != "" {
		priorState = prior
	}
	now := dbTime(time.Now())
	if _, err := s.TxExec(tx, "INSERT INTO asset_proposals (asset_id, prior_state, record, is_new, proposed_by, via, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, priorState, rec, isNew, s.reviewerID(), s.via, now, now); err != nil {
		return fmt.Errorf("recording the proposal: %w", err)
	}
	return nil
}

// proposalRow is a proposal as stored.
type proposalRow struct {
	prior  sql.NullString
	record sql.NullString
	isNew  bool
}

func (s *Store) proposalTx(tx *sql.Tx, id string) (proposalRow, error) {
	var p proposalRow
	err := s.TxQueryRow(tx, "SELECT prior_state, record, is_new FROM asset_proposals WHERE asset_id = ?", id).Scan(&p.prior, &p.record, &p.isNew)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNoProposal
	}
	if err != nil {
		return p, fmt.Errorf("reading the proposal: %w", err)
	}
	return p, nil
}

// AcceptProposal takes the changes an assistant made to an asset as the
// accepter's own (docs/review.md#assistants). The asset takes the state a
// person's own edit would have left it in: the one it had before, where
// its categories keep the state on editing, or else a draft; or confirmed
// when confirm is set, which the caller checks the accepter may do.
func (s *Store) AcceptProposal(id string, confirm bool) (*models.AssetWithDetails, error) {
	if s.via != "" {
		return nil, ErrReviewByAssistant
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	p, err := s.proposalTx(tx, id)
	if err != nil {
		return nil, err
	}
	row, rules, archived, err := s.reviewOfTx(tx, id)
	if err != nil {
		return nil, err
	}
	if archived {
		return nil, ErrAssetArchived
	}
	if _, err := s.TxExec(tx, "DELETE FROM asset_proposals WHERE asset_id = ?", id); err != nil {
		return nil, fmt.Errorf("accepting the proposal: %w", err)
	}
	from, to := row.current(), models.ReviewDraft
	prior := p.prior.String
	if !p.isNew && rules.OnEdit == models.ReviewOnEditKeep && (prior == models.ReviewConfirmed || prior == models.ReviewVerified) {
		to = prior
	}
	var reviewed *time.Time
	if confirm {
		now := time.Now().UTC()
		to, reviewed = models.ReviewConfirmed, &now
	}
	if to == from && reviewed == nil {
		// It stays a draft: the move records the acceptance.
		err = s.recordAssetEvent(tx, models.EventAssetReviewChanged, id, models.EventPayload{
			Trigger: models.TriggerManual,
			Review:  &models.EventReview{From: from, To: to, Cause: models.ReviewByAccept, DueAt: timePtr(row.due)},
		})
	} else {
		err = s.moveTx(tx, id, from, to, models.ReviewByAccept, "", rules, reviewed)
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetAssetByID(id)
}

// RejectProposal undoes the changes an assistant made to an asset
// (docs/review.md#assistants): one it made is archived, and any other is
// put back as it was before them, in the state it had then. Its
// attachments and quantity are left as they are. note says why, if given.
func (s *Store) RejectProposal(id, note string) (*models.AssetWithDetails, error) {
	if s.via != "" {
		return nil, ErrReviewByAssistant
	}
	if len([]rune(note)) > MaxReviewNote {
		return nil, fmt.Errorf("%w: the note is longer than %d characters", ErrInvalidSetting, MaxReviewNote)
	}
	var p proposalRow
	err := func() error {
		tx, err := s.DB.Begin()
		if err != nil {
			return fmt.Errorf("beginning transaction: %w", err)
		}
		defer tx.Rollback()
		p, err = s.proposalTx(tx, id)
		return err
	}()
	if err != nil {
		return nil, err
	}
	if p.isNew {
		if err := s.DeleteAsset(id); err != nil {
			return nil, err
		}
		err := func() error {
			tx, err := s.DB.Begin()
			if err != nil {
				return fmt.Errorf("beginning transaction: %w", err)
			}
			defer tx.Rollback()
			if _, err := s.TxExec(tx, "DELETE FROM asset_proposals WHERE asset_id = ?", id); err != nil {
				return fmt.Errorf("rejecting the proposal: %w", err)
			}
			if err := s.recordAssetEvent(tx, models.EventAssetReviewChanged, id, models.EventPayload{
				Trigger: models.TriggerManual, Note: note,
				Review: &models.EventReview{From: models.ReviewDraft, To: models.ReviewDraft, Cause: models.ReviewByReject},
			}); err != nil {
				return err
			}
			return tx.Commit()
		}()
		if err != nil {
			return nil, err
		}
		return s.GetAssetByID(id)
	}
	if !p.record.Valid {
		return nil, ErrProposalNotUndoable
	}
	var rec assetRecord
	if err := json.Unmarshal([]byte(p.record.String), &rec); err != nil {
		return nil, fmt.Errorf("reading the asset's record: %w", err)
	}
	quiet := s.quietReview()
	err = s.withInventory(func(ic *inventoryChange) error {
		tx := ic.tx
		if _, err := quiet.proposalTx(tx, id); err != nil {
			return err
		}
		row, rules, archived, err := quiet.reviewOfTx(tx, id)
		if err != nil {
			return err
		}
		if archived {
			return ErrAssetArchived
		}
		if err := quiet.restoreRecordTx(tx, id, rec); err != nil {
			return err
		}
		if _, err := s.TxExec(tx, "DELETE FROM asset_proposals WHERE asset_id = ?", id); err != nil {
			return fmt.Errorf("rejecting the proposal: %w", err)
		}
		// Its categories may be others now.
		if _, rules, _, err = quiet.reviewOfTx(tx, id); err != nil {
			return err
		}
		from := row.current()
		to := p.prior.String
		if !p.prior.Valid {
			if _, err := s.TxExec(tx, "UPDATE assets SET review_state = NULL WHERE id = ?", id); err != nil {
				return fmt.Errorf("putting back the review state: %w", err)
			}
			to = models.ReviewDraft
		} else if _, err := s.TxExec(tx, "UPDATE assets SET review_state = ? WHERE id = ?", to, id); err != nil {
			return fmt.Errorf("putting back the review state: %w", err)
		}
		var reviewed sql.NullTime
		if err := s.TxQueryRow(tx, "SELECT reviewed_at FROM assets WHERE id = ?", id).Scan(&reviewed); err != nil {
			return fmt.Errorf("reading the asset's review: %w", err)
		}
		row.state, row.reviewedAt = p.prior, reviewed
		if err := quiet.refreshDueTx(tx, id, row, rules); err != nil {
			return err
		}
		return s.recordAssetEvent(tx, models.EventAssetReviewChanged, id, models.EventPayload{
			Trigger: models.TriggerManual, Note: note,
			Review: &models.EventReview{From: from, To: to, Cause: models.ReviewByReject, DueAt: dueFor(to, timePtr(reviewed), rules)},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.GetAssetByID(id)
}

// assetRecord is what rejecting an assistant's changes puts back: an
// asset's name, categories, codes, field values by field ID, how its stock
// is counted, its own icons, purchase details and links.
type assetRecord struct {
	Name       string            `json:"name"`
	Categories []string          `json:"categories"`
	Codes      map[string]string `json:"codes"`
	Fields     map[string]string `json:"fields"`
	Stock      *recordStock      `json:"stock"`
	Icon       *string           `json:"icon"`
	LabelIcon  *string           `json:"label_icon"`
	Purchase   recordPurchase    `json:"purchase"`
	Links      []recordLink      `json:"links"`
}

type recordStock struct {
	Unit      string `json:"unit"`
	Threshold *int   `json:"threshold"`
}

type recordPurchase struct {
	VendorID     string `json:"vendor_id,omitempty"`
	OrderDate    string `json:"order_date,omitempty"`
	ReceivedDate string `json:"received_date,omitempty"`
	WarrantyEnd  string `json:"warranty_end,omitempty"`
	Price        string `json:"price,omitempty"`
	Currency     string `json:"currency,omitempty"`
	OrderNumber  string `json:"order_number,omitempty"`
	InvoiceID    string `json:"invoice_id,omitempty"`
}

type recordLink struct {
	Type string `json:"type"`
	From string `json:"from"`
	To   string `json:"to"`
}

// readRecordTx reads an asset's record within tx.
func (s *Store) readRecordTx(tx *sql.Tx, id string) (assetRecord, error) {
	rec := assetRecord{Categories: []string{}, Codes: map[string]string{}, Fields: map[string]string{}, Links: []recordLink{}}
	var (
		sc              stockScan
		icon, labelIcon sql.NullString
	)
	err := s.TxQueryRow(tx, "SELECT name, icon_name, label_icon_name, "+stockColumns+" FROM assets WHERE id = ?", id).
		Scan(append([]any{&rec.Name, &icon, &labelIcon}, sc.dest()...)...)
	if errors.Is(err, sql.ErrNoRows) {
		return rec, ErrAssetNotFound
	}
	if err != nil {
		return rec, fmt.Errorf("reading the asset's record: %w", err)
	}
	if icon.Valid {
		rec.Icon = &icon.String
	}
	if labelIcon.Valid {
		rec.LabelIcon = &labelIcon.String
	}
	if st := sc.stock(); st != nil {
		rec.Stock = &recordStock{Unit: st.Unit, Threshold: st.LowStockThreshold}
	}
	if rec.Categories, err = s.assetCategoriesTx(tx, id); err != nil {
		return rec, err
	}
	sort.Strings(rec.Categories)
	if err := s.scanPairs(tx, "SELECT generator_type, assigned_code FROM asset_generator_keys WHERE asset_id = ?", id,
		func(k, v string) { rec.Codes[k] = v }); err != nil {
		return rec, err
	}
	if err := s.scanPairs(tx, "SELECT field_id, value FROM asset_field_values WHERE asset_id = ?", id,
		func(k, v string) { rec.Fields[k] = v }); err != nil {
		return rec, err
	}
	p, err := s.purchaseTx(tx, id)
	if err != nil {
		return rec, err
	}
	in := PurchaseInputOf(p)
	rec.Purchase = recordPurchase{VendorID: in.VendorID, OrderDate: in.OrderDate, ReceivedDate: in.ReceivedDate, WarrantyEnd: in.WarrantyEnd,
		Price: in.Price, Currency: in.Currency, OrderNumber: in.OrderNumber, InvoiceID: in.InvoiceID}
	links, err := s.linksOfTx(tx, id)
	if err != nil {
		return rec, err
	}
	for _, l := range links {
		rec.Links = append(rec.Links, l.recordLink)
	}
	return rec, nil
}

// storedLink is one of an asset's links as stored.
type storedLink struct {
	id string
	recordLink
}

func (s *Store) linksOfTx(tx *sql.Tx, id string) ([]storedLink, error) {
	rows, err := s.TxQuery(tx, "SELECT id, relation_type_id, from_asset_id, to_asset_id FROM asset_links WHERE from_asset_id = ? OR to_asset_id = ? ORDER BY id", id, id)
	if err != nil {
		return nil, fmt.Errorf("reading the asset's links: %w", err)
	}
	defer rows.Close()
	var out []storedLink
	for rows.Next() {
		var l storedLink
		if err := rows.Scan(&l.id, &l.Type, &l.From, &l.To); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// restoreRecordTx puts an asset back as rec records it, recording what
// changed in its history and in that of the assets its links lead to.
// Links to assets archived or gone since are not put back.
func (s *Store) restoreRecordTx(tx *sql.Tx, id string, rec assetRecord) error {
	before, err := s.snapshotAssetTx(tx, id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if _, err := s.TxExec(tx, "UPDATE assets SET name = ?, icon_name = ?, label_icon_name = ?, updated_at = ? WHERE id = ?",
		rec.Name, rec.Icon, rec.LabelIcon, now, id); err != nil {
		return fmt.Errorf("putting back the asset: %w", err)
	}
	cats := rec.Categories
	if len(cats) == 0 {
		cats = []string{UncategorizedCategoryID}
	}
	if _, err := s.TxExec(tx, "DELETE FROM asset_categories WHERE asset_id = ?", id); err != nil {
		return fmt.Errorf("putting back categories: %w", err)
	}
	for _, c := range cats {
		var n int
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM categories WHERE id = ?", c).Scan(&n); err != nil {
			return fmt.Errorf("checking a category: %w", err)
		}
		if n == 0 {
			continue // deleted since
		}
		if _, err := s.TxExec(tx, "INSERT INTO asset_categories (asset_id, category_id) VALUES (?, ?)", id, c); err != nil {
			return fmt.Errorf("putting back categories: %w", err)
		}
	}
	if _, err := s.TxExec(tx, "DELETE FROM asset_generator_keys WHERE asset_id = ?", id); err != nil {
		return fmt.Errorf("putting back codes: %w", err)
	}
	for _, t := range sortedKeys(rec.Codes) {
		if _, err := s.TxExec(tx, "INSERT INTO asset_generator_keys (asset_id, generator_type, assigned_code) VALUES (?, ?, ?)", id, t, rec.Codes[t]); err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("%w (%s: %s)", ErrGeneratorCodeExists, t, rec.Codes[t])
			}
			return fmt.Errorf("putting back codes: %w", err)
		}
	}
	if _, err := s.TxExec(tx, "DELETE FROM asset_field_values WHERE asset_id = ?", id); err != nil {
		return fmt.Errorf("putting back fields: %w", err)
	}
	for _, f := range sortedKeys(rec.Fields) {
		var n int
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM fields WHERE id = ?", f).Scan(&n); err != nil {
			return fmt.Errorf("checking a field: %w", err)
		}
		if n == 0 || rec.Fields[f] == "" {
			continue
		}
		if _, err := s.TxExec(tx, "INSERT INTO asset_field_values (asset_id, field_id, value, updated_at) VALUES (?, ?, ?, ?)", id, f, rec.Fields[f], now); err != nil {
			return fmt.Errorf("putting back fields: %w", err)
		}
	}
	was, err := s.readStockTx(tx, id)
	if err != nil {
		return err
	}
	var recordQuantity func() error
	switch {
	case rec.Stock == nil && was != nil:
		_, err = s.setStockTx(tx, id, was, nil)
	case rec.Stock != nil:
		recordQuantity, err = s.setStockTx(tx, id, was, &StockInput{Unit: rec.Stock.Unit, LowStockThreshold: rec.Stock.Threshold})
	}
	if err != nil {
		return err
	}
	p := rec.Purchase
	in := PurchaseInput{VendorID: p.VendorID, OrderDate: p.OrderDate, ReceivedDate: p.ReceivedDate, WarrantyEnd: p.WarrantyEnd,
		Price: p.Price, Currency: p.Currency, OrderNumber: p.OrderNumber, InvoiceID: p.InvoiceID}
	if in.VendorID != "" {
		var n int
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM vendors WHERE id = ?", in.VendorID).Scan(&n); err != nil {
			return fmt.Errorf("checking the vendor: %w", err)
		}
		if n == 0 {
			in.VendorID = ""
		}
	}
	if in.InvoiceID != "" {
		var n int
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM asset_attachments WHERE id = ? AND asset_id = ?", in.InvoiceID, id).Scan(&n); err != nil {
			return fmt.Errorf("checking the invoice: %w", err)
		}
		if n == 0 {
			in.InvoiceID = ""
		}
	}
	if err := s.setPurchaseTx(tx, id, in); err != nil {
		return err
	}
	linkChanges, err := s.restoreLinksTx(tx, id, rec.Links)
	if err != nil {
		return err
	}
	if err := s.recordAfter(tx, id, before, models.TriggerManual, recordQuantity); err != nil {
		return err
	}
	return s.recordLinkChanges(tx, linkChanges, models.TriggerManual)
}

// restoreLinksTx puts back an asset's links as want lists them, returning
// the changes each asset's history records.
func (s *Store) restoreLinksTx(tx *sql.Tx, id string, want []recordLink) (map[string][]models.EventChange, error) {
	have, err := s.linksOfTx(tx, id)
	if err != nil {
		return nil, err
	}
	wanted := map[recordLink]bool{}
	for _, l := range want {
		wanted[l] = true
	}
	had := map[recordLink]bool{}
	changes := map[string][]models.EventChange{}
	names := map[string]string{}
	name := func(assetID string) (string, bool, error) {
		if n, ok := names[assetID]; ok {
			return n, true, nil
		}
		var (
			n        string
			archived sql.NullTime
		)
		err := s.TxQueryRow(tx, "SELECT name, archived_at FROM assets WHERE id = ?", assetID).Scan(&n, &archived)
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		if err != nil {
			return "", false, fmt.Errorf("reading a linked asset: %w", err)
		}
		names[assetID] = n
		return n, !archived.Valid, nil
	}
	typeNames := func(typeID string) (string, string, bool, error) {
		var n, inv string
		err := s.TxQueryRow(tx, "SELECT name, inverse_name FROM relation_types WHERE id = ?", typeID).Scan(&n, &inv)
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", false, nil
		}
		if err != nil {
			return "", "", false, fmt.Errorf("reading a link type: %w", err)
		}
		return n, inv, true, nil
	}
	note := func(l recordLink, added bool) error {
		n, inv, _, err := typeNames(l.Type)
		if err != nil {
			return err
		}
		fromName, _, err := name(l.From)
		if err != nil {
			return err
		}
		toName, _, err := name(l.To)
		if err != nil {
			return err
		}
		from := models.EventChange{What: "link", Key: l.To, Label: n}
		to := models.EventChange{What: "link", Key: l.From, Label: inv}
		if added {
			from.To, to.To = toName, fromName
		} else {
			from.From, to.From = toName, fromName
		}
		changes[l.From] = append(changes[l.From], from)
		changes[l.To] = append(changes[l.To], to)
		return nil
	}
	for _, l := range have {
		had[l.recordLink] = true
		if wanted[l.recordLink] {
			continue
		}
		if _, err := s.TxExec(tx, "DELETE FROM asset_links WHERE id = ?", l.id); err != nil {
			return nil, fmt.Errorf("putting back links: %w", err)
		}
		if err := note(l.recordLink, false); err != nil {
			return nil, err
		}
	}
	for _, l := range want {
		if had[l] {
			continue
		}
		other := l.To
		if other == id {
			other = l.From
		}
		if _, active, err := name(other); err != nil {
			return nil, err
		} else if !active {
			continue
		}
		if _, _, ok, err := typeNames(l.Type); err != nil {
			return nil, err
		} else if !ok {
			continue
		}
		if _, err := s.TxExec(tx, "INSERT INTO asset_links (id, relation_type_id, from_asset_id, to_asset_id, created_at) VALUES (?, ?, ?, ?, ?)",
			uuid.NewString(), l.Type, l.From, l.To, time.Now().UTC()); err != nil {
			if isUniqueViolation(err) {
				continue // put back by now, or taken by another of a single type
			}
			return nil, fmt.Errorf("putting back links: %w", err)
		}
		if err := note(l, true); err != nil {
			return nil, err
		}
	}
	return changes, nil
}

// RecordDueReviews records asset.review_due for each active asset whose
// review falls due within the instance's days of now, today included,
// and has not had one for that date. Each is claimed by setting
// review_notified to the date, as running low is flagged, so servers
// sharing the database record it once; a new date is a new event. It
// returns how many it recorded.
func (s *Store) RecordDueReviews(now time.Time) (int, error) {
	set, err := s.ReviewSettings()
	if err != nil {
		return 0, err
	}
	until := dbTime(now.UTC().AddDate(0, 0, set.DueDays))
	rows, err := s.Query(`SELECT id, review_due FROM assets WHERE archived_at IS NULL AND review_due IS NOT NULL AND review_due <= ?
		AND (review_notified IS NULL OR review_notified <> review_due) ORDER BY review_due, id`, until)
	if err != nil {
		return 0, fmt.Errorf("finding reviews due: %w", err)
	}
	type due struct {
		id string
		at time.Time
	}
	var found []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.at); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scanning a review due: %w", err)
		}
		found = append(found, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	recorded := 0
	for _, d := range found {
		ok, err := s.recordReviewDue(d.id, d.at)
		if err != nil {
			return recorded, err
		}
		if ok {
			recorded++
		}
	}
	return recorded, nil
}

// recordReviewDue claims and records one asset's asset.review_due,
// answering false when another server claimed it first or the date moved.
func (s *Store) recordReviewDue(id string, at time.Time) (bool, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return false, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	res, err := s.TxExec(tx, `UPDATE assets SET review_notified = review_due WHERE id = ? AND review_due = ?
		AND (review_notified IS NULL OR review_notified <> review_due)`, id, dbTime(at))
	if err != nil {
		return false, fmt.Errorf("claiming the review due: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return false, nil
	}
	due := at.UTC()
	if err := s.recordAssetEvent(tx, models.EventAssetReviewDue, id, models.EventPayload{Trigger: models.TriggerSchedule, DueAt: &due}); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
