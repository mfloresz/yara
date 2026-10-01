# Parsers

Yara's site-specific logic lives in user-editable JavaScript files, not in the
Go binary. Each script describes one site: how to recognize it, how to list its
chapters, and how to extract one chapter's text. The server runs those scripts
in an embedded JS engine (`internal/parserhost`) and owns everything else:
HTTP, browser-worker routing, throttling, diffing against the library, and
storage.

Adding a site therefore means dropping one `.js` file in the parsers directory.
No rebuild, no restart.

## Where scripts live

Default: `<data-dir>/parsers` (e.g. `./data/parsers` next to the binary).
Override with `-parsers-dir <path>` or `PARSERS_DIR=<path>`. The directory is
created on boot if missing, and it starts empty — a site is only parsed once a
script for it exists.

Every `*.js` file directly in the directory is loaded. Loading has no cache: the
directory is re-read on each job, so **editing a script takes effect on the next
download or check** without restarting the server.

## Contract

```js
module.exports = {
  name: 'my-site',            // required, non-empty
  apiVersion: 1,              // required, must be exactly 1
  requiresBrowser: false,     // bool; metadata, see "Browser worker" below
  livewireCatalogPattern: '', // optional Go-regexp source; see "Browser worker"
  probe: (url) => boolean,    // can this script handle the URL?
  toc: (ctx, url) => ({       // full snapshot of the site's chapter list
    novel: { title, description, author, coverUrl, language, tags },
    chapters: [{ title, url }],   // in reading order
  }),
  chapter: (ctx, url) => ({ title, contentHtml }),
  chapterKey: (ch) => string, // optional; default identity is the chapter URL
};
```

Rules that matter:

- **`chapters` is a complete, ordered snapshot** of what the site currently has,
  not a delta. The host diffs it against the library. If the site paginates, the
  script loops and calls `ctx.get` as many times as it needs (within the fetch
  budget) — in-site pagination is the script's job.
- **The array order is the reading order.** The host uses each chapter's
  position as its canonical chapter order, so a site whose titles carry a
  different numbering of their own (e.g. "Chapter 92.1" at position 93) still
  gets distinct, correct orders. Do not re-sort the array.
- **Module init must not fetch.** The module body runs once per invocation in a
  throwaway VM. Everything network-bound belongs in `toc` or `chapter`.
- **`chapter`** must return non-empty `contentHtml`. The host converts it to
  markdown and strips a leading line that merely repeats the title.
- **`chapterKey`** is the stable identity used to match chapters across syncs.
  Omit it and the chapter URL is the identity. Supply it when the URL is
  unstable (rotating tokens, session-scoped ids) but the chapter has a durable
  number.
- **`livewireCatalogPattern`** (optional, Go regexp source) tells the host which
  of the script's URLs carry a Livewire-rendered chapter catalog. On the
  browser-worker path, a matching URL is fetched with the worker's
  `fetch_livewire` operation instead of a plain page fetch; if that request
  fails the host falls back to `fetch_page`. Chapter URLs must not match, so
  anchor the pattern to the catalog page only.

Minimal example:

```js
module.exports = {
  name: 'my-site',
  apiVersion: 1,
  probe: (url) => /^https?:\/\/(www\.)?my-site\.com\/book\//.test(url),
  toc: (ctx, url) => {
    const doc = ctx.get(url);
    const list = ctx.css1(doc, 'ul.chapter-list');
    if (list === null) ctx.fail('site_layout_changed', 'no chapter list');
    const chapters = ctx.css(list, 'li a').map((a) => ({
      title: ctx.css1(a, 'span.title').text,
      url: a.href,               // already absolute
    }));
    return {
      novel: { title: ctx.css1(doc, 'h1').text, author: '', coverUrl: '', tags: [] },
      chapters,
    };
  },
  chapter: (ctx, url) => {
    const doc = ctx.get(url);
    const body = ctx.css1(doc, 'div.content');
    if (body === null) ctx.fail('site_layout_changed', 'no content block');
    return { title: '', contentHtml: body.html };
  },
};
```

## Bindings

Passed to `toc` and `chapter` as the first argument. `chapterKey` receives no
context.

| Binding | Returns | Notes |
| --- | --- | --- |
| `ctx.get(url)` | `{status, url, body}` | `body` is the **raw** response body, so `JSON.parse(doc.body)` works for JSON-driven sites. `url` is the post-redirect URL. |
| `ctx.css(target, sel)` | array of nodes | `target` is a `doc`, another node, or a raw HTML string. |
| `ctx.css1(target, sel)` | node or `null` | First match only. |
| `ctx.resolveUrl(base, rel)` | string | Absolute-URL resolution (goja has no WHATWG `URL`). |
| `ctx.fail(code, msg)` | throws | Raise a taxonomy error; see below. |
| `ctx.log(msg)` | — | Appears in the server log tagged with the parser name. |

Node handles expose `text`, `html`, `href`, `src`, `nodeName` as live
properties, plus `attr(name)` and `remove()`. `href`/`src` are resolved against
the document's URL. Properties are live, so removing ad blocks before reading
`.html` works as expected.

`ctx.css` returns matches in **document order** — including comma-separated
selector lists, which do not group results by selector. To tell mixed tag
types apart (`<p>`, `<h2>`, `<hr>` in one story body), match them all in one
call and branch on `nodeName`. Only separate `ctx.css` calls group per call.

## Limits

Per invocation, defaults from `internal/parserhost`:

| Limit | Default | On breach |
| --- | --- | --- |
| Wall-clock (`chapter`) | 30s | `parser_timeout` |
| Wall-clock (`toc`) | 120s | `parser_timeout` |
| `ctx.get` calls | 200 | `parser_timeout` |
| Response body | 5 MiB | `script_error` |

The `toc` ceiling is longer because a paginating catalog pays the download
throttle on every page. Both timeouts are enforced by the engine and cannot be
caught from the script.

Between consecutive fetches to the same site (URL host), Yara waits a random
interval in `[DOWNLOAD_MIN_DELAY_MS, DOWNLOAD_MAX_DELAY_MS]` (defaults
5000/10000 ms). The first fetch to each host is not delayed, and sites never
block each other: two concurrent downloads from different hosts run in
parallel. Throttling is the server's job — do not add sleeps inside scripts.

## Error taxonomy

Raise a code with `ctx.fail(code, message)`; every other fault (uncaught throw,
wrong return shape, timeout) is mapped by the host. Network failures are
**not** taxonomy errors — they surface as plain errors, which is how a broken
site stays distinguishable from a broken script.

| Code | Meaning | What the user sees |
| --- | --- | --- |
| `not_my_site` | `probe` returned false, or no script claims the URL | "Unsupported URL: no installed parser script handles this site." |
| `site_layout_changed` | Expected elements are gone | "The site layout changed… verify it with `--check-parser`." |
| `blocked` | Challenge page that was not resolved | "The site blocked the request… connect the browser worker." |
| `parser_timeout` | CPU interrupt or fetch budget exceeded | "The parser took too long… verify it with `--check-parser`." |
| `script_error` | Any other script or host error | "Parser script error: …" |

`site_layout_changed` is the one to reach for whenever a selector comes back
empty. It is the difference between "the site changed" and "this script is
broken" in the job error message.

## Hybrid parsers (host helpers)

A few sites need part of the extraction done in Go because the JS engine
cannot express it — binary font parsing, Brotli decompression, cipher work.
For those, Yara runs a small **host-side helper**: a Go function in
`internal/api/parser_cgfont.go` that post-processes a fetch response for one
site's host before the script sees it. The split of responsibilities is
fixed:

- The **helper** handles what goja cannot: today, decoding the
  chrysanthemumgarden obfuscation fonts (the `cg-scrape-protection` plugin)
  so protected spans arrive as real letters.
- The **script** keeps every DOM decision: selectors, ordering, noise
  removal, error taxonomy. It neither knows nor invokes the helper.

A helper failure fails the whole fetch (surfacing as `blocked`), never a
partially decoded response — a site whose protection cannot be decoded must
error out, not store scrambled chapter text. Helpers apply in the server and
in `-check-parser` mode alike, so `--check-parser` shows what production
would download.

This is a deliberate exception to "all site-specific logic lives in the
scripts": it is only worth it when the work is impossible in JS, and it means
that parser's full behavior spans two files.

## Checking a script

```bash
./bin/translator-server -check-parser ./data/parsers/my-site.js \
                        -check-url https://my-site.com/book/12345
```

Check mode runs **before** the server starts and exits without touching
PocketBase, the store, or the data directory — safe against a live install.
It applies the same site shims as production (required headers, GBK-family
decoding) but has no SSRF guard and no throttling: it is an interactive
debugging tool run against an explicit URL.
It exits `0` on success and `1` on failure, printing a readable
`<stage> failed [<code>]: <message>` line on error. The stdout snapshot is
indented JSON, so it pipes cleanly:

```bash
./bin/translator-server -check-parser ./data/parsers/my-site.js \
                        -check-url https://my-site.com/book/12345 \
  | jq '.chapters[0], .chapters | length'
```

The snapshot contains the parser name, the parsed novel metadata, every chapter
with its `index`/`title`/`url`/`key`, and a truncated `chapterPreview` of the
first chapter. A `key` that changes between runs means `chapterKey` is not
stable, which will make update checks re-download chapters that are already
stored.

Debug loop: run it, read the shape, fix the selector, repeat.

## Versions & auto-update

Parsers are versioned by **content, not numbers**: every tracked script in
`parsers/` is listed in `parsers/index.json` with the sha256 of its current
body. That manifest is the release channel — it is served from the project
repository (`https://raw.githubusercontent.com/mfloresz/yara/main/parsers/index.json`)
and each entry doubles as the address of the script next to it.

A running server keeps its installed scripts current on its own. Parsers are
per host, so every update is scoped to the one script a URL resolved to —
a failure on one site never refreshes or retries another site's scripts:

- Before any execution that would run a script — a TOC fetch, a chapter
  download or a whole download job — the installed copy's digest is compared
  against the manifest, and a different version is downloaded, validated
  (signature via the manifest, digest, compile, contract shape) and atomically
  swapped in. Read-only lookups (`canUpdate`, `requiresBrowser` on the novel
  form) never download.
- If a script fails at runtime (`site_layout_changed`, `script_error`, …),
  the server checks that script against the manifest once and retries the
  failed call against the published version before reporting the error. If no
  update was available, the error is reported as-is.
- If no installed script claims a URL at all, scripts listed in the manifest
  but missing locally (fresh installs start with an empty directory) are
  downloaded once and the selection is retried. Already-installed scripts are
  never bulk-refreshed on this path.
- Failures are never fatal: an unreachable or badly signed manifest, a digest
  mismatch or a script that fails validation logs a warning and execution
  proceeds with the installed copy.
- The manifest is cached for 5 minutes; a script failure with no update
  available invalidates that cache so the next attempt re-checks immediately
  — the publish-fix-retry loop needs only one retry to pick up a released fix.

The manifest is **signed**: `parsers/index.json` carries an ed25519
`signature` over its contents, and installs only auto-update from manifests
signed with the trusted key (embedded in the binary, overridable with
`PARSERS_MANIFEST_PUBKEY`). An unsigned or badly signed manifest is ignored
with a warning — execution proceeds with what is installed. Sign after
regenerating:

```bash
make parsers-manifest   # writes parsers/index.json from the parsers/ directory, then signs it
```

Signing reads the private seed from `PARSERS_SIGNING_KEY` or
`.parsers-signing.key` (gitignored, mode 0600); `go run
./tools/sign-parsers-manifest -keygen` prints a fresh keypair for rotation.
A fork serving its own manifest signs with its own key and tells installs to
set `PARSERS_MANIFEST_PUBKEY` to match.

Regenerate + sign the manifest after editing any tracked parser (the digest
and the signature both change). The generated manifest is what a release
publishes; until the parsers land on the repository's default branch, the
default URL 404s and the server simply runs what is installed.

Three env vars tune this:

| Env var | Default | Purpose |
| --- | --- | --- |
| `PARSERS_AUTO_UPDATE` | on | Set to `0`/`false` to disable the update channel entirely — e.g. while hand-editing installed scripts, which updates would otherwise overwrite. |
| `PARSERS_MANIFEST_URL` | GitHub raw URL | Point at a fork or a self-hosted mirror serving the same signed manifest layout. |
| `PARSERS_MANIFEST_PUBKEY` | embedded release key | Hex ed25519 public key the manifest must be signed with. Set it when the manifest URL serves a fork signed with a different key. |

`-check-parser` never auto-updates: check mode is a development tool and runs
exactly the script you point it at.

## Browser worker

Sites behind Cloudflare or JS-rendered pages need a real browser. The
`browser-worker-chrome` / `browser-worker-firefox` extensions connect to the
running server and relay fetches through the user's own browser, reusing their
cookies and IP.

Set `requiresBrowser: true` when a site only works that way. The server then
routes the script's fetches through the user's connected worker.

Routing is also automatic in the other direction: even with
`requiresBrowser: false`, a direct fetch that fails at the transport level,
returns 4xx/5xx, or comes back as a Cloudflare challenge page (`Just a moment`,
`Checking your browser`, `cf-browser-verification`) is retried through the
worker when one is connected.

This flag is what the `requiresBrowser` field on a novel reports to the UI, so
set it honestly.

A script can also declare `livewireCatalogPattern` (see the contract above).
URLs matching it are relayed through the worker's `fetch_livewire` operation,
which waits for the framework to hydrate the component instead of returning the
lazy, unloaded DOM a plain page fetch sees. A script that depends on a
Livewire-rendered catalog must declare the pattern, otherwise its `toc` only
sees the placeholder markup and falls back to crawling.

**Known gap — check mode has no browser worker.** The relay needs a live
authenticated WebSocket from an extension, which does not exist before the
server is up. A script declaring `requiresBrowser` can only be *partially*
verified with `-check-parser`; it will be fetched as plain HTTP and may be
blocked. Check mode prints a warning when it sees the flag. For such sites,
verify the script against the running server (import, then update-check) with
the extension connected.

## How a sync uses a script

1. Every `*.js` in the directory is compiled (no cache — this is what makes
   hot reload work). A script that fails to load is skipped with a warning;
   the rest still run, so one broken file can never take down all sites.
2. Each `probe` runs in order; the first script that claims the URL wins. If
   none does, the job fails with `not_my_site`. (Read-only `canUpdate` /
   `requiresBrowser` lookups cache the outcome per URL host for 5 minutes —
   1 minute for unsupported hosts — so the novel list does not recompile
   every script per row. Executions always resolve fresh.)
3. `toc` returns the full snapshot. The host diffs it against the library. A
   single chapter whose `chapterKey` throws falls back to its URL rather than
   failing the whole snapshot.
4. For each chapter that is new, `chapter` is called and the result is stored,
   then novel stats are recalculated. The stored title comes from the chapter
   page first — TOC entries sometimes prefix every chapter with the novel
   name — with the TOC title as fallback.

### Fetch safety

Script-requested URLs are untrusted (scripts are user-editable and
auto-updated), so direct server-side fetches are SSRF-guarded: only
`http(s)` URLs whose host resolves exclusively to public IPs are fetched.
Cloud metadata endpoints, loopback and private ranges are refused; the fetch
falls back to the user's browser worker when one is connected. Set
`PARSERS_ALLOW_PRIVATE_NETS=1` to disable the guard for local development
only — never on an internet-exposed server.

### Chapter matching, and the `source_key` field

`source_key` stores each chapter's `chapterKey` (default: its URL) on the
`chapters` record. The diff runs in one of two modes, chosen per novel:

- **Keyed mode** — the novel has at least one stored `source_key`. Identity is
  authoritative: a chapter whose key is not stored is genuinely new, even if its
  title collides with a stored chapter's.
- **Legacy mode** — the novel has no `source_key` at all, i.e. every chapter
  predates the field. Identity cannot decide anything, so the older
  title/order heuristic runs. This is what stops a pre-existing novel from
  reporting its entire library as new on the first sync after upgrading.

A novel mid-migration (some chapters keyed, some not) is treated as keyed, so
its untouched legacy chapters are reported as new and re-downloaded **once**,
after which the sync self-heals. That costs a re-download; the alternative
would silently skip chapters, which is the failure mode this field exists to
prevent.

Legacy rows acquire keys through an automatic, all-or-nothing backfill: before
the update flow diffs, every still-keyless chapter is paired against the
snapshot — positionally first (stored order N ↔ snapshot position N−1, gated
on title equality), then by unique title match for novels whose stored orders
have gaps the snapshot positions don't share. Any row that cannot be paired
confidently aborts the whole plan and the novel stays on the legacy
heuristic, unchanged — no migration flag, no user action. Checks and previews
plan in memory only and never write; only the update flow that enqueues
downloads persists the plan.

A chapter that disappears upstream is **reported as missing but never
deleted** — matching the behavior of the flows this replaced.

`source_key` is added idempotently on boot. Existing rows simply have an empty
value and stay in legacy mode until the update flow backfills them (see
above) — no migration flag, no boot-time data rewrite.
