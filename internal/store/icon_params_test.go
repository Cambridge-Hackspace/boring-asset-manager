package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// A server as tall as its rack units, with a down variant.
const serverSrc = `icon "server" { category "compute"; param u 1; footprint 1 1 "u * 0.5"; axes "state"; part "box" { box 0 0 0 1 1 "u * 0.5" } }`

// paramWorld is Servers, drawn as a server whose u is bound to the number
// field rack_units.
type paramWorld struct {
	st               *Store
	c                IconCompiler
	servers, fieldID string
}

func newParamWorld(t *testing.T) paramWorld {
	t.Helper()
	st := setupTestStore(t)
	c := compilerFor(t)
	ctx := context.Background()
	if _, save, err := st.CreateIconLibraryFile(ctx, c, "state.ilx", stateAxis); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	servers, err := st.CreateCategoryFrom(CategoryInput{Name: "Servers"})
	if err != nil {
		t.Fatal(err)
	}
	if s := saveIcon(t, st, c, servers.ID, "server", serverSrc); !s.Published {
		t.Fatalf("%+v", s.Errors)
	}
	field, err := st.CreateField("rack_units", "Rack units", "number", nil)
	if err != nil {
		t.Fatal(err)
	}
	return paramWorld{st: st, c: c, servers: servers.ID, fieldID: field.ID}
}

func (w paramWorld) server(t *testing.T, name, units string) string {
	t.Helper()
	values := map[string]string{}
	if units != "" {
		values["rack_units"] = units
	}
	a, err := w.st.CreateAsset(name, []string{w.servers}, nil, values)
	if err != nil {
		t.Fatal(err)
	}
	return a.Asset.ID
}

// An asset is drawn with its icon's params set to its values of the
// fields bound to them, once a build has a wrapper for them.
func TestIconParamsFollowAssetFields(t *testing.T) {
	w := newParamWorld(t)
	ctx := context.Background()
	two, twin, none := w.server(t, "Web", "2"), w.server(t, "Web 2", "2.0"), w.server(t, "Unknown", "")
	gone := w.server(t, "Old", "9")
	if err := w.st.DeleteAsset(gone); err != nil {
		t.Fatal(err)
	}
	// nothing bound yet: drawn plain
	if ic := resolved(t, w.st, two); ic.Name != "server" || ic.Bound != "" {
		t.Fatalf("unbound: %+v", ic)
	}

	bindings, save, err := w.st.SetIconParamBindings(ctx, w.c, w.servers, map[string]string{"u": w.fieldID})
	if err != nil || len(bindings) != 1 || bindings[0].FieldKey != "rack_units" || save == nil || !save.Published {
		t.Fatalf("%+v %+v %v", bindings, save, err)
	}
	names, _ := w.st.BuiltIconNames()
	for _, n := range []string{"bam--server--u_2", "bam--server--u_2-down"} {
		if !names[n] {
			t.Errorf("no %s", n)
		}
	}
	if names["bam--server--u_9"] {
		t.Error("an archived asset's values made a wrapper")
	}
	for _, id := range []string{two, twin} {
		ic := resolved(t, w.st, id)
		if ic.Name != "bam--server--u_2" || ic.Bound != ic.Name || ic.Base != "server" || ic.Params["u"] != 2 || ic.Plain() != "server" {
			t.Errorf("2U: %+v", ic)
		}
	}
	if ic := resolved(t, w.st, none); ic.Name != "server" || ic.Base != "" || ic.Params != nil {
		t.Errorf("no value: %+v", ic)
	}

	// the wrapper is drawn as tall as its units
	b, _ := w.st.CurrentIconBuild()
	if !strings.Contains(string(b.IR), `"name":"bam--server--u_2"`) {
		t.Fatal("not in the IR")
	}
	files, err := w.st.IconLexiconFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Name == paramsFile {
			t.Error("the export has the wrappers")
		}
	}

	// a value changed is drawn plain until the next build, then as it is
	if _, err := w.st.UpdateAsset(two, "Web", []string{w.servers}, nil, map[string]string{"rack_units": "-4"}); err != nil {
		t.Fatal(err)
	}
	if ic := resolved(t, w.st, two); ic.Name != "server" {
		t.Errorf("before the build: %+v", ic)
	}
	if save, err := w.st.RebuildIcons(ctx, w.c); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	if ic := resolved(t, w.st, two); ic.Name != "bam--server--u_m4" || ic.Params["u"] != -4 {
		t.Errorf("after: %+v", ic)
	}

	// in a state, the wrapper's variant
	if _, _, err := w.st.SetAssetCondition(twin, models.ConditionOutOfService, ""); err != nil {
		t.Fatal(err)
	}
	if ic := resolved(t, w.st, twin); ic.Name != "bam--server--u_2-down" || ic.Base != "server" || ic.Bound != "bam--server--u_2" || ic.State != models.IconStateOutOfService {
		t.Errorf("down: %+v", ic)
	}

	// no one may be given a wrapper as their own icon
	if _, err := w.st.CreateAssetWithIcon("Odd", nil, nil, nil, nil, "bam--server--u_2"); err == nil {
		t.Error("a wrapper as an asset's own icon")
	}

	// unbinding draws them plain again
	if _, _, err := w.st.SetIconParamBindings(ctx, w.c, w.servers, nil); err != nil {
		t.Fatal(err)
	}
	if ic := resolved(t, w.st, twin); ic.Name != "server-down" || ic.Bound != "" {
		t.Errorf("unbound: %+v", ic)
	}

	// bindings refused
	for name, params := range map[string]map[string]string{
		"a bad name":   {"2u": w.fieldID},
		"x":            {"x": w.fieldID},
		"no field":     {"u": "7c9e6679-7425-40de-944b-e07fc1f90ae7"},
		"a text field": {"u": textField(t, w.st)},
	} {
		if _, _, err := w.st.SetIconParamBindings(ctx, w.c, w.servers, params); !errors.Is(err, ErrInvalidIconParams) {
			t.Errorf("%s: %v", name, err)
		}
	}
	plain, _ := w.st.CreateCategoryFrom(CategoryInput{Name: "Chairs"})
	if _, _, err := w.st.SetIconParamBindings(ctx, w.c, plain.ID, map[string]string{"u": w.fieldID}); !errors.Is(err, ErrInvalidIconParams) {
		t.Errorf("a category with no icon: %v", err)
	}
}

func textField(t *testing.T, st *Store) string {
	t.Helper()
	f, err := st.CreateField("notes", "Notes", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	return f.ID
}

// A wrapper that does not compile is left out, and never stops a save; a
// value that breaks the icon it extends leaves them all out, saying so.
func TestWrappersThatDoNotCompileAreLeftOut(t *testing.T) {
	w := newParamWorld(t)
	ctx := context.Background()
	ok, bad := w.server(t, "Web", "2"), w.server(t, "Odd", "3")
	if _, _, err := w.st.SetIconParamBindings(ctx, w.c, w.servers, map[string]string{"u": w.fieldID}); err != nil {
		t.Fatal(err)
	}
	// an icon that cannot be drawn 3 units high
	src := `icon "server" { category "compute"; param u 1; footprint 1 1 "u * 0.5"; axes "state"; part "box" { box 0 0 0 1 1 "u * 0.5" when="1 / (u - 3) != 0" } }`
	save, err := w.st.SaveCategoryIcon(ctx, w.c, w.servers, "server", src)
	if err != nil || !save.Published {
		t.Fatalf("a save with a value that does not draw: %+v %v", save, err)
	}
	if ic := resolved(t, w.st, ok); ic.Name != "bam--server--u_2" {
		t.Errorf("the wrapper that compiles: %+v", ic)
	}
	if ic := resolved(t, w.st, bad); ic.Name != "server" || ic.Bound != "" {
		t.Errorf("the wrapper that does not compile: %+v", ic)
	}
	if len(save.Warnings) != 1 || !strings.Contains(save.Warnings[0].Message, "1 of 2 icons drawn for assets' field values are left out") ||
		!strings.Contains(save.Warnings[0].Message, "division by zero") {
		t.Errorf("warnings: %+v", save.Warnings)
	}
}

// A wrapper with an error on its own line, as one extending an icon no
// longer drawn has, is left out by it, and the rest are kept.
func TestAWrapperErringOnItsLineIsLeftOut(t *testing.T) {
	w := newParamWorld(t)
	ctx := context.Background()
	w.server(t, "Web", "2")
	if _, _, err := w.st.SetIconParamBindings(ctx, w.c, w.servers, map[string]string{"u": w.fieldID}); err != nil {
		t.Fatal(err)
	}
	in, err := w.st.readIconInputs(w.st.Query)
	if err != nil || len(in.wrappers) != 1 {
		t.Fatalf("%+v %v", in.wrappers, err)
	}
	ghost := []paramValue{{"u", "1"}}
	in.wrappers = append([]iconWrapper{{name: wrapperName("ghost", ghost), icon: "ghost", params: ghost}}, in.wrappers...)
	res, err := w.st.compileInputs(ctx, w.c, in)
	if err != nil || !res.OK {
		t.Fatalf("%+v %v", res, err)
	}
	var names []string
	for _, i := range res.Doc.Icons {
		names = append(names, i.Name)
	}
	if got := strings.Join(names, " "); strings.Contains(got, "ghost") || !strings.Contains(got, "bam--server--u_2") {
		t.Errorf("%s", got)
	}
}
