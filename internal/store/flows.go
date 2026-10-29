package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

var (
	ErrFlowNotFound    = errors.New("flow not found")
	ErrFlowRunNotFound = errors.New("flow run not found")
	// ErrInvalidFlow wraps the reason a flow cannot be saved as given; its
	// steps are checked by the caller, with the flows package.
	ErrInvalidFlow = errors.New("invalid flow")
)

// FlowRunLifetime is how long a run is kept unanswered before it is
// dropped, so a page left open overnight starts again.
const FlowRunLifetime = 12 * time.Hour

// FlowInput is a flow as made or changed.
type FlowInput struct {
	Slug       string
	Name       string
	Enabled    bool
	Definition flows.Definition
}

func (in *FlowInput) normalize() error {
	in.Slug = strings.TrimSpace(in.Slug)
	in.Name = strings.TrimSpace(in.Name)
	if !rbac.ValidScopeName(in.Slug) {
		return fmt.Errorf("%w: its address is 1 to 64 lowercase letters, digits, - and _, starting with a letter or digit", ErrInvalidFlow)
	}
	if in.Name == "" {
		return fmt.Errorf("%w: a name is required", ErrInvalidFlow)
	}
	if err := textcheck.Check("flow name", in.Name, textcheck.MaxName); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidFlow, err)
	}
	if err := textcheck.Check("description", in.Definition.Description, textcheck.MaxName); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidFlow, err)
	}
	return nil
}

const flowColumns = "id, slug, name, definition, enabled, created_by, created_at, updated_at"

func scanFlow(row interface{ Scan(...any) error }) (models.Flow, error) {
	var (
		f   models.Flow
		def string
		by  sql.NullString
	)
	if err := row.Scan(&f.ID, &f.Slug, &f.Name, &def, &f.Enabled, &by, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return f, err
	}
	f.CreatedBy = by.String
	if err := json.Unmarshal([]byte(def), &f.Definition); err != nil {
		return f, fmt.Errorf("reading flow %s: %w", f.Slug, err)
	}
	return f, nil
}

// ListFlows lists every flow, by name.
func (s *Store) ListFlows() ([]models.Flow, error) {
	rows, err := s.Query("SELECT " + flowColumns + " FROM flows ORDER BY name, slug")
	if err != nil {
		return nil, fmt.Errorf("listing flows: %w", err)
	}
	defer rows.Close()
	out := []models.Flow{}
	for rows.Next() {
		f, err := scanFlow(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning flow: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetFlow returns the flow at an address, or ErrFlowNotFound.
func (s *Store) GetFlow(slug string) (*models.Flow, error) {
	return s.getFlow("slug", slug)
}

// GetFlowByID returns a flow, or ErrFlowNotFound.
func (s *Store) GetFlowByID(id string) (*models.Flow, error) {
	return s.getFlow("id", id)
}

func (s *Store) getFlow(column, value string) (*models.Flow, error) {
	f, err := scanFlow(s.QueryRow("SELECT "+flowColumns+" FROM flows WHERE "+column+" = ?", value))
	if err == sql.ErrNoRows {
		return nil, ErrFlowNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading flow: %w", err)
	}
	return &f, nil
}

// CreateFlow saves a new flow, made by createdBy. An address in use fails as
// a unique violation.
func (s *Store) CreateFlow(in FlowInput, createdBy string) (*models.Flow, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	def, err := json.Marshal(in.Definition)
	if err != nil {
		return nil, err
	}
	id, now := uuid.NewString(), time.Now().UTC()
	var by any
	if createdBy != "" {
		by = createdBy
	}
	if _, err := s.Exec("INSERT INTO flows (id, slug, name, definition, enabled, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, in.Slug, in.Name, string(def), in.Enabled, by, now, now); err != nil {
		return nil, fmt.Errorf("inserting flow: %w", err)
	}
	return s.GetFlowByID(id)
}

// UpdateFlow changes a flow, its address included. Runs under way go on
// with the flow as it now is.
func (s *Store) UpdateFlow(id string, in FlowInput) (*models.Flow, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	def, err := json.Marshal(in.Definition)
	if err != nil {
		return nil, err
	}
	res, err := s.Exec("UPDATE flows SET slug = ?, name = ?, definition = ?, enabled = ?, updated_at = ? WHERE id = ?",
		in.Slug, in.Name, string(def), in.Enabled, time.Now().UTC(), id)
	if err != nil {
		return nil, fmt.Errorf("updating flow: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return nil, ErrFlowNotFound
	}
	return s.GetFlowByID(id)
}

// DeleteFlow deletes a flow, its runs and its record of what it filed; the
// assets stay.
func (s *Store) DeleteFlow(id string) error {
	res, err := s.Exec("DELETE FROM flows WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting flow: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrFlowNotFound
	}
	return nil
}

// FlowCatalog is what a flow over these categories may name (flows.Catalog):
// fields are those an asset in any of them records, own or inherited.
func (s *Store) FlowCatalog(categoryIDs []string) (flows.Catalog, error) {
	c := flows.Catalog{
		Flows: map[string]bool{}, Categories: map[string]bool{}, Fields: map[string]bool{}, Templates: map[string]bool{},
		Destinations: map[string]bool{}, Actions: map[string]bool{},
		Asset: func(id string) bool {
			a, err := s.GetAssetByID(id)
			return err == nil && a.Asset.ArchivedAt == nil
		},
	}
	all, err := s.ListFlows()
	if err != nil {
		return c, err
	}
	for _, f := range all {
		c.Flows[f.ID] = true
	}
	cats, err := s.ListAllCategories()
	if err != nil {
		return c, err
	}
	for _, cat := range cats {
		c.Categories[cat.ID] = true
	}
	for _, id := range categoryIDs {
		if !c.Categories[id] {
			continue
		}
		fields, err := s.EffectiveFieldsForAsset([]string{id})
		if err != nil {
			return c, err
		}
		for _, f := range fields {
			c.Fields[f.Field.Key] = true
		}
	}
	templates, err := s.ListTemplates()
	if err != nil {
		return c, err
	}
	for _, t := range templates {
		c.Templates[t.ID] = true
	}
	printers, err := s.ListPrinters()
	if err != nil {
		return c, err
	}
	for _, p := range printers {
		c.Destinations[models.DestinationPrinterPrefix+p.ID] = true
	}
	pools, err := s.ListPools()
	if err != nil {
		return c, err
	}
	for _, p := range pools {
		c.Destinations[models.DestinationPoolPrefix+p.ID] = true
	}
	actions, err := s.ListActions()
	if err != nil {
		return c, err
	}
	for _, a := range actions {
		c.Actions[a.Key] = true
	}
	return c, nil
}

// FlowRunState is where a run is: the step it is on, its values, what each
// step it has done did, and whether it is finished.
type FlowRunState struct {
	Step   int               `json:"step"`
	Values flows.Values      `json:"values"`
	Done   []models.FlowDone `json:"done"`
	// Finished is set once the run has reached its last step.
	Finished bool `json:"finished"`
	// Busy is set while a step does its work (ClaimFlowRun).
	Busy bool `json:"busy,omitempty"`
	// Fields are the form's answers that are asset fields, by key, for
	// Create to file.
	Fields map[string]string `json:"fields,omitempty"`
	// Progress counts, by step, how much of its work a step has done, so a
	// step tried again after failing part way does not do it twice.
	Progress map[string]int `json:"progress,omitempty"`
}

// CreateFlowRun starts a run of a flow by a user.
func (s *Store) CreateFlowRun(flowID, userID string, state FlowRunState) (*models.FlowRun, error) {
	body, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	id, now := uuid.NewString(), time.Now().UTC()
	// Runs left long enough go, so the table holds only those under way.
	if _, err := s.Exec("DELETE FROM flow_runs WHERE updated_at < ?", now.Add(-FlowRunLifetime)); err != nil {
		return nil, fmt.Errorf("dropping old runs: %w", err)
	}
	if _, err := s.Exec("INSERT INTO flow_runs (id, flow_id, user_id, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, flowID, userID, string(body), now, now); err != nil {
		return nil, fmt.Errorf("inserting run: %w", err)
	}
	return s.GetFlowRun(id)
}

// GetFlowRun returns a run that has not been left too long, or
// ErrFlowRunNotFound.
func (s *Store) GetFlowRun(id string) (*models.FlowRun, error) {
	var (
		r     models.FlowRun
		state string
	)
	err := s.QueryRow("SELECT id, flow_id, user_id, state, created_at, updated_at FROM flow_runs WHERE id = ? AND updated_at >= ?",
		id, time.Now().UTC().Add(-FlowRunLifetime)).Scan(&r.ID, &r.FlowID, &r.UserID, &state, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrFlowRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading run: %w", err)
	}
	var st FlowRunState
	if err := json.Unmarshal([]byte(state), &st); err != nil {
		return nil, fmt.Errorf("reading run %s: %w", id, err)
	}
	r.Step, r.Values, r.Done, r.Finished, r.Fields, r.Progress = st.Step, st.Values, st.Done, st.Finished, st.Fields, st.Progress
	if r.Done == nil {
		r.Done = []models.FlowDone{}
	}
	return &r, nil
}

// ClaimFlowRun marks a run as busy with the step it is on, if it is still
// on step from and not busy or finished, before that step does anything: of
// two answers to a step at once, one does its work and the other is refused
// (ErrFlowRunMoved). SaveFlowRun then records where the run is, and frees
// it. A run left busy, its server stopped part way, ends when it is left
// too long.
func (s *Store) ClaimFlowRun(id string, from int) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	// Writing the row first holds it until the transaction ends.
	res, err := s.TxExec(tx, "UPDATE flow_runs SET updated_at = ? WHERE id = ?", time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("claiming run: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrFlowRunNotFound
	}
	var current string
	if err := s.TxQueryRow(tx, "SELECT state FROM flow_runs WHERE id = ?", id).Scan(&current); err != nil {
		return fmt.Errorf("reading run: %w", err)
	}
	var st FlowRunState
	if err := json.Unmarshal([]byte(current), &st); err != nil {
		return fmt.Errorf("reading run %s: %w", id, err)
	}
	if st.Step != from || st.Busy || st.Finished {
		return ErrFlowRunMoved
	}
	st.Busy = true
	body, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if _, err := s.TxExec(tx, "UPDATE flow_runs SET state = ? WHERE id = ?", string(body), id); err != nil {
		return fmt.Errorf("claiming run: %w", err)
	}
	return tx.Commit()
}

// SaveFlowRun records where a run is, freeing it.
func (s *Store) SaveFlowRun(id string, state FlowRunState) error {
	state.Busy = false
	body, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if _, err := s.Exec("UPDATE flow_runs SET state = ?, updated_at = ? WHERE id = ?", string(body), time.Now().UTC(), id); err != nil {
		return fmt.Errorf("saving run: %w", err)
	}
	return nil
}

// ErrFlowRunMoved is returned for an answer to a step the run is no longer
// on: answered already, or skipped.
var ErrFlowRunMoved = errors.New("this run has moved on from that step")

// DeleteFlowRun ends a run, finished or given up.
func (s *Store) DeleteFlowRun(id string) error {
	_, err := s.Exec("DELETE FROM flow_runs WHERE id = ?", id)
	return err
}

// RecordFiling notes that a flow filed an asset, run by userID.
func (s *Store) RecordFiling(flowID, assetID, userID string) error {
	var by any
	if userID != "" {
		by = userID
	}
	_, err := s.Exec("INSERT INTO flow_filings (flow_id, asset_id, user_id, filed_at) VALUES (?, ?, ?, ?)", flowID, assetID, by, time.Now().UTC())
	if err != nil && !IsUniqueViolation(err) {
		return fmt.Errorf("recording what the flow filed: %w", err)
	}
	return nil
}

// FiledBy lists the IDs of the assets a flow filed.
func (s *Store) FiledBy(flowID string) (map[string]bool, error) {
	rows, err := s.Query("SELECT asset_id FROM flow_filings WHERE flow_id = ?", flowID)
	if err != nil {
		return nil, fmt.Errorf("reading what the flow filed: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
