// novelfire.net / novelphoenix.com — the two domains mirror each other and
// serve a small JS redirect page ("Loading...") at each other.
const AJAX_PREFIX = "/listChapterDataAjax";
const AJAX_SUFFIX = "&draw=1&columns%5B0%5D%5Bdata%5D=title&columns%5B0%5D%5Bname%5D=&columns%5B0%5D%5Bsearchable%5D=true&columns%5B0%5D%5Borderable%5D=false&columns%5B0%5D%5Bsearch%5D%5Bvalue%5D=&columns%5B0%5D%5Bsearch%5D%5Bregex%5D=false&columns%5B1%5D%5Bdata%5D=created_at&columns%5B1%5D%5Bname%5D=&columns%5B1%5D%5Bsearchable%5D=true&columns%5B1%5D%5Borderable%5D=true&columns%5B1%5D%5Bsearch%5D%5Bvalue%5D=&columns%5B1%5D%5Bsearch%5D%5Bregex%5D=false&columns%5B2%5D%5Bdata%5D=n_sort&columns%5B2%5D%5Bname%5D=&columns%5B2%5D%5Bsearchable%5D=false&columns%5B2%5D%5Borderable%5D=true&columns%5B2%5D%5Bsearch%5D%5Bvalue%5D=&columns%5B2%5D%5Bsearch%5D%5Bregex%5D=false&order%5B0%5D%5Bcolumn%5D=2&order%5B0%5D%5Bdir%5D=asc&start=0&length=-1&search%5Bvalue%5D=&search%5Bregex%5D=false";

const clean = (s) => (s || "").replace(/\s+/g, " ").trim();

function host(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(u);
  return m ? m[1].toLowerCase().replace(/^www\./, "") : "";
}

function hostname(u) {
  return host(u).replace(/:\d+$/, "");
}

// Swaps novelfire.net<->novelphoenix.com and the /book/ <-> /novel/ path prefix.
function fallbackURL(raw) {
  const m = /^(https?:\/\/)([^/?#]+)([^?#]*)(.*)$/.exec(raw);
  if (!m) return "";
  const h = m[2].toLowerCase().replace(/^www\./, "");
  if (h === "novelfire.net") {
    const p = m[3].indexOf("/book/") === 0 ? "/novel" + m[3].slice(5) : m[3];
    return m[1] + m[2].replace("novelfire.net", "novelphoenix.com") + p + m[4];
  }
  if (h === "novelphoenix.com") {
    const p = m[3].indexOf("/novel/") === 0 ? "/book" + m[3].slice(6) : m[3];
    return m[1] + m[2].replace("novelphoenix.com", "novelfire.net") + p + m[4];
  }
  return "";
}

function isRedirectPage(ctx, doc) {
  const t = ctx.css1(doc, "title");
  if (t && t.text === "Loading...") return true;
  for (const s of ctx.css(doc, "script")) {
    if (s.text.indexOf("novelphoenix.com") >= 0) return true;
  }
  return false;
}

function metaContent(ctx, doc, selector) {
  const n = ctx.css1(doc, selector);
  if (!n) return "";
  const c = n.attr("content");
  return c === null ? "" : c.trim();
}

function firstText(ctx, doc, selectors) {
  for (const sel of selectors) {
    const n = ctx.css1(doc, sel);
    if (n && n.text) return n.text;
  }
  return "";
}

function cleanTitle(title) {
  const suffixes = [" Novel Chapters - Novel Fire", " - Novel Fire", " Novel Fire", " - Novelfire"];
  for (const s of suffixes) {
    if (title.slice(-s.length) === s) return title.slice(0, -s.length).trim();
  }
  return title.trim();
}

function authorOf(ctx, doc) {
  const a = firstText(ctx, doc, [
    "span[itemprop='author']",
    "a[itemprop='author']",
    ".author a",
    ".author",
    "ul.books a[href*='/author/']"
  ]);
  if (a) return a;
  const m = metaContent(ctx, doc, "meta[name='author']");
  if (m && m.toLowerCase() !== "novel fire") return m;
  return "";
}

function descriptionOf(ctx, doc) {
  const m = metaContent(ctx, doc, "meta[itemprop='description']");
  if (m) return m;
  const s = firstText(ctx, doc, [".summary .content", "div.summary"]);
  if (s) return s;
  const d = metaContent(ctx, doc, "meta[name='description']");
  if (d && d.toLowerCase().indexOf("read ") !== 0) return d;
  return "";
}

function novelOf(ctx, doc) {
  const title = firstText(ctx, doc, ["div.main-head h1", ".main-head h1", "div.novel-info h1", "h1"]);
  return {
    title: title ? cleanTitle(title) : "",
    author: authorOf(ctx, doc),
    description: descriptionOf(ctx, doc),
    coverUrl: metaContent(ctx, doc, "meta[property='og:image']") ||
      metaContent(ctx, doc, "meta[name='twitter:image']"),
    language: "",
    tags: []
  };
}

// The DataTables fragment in the page carries the query string of the chapter
// list request; the rest of the DataTables payload is irrelevant here.
function ajaxURL(ctx, doc, baseURL) {
  for (const s of ctx.css(doc, "script")) {
    const text = s.text;
    const i = text.indexOf(AJAX_PREFIX);
    if (i < 0) continue;
    let end = text.indexOf('"', i);
    if (end < 0) end = text.indexOf("'", i);
    if (end < 0) continue;
    return "https://" + hostname(baseURL) + text.slice(i, end) + AJAX_SUFFIX;
  }
  return "";
}

// n_sort is the sequential URL number and therefore the canonical position;
// the titles carry the novel's own numbering and cannot be used to order.
function chaptersFromJSON(ctx, ajaxURL, pageURL) {
  const data = JSON.parse(ctx.get(ajaxURL).body);
  if (!data || !Array.isArray(data.data) || data.data.length === 0) return [];
  let root = pageURL;
  if (root.slice(-9) === "/chapters") root = root.slice(0, -9);
  return data.data.map((ch) => ({ title: clean(ch.title), url: root + "/chapter-" + ch.n_sort }));
}

function sortKey(u) {
  const m = /chapter-(\d+)$/.exec(u);
  return m ? parseInt(m[1], 10) : 0;
}

function chaptersFromHTML(ctx, doc, pageURL) {
  const seen = new Set();
  const collected = [];

  const collect = (d) => {
    for (const a of ctx.css(d, "ul.chapter-list a")) {
      const href = a.attr("href");
      if (href === null) continue;
      const full = ctx.resolveUrl(pageURL, href);
      if (seen.has(full)) continue;
      seen.add(full);
      const t = ctx.css1(a, ".chapter-title");
      const title = t && t.text ? t.text : a.text;
      collected.push({ title: clean(title), url: full, key: sortKey(full), i: collected.length });
    }
  };

  collect(doc);

  const pageRe = /[?&]page=(\d+)/;
  const processed = new Set([pageURL]);
  const pages = [pageURL];
  for (let i = 0; i < pages.length; i++) {
    let d = doc;
    if (pages[i] !== pageURL) {
      try {
        d = ctx.get(pages[i]);
      } catch (e) {
        continue;
      }
    }
    collect(d);
    for (const a of ctx.css(d, "ul.pagination a")) {
      const href = a.attr("href");
      if (href === null) continue;
      const full = ctx.resolveUrl(pageURL, href);
      if (!processed.has(full) && pageRe.test(full)) {
        processed.add(full);
        pages.push(full);
      }
    }
  }

  collected.sort((x, y) => x.key - y.key || x.i - y.i);
  return collected.map((c) => ({ title: c.title, url: c.url }));
}

function chapterRefs(ctx, doc, pageURL) {
  const ajax = ajaxURL(ctx, doc, pageURL);
  if (ajax) {
    let chapters = [];
    try {
      chapters = chaptersFromJSON(ctx, ajax, pageURL);
    } catch (e) {
      chapters = [];
    }
    if (chapters.length > 0) return chapters;
  }
  return chaptersFromHTML(ctx, doc, pageURL);
}

function collect(ctx, pageURL, depth) {
  let mainURL = pageURL;
  let chaptersURL = pageURL;
  if (chaptersURL.slice(-9) === "/chapters") {
    mainURL = chaptersURL.slice(0, -9);
  } else {
    chaptersURL += chaptersURL.slice(-1) === "/" ? "chapters" : "/chapters";
  }

  const mainDoc = ctx.get(mainURL);
  if (isRedirectPage(ctx, mainDoc)) {
    const fb = fallbackURL(pageURL);
    if (fb && depth < 3) return collect(ctx, fb, depth + 1);
  }

  const novel = novelOf(ctx, mainDoc);
  const chaptersDoc = ctx.get(chaptersURL);
  if (isRedirectPage(ctx, chaptersDoc)) {
    const fb = fallbackURL(pageURL);
    if (fb && depth < 3) return collect(ctx, fb, depth + 1);
  }

  return { novel: novel, chapters: chapterRefs(ctx, chaptersDoc, chaptersURL) };
}

function parseChapter(ctx, chapterURL, depth) {
  const doc = ctx.get(chapterURL);
  if (isRedirectPage(ctx, doc)) {
    const fb = fallbackURL(chapterURL);
    if (fb && depth < 3) return parseChapter(ctx, fb, depth + 1);
  }

  let title = "";
  const t = ctx.css1(doc, "span.chapter-title");
  if (t) title = t.text;
  if (!title) {
    const h = ctx.css1(doc, "h1, h2");
    if (h) title = h.text;
  }

  let content = ctx.css1(doc, "div.chapter-content");
  if (!content) content = ctx.css1(doc, "div#content");
  if (!content) {
    const fb = fallbackURL(chapterURL);
    if (fb && depth < 3) return parseChapter(ctx, fb, depth + 1);
    ctx.fail("site_layout_changed", "no chapter content found at " + chapterURL);
  }

  for (const n of ctx.css(content, "script, style, noscript")) n.remove();
  for (const n of ctx.css(content, "*")) {
    const style = n.attr("style");
    if (style !== null && style.toLowerCase().indexOf("display:none") >= 0) n.remove();
  }
  for (const n of ctx.css(content, "p")) {
    const cls = n.attr("class");
    if (cls !== null && cls !== "") n.remove();
  }
  for (const n of ctx.css(content, "div dl dt")) n.remove();

  return { title: title, contentHtml: content.html.trim() };
}

module.exports = {
  name: "novelfire",
  apiVersion: 1,
  requiresBrowser: false,

  probe: (url) => {
    const h = host(url);
    return h === "novelfire.net" || h === "novelphoenix.com";
  },

  toc: (ctx, url) => collect(ctx, url, 0),

  chapter: (ctx, url) => parseChapter(ctx, url, 0)
};
