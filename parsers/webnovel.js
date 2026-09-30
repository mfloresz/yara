// webnovel.com — book, catalog and chapter pages are static SSR HTML. The
// chapter list is NOT embedded in the book page (its #contents pane loads via
// AJAX), so the catalog page is always fetched separately.
//
// Quirk: some chapter slugs carry a trailing U+FEFF, percent-encoded as
// %EF%BB%BF, which must be stripped before the path can be classified.
const ZERO_WIDTH = String.fromCharCode(0xfeff);

const clean = (s) => (s || "").replace(/\s+/g, " ").trim();

function host(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(u);
  if (!m) return "";
  return m[1].toLowerCase().replace(/^www\./, "").replace(/^m\./, "");
}

function baseURL(u) {
  const m = /^(https?:\/\/[^/?#]+)/i.exec(u);
  return m ? m[1] : "";
}

function pathOf(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/[^/?#]*(\/[^?#]*)/i.exec(u);
  return m ? m[1] : "/";
}

function stripZWS(s) {
  return s.split(ZERO_WIDTH).join("");
}

// The first run of 5+ digits after /book/ is the book id; slugs never contain
// such a run, so this skips both the slug and a chapter's short number.
function bookID(path) {
  const i = path.indexOf("/book/");
  if (i < 0) return "";
  const rest = path.slice(i + 6);
  let start = -1;
  for (let k = 0; k < rest.length; k++) {
    const c = rest.charCodeAt(k);
    if (c >= 48 && c <= 57) {
      if (start < 0) start = k;
    } else {
      if (start >= 0 && k - start >= 5) return rest.slice(start, k);
      start = -1;
    }
  }
  if (start >= 0 && rest.length - start >= 5) return rest.slice(start);
  return "";
}

// kind: "book" | "chapter"
function parseURL(rawURL) {
  if (host(rawURL) !== "webnovel.com") return null;
  const path = pathOf(rawURL);
  if (path.indexOf("/book/") < 0) return null;
  const id = bookID(path);
  if (id === "") return null;
  return { id: id, kind: stripZWS(path).indexOf("/chapter-") >= 0 ? "chapter" : "book" };
}

function isASCIILetters(s) {
  if (s.length === 0) return false;
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    const lower = c >= 97 && c <= 122;
    const upper = c >= 65 && c <= 90;
    if (!lower && !upper) return false;
  }
  return true;
}

// Drops a leading locale segment (e.g. /es, /pt) so emitted chapter URLs stay
// in the canonical locale-free form instead of inheriting a UI locale.
function stripLocale(href) {
  if (href.length > 7 && href[0] === "/" && href[3] === "/" &&
      isASCIILetters(href.slice(1, 3)) && href.slice(3, 8) === "/book") {
    return href.slice(3);
  }
  return href;
}

function metaContent(ctx, doc, selector) {
  const n = ctx.css1(doc, selector);
  if (!n) return "";
  const c = n.attr("content");
  return c === null ? "" : c.trim();
}

function extractChapters(ctx, doc, pageURL) {
  const chapters = [];
  const seen = new Set();
  // Scoped to li[data-cid] so the header shortcuts (read-now / latest-chapter
  // links, which duplicate real chapters) are never picked up.
  for (const a of ctx.css(doc, "li[data-cid] a[href]")) {
    const href = a.attr("href");
    if (href === null || href === "" || href.indexOf("chapter-") < 0) continue;
    const url = ctx.resolveUrl(pageURL, stripLocale(href));
    if (seen.has(url)) continue;
    seen.add(url);

    let title = "";
    const strong = ctx.css1(a, "strong");
    if (strong) title = strong.text;
    if (!title) {
      const t = a.attr("title");
      if (t) title = t.trim();
    }
    if (!title) title = a.text;
    chapters.push({ title: clean(stripZWS(title)), url: url });
  }
  return chapters;
}

// Preserving the inline markup keeps the emphasis the source carries.
function chapterContent(ctx, doc) {
  let sel = ctx.css1(doc, "div.chapter_content div.cha-words");
  if (!sel) sel = ctx.css1(doc, "div.cha-words");
  if (!sel) return "";
  const parts = [];
  for (const p of ctx.css(sel, "p")) {
    if (p.text) parts.push("<p>" + p.html.trim() + "</p>");
  }
  return parts.join("\n");
}

module.exports = {
  name: "webnovel",
  apiVersion: 1,
  requiresBrowser: false,

  probe: (url) => parseURL(url) !== null,

  toc: (ctx, url) => {
    const parsed = parseURL(url);
    if (!parsed) ctx.fail("not_my_site", "invalid webnovel URL: " + url);

    // A chapter URL carries no synopsis, so metadata always comes from the
    // canonical book page.
    const bookURL = baseURL(url) + "/book/" + parsed.id;
    const doc = ctx.get(bookURL);

    let title = metaContent(ctx, doc, "meta[property='og:title']");
    if (!title) {
      const h = ctx.css1(doc, "div.det-info h1");
      if (h) title = h.text;
    }
    if (!title) ctx.fail("site_layout_changed", "book " + parsed.id + " not found or has no title");

    let author = metaContent(ctx, doc, "meta[property='og:author']");
    if (!author) {
      const a = ctx.css1(doc, "address a");
      if (a) author = a.text;
    }

    let description = metaContent(ctx, doc, "meta[property='og:description']");
    if (!description) description = metaContent(ctx, doc, "meta[name='description']");

    let cover = metaContent(ctx, doc, "meta[property='og:image']");
    if (cover.indexOf("//") === 0) cover = "https:" + cover;

    const catalog = ctx.get(baseURL(url) + "/book/" + parsed.id + "/catalog");
    const chapters = extractChapters(ctx, catalog, url);
    if (chapters.length === 0) {
      ctx.fail("site_layout_changed", "book " + parsed.id + " has no chapters in its catalog");
    }

    return {
      novel: {
        title: clean(stripZWS(title)),
        author: stripZWS(author).trim(),
        description: stripZWS(description).trim(),
        coverUrl: cover.trim(),
        language: "",
        tags: []
      },
      chapters: chapters
    };
  },

  chapter: (ctx, url) => {
    const parsed = parseURL(url);
    if (!parsed || parsed.kind !== "chapter") ctx.fail("not_my_site", "not a webnovel chapter URL: " + url);

    const doc = ctx.get(url);

    // Locked (VIP/paywalled) chapters render an empty body behind a lock gate
    // with data-islock="1"; anonymous fetches cannot unfold them.
    if (
      ctx.css1(doc, 'div.chapter_content[data-islock="1"]') ||
      ctx.css1(doc, "div.cha-content._lock")
    ) {
      ctx.fail("blocked", "chapter is locked (VIP/paywalled): open it in the WebNovel app or a logged-in browser to read it");
    }

    // data-chaptername is the clean "Chapter N" title; the visible h1 carries a
    // locale prefix ("Capítulo 1: Chapter 1").
    const container = ctx.css1(doc, "div.chapter_content");
    let title = "";
    if (container) {
      const name = container.attr("data-chaptername");
      if (name !== null) title = name.trim();
    }
    if (!title) {
      const h = ctx.css1(doc, "div.cha-tit h1");
      if (h && h.text) {
        const i = h.text.lastIndexOf(": ");
        title = i >= 0 ? h.text.slice(i + 2).trim() : h.text;
      }
    }

    const content = chapterContent(ctx, doc);
    if (content.trim() === "") {
      ctx.fail("site_layout_changed", "chapter page has no readable content at " + url + " (it may be locked or removed)");
    }

    return { title: clean(stripZWS(title)), contentHtml: content };
  }
};
