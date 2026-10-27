package isolex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/svg"
)

var (
	shared     *Compiler
	sharedOnce sync.Once
)

var update = flag.Bool("update", false, "rewrite unknown.ir.json from unknown.ilx")

// compiler is the module built into this test binary. Without one the
// tests that need it skip, unless BAM_REQUIRE_ISOLEX is set, as reaper
// and the release build set it: there a missing compiler fails.
func compiler(t *testing.T, opts Options) *Compiler {
	t.Helper()
	var c *Compiler
	if opts == (Options{}) {
		// one, compiled once, for every test that takes the defaults
		sharedOnce.Do(func() { shared = New(Options{}) })
		c = shared
	} else {
		c = New(opts)
		t.Cleanup(func() { _ = c.Close(context.Background()) })
	}
	if !c.Available() {
		if os.Getenv("BAM_REQUIRE_ISOLEX") != "" {
			t.Fatalf("BAM_REQUIRE_ISOLEX is set, but: %v", c.err)
		}
		t.Skipf("%v", c.err)
	}
	return c
}

func compile(t *testing.T, c *Compiler, files ...File) *Result {
	t.Helper()
	r, err := c.Compile(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTheLockPinsARelease(t *testing.T) {
	l := Pinned()
	if !strings.Contains(l.URL, "/"+l.Version+"/isolex-"+l.Version+".wasm") {
		t.Errorf("the URL %s is not release %s's module", l.URL, l.Version)
	}
	for _, bad := range []string{
		"version 1\nurl u\n",
		"version 1\nurl u\nsha256 abc\n",
		"version 1\nurl u\nsha256 " + strings.Repeat("0", 64) + "\nmirror m\n",
	} {
		if _, err := parseLock(bad); err == nil {
			t.Errorf("parseLock accepted %q", bad)
		}
	}
}

// A module that is not the pinned one is refused, however it got there,
// and so is none: the server then runs with icons off.
func TestOnlyThePinnedModuleRuns(t *testing.T) {
	for name, module := range map[string][]byte{"missing": nil, "another": []byte("\x00asm\x01\x00\x00\x00")} {
		c := newCompiler(module, Pinned(), Options{})
		if c.Available() {
			t.Fatalf("%s module: available", name)
		}
		_, err := c.Compile(context.Background(), nil)
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("%s module: %v", name, err)
		}
		if module != nil && !strings.Contains(err.Error(), "wasm.lock pins "+Pinned().SHA256) {
			t.Errorf("%s module: %v", name, err)
		}
	}
}

func TestTheModuleIsThePinnedVersion(t *testing.T) {
	c := compiler(t, Options{})
	v, err := c.Version()
	if err != nil {
		t.Fatal(err)
	}
	if v != Pinned().Version {
		t.Errorf("the module says %s; wasm.lock pins %s", v, Pinned().Version)
	}
}

// isolex's own fixtures compile here to isolex's own goldens, byte for
// byte: the module is the compiler, not an approximation of it. They are
// the pinned release's (testdata/isolex-0.5.0): its sample, and its
// transforms and text, in IR version 2.
func TestTheSampleCompilesToIsolexsGolden(t *testing.T) {
	c := compiler(t, Options{})
	var r *Result
	for _, fixture := range []string{"sample", "transforms", "text"} {
		got := compile(t, c, File{fixture + ".ilx", readFile(t, "testdata/isolex-0.5.0/"+fixture+".ilx")})
		if !got.OK || len(got.Errors) != 0 {
			t.Fatalf("%s: errors: %+v", fixture, got.Errors)
		}
		var want bytes.Buffer
		if err := json.Compact(&want, []byte(readFile(t, "testdata/isolex-0.5.0/"+fixture+".ir.golden.json"))); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.IR, want.Bytes()) {
			t.Errorf("%s: the IR differs from isolex's golden:\n got %.300s\nwant %.300s", fixture, got.IR, want.Bytes())
		}
		if got.Doc.IRVersion != 2 {
			t.Errorf("%s: IR version %d", fixture, got.Doc.IRVersion)
		}
		if fixture == "sample" {
			r = got
		}
	}
	if len(r.Doc.Icons) == 0 || r.Doc.Icon("switch-l3") == nil {
		t.Errorf("parsed %d icons, without switch-l3", len(r.Doc.Icons))
	}
	// the sample grants accent and reserves nothing, which isolex warns of
	if len(r.Warnings) != 1 || r.Warnings[0] != (Diag{File: "sample.ilx", Line: 82, Col: 3, Message: r.Warnings[0].Message}) {
		t.Errorf("warnings: %+v", r.Warnings)
	}
}

// One icon compiles alone with params set on it, as a child setting them
// would draw it; many compile from one parse, each as it would alone, and
// a request the lexicon cannot answer is refused on its own.
func TestOneIconAndManyFromOneParse(t *testing.T) {
	c := compiler(t, Options{})
	ctx := context.Background()
	src := readFile(t, "testdata/isolex-0.5.0/text.ilx")
	files := []File{{"text.ilx", src}}

	one, err := c.CompileOne(ctx, files, "port-label", map[string]any{"ports": 24.0})
	if err != nil || !one.OK || len(one.Doc.Icons) != 1 {
		t.Fatalf("%+v %v", one, err)
	}
	child := compile(t, c, File{"text.ilx", strings.Replace(src, "param ports 48", "param ports 24", 1)})
	got, _ := json.Marshal(one.Doc.Icons[0])
	want, _ := json.Marshal(child.Doc.Icon("port-label"))
	if string(got) != string(want) {
		t.Errorf("port-label with 24:\n got %.200s\nwant %.200s", got, want)
	}
	for _, icon := range []string{"nope", ""} {
		if _, err := c.CompileOne(ctx, files, icon, nil); !errors.Is(err, ErrRequest) {
			t.Errorf("%q: %v", icon, err)
		}
	}

	m, err := c.CompileMany(ctx, files, []Request{
		{Icon: "port-label", Params: map[string]any{"ports": 24.0}},
		{Icon: "tagged-a", Params: map[string]any{"tag": "X"}},
		{Icon: "nope"},
		{Icon: "port-label", Params: map[string]any{"ports": "24"}},
	})
	if err != nil || !m.OK || len(m.Results) != 4 {
		t.Fatalf("%+v %v", m, err)
	}
	if !bytes.Equal(m.Results[0].IR, one.IR) {
		t.Errorf("the first differs from it alone:\n got %.200s\nwant %.200s", m.Results[0].IR, one.IR)
	}
	alone, _ := c.CompileOne(ctx, files, "tagged-a", map[string]any{"tag": "X"})
	if m.Results[1].Result == nil || !bytes.Equal(m.Results[1].IR, alone.IR) {
		t.Errorf("the second differs from it alone")
	}
	if r := m.Results[2]; r.Result != nil || r.Refused != "the lexicon emits no icon 'nope'" {
		t.Errorf("an icon it does not emit: %+v", r)
	}
	if r := m.Results[3]; r.Result != nil || r.Refused != "param 'ports' of icon 'port-label' is a number; give it a number" {
		t.Errorf("a param of the other kind: %+v", r)
	}

	// a lexicon with errors: they, and no results; none asked, none given
	m, err = c.CompileMany(ctx, []File{{"a.ilx", "icon \"a\" {\n  patr \"p\" { }\n}\n"}}, []Request{{Icon: "a"}})
	if err != nil || m.OK || len(m.Errors) == 0 || m.Results != nil {
		t.Errorf("%+v %v", m, err)
	}
	if m, err := c.CompileMany(ctx, files, nil); err != nil || !m.OK || len(m.Results) != 0 {
		t.Errorf("none: %+v %v", m, err)
	}
	if _, err := c.CompileMany(ctx, files, []Request{{}}); !errors.Is(err, ErrRequest) {
		t.Errorf("no icon named: %v", err)
	}
}

func TestMistakesAreLocatedAndEmitNothing(t *testing.T) {
	c := compiler(t, Options{})
	r := compile(t, c,
		File{"a.ilx", "icon \"one\" { category \"c\"; canvas 9 9 }\n"},
		File{"b.ilx", "icon \"two\" {\n  patr \"p\" { }\n}\n"},
	)
	if r.OK || r.IR != nil || r.Doc != nil {
		t.Fatalf("a lexicon with errors emitted IR: %+v", r)
	}
	d := r.Errors[0]
	if d.File != "b.ilx" || d.Line != 2 || d.Col != 3 || !strings.Contains(d.Message, "did you mean 'part'?") {
		t.Errorf("first error: %+v", d)
	}
}

func TestCompilesShareNothing(t *testing.T) {
	c := compiler(t, Options{})
	src := File{"sample.ilx", readFile(t, "testdata/sample.ilx")}
	want := compile(t, c, src).IR
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			files := []File{src}
			if i%2 == 1 {
				// every other one fails, which must not touch the rest
				files = append(files, File{"bad.ilx", "icom \"x\" { }"})
			}
			r, err := c.Compile(context.Background(), files)
			switch {
			case err != nil:
				errs <- err
			case i%2 == 0 && !bytes.Equal(r.IR, want):
				errs <- errors.New("a concurrent compile gave different IR")
			case i%2 == 1 && r.OK:
				errs <- errors.New("a concurrent compile lost its error")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// A lexicon that switches its limits off can ask for more than any
// server has. The deadline and the memory cap stop it, and the compiler
// carries on.
const runaway = `lexicon { shape-limit #null; repeat-limit #null }
icon "big" { category "c"; footprint 1 1 1
  part "p" { repeat 100000 "i" { repeat 100000 "j" { tile 0 0 0 1 1 } } }
}
`

func TestTheDeadlineStopsARunawayCompile(t *testing.T) {
	c := compiler(t, Options{Timeout: 300 * time.Millisecond})
	// Compile the module to machine code first, which the deadline does
	// not bound and which takes seconds under -race on a slow machine, so
	// that only stopping the compile is timed.
	if _, err := c.Version(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := c.Compile(context.Background(), []File{{"big.ilx", runaway}})
	if err == nil || !strings.Contains(err.Error(), "took longer than 300ms") {
		t.Fatalf("err = %v", err)
	}
	d := time.Since(start)
	t.Logf("a 300ms deadline stopped the compile after %s", d)
	if d > 5*time.Second {
		t.Errorf("stopping took %s", d)
	}
	if r := compile(t, c, File{"sample.ilx", readFile(t, "testdata/sample.ilx")}); !r.OK {
		t.Errorf("the next compile failed: %+v", r.Errors)
	}
}

func TestTheMemoryCapStopsARunawayCompile(t *testing.T) {
	c := compiler(t, Options{MemoryLimitPages: 128, Timeout: time.Minute})
	_, err := c.Compile(context.Background(), []File{{"big.ilx", runaway}})
	if err == nil || !strings.Contains(err.Error(), "more than 8 MiB") {
		t.Fatalf("err = %v", err)
	}
	if r := compile(t, c, File{"sample.ilx", readFile(t, "testdata/sample.ilx")}); !r.OK {
		t.Errorf("the next compile failed: %+v", r.Errors)
	}
}

func TestLexiconBlock(t *testing.T) {
	block, err := LexiconBlock(Vocabulary{
		Tokens:       []string{"ink", "body-top"},
		StrokeWidths: []float64{1.2, 2},
		Reserved:     []string{"ink"},
		Limits:       map[string]int64{"shape-limit": 5, "icon-limit": 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "// Written by bam from the palette and the icon settings.\nlexicon {\n" +
		"    tokens \"ink\" \"body-top\"\n    stroke-widths 1.2 2\n    reserved \"ink\"\n" +
		"    icon-limit #null\n    shape-limit 5\n}\n"
	if block != want {
		t.Errorf("got\n%s\nwant\n%s", block, want)
	}
	if b, _ := LexiconBlock(Vocabulary{}); b != "" {
		t.Errorf("an empty vocabulary wrote %q", b)
	}
	for _, v := range []Vocabulary{
		{StrokeWidths: []float64{0}},
		{Limits: map[string]int64{"shape-limt": 5}},
		{Limits: map[string]int64{"shape-limit": -1}},
	} {
		if _, err := LexiconBlock(v); err == nil {
			t.Errorf("accepted %+v", v)
		}
	}
	if got := Quote("a\"b\\c\nd\x01"); got != `"a\"b\\c\nd\u{1}"` {
		t.Errorf("Quote: %s", got)
	}
}

// The block bam writes is one the compiler reads, and holds icons to.
func TestTheCompilerHoldsIconsToTheBlock(t *testing.T) {
	c := compiler(t, Options{})
	block, err := LexiconBlock(Vocabulary{
		Tokens: []string{"ink", "outline", "body-top", "body-left", "body-right"},
		Limits: map[string]int64{"shape-limit": 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	lex := File{LexiconFileName, block}
	ok := compile(t, c, lex, File{"a.ilx", `icon "a" { category "c"; footprint 1 1 1; part "p" { tile 0 0 0 1 1 } }`})
	if !ok.OK {
		t.Fatalf("errors: %+v", ok.Errors)
	}
	for _, tc := range []struct{ src, want string }{
		{`icon "a" { category "c"; footprint 1 1 1; part "p" { tile 0 0 0 1 1 fill="red" } }`, "'red'"},
		{`icon "a" { category "c"; footprint 1 1 1; part "p" { repeat 3 "i" { tile 0 0 0 1 1 } } }`, "shape-limit"},
	} {
		r := compile(t, c, lex, File{"a.ilx", tc.src})
		if r.OK || len(r.Errors) == 0 || !strings.Contains(r.Errors[0].Message, tc.want) || r.Errors[0].File != "a.ilx" {
			t.Errorf("%s: %+v", tc.src, r.Errors)
		}
	}
}

// unknown.ir.json is unknown.ilx compiled by the pinned module; -update
// rewrites it.
func TestUnknownIconIsCompiled(t *testing.T) {
	c := compiler(t, Options{})
	r := compile(t, c, File{"unknown.ilx", unknownSource})
	if !r.OK || len(r.Warnings) != 0 {
		t.Fatalf("errors %+v, warnings %+v", r.Errors, r.Warnings)
	}
	if *update {
		if err := os.WriteFile("unknown.ir.json", append(r.IR, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if !bytes.Equal(r.IR, bytes.TrimSpace(unknownIR)) {
		t.Errorf("unknown.ir.json is not unknown.ilx compiled; run go test ./internal/isolex/ -run TestUnknownIconIsCompiled -update")
	}
}

func TestTheUnknownIconNeedsNoCompiler(t *testing.T) {
	icon := Unknown()
	if icon.Name != UnknownName || icon.Footprint == nil || len(icon.Shapes) < 4 {
		t.Errorf("%+v", icon)
	}
}

// The whole chain: isolex's sample compiled by the module and drawn by
// package svg is isolex's own SVG for it.
func TestTheCompiledSampleDrawsAsIsolexDraws(t *testing.T) {
	c := compiler(t, Options{})
	r := compile(t, c, File{"sample.ilx", readFile(t, "testdata/isolex-0.5.0/sample.ilx")})
	want := readFile(t, "testdata/isolex-0.5.0/sample.svg.golden")
	for i := range r.Doc.Icons {
		icon := &r.Doc.Icons[i]
		got := "== " + icon.Name + ".svg ==\n" + svg.Icon(icon, svg.Palette(svg.IsolexPalette))
		if !strings.Contains(want, got) {
			t.Errorf("%s is not drawn as isolex draws it:\n%s", icon.Name, got)
		}
	}
}
