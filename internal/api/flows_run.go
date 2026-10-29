package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/mcpserver"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Flows are run here (README §6.7): each step that changes something asks
// the API's own handlers, in process, as the person running the flow, so it
// is held to their permissions and scopes and to every rule the endpoint
// behind it keeps, and recorded in history as made through the flow.

// flowError is a step refused: by the API behind it, with its status and
// words, or by the flow itself (400).
type flowError struct {
	Status  int
	Message string
}

func (e *flowError) Error() string { return e.Message }

func refuseStep(format string, args ...any) error {
	return &flowError{Status: http.StatusBadRequest, Message: fmt.Sprintf(format, args...)}
}

// flowRunner runs one flow as the person making a request.
type flowRunner struct {
	s    *Server
	r    *http.Request
	flow *models.Flow
}

// call asks the API, in process, as the person running the flow, and reads
// its answer into out. A refusal is a flowError in the API's words.
func (f *flowRunner) call(method, path string, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	}
	ctx := authenticatedAs(mcpserver.WithVia(f.r.Context(), "flow “"+f.flow.Name+"”"), f.r.Context())
	req, err := http.NewRequestWithContext(ctx, method, path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.RequestURI = path
	req.RemoteAddr = f.r.RemoteAddr
	req.Host = f.r.Host
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := &recorder{header: http.Header{}}
	f.s.httpServer.Handler.ServeHTTP(rec, req)
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	if rec.status < 200 || rec.status > 299 {
		var e ErrorResponse
		_ = json.Unmarshal(rec.body.Bytes(), &e)
		if e.Error == "" {
			e.Error = http.StatusText(rec.status)
		}
		return &flowError{Status: rec.status, Message: e.Error}
	}
	if out != nil {
		return json.Unmarshal(rec.body.Bytes(), out)
	}
	return nil
}

// flowCategories maps each category to its parent, for finding what is in
// a flow's categories or below them.
type flowCategories struct {
	byID map[string]models.Category
}

func (s *Server) flowCategories() (flowCategories, error) {
	cats, err := s.store.ListAllCategories()
	if err != nil {
		return flowCategories{}, err
	}
	fc := flowCategories{byID: map[string]models.Category{}}
	for _, c := range cats {
		fc.byID[c.ID] = c
	}
	return fc, nil
}

func (fc flowCategories) name(id string) string { return fc.byID[id].Name }

// within reports whether category id is one of these or below one.
func (fc flowCategories) within(id string, of []string) bool {
	for depth := 0; id != "" && depth < 100; depth++ {
		if slices.Contains(of, id) {
			return true
		}
		c, ok := fc.byID[id]
		if !ok || c.ParentID == nil {
			return false
		}
		id = *c.ParentID
	}
	return false
}

// mayRun reports whether the request holds flows.run where a flow's
// categories put assets: in the scopes of at least one of them.
func (s *Server) mayRun(r *http.Request, f *models.Flow) (bool, error) {
	scopesOf, err := s.store.ScopeResolver()
	if err != nil {
		return false, err
	}
	for _, id := range f.Definition.CategoryIDs {
		if Allows(r.Context(), rbac.PermFlowsRun, scopesOf([]string{id})) {
			return true, nil
		}
	}
	return false, nil
}

// runState is a run's state as the store keeps it.
func runState(run *models.FlowRun) store.FlowRunState {
	return store.FlowRunState{Step: run.Step, Values: run.Values, Done: run.Done, Finished: run.Finished, Fields: run.Fields, Progress: run.Progress}
}

// startRun begins a run of a flow by the person making the request, and
// does the steps that ask nothing, up to the first that asks something.
func (f *flowRunner) startRun() (*models.FlowRun, error) {
	u := CurrentUser(f.r.Context())
	if u == nil {
		return nil, &flowError{Status: http.StatusForbidden, Message: "Running a flow needs someone signed in"}
	}
	if may, err := f.s.mayRun(f.r, f.flow); err != nil {
		return nil, err
	} else if !may || !f.flow.Enabled {
		return nil, &flowError{Status: http.StatusNotFound, Message: "Flow not found"}
	}
	fc, err := f.s.flowCategories()
	if err != nil {
		return nil, err
	}
	name := u.Name
	if name == "" {
		name = "Unnamed user"
	}
	st := store.FlowRunState{Values: flows.Start(f.flow.Name, name, time.Now(), &f.flow.Definition, fc.name), Done: []models.FlowDone{}}
	st.Step = f.flow.Definition.Next(0, st.Values)
	run, err := f.s.store.CreateFlowRun(f.flow.ID, u.ID, st)
	if err != nil {
		return nil, err
	}
	if err := f.s.store.ClaimFlowRun(run.ID, run.Step); err != nil {
		return nil, err
	}
	st, stepErr := f.advance(st)
	if err := f.s.store.SaveFlowRun(run.ID, st); err != nil {
		return nil, err
	}
	run, err = f.s.store.GetFlowRun(run.ID)
	if err != nil {
		return nil, err
	}
	return run, stepErr
}

// FlowAnswer answers the step a run is on. Step names it, so an answer
// meant for another step is refused rather than taken for this one.
type FlowAnswer struct {
	Step string `json:"step"`
	// Choose category.
	CategoryID string `json:"category_id,omitempty"`
	// Form: the asset's name, the fields by key, and the run's own
	// questions by key.
	Name    string            `json:"name,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"`
	Answers map[string]string `json:"answers,omitempty"`
	// Find: the asset chosen from a list or search, or a code scanned.
	AssetID string `json:"asset_id,omitempty"`
	Code    string `json:"code,omitempty"`
	// Create: the code of the place it is put, when it is scanned.
	PlaceCode string `json:"place_code,omitempty"`
}

// answer takes an answer to the step a run is on, then does the steps that
// ask nothing, up to the next that asks something or the end. A step
// that does something and fails (an asset the person may not change, a
// printer that is off) leaves the run on that step, to try again: answering
// it with only its name does.
func (f *flowRunner) answer(run *models.FlowRun, a FlowAnswer) (*models.FlowRun, error) {
	if run.Finished {
		return nil, &flowError{Status: http.StatusConflict, Message: "This run is finished; start again"}
	}
	d := &f.flow.Definition
	if run.Step >= len(d.Steps) || d.Steps[run.Step].ID != a.Step {
		return nil, &flowError{Status: http.StatusConflict, Message: store.ErrFlowRunMoved.Error()}
	}
	if err := f.s.store.ClaimFlowRun(run.ID, run.Step); err != nil {
		if errors.Is(err, store.ErrFlowRunMoved) {
			return nil, &flowError{Status: http.StatusConflict, Message: err.Error()}
		}
		return nil, err
	}
	st := runState(run)
	step := &d.Steps[st.Step]
	stepErr := f.take(&st, step, a)
	if stepErr == nil {
		st, stepErr = f.advance(st)
	}
	if err := f.s.store.SaveFlowRun(run.ID, st); err != nil {
		return nil, err
	}
	if run, err := f.s.store.GetFlowRun(run.ID); err != nil {
		return nil, err
	} else {
		return run, stepErr
	}
}

// take applies an answer to the step the run is on, and moves past it.
func (f *flowRunner) take(st *store.FlowRunState, step *flows.Step, a FlowAnswer) error {
	var err error
	switch step.Kind {
	case flows.KindChooseCategory:
		err = f.chooseCategory(st, step, a.CategoryID)
	case flows.KindForm:
		err = f.takeForm(st, step, a)
	case flows.KindFind:
		err = f.find(st, step, a)
	case flows.KindCreate:
		err = f.create(st, step, a.PlaceCode)
	case flows.KindLabel, flows.KindChange:
		// Answered only to try again.
		err = f.do(st, step)
	case flows.KindConfirm:
	case flows.KindDone:
		st.Finished = true
		return nil
	}
	if err != nil {
		return err
	}
	st.Step = f.flow.Definition.Next(st.Step+1, st.Values)
	return nil
}

// advance does the steps that ask nothing, from the one the run is on, up
// to the next that asks something. Reaching Done finishes the run.
func (f *flowRunner) advance(st store.FlowRunState) (store.FlowRunState, error) {
	d := &f.flow.Definition
	for st.Step < len(d.Steps) {
		step := &d.Steps[st.Step]
		if step.Kind == flows.KindDone {
			st.Finished = true
			return st, nil
		}
		if !step.Auto() {
			return st, nil
		}
		if err := f.do(&st, step); err != nil {
			return st, err
		}
		st.Step = d.Next(st.Step+1, st.Values)
	}
	st.Finished = true
	return st, nil
}

// do does a step that asks nothing.
func (f *flowRunner) do(st *store.FlowRunState, step *flows.Step) error {
	switch step.Kind {
	case flows.KindChooseCategory:
		return f.chooseCategory(st, step, step.Fixed)
	case flows.KindCreate:
		return f.create(st, step, "")
	case flows.KindLabel:
		return f.label(st, step)
	case flows.KindChange:
		return f.change(st, step)
	}
	return nil
}

// progress is how much of its work a step has done; advanceProgress records one more
// part of it done.
func progress(st *store.FlowRunState, step *flows.Step) int { return st.Progress[step.ID] }

func advanceProgress(st *store.FlowRunState, step *flows.Step) {
	if st.Progress == nil {
		st.Progress = map[string]int{}
	}
	st.Progress[step.ID]++
}

func (f *flowRunner) said(st *store.FlowRunState, step *flows.Step, format string, args ...any) {
	st.Done = append(st.Done, models.FlowDone{Step: step.ID, Kind: step.Kind, Said: fmt.Sprintf(format, args...)})
}

// offered are the categories a Choose category step offers the person:
// those it names (or all the flow's), in whose scopes they may run flows.
func (f *flowRunner) offered(step *flows.Step) ([]string, error) {
	ids := step.Categories
	if len(ids) == 0 {
		ids = f.flow.Definition.CategoryIDs
	}
	scopesOf, err := f.s.store.ScopeResolver()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, id := range ids {
		if Allows(f.r.Context(), rbac.PermFlowsRun, scopesOf([]string{id})) {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f *flowRunner) chooseCategory(st *store.FlowRunState, step *flows.Step, id string) error {
	ids, err := f.offered(step)
	if err != nil {
		return err
	}
	if step.Fixed != "" {
		ids = []string{step.Fixed}
	}
	if !slices.Contains(ids, id) {
		return refuseStep("Choose one of the categories offered")
	}
	fc, err := f.s.flowCategories()
	if err != nil {
		return err
	}
	st.Values[flows.ValueCategoryID], st.Values[flows.ValueCategory] = id, fc.name(id)
	return nil
}

// FlowField is a field a Form asks for, as the chosen category records it
// and the flow's creator set it.
type FlowField struct {
	Key      string          `json:"key"`
	Label    string          `json:"label"`
	Help     string          `json:"help,omitempty"`
	Type     string          `json:"type"`
	Options  json.RawMessage `json:"options,omitempty"`
	Default  string          `json:"default,omitempty"`
	Required bool            `json:"required"`
}

// formFields are the fields a Form asks for, for the category chosen: the
// union of those its assets record, own and inherited, in the creator's
// order with their settings on top, those they hid left out. A field the
// category requires is asked, and required, whatever the settings say.
func (f *flowRunner) formFields(step *flows.Step, v flows.Values) ([]FlowField, error) {
	effective, err := f.s.store.EffectiveFieldsForAsset([]string{v[flows.ValueCategoryID]})
	if err != nil {
		return nil, err
	}
	byKey := map[string]models.EffectiveField{}
	var order []string
	for _, ef := range effective {
		byKey[ef.Field.Key] = ef
		order = append(order, ef.Field.Key)
	}
	settings := map[string]flows.FieldSetting{}
	var first []string
	for _, fs := range step.Fields {
		settings[fs.Key] = fs
		if _, ok := byKey[fs.Key]; ok {
			first = append(first, fs.Key)
		}
	}
	for _, k := range order {
		if !slices.Contains(first, k) {
			first = append(first, k)
		}
	}
	out := []FlowField{}
	for _, k := range first {
		ef, set := byKey[k], settings[k]
		if set.Hidden && !ef.IsRequired {
			continue
		}
		ff := FlowField{Key: k, Label: ef.Field.Label, Help: set.Help, Type: ef.Field.ValidationType, Options: ef.Field.Options,
			Default: flows.Render(set.Default, v), Required: ef.IsRequired || set.Required}
		if set.Label != "" {
			ff.Label = set.Label
		}
		out = append(out, ff)
	}
	return out, nil
}

func (f *flowRunner) takeForm(st *store.FlowRunState, step *flows.Step, a FlowAnswer) error {
	asked, err := f.formFields(step, st.Values)
	if err != nil {
		return err
	}
	if step.AskName {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			return refuseStep("%s is required", nameLabel(step))
		}
		st.Values[flows.ValueName] = name
	}
	for _, ff := range asked {
		val := strings.TrimSpace(a.Fields[ff.Key])
		if val == "" && ff.Required {
			return refuseStep("%s is required", ff.Label)
		}
		if val != "" {
			field := models.Field{Key: ff.Key, Label: ff.Label, ValidationType: ff.Type, Options: ff.Options}
			if err := fields.ValidateFieldValue(&field, val); err != nil {
				return refuseStep("%s: %v", ff.Label, err)
			}
		}
		st.Values[ff.Key] = val
		if st.Fields == nil {
			st.Fields = map[string]string{}
		}
		st.Fields[ff.Key] = val
	}
	for _, q := range step.Questions {
		val := strings.TrimSpace(a.Answers[q.Key])
		if val == "" && q.Required {
			return refuseStep("%s is required", q.Label)
		}
		if val != "" {
			switch q.Type {
			case "number":
				if _, err := strconv.ParseFloat(val, 64); err != nil {
					return refuseStep("%s is a number", q.Label)
				}
			case "date":
				if _, err := time.Parse("2006-01-02", val); err != nil {
					return refuseStep("%s is a date, as 2026-10-30", q.Label)
				}
			}
		}
		st.Values[q.Key] = val
	}
	return nil
}

func nameLabel(step *flows.Step) string {
	if step.NameLabel != "" {
		return step.NameLabel
	}
	return "Its name"
}

// candidates are the assets a Find step can find for the person: active,
// in the flow's categories or below them, ones they may read and run
// flows on, narrowed as the step says and by search.
func (f *flowRunner) candidates(step *flows.Step, v flows.Values, search string) ([]models.AssetWithDetails, error) {
	all, err := f.s.store.FilterAssets(store.AssetFilter{})
	if err != nil {
		return nil, err
	}
	fc, err := f.s.flowCategories()
	if err != nil {
		return nil, err
	}
	var filed map[string]bool
	if step.FiledBy != "" {
		if filed, err = f.s.store.FiledBy(step.FiledBy); err != nil {
			return nil, err
		}
	}
	match := ""
	if step.Match != nil {
		match = flows.Render(step.Match.Value, v)
	}
	out := []models.AssetWithDetails{}
	for _, a := range all {
		if !f.findable(a, fc) || (filed != nil && !filed[a.Asset.ID]) {
			continue
		}
		if step.Match != nil && !strings.EqualFold(strings.TrimSpace(a.FieldValues[step.Match.Field]), strings.TrimSpace(match)) {
			continue
		}
		out = append(out, a)
	}
	return searched(out, search), nil
}

// findable reports whether a Find can find an asset at all: in the flow's
// categories or below, and one the person may read and run flows on.
func (f *flowRunner) findable(a models.AssetWithDetails, fc flowCategories) bool {
	if !Allows(f.r.Context(), rbac.PermAssetsRead, a.Scopes) || !Allows(f.r.Context(), rbac.PermFlowsRun, a.Scopes) {
		return false
	}
	for _, c := range a.Categories {
		if fc.within(c.ID, f.flow.Definition.CategoryIDs) {
			return true
		}
	}
	return false
}

func (f *flowRunner) find(st *store.FlowRunState, step *flows.Step, a FlowAnswer) error {
	id := a.AssetID
	if a.Code != "" {
		if !slices.Contains(step.FindBy, flows.FindScan) {
			return refuseStep("This step does not find by scanning")
		}
		var scanned ScanResponse
		if err := f.call(http.MethodGet, "/api/v1/scan/"+url.PathEscape(strings.TrimSpace(a.Code)), nil, &scanned); err != nil {
			return &flowError{Status: http.StatusBadRequest, Message: "Nothing has the code " + strings.TrimSpace(a.Code)}
		}
		id = scanned.Asset.ID
	}
	if id == "" {
		return refuseStep("Choose what to find, or scan its code")
	}
	found, err := f.candidates(step, st.Values, "")
	if err != nil {
		return err
	}
	for _, c := range found {
		if c.Asset.ID == id {
			setAsset(st.Values, &c)
			return nil
		}
	}
	return refuseStep("That is not something this flow can find")
}

// setAsset sets the values of an asset found or made: its name, ID, first
// code and field values, for later steps to use.
func setAsset(v flows.Values, a *models.AssetWithDetails) {
	for k, val := range a.FieldValues {
		v[k] = val
	}
	v[flows.ValueAsset], v[flows.ValueAssetID], v[flows.ValueCode] = a.Asset.Name, a.Asset.ID, ""
	if len(a.GeneratorKeys) > 0 {
		v[flows.ValueCode] = a.GeneratorKeys[0].AssignedCode
	}
}

// locationType is the relation type that puts assets in places.
func (f *flowRunner) locationType() (string, error) {
	types, err := f.s.store.ListRelationTypes()
	if err != nil {
		return "", err
	}
	for _, t := range types {
		if t.IsLocation {
			return t.ID, nil
		}
	}
	return "", refuseStep("No link type puts things in places")
}

func (f *flowRunner) create(st *store.FlowRunState, step *flows.Step, placeCode string) error {
	place := ""
	if l := step.Location; l != nil {
		switch l.Mode {
		case flows.PlaceFixed:
			place = l.AssetID
		case flows.PlaceScan:
			if strings.TrimSpace(placeCode) == "" {
				return refuseStep("Scan where you put it")
			}
			var scanned ScanResponse
			if err := f.call(http.MethodGet, "/api/v1/scan/"+url.PathEscape(strings.TrimSpace(placeCode)), nil, &scanned); err != nil {
				return refuseStep("Nothing has the code %s", strings.TrimSpace(placeCode))
			}
			place = scanned.Asset.ID
		}
	}
	if progress(st, step) > 0 {
		// Made already, and placing it failed: place it again.
		return f.place(st, step, st.Values[flows.ValueAssetID], place)
	}
	name := strings.TrimSpace(flows.Render(step.Name, st.Values))
	if name == "" {
		return refuseStep("Its name came out empty: give the details its name is made from")
	}
	body := CreateAssetRequest{Name: name, CategoryIDs: []string{st.Values[flows.ValueCategoryID]}, FieldValues: map[string]string{}}
	for k, v := range st.Fields {
		if v != "" {
			body.FieldValues[k] = v
		}
	}
	var made struct {
		Asset models.AssetWithDetails `json:"asset"`
	}
	if err := f.call(http.MethodPost, "/api/v1/assets", body, &made); err != nil {
		return err
	}
	setAsset(st.Values, &made.Asset)
	if err := f.s.store.RecordFiling(f.flow.ID, made.Asset.Asset.ID, CurrentUser(f.r.Context()).ID); err != nil {
		return err
	}
	advanceProgress(st, step)
	f.said(st, step, "Filed %s in %s.", name, st.Values[flows.ValueCategory])
	return f.place(st, step, made.Asset.Asset.ID, place)
}

// place puts a new asset in a place, if the step names one.
func (f *flowRunner) place(st *store.FlowRunState, step *flows.Step, id, place string) error {
	if place == "" {
		return nil
	}
	typeID, err := f.locationType()
	if err != nil {
		return err
	}
	var placed struct {
		Asset models.AssetWithDetails `json:"asset"`
	}
	if err := f.call(http.MethodPost, "/api/v1/assets/"+url.PathEscape(id)+"/links", AddLinkRequest{TypeID: typeID, AssetID: place}, &placed); err != nil {
		return err
	}
	if n := len(placed.Asset.Location); n > 0 {
		f.said(st, step, "Put it in %s.", placed.Asset.Location[n-1].Name)
	}
	return nil
}

func (f *flowRunner) label(st *store.FlowRunState, step *flows.Step) error {
	l := step.Label
	id := st.Values[flows.ValueAssetID]
	if l == nil || l.Mode == flows.LabelShow {
		f.said(st, step, "Its label is ready to show.")
		return nil
	}
	add := SpoolAddRequest{AssetIDs: []string{id}, TemplateID: l.TemplateID}
	if l.Destination != "" {
		dest := l.Destination
		add.Destination = &dest
	}
	var added SpoolAddResponse
	if err := f.call(http.MethodPost, "/api/v1/spool", add, &added); err != nil {
		return err
	}
	if len(added.Skipped) > 0 {
		return refuseStep("Its label could not be made: %s", added.Skipped[0].Reason)
	}
	if l.Mode == flows.LabelQueue {
		f.said(st, step, "Its label is in the print queue.")
		return nil
	}
	var entries []string
	for _, e := range added.Added {
		entries = append(entries, e.ID)
	}
	if len(entries) == 0 {
		// Its label was waiting already: print what is waiting for it.
		f.said(st, step, "Its label was already in the print queue.")
		return nil
	}
	var printed SpoolPrintResponse
	if err := f.call(http.MethodPost, "/api/v1/spool/print", SpoolPrintRequest{EntryIDs: entries}, &printed); err != nil {
		return err
	}
	if len(printed.Skipped) > 0 {
		return refuseStep("Its label did not print: %s", printed.Skipped[0].Reason)
	}
	f.said(st, step, "Its label is printing.")
	return nil
}

func (f *flowRunner) change(st *store.FlowRunState, step *flows.Step) error {
	id := st.Values[flows.ValueAssetID]
	path := "/api/v1/assets/" + url.PathEscape(id)
	name := st.Values[flows.ValueAsset]
	var updated struct {
		Asset models.AssetWithDetails `json:"asset"`
	}
	// Those done before it failed last time are not done again.
	for _, c := range step.Changes[progress(st, step):] {
		var err error
		switch c.Kind {
		case flows.ChangeArchive:
			if err = f.call(http.MethodDelete, path, nil, nil); err == nil {
				f.said(st, step, "Archived %s.", name)
			}
		case flows.ChangeCondition:
			err = f.call(http.MethodPut, path+"/condition", ConditionRequest{Condition: c.Condition, Note: flows.Render(c.Note, st.Values)}, nil)
			if err == nil {
				f.said(st, step, "Marked %s %s.", name, strings.ReplaceAll(c.Condition, "_", " "))
			}
		case flows.ChangeBorrow:
			if err = f.call(http.MethodPost, path+"/checkout", CheckOutRequest{}, nil); err == nil {
				f.said(st, step, "Lent %s to %s.", name, st.Values[flows.ValueUser])
			}
		case flows.ChangeReturn:
			if err = f.call(http.MethodPost, path+"/checkin", CheckInRequest{}, nil); err == nil {
				f.said(st, step, "Returned %s.", name)
			}
		case flows.ChangePlace:
			var typeID string
			if typeID, err = f.locationType(); err == nil {
				err = f.call(http.MethodPost, path+"/links", AddLinkRequest{TypeID: typeID, AssetID: c.AssetID}, &updated)
			}
			if err == nil && len(updated.Asset.Location) > 0 {
				f.said(st, step, "Moved %s to %s.", name, updated.Asset.Location[len(updated.Asset.Location)-1].Name)
			}
		case flows.ChangeCategory:
			if err = f.call(http.MethodPut, path, UpdateAssetRequest{CategoryIDs: []string{c.CategoryID}}, &updated); err == nil {
				fc, _ := f.s.flowCategories()
				f.said(st, step, "Moved %s to %s.", name, fc.name(c.CategoryID))
			}
		case flows.ChangeFields:
			vals := map[string]string{}
			for k, tmpl := range c.Fields {
				vals[k] = flows.Render(tmpl, st.Values)
			}
			if err = f.call(http.MethodPut, path, UpdateAssetRequest{FieldValues: vals}, &updated); err == nil {
				f.said(st, step, "Updated %s.", name)
			}
		case flows.ChangeAction:
			if err = f.call(http.MethodPost, path+"/actions/"+url.PathEscape(c.Action), nil, nil); err == nil {
				f.said(st, step, "Pressed %s on %s.", c.Action, name)
			}
		}
		if err != nil {
			return err
		}
		advanceProgress(st, step)
	}
	return nil
}

// checkFlow lists what stops a flow being saved by the person making the
// request: what flows.Check finds against this instance's catalog, and any
// permission its steps use that they do not hold, so a flow never offers
// more than its creator could do.
func (s *Server) checkFlow(r *http.Request, d *flows.Definition) ([]flows.Problem, error) {
	catalog, err := s.store.FlowCatalog(d.CategoryIDs)
	if err != nil {
		return nil, err
	}
	problems := d.Check(catalog)
	for _, p := range d.Permissions() {
		if !HasPermission(r.Context(), p) {
			problems = append(problems, flows.Problem{Message: fmt.Sprintf("its steps use %s, which you do not hold", p)})
		}
	}
	return problems, nil
}
