package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/assetcsv"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Person fields (docs/data-model.md#person-fields) name people by user ID.
// They follow the rule people follow: names show to those with
// assets.track for the asset, and to anyone who can read it when the field
// is public. Others see that someone is set, but not who, except
// themselves. Emails never show. The people a field may be set to are
// offered only to those who see who, and a value may name no one else.

// mayAddPeople reports whether the request may add people without
// accounts: people.add, which users.manage includes.
func mayAddPeople(ctx context.Context) bool {
	return HasPermission(ctx, rbac.PermPeopleAdd) || HasPermission(ctx, rbac.PermUsersManage)
}

// seesFieldPeople reports whether the request sees whom a person field of
// an asset in these scopes names: with assets.track, or a public field.
func seesFieldPeople(ctx context.Context, public bool, scopes []string) bool {
	return public || Allows(ctx, rbac.PermAssetsTrack, scopes)
}

// redactFieldPeople leaves an asset's person fields naming only whom the
// request may see, counting the rest in HiddenPeople, and its people only
// those named. The requester always sees themselves.
func redactFieldPeople(ctx context.Context, a *models.AssetWithDetails) {
	if len(a.PersonFields) == 0 {
		return
	}
	me := ""
	if u := CurrentUser(ctx); u != nil {
		me = u.ID
	}
	values := make(map[string]string, len(a.FieldValues))
	for k, v := range a.FieldValues {
		values[k] = v
	}
	people := map[string]models.FieldPerson{}
	hidden := map[string]int{}
	for key, public := range a.PersonFields {
		sees := seesFieldPeople(ctx, public, a.Scopes)
		var shown []string
		for _, id := range fields.PersonIDs(values[key]) {
			p, ok := a.People[id]
			switch {
			case !ok:
				// names no one: shown as no one
			case sees || id == me:
				shown = append(shown, id)
				people[id] = p
			default:
				hidden[key]++
			}
		}
		if len(shown) > 0 {
			values[key] = fields.PersonValue(shown)
		} else {
			delete(values, key)
		}
	}
	a.FieldValues, a.People, a.HiddenPeople = values, nil, nil
	if len(people) > 0 {
		a.People = people
	}
	if len(hidden) > 0 {
		a.HiddenPeople = hidden
	}
}

// personFields are the person fields, by key.
func (s *Server) personFields() (map[string]models.Field, error) {
	all, err := s.store.ListFields()
	if err != nil {
		return nil, err
	}
	out := map[string]models.Field{}
	for _, f := range all {
		if f.ValidationType == fields.TypePerson {
			out[f.Key] = f
		}
	}
	return out, nil
}

// choosable are the people a person field of an asset in these scopes may
// be set to by the request: those ChoosablePeople lists, to a request
// with assets.track there, and, always, the requester.
func (s *Server) choosable(ctx context.Context, scopes []string) (map[string]bool, error) {
	people, err := s.choosableList(ctx, scopes)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, p := range people {
		out[p.ID] = true
	}
	return out, nil
}

// choosableList are the people choosable answers, by name.
func (s *Server) choosableList(ctx context.Context, scopes []string) ([]models.FieldPerson, error) {
	var out []models.FieldPerson
	if Allows(ctx, rbac.PermAssetsTrack, scopes) {
		people, err := s.store.ChoosablePeople(scopes)
		if err != nil {
			return nil, err
		}
		out = people
	}
	if u := CurrentUser(ctx); u != nil && u.IsActive && u.ArchivedAt == nil && !slices.ContainsFunc(out, func(p models.FieldPerson) bool { return p.ID == u.ID }) {
		out = append(out, models.FieldPerson{ID: u.ID, Name: store.PersonName(models.Person{Name: u.Name})})
	}
	return out, nil
}

// importPerson finds, for an import, the one person a cell names for an
// asset in these scopes, among those the importer may choose there: by
// user ID, by email, or by a name only one of them has, ignoring case.
// Whom they may not choose is no one, so an import learns no more than
// the form would show.
func (s *Server) importPerson(ctx context.Context) func(ref string, scopes []string) (string, error) {
	lists := map[string][]models.FieldPerson{}
	return func(ref string, scopes []string) (string, error) {
		key := strings.Join(scopes, ",")
		people, ok := lists[key]
		if !ok {
			var err error
			if people, err = s.choosableList(ctx, scopes); err != nil {
				return "", err
			}
			lists[key] = people
		}
		may := func(id string) bool {
			return slices.ContainsFunc(people, func(p models.FieldPerson) bool { return p.ID == id })
		}
		if u, err := s.store.GetUserByID(ref); err == nil {
			if may(u.ID) {
				return u.ID, nil
			}
			return "", assetcsv.ErrNoSuchPerson
		}
		if strings.Contains(ref, "@") {
			if u, err := s.store.GetUserByEmail(ref); err == nil && may(u.ID) {
				return u.ID, nil
			}
			return "", assetcsv.ErrNoSuchPerson
		}
		var found []string
		for _, p := range people {
			if strings.EqualFold(p.Name, ref) {
				found = append(found, p.ID)
			}
		}
		switch len(found) {
		case 0:
			return "", assetcsv.ErrNoSuchPerson
		case 1:
			return found[0], nil
		}
		return "", assetcsv.ErrSeveralPeople
	}
}

// keepHiddenPeople keeps, in the person fields an import sets on assets it
// updates, whom the importer does not see, as checkFieldPeople does for a
// form.
func (s *Server) keepHiddenPeople(ctx context.Context, rows []store.ImportRow) error {
	defs, err := s.personFields()
	if err != nil {
		return err
	}
	byID := map[string]models.Field{}
	for _, f := range defs {
		byID[f.ID] = f
	}
	for i := range rows {
		row := &rows[i]
		if row.ID == "" {
			continue
		}
		var had *models.AssetWithDetails
		for fieldID, v := range row.FieldValues {
			f, ok := byID[fieldID]
			if !ok {
				continue
			}
			if had == nil {
				if had, err = s.store.GetAssetByID(row.ID); err != nil {
					return err
				}
			}
			seen := *had
			redactFieldPeople(ctx, &seen)
			ids := fields.PersonIDs(v)
			if fields.PersonOptionsOf(&f).Multiple || len(ids) == 0 {
				shown := fields.PersonIDs(seen.FieldValues[f.Key])
				for _, id := range fields.PersonIDs(had.FieldValues[f.Key]) {
					if !slices.Contains(shown, id) && !slices.Contains(ids, id) {
						ids = append(ids, id)
					}
				}
			}
			row.FieldValues[fieldID] = fields.PersonValue(ids)
		}
	}
	return nil
}

// errNotChoosable is a person field set to someone the request may not
// choose there.
var errNotChoosable = errors.New("a person field can be set only to someone you may choose there")

// checkFieldPeople checks the person fields a request sets on an asset in
// these scopes, had as it was, nil for a new one: each person it adds must
// be one the request may choose there. The people it names that the
// request does not see stay, as it could not have meant to take them out.
// It answers the values to store.
func (s *Server) checkFieldPeople(ctx context.Context, values map[string]string, scopes []string, had *models.AssetWithDetails) (map[string]string, error) {
	if len(values) == 0 {
		return values, nil
	}
	defs, err := s.personFields()
	if err != nil {
		return nil, err
	}
	var seen *models.AssetWithDetails
	if had != nil {
		c := *had
		seen = &c
		redactFieldPeople(ctx, seen)
	}
	var may map[string]bool
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
		f, ok := defs[key]
		if !ok {
			continue
		}
		var was, shown []string
		if had != nil {
			was = fields.PersonIDs(had.FieldValues[key])
			shown = fields.PersonIDs(seen.FieldValues[key])
		}
		ids := fields.PersonIDs(value)
		for _, id := range ids {
			if slices.Contains(was, id) {
				continue
			}
			if may == nil {
				if may, err = s.choosable(ctx, scopes); err != nil {
					return nil, err
				}
			}
			if !may[id] {
				return nil, errNotChoosable
			}
		}
		// Whom it names that the request does not see stay, but in a field
		// naming one person, whom the request names instead.
		if fields.PersonOptionsOf(&f).Multiple || len(ids) == 0 {
			for _, id := range was {
				if !slices.Contains(shown, id) && !slices.Contains(ids, id) {
					ids = append(ids, id)
				}
			}
		}
		out[key] = fields.PersonValue(ids)
	}
	return out, nil
}

// scopesOfCategories are the scopes an asset in these categories is in.
func (s *Server) scopesOfCategories(categoryIDs []string) ([]string, error) {
	return s.store.ScopesForCategories(categoryIDs)
}

// handleChoosePeople lists the people a person field may be set to, by
// name, those matching q: GET /api/v1/people/choices?q=…, or the one ref
// names, by user ID, email or a name only one has, for an asset in the
// categories category_id names (repeated), or the asset asset_id names.
// It takes assets.track in those scopes, as only those who see the people
// around assets learn who they are. can_add says whether the request may
// add someone without an account.
func (s *Server) handleChoosePeople(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var scopes []string
	var err error
	switch {
	case len(q["category_id"]) > 0:
		scopes, err = s.scopesOfCategories(q["category_id"])
	case q.Get("asset_id") != "":
		a, ok := s.assetFor(w, r, q.Get("asset_id"))
		if !ok {
			return
		}
		scopes = a.Scopes
	default:
		scopes = []string{rbac.DefaultScope}
	}
	if err != nil {
		writeScopeError(w, err, "read the categories' scopes")
		return
	}
	ctx := r.Context()
	// ?ref= finds the one person a reference names, as an import does.
	if ref := strings.TrimSpace(q.Get("ref")); ref != "" {
		id, err := s.importPerson(ctx)(ref, scopes)
		switch {
		case errors.Is(err, assetcsv.ErrNoSuchPerson):
			writeJSONError(w, http.StatusNotFound, "No one you may choose here is "+strconvQuote(ref))
		case errors.Is(err, assetcsv.ErrSeveralPeople):
			writeJSONError(w, http.StatusConflict, "Several people are named "+strconvQuote(ref)+": give their email or user ID instead")
		case err != nil:
			writeServerError(w, err, "find the person")
		default:
			people, err := s.store.FieldPeople([]string{id})
			if err != nil {
				writeServerError(w, err, "find the person")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"people": []models.FieldPerson{people[id]}, "can_add": mayAddPeople(ctx)})
		}
		return
	}
	out := []models.FieldPerson{}
	if Allows(ctx, rbac.PermAssetsTrack, scopes) {
		people, err := s.store.ChoosablePeople(scopes)
		if err != nil {
			writeServerError(w, err, "list the people")
			return
		}
		out = append(out, store.MatchPeople(people, q.Get("q"))...)
	} else if u := CurrentUser(ctx); u != nil {
		// Anyone may name themselves.
		me := models.FieldPerson{ID: u.ID, Name: store.PersonName(models.Person{Name: u.Name})}
		out = append(out, store.MatchPeople([]models.FieldPerson{me}, q.Get("q"))...)
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": out, "can_add": mayAddPeople(ctx)})
}

// AddPersonRequest adds a person without an account: a name, and an email
// or none.
type AddPersonRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// handleAddPerson adds a person without an account, to name in person
// fields: POST /api/v1/people with {"name", "email"}. It takes people.add,
// or users.manage, which includes it.
func (s *Server) handleAddPerson(w http.ResponseWriter, r *http.Request) {
	if !mayAddPeople(r.Context()) {
		writeJSONError(w, http.StatusForbidden, "Forbidden: adding people takes people.add")
		return
	}
	var req AddPersonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	u, err := s.store.CreatePerson(req.Name, req.Email)
	switch {
	case errors.Is(err, store.ErrInvalidUser):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrEmailTaken):
		writeJSONError(w, http.StatusConflict, "Someone here has that email already: choose them instead")
	case err != nil:
		writeStoreError(w, err, "add the person")
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"person": models.FieldPerson{ID: u.ID, Name: store.PersonName(models.Person{Name: u.Name}), NoAccount: true}})
	}
}

// MergePersonRequest names whom a person without an account merges into.
type MergePersonRequest struct {
	Into string `json:"into"`
}

// handleMergePerson merges a person without an account into another
// person: POST /api/v1/users/{id}/merge with {"into"}. Every field naming
// them names the other instead, each asset's history saying so, and they
// are archived.
func (s *Server) handleMergePerson(w http.ResponseWriter, r *http.Request) {
	var req MergePersonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Into) == "" {
		writeJSONError(w, http.StatusBadRequest, "into is required: the user ID to merge them into")
		return
	}
	n, err := s.actingStore(r).MergePerson(r.PathValue("id"), req.Into)
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, "User not found")
	case errors.Is(err, store.ErrHasAccount):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrInvalidUser):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeServerError(w, err, "merge the person")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"assets": n})
	}
}

// personFilter keeps the assets, as the request sees them, whose person
// field keyed field, or any person field when it is empty, names person.
func personFilter(assets []models.AssetWithDetails, person, field string) []models.AssetWithDetails {
	kept := assets[:0]
	for _, a := range assets {
		for key := range a.PersonFields {
			if (field == "" || key == field) && slices.Contains(fields.PersonIDs(a.FieldValues[key]), person) {
				kept = append(kept, a)
				break
			}
		}
	}
	return kept
}

// writeFieldPeopleError answers a failure of checkFieldPeople, reporting
// whether there was none.
func writeFieldPeopleError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, errNotChoosable):
		writeJSONError(w, http.StatusForbidden, "Forbidden: "+err.Error())
	default:
		writeServerError(w, err, "check the people in its fields")
	}
	return false
}

// strconvQuote quotes text for a message.
func strconvQuote(s string) string { return strconv.Quote(s) }
