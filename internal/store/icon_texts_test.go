package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// A font of a few strokes, and a tag that writes an asset's texts on it.
const (
	miniFont = `font "mini" { em 10; ascent 7; descent 3; kind "stroke"
  glyph "A" advance=5 { path d="M0,0 L2,7 L4,0" }
  glyph "B" advance=5 { path d="M0,0 V7 H3 L0,3.5 L3,0 Z" }
  glyph "1" advance=5 { path d="M1,6 L2,7 V0" }
  notdef advance=5 { path d="M0,0 H4 V7 H0 Z" }
}`
	tagSrc = `icon "tag" { category "labels"; footprint 1 1 1; axes "state"
  param label (text)"?"
  param note (text)"-"
  param u 1
  part "box" { box 0 0 0 1 1 "u * 0.5" }
  part "label" { at 0.5 0.5 1 { text 0 0 "{label}" font="mini" size=8 } }
  part "note" { at 0.5 0.5 1 { text 0 9 "{note}" font="mini" size=6 } }
}`
)

// textWorld is Tags, drawn as a tag, with a text field and a number field.
type textWorld struct {
	st                  *Store
	c                   *isolex.Compiler
	tags, notes, height string
}

func newTextWorld(t *testing.T) textWorld {
	t.Helper()
	st := setupTestStore(t)
	c := compilerFor(t)
	ctx := context.Background()
	for _, f := range [][2]string{{"state.ilx", stateAxis}, {"fonts/mini.ilx", miniFont}} {
		if _, save, err := st.CreateIconLibraryFile(ctx, c, f[0], f[1]); err != nil || !save.Published {
			t.Fatalf("%s: %+v %v", f[0], save, err)
		}
	}
	tags, err := st.CreateCategoryFrom(CategoryInput{Name: "Tags"})
	if err != nil {
		t.Fatal(err)
	}
	if s := saveIcon(t, st, c, tags.ID, "tag", tagSrc); !s.Published {
		t.Fatalf("%+v", s.Errors)
	}
	notes, err := st.CreateField("note", "Note", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	height, err := st.CreateField("height", "Height", "number", nil)
	if err != nil {
		t.Fatal(err)
	}
	return textWorld{st: st, c: c, tags: tags.ID, notes: notes.ID, height: height.ID}
}

func (w textWorld) bind(t *testing.T, bindings map[string]string) {
	t.Helper()
	if _, save, err := w.st.SetIconParamBindings(context.Background(), w.c, w.tags, bindings); err != nil || save == nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
}

func (w textWorld) tag(t *testing.T, name, code string, values map[string]string) string {
	t.Helper()
	var keys []models.AssetGeneratorKey
	if code != "" {
		keys = []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: code}}
	}
	a, err := w.st.CreateAsset(name, []string{w.tags}, keys, values)
	if err != nil {
		t.Fatal(err)
	}
	return a.Asset.ID
}

// alone is the icon as compiled alone from the build's files with these
// texts: what an asset with them must be drawn as.
func (w textWorld) alone(t *testing.T, name string, texts map[string]any) string {
	t.Helper()
	_, files, err := w.st.buildFiles()
	if err != nil || files == nil {
		t.Fatalf("the build keeps no files: %v", err)
	}
	r, err := w.c.CompileOne(context.Background(), files, name, texts)
	if err != nil || !r.OK {
		t.Fatalf("%+v %v", r, err)
	}
	return string(iconOf(isolex.One{Result: r}, name))
}

// An asset's name, first code and fields are written on its icon, as
// compiling it alone with them draws it; with no value, a param keeps its
// default. One in a state is drawn with its texts with and without it.
func TestAssetsTextsAreDrawnOnTheirIcons(t *testing.T) {
	w := newTextWorld(t)
	plain := w.tag(t, "AB", "A1", nil)
	if ic := resolved(t, w.st, plain); ic.Texts != nil || ic.Drawn != nil {
		t.Fatalf("nothing bound: %+v", ic)
	}

	w.bind(t, map[string]string{"label": TextFromName, "note": w.notes})
	ic := resolved(t, w.st, plain)
	if len(ic.Texts) != 1 || ic.Texts["label"] != "AB" {
		t.Fatalf("texts: %+v", ic.Texts)
	}
	if got, want := string(ic.Drawn["tag"]), w.alone(t, "tag", map[string]any{"label": "AB"}); got != want || len(ic.Drawn) != 1 {
		t.Errorf("drawn %s\nwant %s", got, want)
	}

	// a field's value, and the code in place of the name
	noted := w.tag(t, "BA", "", map[string]string{"note": " 1 "})
	ic = resolved(t, w.st, noted)
	if ic.Texts["label"] != "BA" || ic.Texts["note"] != "1" {
		t.Fatalf("texts: %+v", ic.Texts)
	}
	if got, want := string(ic.Drawn["tag"]), w.alone(t, "tag", map[string]any{"label": "BA", "note": "1"}); got != want {
		t.Errorf("drawn %s\nwant %s", got, want)
	}
	w.bind(t, map[string]string{"label": TextFromCode})
	if ic := resolved(t, w.st, plain); ic.Texts["label"] != "A1" || ic.Drawn["tag"] == nil {
		t.Errorf("by code: %+v", ic)
	}
	if ic := resolved(t, w.st, noted); ic.Texts != nil || ic.Drawn != nil {
		t.Errorf("no code: %+v", ic)
	}

	// in a state: drawn as its variant, and plain for labels that leave it out
	w.bind(t, map[string]string{"label": TextFromName})
	if _, _, err := w.st.SetAssetCondition(plain, models.ConditionOutOfService, ""); err != nil {
		t.Fatal(err)
	}
	ic = resolved(t, w.st, plain)
	if ic.Name != "tag-down" || ic.Shape() != "tag" {
		t.Fatalf("%+v", ic)
	}
	if got, want := string(ic.Drawn["tag-down"]), w.alone(t, "tag-down", map[string]any{"label": "AB"}); got != want {
		t.Errorf("drawn down %s\nwant %s", got, want)
	}
	if got, want := string(ic.Drawn["tag"]), w.alone(t, "tag", map[string]any{"label": "AB"}); got != want {
		t.Errorf("drawn plain %s\nwant %s", got, want)
	}

	// with a number param bound too: drawn as its wrapper, with its texts
	w.bind(t, map[string]string{"label": TextFromName, "u": w.height})
	tall := w.tag(t, "B1", "", map[string]string{"height": "2"})
	if save, err := w.st.RebuildIcons(context.Background(), w.c); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	ic = resolved(t, w.st, tall)
	if ic.Name != "bam--tag--u_2" || ic.Texts["label"] != "B1" {
		t.Fatalf("%+v", ic)
	}
	if got, want := string(ic.Drawn[ic.Name]), w.alone(t, ic.Name, map[string]any{"label": "B1"}); got != want {
		t.Errorf("drawn wrapper %s\nwant %s", got, want)
	}
}

// counter counts the compiles drawing texts, and the icons each asks for.
type counter struct {
	c      *isolex.Compiler
	mu     sync.Mutex
	calls  int
	icons  int
	refuse bool
}

func (c *counter) CompileMany(ctx context.Context, files []isolex.File, requests []isolex.Request) (*isolex.Many, error) {
	c.mu.Lock()
	c.calls++
	c.icons += len(requests)
	c.mu.Unlock()
	if c.refuse {
		return nil, errors.New("the compiler stopped")
	}
	return c.c.CompileMany(ctx, files, requests)
}

func (c *counter) seen() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls, c.icons
}

// A list's icons are drawn in one compile, those with the same texts once;
// they are kept, by the build, so reading them again compiles nothing,
// until the build changes. A compiler that fails leaves them drawn as the
// build draws them.
func TestAListsTextsAreDrawnTogetherAndKept(t *testing.T) {
	w := newTextWorld(t)
	w.bind(t, map[string]string{"label": TextFromName})
	// made without drawing, as an import makes them
	w.st.DrawIconTextsWith(&counter{c: w.c, refuse: true})
	for _, name := range []string{"A", "B", "AB", "AB"} {
		w.tag(t, name, "", nil)
	}
	n := &counter{c: w.c}
	w.st.DrawIconTextsWith(n)

	list, err := w.st.ListAssets(w.tags, "")
	if err != nil || len(list) != 4 {
		t.Fatalf("%d %v", len(list), err)
	}
	for _, a := range list {
		if a.ResolvedIcon.Drawn["tag"] == nil {
			t.Errorf("%s: not drawn", a.Asset.Name)
		}
	}
	if calls, icons := n.seen(); calls != 1 || icons != 3 {
		t.Errorf("first read: %d compiles of %d icons; want 1 of 3", calls, icons)
	}
	if _, err := w.st.ListAssets(w.tags, ""); err != nil {
		t.Fatal(err)
	}
	if calls, _ := n.seen(); calls != 1 {
		t.Errorf("read again: %d compiles", calls)
	}
	// the same, through a view of the store
	if _, err := w.st.As("someone").AssetsByIDs([]string{list[0].Asset.ID}); err != nil {
		t.Fatal(err)
	}
	if calls, _ := n.seen(); calls != 1 {
		t.Errorf("through a view: %d compiles", calls)
	}

	// a new build draws them again
	if s := saveIcon(t, w.st, w.c, w.tags, "tag", strings.Replace(tagSrc, "size=8", "size=7", 1)); !s.Published {
		t.Fatalf("%+v", s.Errors)
	}
	if _, err := w.st.ListAssets(w.tags, ""); err != nil {
		t.Fatal(err)
	}
	if calls, icons := n.seen(); calls != 2 || icons != 6 {
		t.Errorf("after a build: %d compiles of %d icons", calls, icons)
	}

	// a failing compiler: drawn as the build draws them, and tried again
	w.st.DrawIconTextsWith(&counter{c: w.c, refuse: true})
	if s := saveIcon(t, w.st, w.c, w.tags, "tag", strings.Replace(tagSrc, "size=8", "size=6", 1)); !s.Published {
		t.Fatalf("%+v", s.Errors)
	}
	a, err := w.st.GetAssetByID(list[0].Asset.ID)
	if err != nil || a.ResolvedIcon.Texts == nil || a.ResolvedIcon.Drawn != nil {
		t.Fatalf("%+v %v", a.ResolvedIcon, err)
	}
	w.st.DrawIconTextsWith(nil)
	if ic := resolved(t, w.st, list[0].Asset.ID); ic.Drawn["tag"] == nil {
		t.Errorf("tried again: %+v", ic)
	}
}

// A build made before builds kept their files draws no texts, and is made
// again, with them.
func TestABuildWithoutItsFilesIsMadeAgain(t *testing.T) {
	w := newTextWorld(t)
	w.bind(t, map[string]string{"label": TextFromName})
	id := w.tag(t, "AB", "", nil)
	if _, err := w.st.Exec(`UPDATE icon_build SET inputs = NULL`); err != nil {
		t.Fatal(err)
	}
	// as a server upgraded from a bam before them starts: nothing kept
	w.st.texts = newIconTexts()
	if ic := resolved(t, w.st, id); ic.Texts["label"] != "AB" || ic.Drawn != nil {
		t.Fatalf("%+v", ic)
	}
	if save, err := w.st.RebuildIcons(context.Background(), w.c); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	var files string
	if err := w.st.QueryRow(`SELECT inputs FROM icon_build`).Scan(&files); err != nil || !json.Valid([]byte(files)) {
		t.Fatalf("%q %v", files, err)
	}
	if ic := resolved(t, w.st, id); ic.Drawn["tag"] == nil {
		t.Errorf("%+v", ic)
	}
}

// A binding is held to the icon: a param it has, a text param to a text,
// a number param to a number field; and is listed with where it comes
// from. Deleting the field drops it.
func TestTextBindingsAreHeldToTheIcon(t *testing.T) {
	w := newTextWorld(t)
	ctx := context.Background()
	for _, tc := range []struct {
		bindings map[string]string
		want     string
	}{
		{map[string]string{"label": w.height}, "param 'label' of icon 'tag' is text; bind it to the asset's name, a code or a field that is not a number"},
		{map[string]string{"u": TextFromName}, "param 'u' of icon 'tag' is a number; bind it to a number field"},
		{map[string]string{"labl": TextFromName}, "icon 'tag' has no param 'labl' (did you mean 'label'?)"},
		{map[string]string{"label": "nope"}, `there is no field "nope"`},
	} {
		_, _, err := w.st.SetIconParamBindings(ctx, w.c, w.tags, tc.bindings)
		if !errors.Is(err, ErrInvalidIconParams) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: %v", tc.bindings, err)
		}
	}
	w.bind(t, map[string]string{"label": TextFromCode, "note": w.notes, "u": w.height})
	got, err := w.st.IconParamBindings(w.tags)
	if err != nil {
		t.Fatal(err)
	}
	want := []models.IconParamBinding{
		{Param: "label", Source: TextFromCode},
		{Param: "note", Source: TextFromField, FieldID: w.notes, FieldKey: "note", FieldLabel: "Note"},
		{Param: "u", Source: TextFromField, FieldID: w.height, FieldKey: "height", FieldLabel: "Height"},
	}
	if g, _ := json.Marshal(got); string(g) != mustJSON(t, want) {
		t.Errorf("%s\nwant %s", g, mustJSON(t, want))
	}
	if err := w.st.DeleteField(w.notes); err != nil {
		t.Fatal(err)
	}
	if got, _ := w.st.IconParamBindings(w.tags); len(got) != 2 {
		t.Errorf("after deleting the field: %+v", got)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
