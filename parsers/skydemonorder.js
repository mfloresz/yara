// skydemonorder.com — Cloudflare-protected; the project page's chapter list is
// a lazy-loaded Livewire component, so the catalog only exists after the
// browser evaluates it. The Go host routes the project page through the
// worker's fetch_livewire operation (declared below via livewireCatalogPattern)
// and the script reads the freeChapters payload from that response. Walking the
// chapters via "Next chapter" links is only the fallback for a worker without
// Livewire support.
function host(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(u);
  return m ? m[1].toLowerCase().replace(/^www\./, "") : "";
}

function metaContent(ctx, doc, selector) {
  const n = ctx.css1(doc, selector);
  if (!n) return "";
  const c = n.attr("content");
  return c === null ? "" : c.trim();
}

function titleOf(ctx, doc) {
  const t = ctx.css1(doc, "h1.font-title");
  if (t && t.text) return t.text;
  const h = ctx.css1(doc, "h1");
  return h ? h.text : "";
}

function coverOf(ctx, doc) {
  const el = ctx.css1(doc, "div.w-full.max-w-72 img");
  if (el) {
    const src = el.attr("src");
    if (src !== null) {
      const u = src.trim();
      if (u !== "" && u.indexOf("data:") !== 0) return u;
    }
  }
  return metaContent(ctx, doc, "meta[property='og:image']");
}

function descriptionOf(ctx, doc) {
  // The synopsis lives in a div whose class includes "line-clamp-3"; the
  // clamped view is just CSS over the real <p> paragraphs.
  const el = ctx.css1(doc, "div[class*='line-clamp-3']");
  if (el && el.text) return el.text;
  return metaContent(ctx, doc, "meta[name='description']");
}

function firstChapterURL(ctx, doc, pageURL) {
  for (const a of ctx.css(doc, "a[href*='/projects/']")) {
    if (a.text.toLowerCase().indexOf("start reading") >= 0) {
      const href = a.attr("href");
      if (href !== null && href !== "") return ctx.resolveUrl(pageURL, href);
    }
  }
  return pageURL.replace(/\/+$/, "") + "/1";
}

function extractChaptersFromHTML(ctx, body, pageURL) {
  const match = /freeChapters:\s*JSON\.parse\('([^']+)'\)/.exec(body);
  if (!match) return [];

  // The captured text is a JS string literal body using JSON escapes, so it
  // round-trips through JSON.parse as a quoted string.
  let items;
  try {
    items = JSON.parse('"' + match[1] + '"');
  } catch (e) {
    ctx.fail("site_layout_changed", "cannot decode the freeChapters payload: " + e.message);
  }
  let parsed;
  try {
    parsed = JSON.parse(items);
  } catch (e) {
    ctx.fail("site_layout_changed", "cannot parse the freeChapters payload: " + e.message);
  }

  const base = pageURL.replace(/\/+$/, "");
  const chapters = [];
  for (const ch of parsed) {
    if (!ch.slug) continue;
    chapters.push({
      title: (ch.title || "").trim(),
      url: base + "/" + ch.slug,
      key: ch.episode,
      i: chapters.length
    });
  }
  chapters.sort((x, y) => x.key - y.key);
  return chapters.map((c) => ({ title: c.title, url: c.url }));
}

function nextChapterHref(ctx, doc) {
  const a = ctx.css1(doc, "a[aria-label='Next chapter']");
  if (a) {
    const href = a.attr("href");
    if (href !== null) return href.trim();
  }
  // Fallback: the reader also wires the right-arrow key to the next URL.
  for (const n of ctx.css(doc, "div[class*='keydown.right']")) {
    const html = n.html;
    const marker = "window.location.href = '";
    const i = html.indexOf(marker);
    if (i === -1) continue;
    const rest = html.slice(i + marker.length);
    const end = rest.indexOf("'");
    if (end !== -1) return rest.slice(0, end);
  }
  return "";
}

// The site renders no chapter index server-side, so this sequential crawl is
// the only way to enumerate the catalog when the Alpine payload is absent.
// The host's fetch budget bounds it long before the 10k cap below.
function walkChapters(ctx, startURL, pageURL) {
  const MAX_CHAPTERS = 10000;
  const seen = new Set();
  const chapters = [];
  let current = startURL;

  while (chapters.length < MAX_CHAPTERS) {
    if (seen.has(current)) break;
    seen.add(current);

    const doc = ctx.get(current);
    chapters.push({ title: titleOf(ctx, doc).trim(), url: current });

    const next = nextChapterHref(ctx, doc);
    if (next === "") break;
    current = ctx.resolveUrl(pageURL, next);
  }

  return chapters;
}

module.exports = {
  name: "skydemonorder",
  apiVersion: 1,
  requiresBrowser: true,
  // Only the project page (the TOC) carries the Livewire catalog; chapter URLs
  // have a second path segment and keep using the plain page fetch.
  livewireCatalogPattern: "^https?://(?:www\\.)?skydemonorder\\.com/projects/[^/?#]+/?$",

  probe: (url) => host(url) === "skydemonorder.com",

  toc: (ctx, url) => {
    const doc = ctx.get(url);
    const title = titleOf(ctx, doc);

    // Only free chapters: premium ones need a subscription to read.
    let chapters = extractChaptersFromHTML(ctx, doc.body, url);
    if (chapters.length === 0) {
      chapters = walkChapters(ctx, firstChapterURL(ctx, doc, url), url);
    }

    return {
      novel: {
        title: title.trim(),
        author: "",
        description: descriptionOf(ctx, doc).trim(),
        coverUrl: coverOf(ctx, doc),
        language: "",
        tags: []
      },
      chapters: chapters
    };
  },

  chapter: (ctx, url) => {
    const doc = ctx.get(url);
    const title = titleOf(ctx, doc);

    const sel = ctx.css1(doc, "#chapter-body") || ctx.css1(doc, "#chapter-content");
    if (!sel) ctx.fail("site_layout_changed", "no chapter content found at " + url);

    for (const n of ctx.css(sel, "script, style, noscript")) n.remove();

    return { title: title.trim(), contentHtml: sel.html.trim() };
  }
};
