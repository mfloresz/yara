// vritrascans.com — custom WordPress theme. One category page per novel,
// chapters listed newest-first inside ul.chapter-list.
//
// The theme emits a rel="next" link even when the whole list fits on the first
// page (page 2 repeats page 1 verbatim), so pagination walks pages only while
// they keep adding new chapters.

const clean = (s) => (s || "").replace(/\s+/g, " ").trim();

function host(url) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(url);
  return m ? m[1].toLowerCase().replace(/^www\./, "") : "";
}

function metaContent(ctx, doc, selector) {
  const n = ctx.css1(doc, selector);
  if (!n) return "";
  const c = n.attr("content");
  return c === null ? "" : c.trim();
}

function novelOf(ctx, doc) {
  const titleN = ctx.css1(doc, "h1.novel-title");
  const coverN = ctx.css1(doc, ".novel-header__cover img");
  const tags = ctx.css(doc, ".genre-chip").map((n) => clean(n.text)).filter(Boolean);

  let description = "";
  const syn = ctx.css1(doc, ".novel-synopsis");
  if (syn) {
    // The site prefixes the synopsis block with a literal "Synopsis:".
    description = clean(syn.text).replace(/^synopsis:?\s*/i, "");
  }

  return {
    title: titleN ? clean(titleN.text) : "",
    author: "",
    description: description,
    coverUrl: coverN && coverN.src ? coverN.src : metaContent(ctx, doc, "meta[property='og:image']"),
    language: "",
    tags: tags
  };
}

function chaptersFromPage(ctx, doc) {
  const out = [];
  for (const li of ctx.css(doc, "ul.chapter-list li")) {
    const a = ctx.css1(li, "a");
    if (!a || !a.href) continue;
    const t = ctx.css1(li, ".chapter-list__title");
    out.push({ title: t && t.text ? clean(t.text) : clean(a.text), url: a.href });
  }
  return out;
}

function chaptersOf(ctx, doc, startURL) {
  const seen = {};
  const collected = [];
  let current = startURL;

  while (current) {
    const before = collected.length;
    for (const ch of chaptersFromPage(ctx, doc)) {
      if (seen[ch.url]) continue;
      seen[ch.url] = true;
      collected.push(ch);
    }
    if (collected.length === before) break; // repeated/empty page — stop

    const link = ctx.css1(doc, "link[rel='next']");
    const rel = link ? link.attr("href") : null;
    if (!rel || rel === "") break;
    current = ctx.resolveUrl(current, rel);
    doc = ctx.get(current); // a broken later page surfaces as a network error
  }

  if (collected.length === 0) {
    ctx.fail("site_layout_changed", "no chapters found at " + startURL);
  }

  // The list is newest-first; the array order is the reading order.
  collected.reverse();
  return collected;
}

function parseChapter(ctx, chapterURL) {
  const doc = ctx.get(chapterURL);

  const content = ctx.css1(doc, "article#chapter-content") || ctx.css1(doc, "article.chapter-content");
  if (!content) {
    ctx.fail("site_layout_changed", "no chapter content at " + chapterURL);
  }

  for (const n of ctx.css(content, "script, style, noscript")) n.remove();

  // The first heading without an image carries the real per-chapter title
  // ("Chapter 12: ... (2)"); the site's page title is just the novel name.
  // Take it out of the body once read so it is not stored twice.
  let title = "";
  for (const h of ctx.css(content, "h2, h3")) {
    if (ctx.css1(h, "img") !== null) continue;
    if (!h.text || clean(h.text) === "") continue;
    title = clean(h.text);
    h.remove();
    break;
  }

  return { title: title, contentHtml: content.html.trim() };
}

module.exports = {
  name: "vritrascans",
  apiVersion: 1,
  requiresBrowser: false,

  probe: (url) => host(url) === "vritrascans.com",

  toc: (ctx, url) => {
    const doc = ctx.get(url);
    return { novel: novelOf(ctx, doc), chapters: chaptersOf(ctx, doc, url) };
  },

  chapter: (ctx, url) => parseChapter(ctx, url)
};
