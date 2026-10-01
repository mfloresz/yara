// empirenovel.com — Cloudflare-protected; the chapter list is paginated with
// ?page=N and arrives newest-first, so it is re-sorted ascending by the
// trailing chapter number to keep range-based downloads correct.
const BASE = "https://www.empirenovel.com";

// Some chapter listings glue the publication date to the title with no
// separator ("Chapter 7Jun 24, 2026").
const CHAPTER_DATE =
  /(jan(uary)?|feb(ruary)?|mar(ch)?|apr(il)?|may|jun(e)?|jul(y)?|aug(ust)?|sep(t(ember)?)?|oct(ober)?|nov(ember)?|dec(ember)?)[a-z]*\.?\s+\d{1,2}(st|nd|rd|th)?,?\s+\d{4}/gi;

const clean = (s) => (s || "").replace(/\s+/g, " ").trim();

function metaContent(ctx, doc, selector) {
  const n = ctx.css1(doc, selector);
  if (!n) return "";
  const c = n.attr("content");
  return c === null ? "" : c.trim();
}

// Parses the leading integer of the last path segment, so range-style
// suffixes such as "420-421" still sort by their starting chapter.
function chapterNumber(urlStr) {
  const trimmed = urlStr.replace(/\/$/, "");
  const i = trimmed.lastIndexOf("/");
  if (i === -1) return 0;
  const suffix = trimmed.slice(i + 1).match(/^\d+/);
  return suffix ? parseInt(suffix[0], 10) : 0;
}

function isChapterURL(href, novelURL) {
  const novelPath = novelURL.replace(/\/$/, "");
  const hrefPath = href.replace(/\/$/, "");
  if (hrefPath.indexOf(novelPath) !== 0) return false;
  const suffix = hrefPath.slice(novelPath.length).replace(/^\//, "");
  return suffix !== "" && /^\d+$/.test(suffix);
}

function collect(ctx, doc, novelURL, seen, all) {
  for (const a of ctx.css(doc, "a[href]")) {
    let href = a.attr("href");
    if (href === null || href === "") continue;
    if (href.indexOf("http") !== 0) href = BASE + href;
    href = href.replace(/\/$/, "");
    if (!isChapterURL(href, novelURL)) continue;
    if (seen.has(href)) continue;
    seen.add(href);

    let title = a.text;
    const nl = title.indexOf("\n");
    if (nl >= 0) title = title.slice(0, nl);
    title = clean(title.replace(CHAPTER_DATE, ""));
    all.push({ title: title, url: href, key: chapterNumber(href), i: all.length });
  }
}

function findTotalPages(ctx, doc) {
  let maxPage = 1;
  for (const a of ctx.css(doc, "a[href*='page=']")) {
    const href = a.attr("href");
    if (href === null) continue;
    const i = href.lastIndexOf("page=");
    if (i === -1) continue;
    const m = /^\d+/.exec(href.slice(i + 5));
    if (!m) continue;
    const num = parseInt(m[0], 10);
    if (num > maxPage) maxPage = num;
  }
  return maxPage;
}

function fetchAllChapterRefs(ctx, firstDoc, novelURL) {
  const seen = new Set();
  const all = [];
  collect(ctx, firstDoc, novelURL, seen, all);

  const totalPages = findTotalPages(ctx, firstDoc);
  for (let page = 2; page <= totalPages; page++) {
    const pageURL = novelURL + (novelURL.indexOf("?") >= 0 ? "&page=" : "?page=") + page;
    let doc;
    try {
      doc = ctx.get(pageURL);
    } catch (e) {
      continue;
    }
    collect(ctx, doc, novelURL, seen, all);
  }

  all.sort((x, y) => x.key - y.key || x.i - y.i);
  return all.map((c) => ({ title: c.title, url: c.url }));
}

// Strict host check: only this site's domain (and its subdomains) is
// claimed, never a URL that merely mentions the domain in a query string.
function isSiteHost(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^\/?#]*)/i.exec(u || "");
  if (!m) return false;
  const h = m[1].toLowerCase().replace(/^www\./, "");
  return h === "empirenovel.com" || h.endsWith(".empirenovel.com");
}

module.exports = {
  name: "empirenovel",
  apiVersion: 1,
  requiresBrowser: true,

  probe: isSiteHost,

  toc: (ctx, url) => {
    const doc = ctx.get(url);

    let title = "";
    const h = ctx.css1(doc, "h1.show_title");
    if (h) title = h.text;
    if (!title) title = metaContent(ctx, doc, "meta[property='og:title']");
    if (!title) {
      const f = ctx.css1(doc, "h1");
      if (f) title = f.text;
    }

    let author = "";
    for (const a of ctx.css(doc, "a[href*='author']")) {
      if (a.text && !author) author = a.text;
    }

    let description = metaContent(ctx, doc, "meta[name='description']");
    if (!description) description = metaContent(ctx, doc, "meta[property='og:description']");

    const cover = ctx.css1(doc, ".cover img");
    let coverUrl = "";
    if (cover) {
      const src = cover.attr("src");
      if (src) coverUrl = src.indexOf("http") === 0 ? src : BASE + src;
    }
    if (!coverUrl) coverUrl = metaContent(ctx, doc, "meta[property='og:image']");

    return {
      novel: {
        title: title.trim(),
        author: author.trim(),
        description: description,
        coverUrl: coverUrl,
        language: "",
        tags: []
      },
      chapters: fetchAllChapterRefs(ctx, doc, url)
    };
  },

  chapter: (ctx, url) => {
    const doc = ctx.get(url);

    // Legacy layout: an h3 inside the content area.
    let title = "";
    for (const h of ctx.css(doc, ".mx-2 h3, .mx-sm-5 h3")) title += h.text;
    if (!title) {
      const h = ctx.css1(doc, "h3");
      if (h) title = h.text;
    }

    const sel =
      ctx.css1(doc, ".mx-2.mx-sm-5.p-1.p-sm-5") || ctx.css1(doc, ".reader-page .mx-2");
    if (!sel) ctx.fail("site_layout_changed", "no chapter content container at " + url);

    for (const n of ctx.css(sel, "script, style, noscript, iframe, nav, header, footer, .ads, .ad")) n.remove();

    // New layout: the page h1 is the (truncated) novel title, so the chapter
    // title is the first line of the content itself.
    let titleFromFirstLine = false;
    if (title === "") {
      const first = ctx.css1(sel, "p");
      if (first && first.text) {
        title = first.text;
        titleFromFirstLine = true;
      }
    }
    if (title === "") {
      const n = chapterNumber(url);
      if (n > 0) title = "Chapter " + n;
    }

    const parts = [];
    for (const p of ctx.css(sel, "p")) {
      if (p.text) parts.push("<p>" + p.text + "</p>");
    }
    if (titleFromFirstLine && parts.length > 0) parts.shift();

    // Fallback: extract all text if no paragraphs found.
    if (parts.length === 0) {
      for (const line of sel.text.split("\n")) {
        const t = line.trim();
        if (t) parts.push("<p>" + t + "</p>");
      }
    }

    return { title: title, contentHtml: parts.join("\n") };
  }
};
