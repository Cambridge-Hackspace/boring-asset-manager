// Package isolex runs the isolex compiler (README §1, "Icon compiler"):
// icon sources in, shape IR and located diagnostics out. isolex is the
// work of Axonibyte Innovations, LLC, under the Apache License 2.0 (see
// NOTICE and LICENSE-isolex in this directory). The compiler is isolex's
// own, built to WebAssembly and fetched at build time against the
// checksum in wasm.lock; it runs in-process under wazero, which needs no
// cgo, with nothing granted to it (the module imports nothing), a memory
// cap and a deadline. Every compile gets a fresh instance, so
// compiles share nothing and may run concurrently.
//
// A binary built without the module still runs: Available reports false,
// and Compile returns ErrUnavailable.
package isolex

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/ir"
)

//go:embed wasm.lock
var lockFile string

//go:embed wasm
var moduleDir embed.FS

// ErrUnavailable means this binary was built without the compiler, or
// with one that does not match wasm.lock.
var ErrUnavailable = errors.New("the icon compiler is not built into this binary (tools/isolex/fetch.sh)")

// ErrRequest is CompileOne asked for what the lexicon does not have: an
// icon it does not emit, or a param the icon does not have or of another
// type.
var ErrRequest = errors.New("isolex: the lexicon cannot answer this")

// Lock is what wasm.lock pins.
type Lock struct {
	Version string
	URL     string
	SHA256  string
}

// Pinned returns the release wasm.lock pins.
func Pinned() Lock {
	l, err := parseLock(lockFile)
	if err != nil {
		panic(err)
	}
	return l
}

func parseLock(text string) (Lock, error) {
	var l Lock
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, _ := strings.Cut(line, " ")
		value = strings.TrimSpace(value)
		switch key {
		case "version":
			l.Version = value
		case "url":
			l.URL = value
		case "sha256":
			l.SHA256 = value
		default:
			return l, fmt.Errorf("wasm.lock: unknown key %q", key)
		}
	}
	if l.Version == "" || l.URL == "" || len(l.SHA256) != 64 {
		return l, errors.New("wasm.lock: needs version, url and a 64-digit sha256")
	}
	return l, nil
}

// File is one source file, named as diagnostics should name it.
type File struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// Diag is an error or warning at a place in a file.
type Diag struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Message string `json:"message"`
}

// Result is what a compile came to. IR and Doc are set only when OK, as
// the compiler emits nothing for a lexicon with errors.
type Result struct {
	OK       bool
	Errors   []Diag
	Warnings []Diag
	// IR is the document as the compiler wrote it, byte for byte.
	IR  json.RawMessage
	Doc *ir.Document
	// Palette is the lexicon's `palette` block (isolex SPEC §2), by token,
	// whether it compiled or not; nil without one.
	Palette map[string]Colors
}

// Colors are a token's colors in a `palette` block: on a light ground and
// on a dark one.
type Colors struct {
	Light string `json:"light"`
	Dark  string `json:"dark"`
}

// Options bound a compile. Zero values take the defaults.
type Options struct {
	// Timeout for one compile; default 10s.
	Timeout time.Duration
	// MemoryLimitPages caps one compile's memory, in 64 KiB pages;
	// default 4096 (256 MiB).
	MemoryLimitPages uint32
}

// Compiler compiles lexicons. The module is compiled to machine code
// once, on first use.
type Compiler struct {
	opts   Options
	module []byte

	once     sync.Once
	runtime  wazero.Runtime
	compiled wazero.CompiledModule
	version  string
	err      error
}

var (
	defaultCompiler     *Compiler
	defaultCompilerOnce sync.Once
)

// Default returns the process's compiler, with the default options:
// one, so the module is compiled to machine code once however many
// servers or tests use it.
func Default() *Compiler {
	defaultCompilerOnce.Do(func() { defaultCompiler = New(Options{}) })
	return defaultCompiler
}

// New returns a compiler for the module built into this binary.
func New(opts Options) *Compiler {
	module, err := fs.ReadFile(moduleDir, "wasm/isolex.wasm")
	if err != nil {
		module = nil
	}
	return newCompiler(module, Pinned(), opts)
}

func newCompiler(module []byte, lock Lock, opts Options) *Compiler {
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.MemoryLimitPages == 0 {
		opts.MemoryLimitPages = 4096
	}
	c := &Compiler{opts: opts}
	if module == nil {
		c.err = ErrUnavailable
		return c
	}
	sum := sha256.Sum256(module)
	if got := hex.EncodeToString(sum[:]); got != lock.SHA256 {
		c.err = fmt.Errorf("%w: its SHA-256 is %s, but wasm.lock pins %s", ErrUnavailable, got, lock.SHA256)
		return c
	}
	c.module = module
	return c
}

// Available reports whether this binary can compile icons.
func (c *Compiler) Available() bool { return c.err == nil }

func (c *Compiler) init() error {
	c.once.Do(func() {
		if c.err != nil {
			return
		}
		ctx := context.Background()
		cfg := wazero.NewRuntimeConfig().
			WithCloseOnContextDone(true).
			WithMemoryLimitPages(c.opts.MemoryLimitPages)
		c.runtime = wazero.NewRuntimeWithConfig(ctx, cfg)
		c.compiled, c.err = c.runtime.CompileModule(ctx, c.module)
		if c.err != nil {
			c.err = fmt.Errorf("compiling the icon compiler: %w", c.err)
			return
		}
		c.version, c.err = c.callVersion(ctx)
	})
	return c.err
}

// Version is the isolex version the module reports.
func (c *Compiler) Version() (string, error) {
	if err := c.init(); err != nil {
		return "", err
	}
	return c.version, nil
}

// Close releases the compiled module.
func (c *Compiler) Close(ctx context.Context) error {
	if c.runtime == nil {
		return nil
	}
	return c.runtime.Close(ctx)
}

func (c *Compiler) instantiate(ctx context.Context) (api.Module, error) {
	return c.runtime.InstantiateModule(ctx, c.compiled, wazero.NewModuleConfig().WithName(""))
}

func (c *Compiler) callVersion(ctx context.Context) (string, error) {
	mod, err := c.instantiate(ctx)
	if err != nil {
		return "", err
	}
	defer mod.Close(ctx)
	res, err := mod.ExportedFunction("isolex_version").Call(ctx)
	if err != nil {
		return "", err
	}
	b, err := read(mod, res[0])
	return string(b), err
}

// Compile compiles files, in the order given. An error means the
// compiler failed, ran out of time or memory, or is unavailable; a
// lexicon with mistakes in it is a Result with Errors, not an error.
func (c *Compiler) Compile(ctx context.Context, files []File) (*Result, error) {
	out, err := c.call(ctx, struct {
		Files []File `json:"files"`
	}{orNone(files)})
	if err != nil {
		return nil, err
	}
	return decode(out, false)
}

// CompileOne compiles one icon the lexicon emits, a variant's name
// included, with params set on it as a child's values would be: each a
// float64 for a number param or a string for a text one. Only that icon
// is drawn, and those it badges; the IR holds it alone, and the same
// inputs always give the same IR. A request the lexicon cannot answer is
// ErrRequest; a lexicon with errors of its own compiles to them, as
// Compile does.
func (c *Compiler) CompileOne(ctx context.Context, files []File, icon string, params map[string]any) (*Result, error) {
	if icon == "" {
		return nil, fmt.Errorf("%w: no icon named", ErrRequest)
	}
	if params == nil {
		params = map[string]any{}
	}
	out, err := c.call(ctx, struct {
		Files  []File         `json:"files"`
		Icon   string         `json:"icon"`
		Params map[string]any `json:"params"`
	}{orNone(files), icon, params})
	if err != nil {
		return nil, err
	}
	return decode(out, true)
}

// Request is one icon of CompileMany's, as CompileOne names it and its
// params.
type Request struct {
	Icon   string         `json:"icon"`
	Params map[string]any `json:"params,omitempty"`
}

// Many is what compiling many icons came to: the lexicon's own outcome,
// and when it compiles, a result for each request.
type Many struct {
	// OK, Errors and Warnings are the lexicon's parsing and resolving; a
	// request's results have no warnings of their own.
	OK       bool
	Errors   []Diag
	Warnings []Diag
	// Palette is the lexicon's `palette` block, as Result's.
	Palette map[string]Colors
	// Results are the requests', in order, when OK: each what CompileOne
	// gives for it, or Refused.
	Results []One
}

// One is a request's result: its Result, or why the lexicon cannot
// answer it.
type One struct {
	*Result
	Refused string
}

// CompileMany compiles icons of a lexicon, each with its params, from one
// parse: each as CompileOne would, but the lexicon is parsed and resolved
// once for all of them, which is most of the work when it holds a font.
func (c *Compiler) CompileMany(ctx context.Context, files []File, requests []Request) (*Many, error) {
	for _, r := range requests {
		if r.Icon == "" {
			return nil, fmt.Errorf("%w: no icon named", ErrRequest)
		}
	}
	if requests == nil {
		requests = []Request{}
	}
	out, err := c.call(ctx, struct {
		Files    []File    `json:"files"`
		Requests []Request `json:"requests"`
	}{orNone(files), requests})
	if err != nil {
		return nil, err
	}
	var o struct {
		OK         bool              `json:"ok"`
		InputError string            `json:"input_error"`
		Errors     []Diag            `json:"errors"`
		Warnings   []Diag            `json:"warnings"`
		Results    []json.RawMessage `json:"results"`
		Palette    map[string]Colors `json:"palette"`
	}
	if err := json.Unmarshal(out, &o); err != nil {
		return nil, fmt.Errorf("isolex: unreadable output from the compiler: %w", err)
	}
	if o.InputError != "" {
		return nil, fmt.Errorf("isolex: the compiler could not read its input: %s", o.InputError)
	}
	m := &Many{OK: o.OK, Errors: o.Errors, Warnings: o.Warnings, Palette: o.Palette}
	if m.Errors == nil {
		m.Errors = []Diag{}
	}
	if m.Warnings == nil {
		m.Warnings = []Diag{}
	}
	if !m.OK {
		return m, nil
	}
	if len(o.Results) != len(requests) {
		return nil, fmt.Errorf("isolex: the compiler answered %d of %d requests; is it older than 0.5.0?", len(o.Results), len(requests))
	}
	m.Results = make([]One, len(o.Results))
	for i, raw := range o.Results {
		r, err := decode(raw, true)
		if errors.Is(err, ErrRequest) {
			m.Results[i].Refused = strings.TrimPrefix(err.Error(), ErrRequest.Error()+": ")
			continue
		}
		if err != nil {
			return nil, err
		}
		m.Results[i].Result = r
	}
	return m, nil
}

// orNone is files, or none rather than null.
func orNone(files []File) []File {
	if files == nil {
		return []File{}
	}
	return files
}

// call runs the compiler on input, as JSON, and returns its output.
func (c *Compiler) call(ctx context.Context, in any) ([]byte, error) {
	if err := c.init(); err != nil {
		return nil, err
	}
	input, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, c.opts.Timeout)
	defer cancel()
	mod, err := c.instantiate(ctx)
	if err != nil {
		return nil, c.failure(ctx, err)
	}
	// Closing the instance frees all of its memory, so nothing the
	// compiler allocated needs freeing one buffer at a time.
	defer mod.Close(context.Background())

	res, err := mod.ExportedFunction("isolex_alloc").Call(ctx, uint64(len(input)))
	if err != nil {
		return nil, c.failure(ctx, err)
	}
	ptr := uint32(res[0])
	if !mod.Memory().Write(ptr, input) {
		return nil, errors.New("isolex: the compiler's buffer is out of range")
	}
	res, err = mod.ExportedFunction("isolex_compile").Call(ctx, uint64(ptr), uint64(len(input)))
	if err != nil {
		return nil, c.failure(ctx, err)
	}
	return read(mod, res[0])
}

// failure names why a compile stopped: the deadline, or the compiler
// trapping (out of memory, or a bug in it).
func (c *Compiler) failure(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("isolex: the compile took longer than %s and was stopped", c.opts.Timeout)
	}
	return fmt.Errorf("isolex: the compiler stopped (it may have needed more than %d MiB): %w",
		c.opts.MemoryLimitPages/16, err)
}

// read copies out a buffer the module returned as ptr<<32 | len.
func read(mod api.Module, packed uint64) ([]byte, error) {
	ptr, n := uint32(packed>>32), uint32(packed)
	b, ok := mod.Memory().Read(ptr, n)
	if !ok {
		return nil, errors.New("isolex: the compiler returned a buffer out of range")
	}
	return bytes.Clone(b), nil
}

// decode reads the compiler's output. An input error answers a request
// for one icon (one) the lexicon cannot; otherwise it is bam's mistake.
func decode(out []byte, one bool) (*Result, error) {
	var o struct {
		OK         bool              `json:"ok"`
		InputError string            `json:"input_error"`
		Errors     []Diag            `json:"errors"`
		Warnings   []Diag            `json:"warnings"`
		IR         json.RawMessage   `json:"ir"`
		Palette    map[string]Colors `json:"palette"`
	}
	if err := json.Unmarshal(out, &o); err != nil {
		return nil, fmt.Errorf("isolex: unreadable output from the compiler: %w", err)
	}
	if o.InputError != "" && one {
		return nil, fmt.Errorf("%w: %s", ErrRequest, o.InputError)
	}
	if o.InputError != "" {
		return nil, fmt.Errorf("isolex: the compiler could not read its input: %s", o.InputError)
	}
	r := &Result{OK: o.OK, Errors: o.Errors, Warnings: o.Warnings, Palette: o.Palette}
	if r.Errors == nil {
		r.Errors = []Diag{}
	}
	if r.Warnings == nil {
		r.Warnings = []Diag{}
	}
	if o.OK {
		doc, err := ir.Parse(o.IR)
		if err != nil {
			return nil, err
		}
		r.IR, r.Doc = o.IR, doc
	}
	return r, nil
}
