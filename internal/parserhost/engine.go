package parserhost

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// APIVersion is the script API revision this host understands. A script
// declaring anything else is a load error rather than a silent mismatch.
const APIVersion = 1

const (
	DefaultTimeout      = 30 * time.Second
	DefaultTOCTimeout   = 120 * time.Second
	DefaultMaxFetches   = 200
	DefaultMaxBodyBytes = 5 << 20
)

// DefaultTOCTimeout covers the slowest legitimate TOC: one Livewire-aware
// browser-worker fetch (tab + retries ≈ 60–90s) or, when that is missing, the
// sequential walk fallback. ponytail: the walk still dies at this ceiling on
// huge catalogs (~8-12 chapters at the download throttle); the real fix is
// keeping each site's catalog payload fetch healthy.

// Options bounds a single script invocation. Zero-valued fields fall back to
// the defaults, so only the limits a test cares about need to be set.
// Timeout caps every entry point; TOCTimeout overrides it for toc() alone and
// defaults to DefaultTOCTimeout when Timeout is left unset.
type Options struct {
	Timeout      time.Duration
	TOCTimeout   time.Duration
	MaxFetches   int
	MaxBodyBytes int64
}

// Engine compiles parser scripts and runs them against a host-supplied
// Fetcher. It holds no per-job state.
type Engine struct {
	fetcher Fetcher
	opts    Options
}

func NewEngine(f Fetcher, opts Options) *Engine {
	explicitTimeout := opts.Timeout > 0
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.TOCTimeout <= 0 {
		// The caller pinned every invocation to a custom Timeout, so toc()
		// follows it too; otherwise toc() gets the long catalog default.
		if explicitTimeout {
			opts.TOCTimeout = opts.Timeout
		} else {
			opts.TOCTimeout = DefaultTOCTimeout
		}
	}
	if opts.MaxFetches <= 0 {
		opts.MaxFetches = DefaultMaxFetches
	}
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = DefaultMaxBodyBytes
	}
	return &Engine{fetcher: f, opts: opts}
}

// Script is a compiled parser. It is immutable after loading and safe for
// concurrent use: every invocation runs in its own VM.
type Script struct {
	name            string
	requiresBrowser bool
	livewirePattern *regexp.Regexp
	hasChapterKey   bool
	prog            *goja.Program
	engine          *Engine
}

func (s *Script) Name() string { return s.name }

// RequiresBrowser reports whether fetching from this site only works reliably
// through the browser worker proxy. Documentation-only: it never changes fetch
// behavior inside the engine.
func (s *Script) RequiresBrowser() bool { return s.requiresBrowser }

// LivewireCatalogPattern is the compiled livewireCatalogPattern the script
// declared, or nil when it did not. The fetcher consults it only on the
// browser-worker path.
func (s *Script) LivewireCatalogPattern() *regexp.Regexp { return s.livewirePattern }

func (e *Engine) LoadFile(path string) (*Script, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading parser script: %w", err)
	}
	prog, err := goja.Compile(path, string(src), true)
	if err != nil {
		return nil, fmt.Errorf("compiling parser script: %w", err)
	}
	s := &Script{prog: prog, engine: e}
	inv := s.begin(context.Background(), 0)
	defer inv.finish()
	if err := inv.init(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, inv.wrap(err))
	}
	name, ok := inv.exportProp("name").Export().(string)
	if !ok || strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("%s: %w", path, shapeError("module.exports.name must be a non-empty string"))
	}
	s.name = name

	version := inv.exportProp("apiVersion")
	if v, isNumber := version.Export().(int64); !isNumber || v != APIVersion {
		return nil, fmt.Errorf("%s: %w", path, shapeError("module.exports.apiVersion must be %d, got %s", APIVersion, version.String()))
	}
	if b, ok := inv.exportProp("requiresBrowser").Export().(bool); ok {
		s.requiresBrowser = b
	}
	if raw, ok := inv.exportProp("livewireCatalogPattern").Export().(string); ok && strings.TrimSpace(raw) != "" {
		re, reErr := regexp.Compile(raw)
		if reErr != nil {
			return nil, fmt.Errorf("%s: %w", path, shapeError("module.exports.livewireCatalogPattern is not a valid regular expression: %v", reErr))
		}
		s.livewirePattern = re
	}
	for _, fn := range []string{"probe", "toc", "chapter"} {
		if _, ok := goja.AssertFunction(inv.exportProp(fn)); !ok {
			return nil, fmt.Errorf("%s: %w", path, shapeError("module.exports.%s must be a function", fn))
		}
	}
	_, s.hasChapterKey = goja.AssertFunction(inv.exportProp("chapterKey"))
	return s, nil
}

// LoadDir compiles every *.js file in dir. There is no cache: compile is
// ms-cheap and callers load per job, which is what makes script edits take
// effect on the next job without a restart.
func (e *Engine) LoadDir(dir string) ([]*Script, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading parser dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".js") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	scripts := make([]*Script, 0, len(names))
	for _, name := range names {
		s, err := e.LoadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		scripts = append(scripts, s)
	}
	return scripts, nil
}

func (s *Script) Probe(rawURL string) (bool, error) {
	inv := s.begin(context.Background(), 0)
	defer inv.finish()
	if err := inv.init(); err != nil {
		return false, inv.wrap(err)
	}
	v, err := inv.call("probe", inv.vm.ToValue(rawURL))
	if err != nil {
		return false, inv.wrap(err)
	}
	ok, isBool := v.Export().(bool)
	if !isBool {
		return false, shapeError("probe must return a boolean, got %s", v.String())
	}
	return ok, nil
}

func (s *Script) TOC(ctx context.Context, rawURL string) (*TOC, error) {
	inv := s.begin(ctx, s.engine.opts.TOCTimeout)
	defer inv.finish()
	if err := inv.init(); err != nil {
		return nil, inv.wrap(err)
	}
	v, err := inv.call("toc", inv.ctxObject(), inv.vm.ToValue(rawURL))
	if err != nil {
		return nil, inv.wrap(err)
	}
	return tocFromValue(v)
}

func (s *Script) Chapter(ctx context.Context, rawURL string) (*Chapter, error) {
	inv := s.begin(ctx, 0)
	defer inv.finish()
	if err := inv.init(); err != nil {
		return nil, inv.wrap(err)
	}
	v, err := inv.call("chapter", inv.ctxObject(), inv.vm.ToValue(rawURL))
	if err != nil {
		return nil, inv.wrap(err)
	}
	return chapterFromValue(v)
}

// ChapterKey returns the script's stable identity for a chapter, defaulting to
// the chapter URL when the script does not export chapterKey.
func (s *Script) ChapterKey(ref ChapterRef) (string, error) {
	if !s.hasChapterKey {
		return ref.URL, nil
	}
	inv := s.begin(context.Background(), 0)
	defer inv.finish()
	if err := inv.init(); err != nil {
		return "", inv.wrap(err)
	}
	v, err := inv.call("chapterKey", inv.vm.ToValue(map[string]any{"title": ref.Title, "url": ref.URL}))
	if err != nil {
		return "", inv.wrap(err)
	}
	key, ok := v.Export().(string)
	if !ok || key == "" {
		return "", shapeError("chapterKey must return a non-empty string, got %s", v.String())
	}
	return key, nil
}

// interruptMarker distinguishes the deadline interrupt from a context
// cancellation interrupt, both of which surface as *goja.InterruptedError.
type interruptMarker string

const (
	markerTimeout  interruptMarker = "timeout"
	markerCanceled interruptMarker = "canceled"
)

// invocation owns one VM and the limits that apply to it. goja runtimes are not
// goroutine-safe and module state must not leak between jobs, so every entry
// point builds a fresh one.
type invocation struct {
	script  *Script
	ctx     context.Context
	vm      *goja.Runtime
	exports *goja.Object
	ctxObj  *goja.Object
	nodes   map[*goja.Object]*node
	fetches int
	timeout time.Duration
	timer   *time.Timer
	stopCtx func() bool
}

func (s *Script) begin(ctx context.Context, timeout time.Duration) *invocation {
	if timeout <= 0 {
		timeout = s.engine.opts.Timeout
	}
	inv := &invocation{script: s, ctx: ctx, timeout: timeout, vm: s.engine.newVM(), nodes: map[*goja.Object]*node{}}
	inv.timer = time.AfterFunc(timeout, func() { inv.vm.Interrupt(markerTimeout) })
	inv.stopCtx = context.AfterFunc(ctx, func() { inv.vm.Interrupt(markerCanceled) })
	return inv
}

// finish stops the deadline and cancellation watchdogs. Both only touch this
// invocation's VM, so a timer that fires late after the call returned is
// harmless: that VM is discarded.
func (inv *invocation) finish() {
	inv.timer.Stop()
	inv.stopCtx()
}

// parserMaxCallStack bounds JavaScript recursion per VM. goja's default cap
// is math.MaxInt32, so runaway recursion would grow a heap-resident call
// stack until the OOM killer takes the whole single-binary server down; the
// wall-clock interrupt fires far too late to prevent that. 1024 frames is
// orders of magnitude above any legitimate parser depth.
const parserMaxCallStack = 1024

func (e *Engine) newVM() *goja.Runtime {
	vm := goja.New()
	vm.SetMaxCallStackSize(parserMaxCallStack)
	// js tags name the fields of the doc handle (status/url/body); uncapMethods
	// exposes node methods to scripts as text/html/attr/href/src/remove.
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("js", true))
	return vm
}

// init runs module.exports in the fresh VM and captures the export object.
func (inv *invocation) init() error {
	module := inv.vm.NewObject()
	inv.vm.Set("module", module)
	inv.vm.Set("exports", module)
	if _, err := inv.vm.RunProgram(inv.script.prog); err != nil {
		return err
	}
	exports, ok := module.Get("exports").(*goja.Object)
	if !ok {
		return shapeError("module.exports must be an object")
	}
	inv.exports = exports
	return nil
}

func (inv *invocation) call(entry string, args ...goja.Value) (goja.Value, error) {
	fn, ok := goja.AssertFunction(inv.exportProp(entry))
	if !ok {
		return nil, shapeError("module.exports.%s must be a function", entry)
	}
	return fn(goja.Undefined(), args...)
}

// exportProp reads a module.exports property. Object.Get returns a nil Value
// for a missing property, so it must not be used directly on optional fields.
func (inv *invocation) exportProp(name string) goja.Value {
	if value := inv.exports.Get(name); value != nil {
		return value
	}
	return inv.vm.ToValue(nil)
}

// wrap maps a goja error onto the taxonomy. Errors raised by host bindings
// (ctx.get) surface as Go errors through the exception and are returned
// unchanged, so a Fetcher failure stays a plain network error.
func (inv *invocation) wrap(err error) error {
	if err == nil {
		return nil
	}
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		if marker, ok := interrupted.Value().(interruptMarker); ok && marker == markerCanceled {
			if ctxErr := inv.ctx.Err(); ctxErr != nil {
				return ctxErr
			}
		}
		return &ScriptError{
			Code:    CodeParserTimeout,
			Message: fmt.Sprintf("script exceeded the %s limit", inv.timeout),
		}
	}
	var scriptErr *ScriptError
	if errors.As(err, &scriptErr) {
		return scriptErr
	}
	var exception *goja.Exception
	if errors.As(err, &exception) {
		if inner := exception.Unwrap(); inner != nil {
			return inner
		}
		return &ScriptError{Code: CodeScriptError, Message: err.Error(), Stack: exception.String()}
	}
	return &ScriptError{Code: CodeScriptError, Message: err.Error()}
}

func tocFromValue(v goja.Value) (*TOC, error) {
	root, ok := v.Export().(map[string]any)
	if !ok {
		return nil, shapeError("toc must return an object, got %s", v.String())
	}
	rawNovel, ok := root["novel"].(map[string]any)
	if !ok {
		return nil, shapeError("toc must return a novel object")
	}
	title, _ := rawNovel["title"].(string)
	if strings.TrimSpace(title) == "" {
		return nil, shapeError("toc novel.title is required")
	}
	novel := Novel{Title: title}
	novel.Description, _ = rawNovel["description"].(string)
	novel.Author, _ = rawNovel["author"].(string)
	novel.CoverURL, _ = rawNovel["coverUrl"].(string)
	novel.Language, _ = rawNovel["language"].(string)
	if tags, ok := rawNovel["tags"].([]any); ok {
		for _, tag := range tags {
			if s, ok := tag.(string); ok {
				novel.Tags = append(novel.Tags, s)
			}
		}
	}

	rawChapters, ok := root["chapters"].([]any)
	if !ok {
		return nil, shapeError("toc must return a chapters array")
	}
	chapters := make([]ChapterRef, 0, len(rawChapters))
	for i, raw := range rawChapters {
		chapter, ok := raw.(map[string]any)
		if !ok {
			return nil, shapeError("toc chapters[%d] must be an object", i)
		}
		chapterURL, ok := chapter["url"].(string)
		if !ok || strings.TrimSpace(chapterURL) == "" {
			return nil, shapeError("toc chapters[%d].url is required", i)
		}
		chapterTitle, ok := chapter["title"].(string)
		if !ok {
			return nil, shapeError("toc chapters[%d].title is required", i)
		}
		chapters = append(chapters, ChapterRef{Title: chapterTitle, URL: chapterURL})
	}
	return &TOC{Novel: novel, Chapters: chapters}, nil
}

func chapterFromValue(v goja.Value) (*Chapter, error) {
	root, ok := v.Export().(map[string]any)
	if !ok {
		return nil, shapeError("chapter must return an object, got %s", v.String())
	}
	content, ok := root["contentHtml"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return nil, shapeError("chapter must return a non-empty contentHtml")
	}
	title, _ := root["title"].(string)
	return &Chapter{Title: title, ContentHTML: content}, nil
}
