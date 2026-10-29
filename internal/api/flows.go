package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// FlowRequest makes or changes a flow (README §6.7).
type FlowRequest struct {
	Slug       string           `json:"slug"`
	Name       string           `json:"name"`
	Enabled    *bool            `json:"enabled"`
	Definition flows.Definition `json:"definition"`
}

// FlowProblems is a flow refused, with what stops it, by step.
type FlowProblems struct {
	Error    string          `json:"error"`
	Problems []flows.Problem `json:"problems"`
}

// flowFor reads the flow at the request's address, for someone who manages
// flows or may run it; anyone else is told there is none (404).
func (s *Server) flowFor(w http.ResponseWriter, r *http.Request) (*models.Flow, bool) {
	f, err := s.store.GetFlow(r.PathValue("slug"))
	if errors.Is(err, store.ErrFlowNotFound) {
		writeJSONError(w, http.StatusNotFound, "Flow not found")
		return nil, false
	}
	if err != nil {
		writeServerError(w, err, "read the flow")
		return nil, false
	}
	if HasPermission(r.Context(), rbac.PermFlowsManage) {
		return f, true
	}
	may, err := s.mayRun(r, f)
	if err != nil {
		writeServerError(w, err, "read the flow")
		return nil, false
	}
	if !may || !f.Enabled {
		writeJSONError(w, http.StatusNotFound, "Flow not found")
		return nil, false
	}
	return f, true
}

// handleListFlows lists flows: every one for those who manage them, and
// otherwise those the requester may run: GET /api/v1/flows
func (s *Server) handleListFlows(w http.ResponseWriter, r *http.Request) {
	all, err := s.store.ListFlows()
	if err != nil {
		writeServerError(w, err, "list flows")
		return
	}
	if HasPermission(r.Context(), rbac.PermFlowsManage) {
		writeJSON(w, http.StatusOK, map[string]any{"flows": all})
		return
	}
	runnable := []models.Flow{}
	for i := range all {
		may, err := s.mayRun(r, &all[i])
		if err != nil {
			writeServerError(w, err, "list flows")
			return
		}
		if may && all[i].Enabled {
			runnable = append(runnable, all[i])
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"flows": runnable})
}

// handleGetFlow reads one flow: GET /api/v1/flows/{slug}
func (s *Server) handleGetFlow(w http.ResponseWriter, r *http.Request) {
	if f, ok := s.flowFor(w, r); ok {
		writeJSON(w, http.StatusOK, map[string]any{"flow": f})
	}
}

// handleFlowStarters gives the ready-made flows to adjust, over the
// categories named: GET /api/v1/flow-starters?category_ids=a,b
func (s *Server) handleFlowStarters(w http.ResponseWriter, r *http.Request) {
	var ids []string
	for _, id := range strings.Split(r.URL.Query().Get("category_ids"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"starters": flows.Starters(ids)})
}

// handleSaveFlow makes a flow, or changes the one at the address:
// POST /api/v1/flows, PUT /api/v1/flows/{slug}. A flow is checked first,
// and refused with what stops it (400).
func (s *Server) handleSaveFlow(w http.ResponseWriter, r *http.Request) {
	var req FlowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	var existing *models.Flow
	if slug := r.PathValue("slug"); slug != "" {
		f, err := s.store.GetFlow(slug)
		if errors.Is(err, store.ErrFlowNotFound) {
			writeJSONError(w, http.StatusNotFound, "Flow not found")
			return
		}
		if err != nil {
			writeServerError(w, err, "read the flow")
			return
		}
		existing = f
	}
	problems, err := s.checkFlow(r, &req.Definition)
	if err != nil {
		writeServerError(w, err, "check the flow")
		return
	}
	if len(problems) > 0 {
		said := make([]string, 0, len(problems))
		for _, p := range problems {
			said = append(said, p.String())
		}
		writeJSON(w, http.StatusBadRequest, FlowProblems{Error: "The flow cannot be saved: " + strings.Join(said, "; "), Problems: problems})
		return
	}
	in := store.FlowInput{Slug: req.Slug, Name: req.Name, Enabled: true, Definition: req.Definition}
	if req.Enabled != nil {
		in.Enabled = *req.Enabled
	} else if existing != nil {
		in.Enabled = existing.Enabled
	}
	var f *models.Flow
	status := http.StatusCreated
	if existing == nil {
		by := ""
		if u := CurrentUser(r.Context()); u != nil {
			by = u.ID
		}
		f, err = s.store.CreateFlow(in, by)
	} else {
		f, err = s.store.UpdateFlow(existing.ID, in)
		status = http.StatusOK
	}
	switch {
	case errors.Is(err, store.ErrInvalidFlow):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case store.IsUniqueViolation(err):
		writeJSONError(w, http.StatusConflict, "Another flow already has this address")
	case err != nil:
		writeStoreError(w, err, "save the flow")
	default:
		writeJSON(w, status, map[string]any{"flow": f})
	}
}

// handleDeleteFlow deletes a flow, its runs and its record of what it
// filed; the assets stay: DELETE /api/v1/flows/{slug}
func (s *Server) handleDeleteFlow(w http.ResponseWriter, r *http.Request) {
	f, err := s.store.GetFlow(r.PathValue("slug"))
	if err == nil {
		err = s.store.DeleteFlow(f.ID)
	}
	switch {
	case errors.Is(err, store.ErrFlowNotFound):
		writeJSONError(w, http.StatusNotFound, "Flow not found")
	case err != nil:
		writeServerError(w, err, "delete the flow")
	default:
		writeJSON(w, http.StatusOK, map[string]string{"message": "Flow deleted"})
	}
}

// FlowRunView is a run as its pages show it: where it is, and what the
// step it is on asks.
type FlowRunView struct {
	Run    *models.FlowRun `json:"run"`
	Flow   string          `json:"flow"`
	Prompt FlowPrompt      `json:"prompt"`
	// Error, with a refused answer, is why; the run is shown as it is.
	Error string `json:"error,omitempty"`
}

// FlowChoice is a category a Choose category step offers.
type FlowChoice struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ColorHex string `json:"color_hex"`
}

// FlowAsset is an asset as a run shows it: one found or made, or offered
// to find.
type FlowAsset struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Code       string   `json:"code,omitempty"`
	Categories []string `json:"categories"`
}

// FlowPrompt is what the step a run is on asks for, and shows.
type FlowPrompt struct {
	Step  string `json:"step"`
	Kind  string `json:"kind"`
	Title string `json:"title,omitempty"`
	Text  string `json:"text,omitempty"`
	// Choose category.
	Categories []FlowChoice `json:"categories,omitempty"`
	// Form.
	Fields    []FlowField      `json:"fields,omitempty"`
	AskName   bool             `json:"ask_name,omitempty"`
	NameLabel string           `json:"name_label,omitempty"`
	AskPhoto  bool             `json:"ask_photo,omitempty"`
	Questions []flows.Question `json:"questions,omitempty"`
	// Find: the ways to find, and, by list or search, the assets.
	FindBy []string    `json:"find_by,omitempty"`
	Assets []FlowAsset `json:"assets,omitempty"`
	// Create, when it asks where the asset was put.
	AskPlace bool `json:"ask_place,omitempty"`
	// Confirm: what the steps after it will do.
	Coming []string `json:"coming,omitempty"`
	// Done: what the run did.
	Done []models.FlowDone `json:"done,omitempty"`
	// Asset is the asset the run has found or made, if any; ShowLabel is
	// set when a Label step said to show its label, drawn with
	// LabelTemplateID (the step's, or else its category's), when it has
	// one.
	Asset           *FlowAsset `json:"asset,omitempty"`
	ShowLabel       bool       `json:"show_label,omitempty"`
	LabelTemplateID string     `json:"label_template_id,omitempty"`
}

// maxFlowAssets is the most assets a Find step lists at once; a search
// narrows the rest.
const maxFlowAssets = 200

func flowAsset(a *models.AssetWithDetails) FlowAsset {
	out := FlowAsset{ID: a.Asset.ID, Name: a.Asset.Name, Categories: []string{}}
	if len(a.GeneratorKeys) > 0 {
		out.Code = a.GeneratorKeys[0].AssignedCode
	}
	for _, c := range a.Categories {
		out.Categories = append(out.Categories, c.Name)
	}
	return out
}

// prompt is what the step a run is on asks for, with search narrowing a
// Find step's list.
func (f *flowRunner) prompt(run *models.FlowRun, search string) (FlowPrompt, error) {
	d := &f.flow.Definition
	if run.Step >= len(d.Steps) {
		return FlowPrompt{Kind: flows.KindDone, Done: run.Done}, nil
	}
	step := &d.Steps[run.Step]
	p := FlowPrompt{Step: step.ID, Kind: step.Kind, Title: flows.Render(step.Title, run.Values), Text: flows.Render(step.Text, run.Values)}
	var asset *models.AssetWithDetails
	if id := run.Values[flows.ValueAssetID]; id != "" {
		if a, err := f.s.store.GetAssetByID(id); err == nil {
			asset = a
			fa := flowAsset(a)
			p.Asset = &fa
		}
	}
	for _, s := range d.Steps[:run.Step] {
		if s.Kind == flows.KindLabel && s.Label != nil && s.Label.Mode == flows.LabelShow && s.When.Holds(run.Values) && asset != nil {
			p.ShowLabel, p.LabelTemplateID = true, s.Label.TemplateID
			if p.LabelTemplateID == "" {
				p.LabelTemplateID = asset.LabelTemplate.TemplateID
			}
		}
	}
	switch step.Kind {
	case flows.KindChooseCategory:
		ids, err := f.offered(step)
		if err != nil {
			return p, err
		}
		fc, err := f.s.flowCategories()
		if err != nil {
			return p, err
		}
		for _, id := range ids {
			c := fc.byID[id]
			p.Categories = append(p.Categories, FlowChoice{ID: id, Name: c.Name, ColorHex: c.ColorHex})
		}
	case flows.KindForm:
		fields, err := f.formFields(step, run.Values)
		if err != nil {
			return p, err
		}
		p.Fields, p.AskName, p.NameLabel, p.AskPhoto = fields, step.AskName, nameLabel(step), step.AskPhoto
		for _, q := range step.Questions {
			q.Default = flows.Render(q.Default, run.Values)
			p.Questions = append(p.Questions, q)
		}
	case flows.KindFind:
		p.FindBy = step.FindBy
		listed := false
		for _, by := range step.FindBy {
			listed = listed || by == flows.FindList || (by == flows.FindSearch && search != "")
		}
		if listed {
			found, err := f.candidates(step, run.Values, search)
			if err != nil {
				return p, err
			}
			for i := range found {
				if i == maxFlowAssets {
					break
				}
				p.Assets = append(p.Assets, flowAsset(&found[i]))
			}
		}
	case flows.KindCreate:
		p.AskPlace = step.Location != nil && step.Location.Mode == flows.PlaceScan
	case flows.KindConfirm:
		p.Coming = f.coming(run)
	case flows.KindDone:
		p.Done = run.Done
	default:
		// A step that asks nothing, left by a failure: answering it tries
		// again.
		p.Coming = f.coming(run)
	}
	return p, nil
}

// coming says what the steps after the one a run is on will do, up to the
// next that asks something.
func (f *flowRunner) coming(run *models.FlowRun) []string {
	d := &f.flow.Definition
	v := run.Values
	out := []string{}
	start := run.Step
	if start < len(d.Steps) && d.Steps[start].Kind == flows.KindConfirm {
		start++
	}
	for i := d.Next(start, v); i < len(d.Steps); i = d.Next(i+1, v) {
		s := &d.Steps[i]
		if !s.Auto() {
			break
		}
		switch s.Kind {
		case flows.KindCreate:
			out = append(out, fmt.Sprintf("File %s in %s.", flows.Render(s.Name, v), v[flows.ValueCategory]))
		case flows.KindLabel:
			switch {
			case s.Label == nil || s.Label.Mode == flows.LabelShow:
				out = append(out, "Show its label.")
			case s.Label.Mode == flows.LabelQueue:
				out = append(out, "Add its label to the print queue.")
			default:
				out = append(out, "Print its label.")
			}
		case flows.KindChange:
			for _, c := range s.Changes[run.Progress[s.ID]:] {
				out = append(out, describeChange(c, v))
			}
		}
	}
	return out
}

func describeChange(c flows.Change, v flows.Values) string {
	name := v[flows.ValueAsset]
	switch c.Kind {
	case flows.ChangeArchive:
		return fmt.Sprintf("Archive %s: it is taken off the lists.", name)
	case flows.ChangeCondition:
		return fmt.Sprintf("Mark %s %s.", name, strings.ReplaceAll(c.Condition, "_", " "))
	case flows.ChangeBorrow:
		return fmt.Sprintf("Lend %s to %s.", name, v[flows.ValueUser])
	case flows.ChangeReturn:
		return fmt.Sprintf("Return %s.", name)
	case flows.ChangePlace:
		return fmt.Sprintf("Move %s to its new place.", name)
	case flows.ChangeCategory:
		return fmt.Sprintf("Move %s to another category.", name)
	case flows.ChangeFields:
		return fmt.Sprintf("Update %s.", name)
	case flows.ChangeAction:
		return fmt.Sprintf("Press %s on %s.", c.Action, name)
	}
	return ""
}

// runView answers with a run and its prompt, and with why an answer was
// refused, if it was, at its status.
func (s *Server) runView(w http.ResponseWriter, f *flowRunner, run *models.FlowRun, search string, stepErr error, ok int) {
	p, err := f.prompt(run, search)
	if err != nil {
		writeServerError(w, err, "read the run")
		return
	}
	view := FlowRunView{Run: run, Flow: f.flow.Slug, Prompt: p}
	status := ok
	if stepErr != nil {
		var fe *flowError
		if !errors.As(stepErr, &fe) {
			writeServerError(w, stepErr, "run the step")
			return
		}
		status, view.Error = fe.Status, fe.Message
	}
	writeJSON(w, status, view)
}

// writeFlowError answers with a flow's refusal, or a server error.
func writeFlowError(w http.ResponseWriter, err error, action string) {
	var fe *flowError
	if errors.As(err, &fe) {
		writeJSONError(w, fe.Status, fe.Message)
		return
	}
	writeServerError(w, err, action)
}

// handleStartFlowRun starts a run of a flow: POST /api/v1/flows/{slug}/runs
func (s *Server) handleStartFlowRun(w http.ResponseWriter, r *http.Request) {
	f, ok := s.flowFor(w, r)
	if !ok {
		return
	}
	runner := &flowRunner{s: s, r: r, flow: f}
	run, err := runner.startRun()
	if run == nil {
		writeFlowError(w, err, "start the run")
		return
	}
	s.runView(w, runner, run, "", err, http.StatusCreated)
}

// runFor reads a run of the flow at the request's address, if it is the
// requester's own; any other is not found.
func (s *Server) runFor(w http.ResponseWriter, r *http.Request) (*flowRunner, *models.FlowRun, bool) {
	f, ok := s.flowFor(w, r)
	if !ok {
		return nil, nil, false
	}
	run, err := s.store.GetFlowRun(r.PathValue("id"))
	u := CurrentUser(r.Context())
	if errors.Is(err, store.ErrFlowRunNotFound) || (err == nil && (run.FlowID != f.ID || u == nil || run.UserID != u.ID)) {
		writeJSONError(w, http.StatusNotFound, "This run is over; start again")
		return nil, nil, false
	}
	if err != nil {
		writeServerError(w, err, "read the run")
		return nil, nil, false
	}
	return &flowRunner{s: s, r: r, flow: f}, run, true
}

// handleGetFlowRun reads a run, and what its step asks; search narrows a
// Find step's list: GET /api/v1/flows/{slug}/runs/{id}?search=
func (s *Server) handleGetFlowRun(w http.ResponseWriter, r *http.Request) {
	if runner, run, ok := s.runFor(w, r); ok {
		s.runView(w, runner, run, r.URL.Query().Get("search"), nil, http.StatusOK)
	}
}

// handleAnswerFlowRun answers the step a run is on:
// POST /api/v1/flows/{slug}/runs/{id}. A refused answer is answered with
// its status, why, and the run as it is.
func (s *Server) handleAnswerFlowRun(w http.ResponseWriter, r *http.Request) {
	runner, run, ok := s.runFor(w, r)
	if !ok {
		return
	}
	var a FlowAnswer
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	after, err := runner.answer(run, a)
	if after == nil {
		writeFlowError(w, err, "answer the step")
		return
	}
	if after.Finished && err == nil {
		// Its work done, it is kept only to show; the next person starts
		// a new one.
		defer func() { _ = s.store.DeleteFlowRun(after.ID) }()
	}
	s.runView(w, runner, after, "", err, http.StatusOK)
}

// handleEndFlowRun gives a run up: DELETE /api/v1/flows/{slug}/runs/{id}
func (s *Server) handleEndFlowRun(w http.ResponseWriter, r *http.Request) {
	_, run, ok := s.runFor(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteFlowRun(run.ID); err != nil {
		writeServerError(w, err, "end the run")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Run ended"})
}
