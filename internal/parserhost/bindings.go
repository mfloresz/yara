package parserhost

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/dop251/goja"
)

// doc is the handle returned by ctx.get. body is the raw response body: no HTML
// parsing is forced here, so JSON-driven sites can JSON.parse(doc.body) and
// cipher-table decryptions can work over the untouched string. Parsing happens
// lazily, only if a script asks for it with ctx.css.
type doc struct {
	Status int    `js:"status"`
	URL    string `js:"url"`
	Body   string `js:"body"`

	base   string
	parsed *goquery.Document
}

func (d *doc) selection() (*goquery.Selection, error) {
	if d.parsed == nil {
		parsed, err := goquery.NewDocumentFromReader(strings.NewReader(d.Body))
		if err != nil {
			return nil, fmt.Errorf("parsing document body: %w", err)
		}
		d.parsed = parsed
	}
	return d.parsed.Selection, nil
}

// node is the host-side record behind a ctx.css / ctx.css1 handle. base is the
// URL that href/src resolve against; it is empty for a bare HTML string, which
// has no origin to resolve against.
type node struct {
	sel  *goquery.Selection
	base string
}

func (n *node) text() string { return strings.TrimSpace(n.sel.Text()) }

func (n *node) attr(name string) (string, bool) { return n.sel.Attr(name) }

func (n *node) absolute(attr string) string {
	value, ok := n.sel.Attr(attr)
	if !ok {
		return ""
	}
	return absoluteURL(n.base, value)
}

// nodeValue builds the handle scripts see. text/html/href/src/nodeName are
// accessor properties rather than snapshots so they reflect the tree as it
// stands when they are read: a script that removes ad blocks and then reads
// .html sees the cleaned markup. attr and remove stay callable.
//
// nodeName is the lowercase tag of the element. Scripts that must preserve
// document order across mixed tag types (story bodies mixing <p>, <h1>-<h6>,
// <hr> and wrappers) cannot be expressed with per-selector ctx.css calls, which
// group results by selector instead of by position.
func (inv *invocation) nodeValue(n *node) *goja.Object {
	obj := inv.vm.NewObject()
	inv.nodes[obj] = n
	_ = obj.DefineAccessorProperty("text", inv.accessor(func() goja.Value {
		return inv.vm.ToValue(n.text())
	}), nil, goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = obj.DefineAccessorProperty("html", inv.accessor(func() goja.Value {
		html, err := n.sel.Html()
		if err != nil {
			return inv.throw(err)
		}
		return inv.vm.ToValue(html)
	}), nil, goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = obj.DefineAccessorProperty("href", inv.accessor(func() goja.Value {
		return inv.vm.ToValue(n.absolute("href"))
	}), nil, goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = obj.DefineAccessorProperty("src", inv.accessor(func() goja.Value {
		return inv.vm.ToValue(n.absolute("src"))
	}), nil, goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = obj.DefineAccessorProperty("nodeName", inv.accessor(func() goja.Value {
		return inv.vm.ToValue(goquery.NodeName(n.sel))
	}), nil, goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = obj.Set("attr", func(call goja.FunctionCall) goja.Value {
		if value, ok := n.attr(call.Argument(0).String()); ok {
			return inv.vm.ToValue(value)
		}
		return inv.null()
	})
	// Remove detaches the node from the document, which is how scripts do
	// ParseChapter-style cleanup (strip script/style/noscript, hidden nodes, ad
	// blocks) before returning contentHtml.
	_ = obj.Set("remove", func() { n.sel.Remove() })
	return obj
}

func (inv *invocation) accessor(fn func() goja.Value) goja.Value {
	return inv.vm.ToValue(func(goja.FunctionCall) goja.Value { return fn() })
}

// throw raises err inside the script as a catchable JS Error that still carries
// the original Go error, so an uncaught throw maps straight back onto the
// taxonomy and a Fetcher failure stays a plain network error.
func (inv *invocation) throw(err error) goja.Value {
	panic(inv.vm.NewGoError(err))
}

func (inv *invocation) null() goja.Value {
	return inv.vm.ToValue(nil)
}

func (inv *invocation) ctxObject() *goja.Object {
	if inv.ctxObj != nil {
		return inv.ctxObj
	}
	obj := inv.vm.NewObject()
	_ = obj.Set("get", inv.get)
	_ = obj.Set("css", inv.css)
	_ = obj.Set("css1", inv.css1)
	_ = obj.Set("resolveUrl", inv.resolveURL)
	_ = obj.Set("fail", inv.fail)
	_ = obj.Set("log", inv.log)
	inv.ctxObj = obj
	return obj
}

func (inv *invocation) get(call goja.FunctionCall) goja.Value {
	rawURL := call.Argument(0).String()
	inv.fetches++
	if inv.fetches > inv.script.engine.opts.MaxFetches {
		return inv.throw(&ScriptError{
			Code:    CodeParserTimeout,
			Message: fmt.Sprintf("fetch budget of %d exceeded", inv.script.engine.opts.MaxFetches),
		})
	}
	result, err := inv.script.engine.fetcher.Fetch(inv.ctx, rawURL)
	if err != nil {
		return inv.throw(fmt.Errorf("fetching %s: %w", rawURL, err))
	}
	if int64(len(result.Body)) > inv.script.engine.opts.MaxBodyBytes {
		return inv.throw(shapeError("response body exceeds limit (%d bytes)", inv.script.engine.opts.MaxBodyBytes))
	}
	// A fetcher that leaves FinalURL empty would silently break relative link
	// resolution, so fall back to the requested URL.
	base := result.FinalURL
	if base == "" {
		base = rawURL
	}
	return inv.vm.ToValue(&doc{Status: result.Status, URL: base, Body: string(result.Body), base: base})
}

func (inv *invocation) css(call goja.FunctionCall) goja.Value {
	root, base, err := inv.resolveTarget(call.Argument(0))
	if err != nil {
		return inv.throw(err)
	}
	matches := root.Find(call.Argument(1).String())
	handles := make([]any, 0, matches.Length())
	for i := range matches.Nodes {
		handles = append(handles, inv.nodeValue(&node{sel: matches.Eq(i), base: base}))
	}
	return inv.vm.NewArray(handles...)
}

func (inv *invocation) css1(call goja.FunctionCall) goja.Value {
	root, base, err := inv.resolveTarget(call.Argument(0))
	if err != nil {
		return inv.throw(err)
	}
	match := root.Find(call.Argument(1).String())
	if match.Length() == 0 {
		return inv.null()
	}
	return inv.nodeValue(&node{sel: match.Eq(0), base: base})
}

// resolveTarget returns the subtree to search and the base URL for resolving
// href/src: a node handle (searched within its subtree), a doc handle (parsed
// lazily), or a bare HTML string.
func (inv *invocation) resolveTarget(target goja.Value) (*goquery.Selection, string, error) {
	if obj, ok := target.(*goja.Object); ok {
		if handle, ok := inv.nodes[obj]; ok {
			return handle.sel, handle.base, nil
		}
	}
	switch typed := target.Export().(type) {
	case *doc:
		root, err := typed.selection()
		return root, typed.base, err
	case string:
		parsed, err := goquery.NewDocumentFromReader(strings.NewReader(typed))
		if err != nil {
			return nil, "", fmt.Errorf("parsing html string: %w", err)
		}
		return parsed.Selection, "", nil
	default:
		return nil, "", shapeError("css target must be a doc, a node, or an html string")
	}
}

// resolveURL exists because goja has no WHATWG URL API; scripts would otherwise
// have to hand-roll absolute URL resolution.
func (inv *invocation) resolveURL(call goja.FunctionCall) goja.Value {
	return inv.vm.ToValue(absoluteURL(call.Argument(0).String(), call.Argument(1).String()))
}

func (inv *invocation) fail(call goja.FunctionCall) goja.Value {
	code := call.Argument(0).String()
	if !validCode(code) {
		return inv.throw(shapeError("ctx.fail: unknown code %q", code))
	}
	return inv.throw(&ScriptError{Code: code, Message: call.Argument(1).String()})
}

func (inv *invocation) log(call goja.FunctionCall) goja.Value {
	slog.Info(call.Argument(0).String(), "parser", inv.script.name)
	return goja.Undefined()
}

func absoluteURL(base, ref string) string {
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return ref
	}
	relative, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return baseURL.ResolveReference(relative).String()
}
