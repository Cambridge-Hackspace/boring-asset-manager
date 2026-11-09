package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/mcpserver"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Flows are run here (docs/flows.md): each step that changes something asks
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
	return store.FlowRunState{Step: run.Step, Values: run.Values, Done: run.Done, Finished: run.Finished, Fields: run.Fields, Progress: run.Progress, Answered: run.Answered, Photos: run.Photos,
		Linked: run.Linked, Made: run.Made}
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
	st := store.FlowRunState{Values: f.startValues(), Done: []models.FlowDone{}}
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

// startValues are the values a run of the flow by the requester starts
// with.
func (f *flowRunner) startValues() flows.Values {
	name := "Unnamed user"
	if u := CurrentUser(f.r.Context()); u != nil && u.Name != "" {
		name = u.Name
	} else if in := currentIncoming(f.r.Context()); in != nil {
		// An incoming hook runs it as itself (docs/hooks.md#incoming-hooks).
		name = in.hook.Name
	}
	fc, _ := f.s.flowCategories()
	return flows.Start(f.flow.Name, name, time.Now(), &f.flow.Definition, fc.name)
}

// firstFind is the flow's first step, if it is a Find: a flow that starts
// by finding something can be started from it (docs/flows.md).
func (f *flowRunner) firstFind() *flows.Step {
	d := &f.flow.Definition
	i := d.Next(0, f.startValues())
	if i < len(d.Steps) && d.Steps[i].Kind == flows.KindFind {
		return &d.Steps[i]
	}
	return nil
}

// UnattendedAnswers answer a flow run from start to end without a person,
// for an incoming hook (docs/hooks.md#incoming-hooks): the category to
// choose, the forms' answers by their questions' keys, the asset a Find
// finds, and the code of the place a Create puts what it files.
type UnattendedAnswers struct {
	CategoryID string
	Answers    map[string]string
	AssetID    string
	PlaceCode  string
}

// runUnattended runs a flow to its end at once, answering each step that
// asks something from in, and keeps no run. It acts as the request does,
// held to its permissions and scopes as a person's run is. A step that
// cannot be answered, or fails, stops it there, saying which.
func (f *flowRunner) runUnattended(in UnattendedAnswers) (store.FlowRunState, error) {
	if may, err := f.s.mayRun(f.r, f.flow); err != nil {
		return store.FlowRunState{}, err
	} else if !may || !f.flow.Enabled {
		return store.FlowRunState{}, &flowError{Status: http.StatusNotFound, Message: "Flow not found, turned off, or not one it may run"}
	}
	d := &f.flow.Definition
	st := store.FlowRunState{Values: f.startValues(), Done: []models.FlowDone{}}
	st.Step = d.Next(0, st.Values)
	st, err := f.advance(st)
	// Each step answered moves the run on, so it ends within as many
	// answers as it has steps.
	for n := 0; err == nil && !st.Finished && n <= len(d.Steps); n++ {
		step := &d.Steps[st.Step]
		a := FlowAnswer{Step: step.ID}
		switch step.Kind {
		case flows.KindChooseCategory:
			a.CategoryID = in.CategoryID
		case flows.KindForm:
			a.Answers = in.Answers
		case flows.KindFind:
			a.AssetID = in.AssetID
		case flows.KindCreate:
			a.PlaceCode = in.PlaceCode
		case flows.KindLink:
			// Nothing to choose with: one not required is passed over.
			a.Done = true
		}
		if err = f.take(&st, step, a); err == nil {
			st, err = f.advance(st)
		}
	}
	var fe *flowError
	if errors.As(err, &fe) && st.Step < len(d.Steps) {
		// Which step stopped it: the one the run is left on.
		err = &flowError{Status: fe.Status, Message: fmt.Sprintf("Step %q: %s", d.Steps[st.Step].ID, fe.Message)}
	}
	if err == nil && !st.Finished {
		err = refuseStep("The flow did not finish")
	}
	return st, err
}

// FlowAnswer answers the step a run is on. Step names it, so an answer
// meant for another step is refused rather than taken for this one.
type FlowAnswer struct {
	Step string `json:"step"`
	// Choose category.
	CategoryID string `json:"category_id,omitempty"`
	// Form: its questions' answers, by key.
	Answers map[string]string `json:"answers,omitempty"`
	// Find: the asset chosen from a list or search, or a code scanned.
	AssetID string `json:"asset_id,omitempty"`
	Code    string `json:"code,omitempty"`
	// Create: the code of the place it is put, when it is scanned.
	PlaceCode string `json:"place_code,omitempty"`
	// Link (#264): what to link the asset to, chosen or scanned (as well
	// as asset_id and code), or something new to file and link to; and
	// done, to move on from it, having linked what it will, or none when
	// it is not required.
	AssetIDs []string      `json:"asset_ids,omitempty"`
	Codes    []string      `json:"codes,omitempty"`
	Create   *FlowNewAsset `json:"create,omitempty"`
	Done     bool          `json:"done,omitempty"`
	// given is set on an answer the server gives for the person, as when a
	// run started from an asset finds it: not counted as a step they took.
	given bool
}

// FlowNewAsset is something new a Link step files to link to, with only a
// name.
type FlowNewAsset struct {
	Name string `json:"name"`
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
	at := st.Step
	step := &d.Steps[at]
	stepErr := f.take(&st, step, a)
	if stepErr == nil {
		// A step that asks something, answered by the person and left: one
		// more toward "step 2 of 3". Trying a failed Label again is not
		// one, nor is each link a Link step that links any number makes.
		if step.Asks() && !a.given && (st.Step != at || st.Finished) {
			st.Answered++
		}
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
	case flows.KindLink:
		var stay bool
		if stay, err = f.takeLink(st, step, a); err == nil && stay {
			// Linking any number, it stays for the next, until done.
			return nil
		}
	case flows.KindLabel, flows.KindChange, flows.KindSetFields:
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
	case flows.KindSetFields:
		return f.setFields(st, step)
	case flows.KindLink:
		return f.link(st, step, step.Assets)
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

// takeForm takes a Form's answers, each the run's value of its question's
// key, refusing one required and left empty or not of its question's type.
func (f *flowRunner) takeForm(st *store.FlowRunState, step *flows.Step, a FlowAnswer) error {
	for _, q := range step.Questions {
		val := strings.TrimSpace(a.Answers[q.Key])
		if val == "" && q.Required {
			return refuseStep("%s is required", q.Label)
		}
		if p := q.Problem(val); p != "" {
			return refuseStep("%s %s", q.Label, p)
		}
		st.Values[q.Key] = val
	}
	return nil
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
	// Filed by the person running it; by no one when an incoming hook does.
	by := ""
	if u := CurrentUser(f.r.Context()); u != nil {
		by = u.ID
	}
	if err := f.s.store.RecordFiling(f.flow.ID, made.Asset.Asset.ID, by); err != nil {
		return err
	}
	// What Set fields gave it is filed; a photo it was given is for the
	// page to add to it now.
	st.Fields = nil
	for i := range st.Photos {
		if st.Photos[i].AssetID == "" {
			st.Photos[i].AssetID = made.Asset.Asset.ID
		}
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

// setFields does a Set fields step (#174): before a Create, it gives the
// new asset that Create files its values; after a Find or Create, it
// updates the asset found or filed (flows.Definition.SetsOn). A row naming
// a field the asset's category does not record is passed over, as one of
// the flow's other categories'; a value that does not suit its field fails
// the step, saying why.
func (f *flowRunner) setFields(st *store.FlowRunState, step *flows.Step) error {
	id := ""
	if f.flow.Definition.SetsOn(st.Step) != flows.OnNew {
		if id = st.Values[flows.ValueAssetID]; id == "" {
			return refuseStep("There is nothing to set fields on: nothing was found or filed")
		}
	}
	cats := []string{st.Values[flows.ValueCategoryID]}
	if id != "" {
		a, err := f.s.store.GetAssetByID(id)
		if err != nil {
			return err
		}
		cats = cats[:0]
		for _, c := range a.Categories {
			cats = append(cats, c.ID)
		}
	}
	effective, err := f.s.store.EffectiveFieldsForAsset(cats)
	if err != nil {
		return err
	}
	byKey := map[string]models.Field{}
	for _, ef := range effective {
		byKey[ef.Field.Key] = ef.Field
	}
	vals := map[string]string{}
	for _, row := range step.Fields {
		field, ok := byKey[row.Field]
		if !ok {
			continue
		}
		val := strings.TrimSpace(flows.Render(row.Value, st.Values))
		if err := fields.ValidateFieldValue(&field, val); err != nil {
			return refuseStep("%s cannot be set to %q: %s", field.Label, val, strings.TrimPrefix(err.Error(), fields.ErrInvalidFieldValue.Error()+": "))
		}
		vals[row.Field] = val
	}
	photo := ""
	if q := step.PhotoQuestion(); q != "" && st.Values[q] != "" {
		photo = q
	}
	if id == "" {
		if st.Fields == nil {
			st.Fields = map[string]string{}
		}
		for k, v := range vals {
			st.Values[k] = v
			if v != "" {
				st.Fields[k] = v
			} else {
				delete(st.Fields, k)
			}
		}
		if photo != "" {
			st.Photos = append(st.Photos, models.FlowPhoto{Question: photo})
		}
		return nil
	}
	if len(vals) > 0 {
		var updated struct {
			Asset models.AssetWithDetails `json:"asset"`
		}
		if err := f.call(http.MethodPut, "/api/v1/assets/"+url.PathEscape(id), UpdateAssetRequest{FieldValues: vals}, &updated); err != nil {
			return err
		}
		setAsset(st.Values, &updated.Asset)
		f.said(st, step, "Updated %s.", st.Values[flows.ValueAsset])
	}
	if photo != "" {
		st.Photos = append(st.Photos, models.FlowPhoto{Question: photo, AssetID: id})
	}
	return nil
}

// relationType is the relation type a Link step links by.
func (f *flowRunner) relationType(step *flows.Step) (*models.RelationType, error) {
	types, err := f.s.store.ListRelationTypes()
	if err != nil {
		return nil, err
	}
	for i := range types {
		if types[i].ID == step.RelationType {
			return &types[i], nil
		}
	}
	return nil, refuseStep("The kind of link this step makes is gone: the flow needs changing")
}

// linkable says whether a Link step may link to an asset, whatever its
// state: in the categories the step finds in, or below them (any, when it
// names none), one the runner may read and change, and narrowed as the
// step says. An archived one is the links endpoint's to refuse.
func (f *flowRunner) linkable(step *flows.Step, v flows.Values) (func(a *models.AssetWithDetails) bool, error) {
	find := step.Find
	fc, err := f.s.flowCategories()
	if err != nil {
		return nil, err
	}
	var filed map[string]bool
	if find.FiledBy != "" {
		if filed, err = f.s.store.FiledBy(find.FiledBy); err != nil {
			return nil, err
		}
	}
	match := ""
	if find.Match != nil {
		match = strings.TrimSpace(flows.Render(find.Match.Value, v))
	}
	ctx := f.r.Context()
	return func(a *models.AssetWithDetails) bool {
		if !Allows(ctx, rbac.PermAssetsRead, a.Scopes) || !Allows(ctx, rbac.PermAssetsWrite, a.Scopes) {
			return false
		}
		if filed != nil && !filed[a.Asset.ID] {
			return false
		}
		if find.Match != nil && !strings.EqualFold(strings.TrimSpace(a.FieldValues[find.Match.Field]), match) {
			return false
		}
		if len(find.Categories) == 0 {
			return true
		}
		for _, c := range a.Categories {
			if fc.within(c.ID, find.Categories) {
				return true
			}
		}
		return false
	}, nil
}

// linkCandidates are the assets a Link step offers to link the run's asset
// to: the active ones it may link to, other than the asset itself,
// narrowed by search.
func (f *flowRunner) linkCandidates(step *flows.Step, v flows.Values, search string) ([]models.AssetWithDetails, error) {
	all, err := f.s.store.FilterAssets(store.AssetFilter{})
	if err != nil {
		return nil, err
	}
	ok, err := f.linkable(step, v)
	if err != nil {
		return nil, err
	}
	out := []models.AssetWithDetails{}
	for i := range all {
		if all[i].Asset.ID != v[flows.ValueAssetID] && ok(&all[i]) {
			out = append(out, all[i])
		}
	}
	return searched(out, search), nil
}

// takeLink takes the answer to a Link step the runner finds for (#264):
// what it names, chosen or scanned, or something new to file, is linked
// to, each through the links endpoint; done moves on. stay is set when
// the step links any number and is not done: the run stays on it for the
// next. Something it filed and failed to link is linked first, on any
// answer, rather than filed again.
func (f *flowRunner) takeLink(st *store.FlowRunState, step *flows.Step, a FlowAnswer) (stay bool, err error) {
	if step.Find == nil {
		// Linking to what the flow names, answered only to try again.
		return false, f.link(st, step, step.Assets)
	}
	var targets []string
	if made, ok := st.Made[step.ID]; ok {
		targets = append(targets, made.ID)
	} else if a.Create != nil {
		if !step.AllowCreate {
			return false, refuseStep("This step does not file anything new")
		}
		name := strings.TrimSpace(a.Create.Name)
		if name == "" {
			return false, refuseStep("Give the new one a name")
		}
		var made struct {
			Asset models.AssetWithDetails `json:"asset"`
		}
		if err := f.call(http.MethodPost, "/api/v1/assets", CreateAssetRequest{Name: name, CategoryIDs: []string{step.CreateCategory}}, &made); err != nil {
			return false, err
		}
		if st.Made == nil {
			st.Made = map[string]models.FlowLinked{}
		}
		st.Made[step.ID] = models.FlowLinked{ID: made.Asset.Asset.ID, Name: made.Asset.Asset.Name}
		fc, _ := f.s.flowCategories()
		f.said(st, step, "Filed %s in %s.", made.Asset.Asset.Name, fc.name(step.CreateCategory))
		targets = append(targets, made.Asset.Asset.ID)
	}
	ids := slices.Clone(a.AssetIDs)
	if a.AssetID != "" {
		ids = append(ids, a.AssetID)
	}
	codes := slices.Clone(a.Codes)
	if a.Code != "" {
		codes = append(codes, a.Code)
	}
	if len(codes) > 0 && !slices.Contains(step.Find.FindBy, flows.FindScan) {
		return false, refuseStep("This step does not find by scanning")
	}
	for _, code := range codes {
		var scanned ScanResponse
		if err := f.call(http.MethodGet, "/api/v1/scan/"+url.PathEscape(strings.TrimSpace(code)), nil, &scanned); err != nil {
			return false, refuseStep("Nothing has the code %s", strings.TrimSpace(code))
		}
		ids = append(ids, scanned.Asset.ID)
	}
	if len(ids) > 0 {
		ok, err := f.linkable(step, st.Values)
		if err != nil {
			return false, err
		}
		for _, id := range ids {
			if id == "" || slices.Contains(targets, id) {
				continue
			}
			other, err := f.s.store.GetAssetByID(id)
			if errors.Is(err, store.ErrAssetNotFound) || (err == nil && !ok(other)) {
				return false, refuseStep("That is not something this step can link to")
			}
			if err != nil {
				return false, err
			}
			targets = append(targets, id)
		}
	}
	linked := map[string]bool{}
	for _, l := range st.Linked[step.ID] {
		linked[l.ID] = true
	}
	if !step.Repeat {
		n := len(linked)
		for _, id := range targets {
			if !linked[id] {
				n++
			}
		}
		if n > 1 {
			return false, refuseStep("This step links to one thing only")
		}
	}
	if len(targets) == 0 {
		if !a.Done {
			return false, refuseStep("Choose what to link it to, or scan its code")
		}
		if step.Required && len(linked) == 0 {
			return false, refuseStep("Choose at least one thing to link it to")
		}
		return false, nil
	}
	if err := f.link(st, step, targets); err != nil {
		return false, err
	}
	return step.Repeat && !a.Done, nil
}

// link links the run's asset to each of these assets, by the step's
// relation type and way round, through the links endpoint as the runner,
// so every rule links follow holds: not to itself, not twice, nothing
// archived, no loop of a single type. Those it linked before, on a try
// that failed part way, are not linked again. A link of a single type
// moves what it holds out of where it was, and says so. {linked} is the
// names of all the step has linked.
func (f *flowRunner) link(st *store.FlowRunState, step *flows.Step, targets []string) error {
	id := st.Values[flows.ValueAssetID]
	if id == "" {
		return refuseStep("There is nothing to link: nothing was found or filed")
	}
	t, err := f.relationType(step)
	if err != nil {
		return err
	}
	done := map[string]bool{}
	for _, l := range st.Linked[step.ID] {
		done[l.ID] = true
	}
	label := t.InverseName
	if step.Outgoing || t.IsSymmetric {
		label = t.Name
	}
	for _, other := range targets {
		if done[other] {
			continue
		}
		this, err := f.s.store.GetAssetByID(id)
		if err != nil {
			return err
		}
		that, err := f.s.store.GetAssetByID(other)
		if errors.Is(err, store.ErrAssetNotFound) {
			return refuseStep("Something it links to is gone")
		}
		if err != nil {
			return err
		}
		who, from := moving(t, step.Outgoing, this, that)
		if err := f.call(http.MethodPost, "/api/v1/assets/"+url.PathEscape(id)+"/links", AddLinkRequest{TypeID: t.ID, AssetID: other, Outgoing: step.Outgoing}, nil); err != nil {
			var fe *flowError
			if errors.As(err, &fe) {
				return &flowError{Status: fe.Status, Message: fmt.Sprintf("%s: %s", that.Asset.Name, fe.Message)}
			}
			return err
		}
		if st.Linked == nil {
			st.Linked = map[string][]models.FlowLinked{}
		}
		st.Linked[step.ID] = append(st.Linked[step.ID], models.FlowLinked{ID: other, Name: that.Asset.Name})
		done[other] = true
		if made, ok := st.Made[step.ID]; ok && made.ID == other {
			delete(st.Made, step.ID)
		}
		moved := ""
		if who != "" {
			moved = fmt.Sprintf(" Moved %s out of %s.", who, from)
		}
		f.said(st, step, "Linked %s: %s %s.%s", this.Asset.Name, label, that.Asset.Name, moved)
	}
	var names []string
	for _, l := range st.Linked[step.ID] {
		names = append(names, l.Name)
	}
	st.Values[flows.ValueLinked] = strings.Join(names, ", ")
	return nil
}

// moving says what a link of a single type between this asset and that
// moves, and out of what: the asset at its far end leaves the one it was
// linked from. Both are empty when it moves nothing.
func moving(t *models.RelationType, outgoing bool, this, that *models.AssetWithDetails) (who, from string) {
	if !t.IsSingle {
		return "", ""
	}
	held, by := that, this
	if !outgoing && !t.IsSymmetric {
		held, by = this, that
	}
	for _, l := range held.Links {
		if l.TypeID == t.ID && !l.Outgoing && l.Asset.ID != by.Asset.ID {
			return held.Asset.Name, l.Asset.Name
		}
	}
	return "", ""
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
		case flows.ChangeHook:
			if err = f.call(http.MethodPost, path+"/hooks/"+url.PathEscape(c.Hook), nil, nil); err == nil {
				f.said(st, step, "Triggered %s on %s.", c.Hook, name)
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
	more, err := s.checkLinkScopes(r, d)
	return append(problems, more...), err
}

// checkLinkScopes lists what a flow's Link steps offer that its creator
// could not do themselves (#264): finding in a category where they may not
// read, filing into one where they may not write, or linking to an asset
// named in the flow that they may not change.
func (s *Server) checkLinkScopes(r *http.Request, d *flows.Definition) ([]flows.Problem, error) {
	var out []flows.Problem
	scopesOf, err := s.store.ScopeResolver()
	if err != nil {
		return nil, err
	}
	fc, err := s.flowCategories()
	if err != nil {
		return nil, err
	}
	name := func(id string) string {
		if n := fc.name(id); n != "" {
			return n
		}
		return id
	}
	for _, step := range d.Steps {
		if step.Kind != flows.KindLink {
			continue
		}
		bad := func(format string, args ...any) {
			out = append(out, flows.Problem{Step: step.ID, Message: fmt.Sprintf(format, args...)})
		}
		if step.Find != nil {
			for _, id := range step.Find.Categories {
				if _, ok := fc.byID[id]; ok && !Allows(r.Context(), rbac.PermAssetsRead, scopesOf([]string{id})) {
					bad("it finds in %s, where you do not hold %s", name(id), rbac.PermAssetsRead)
				}
			}
		}
		if id := step.CreateCategory; step.AllowCreate && id != "" {
			if _, ok := fc.byID[id]; ok && !Allows(r.Context(), rbac.PermAssetsWrite, scopesOf([]string{id})) {
				bad("it files into %s, where you do not hold %s", name(id), rbac.PermAssetsWrite)
			}
		}
		if len(step.Assets) > 0 {
			scopes, err := s.store.AssetScopes(step.Assets)
			if err != nil {
				return nil, err
			}
			for _, id := range step.Assets {
				if !Allows(r.Context(), rbac.PermAssetsWrite, scopes[id]) {
					bad("it links to an asset you may not change")
					break
				}
			}
		}
	}
	return out, nil
}
