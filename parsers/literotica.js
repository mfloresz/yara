// literotica.com — server-rendered HTML with a hashed-CSS class scheme, so the
// selectors below track the current build. Long stories are paginated: the
// "Next Page" links are followed until the control stops being a link.
const clean = (s) => (s || "").replace(/\s+/g, " ").trim();

// /s/{story-slug} is a single story; /series/... pages are series listings.
const STORY_PATH = /\/s\/[a-z0-9-]+\/?(\?.*)?$/;
const TITLE_SUFFIX = /\s+-\s+[^-]+-\s+Literotica\.com\s*$/;

function host(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(u);
  if (!m) return (u || "").indexOf("literotica.com") >= 0;
  const h = m[1].toLowerCase().replace(/:\d+$/, "");
  return h === "literotica.com" || h.endsWith(".literotica.com");
}

function metaContent(ctx, doc, selector) {
  const n = ctx.css1(doc, selector);
  if (!n) return "";
  const c = n.attr("content");
  return c === null ? "" : c.trim();
}

function pageTitle(ctx, doc) {
  const h = ctx.css1(doc, "h1._title_ebp5m_26");
  if (h && h.text) return clean(h.text);
  const f = ctx.css1(doc, "h1");
  if (f && f.text) return clean(f.text);
  return clean(metaContent(ctx, doc, "meta[property='og:title']").replace(TITLE_SUFFIX, ""));
}

function authorName(ctx, doc) {
  const a = ctx.css1(doc, "a._author__title_1wp51_48");
  if (a && a.text) return a.text;
  // Fallback: parse "by <Author>" out of the meta description
  // ("A 8-part Story Series by RickyWrites.").
  const desc = metaContent(ctx, doc, "meta[name='description']");
  const i = desc.lastIndexOf(" by ");
  if (i >= 0) {
    const name = desc.slice(i + 4).trim().replace(/\.$/, "");
    if (name) return name;
  }
  return "";
}

function seriesDescription(ctx, doc) {
  let desc = metaContent(ctx, doc, "meta[name='description']");
  if (!desc) desc = metaContent(ctx, doc, "meta[property='og:description']");

  const keywords = metaContent(ctx, doc, "meta[name='keywords']");
  if (keywords) {
    const tags = keywords.split(",").map((t) => t.trim()).filter((t) => t !== "");
    if (tags.length > 0) desc += "\n\nTags: " + tags.join(", ");
  }

  const dates = ctx.css(doc, "div._date_container_1y595_1422 div._files__date_1y595_672");
  if (dates.length > 0) desc += "\n\n" + dates.map((d) => d.text).join("\n");

  return clean(desc);
}

function extractChapters(ctx, doc, pageURL) {
  const chapters = [];
  const seen = new Set();
  for (const a of ctx.css(doc, "ul._list_qr6sx_43 li._item_qr6sx_49 a._link_qr6sx_55")) {
    const href = a.attr("href");
    if (href === null || href === "") continue;
    const url = ctx.resolveUrl(pageURL, href);
    if (seen.has(url)) continue;
    seen.add(url);
    chapters.push({ title: a.text, url: url });
  }
  return chapters;
}

function storyContent(ctx, doc) {
  const sel = ctx.css1(doc, "div._article__content_138fn_99");
  if (!sel) return "";
  for (const n of ctx.css(sel, "script, style, noscript, iframe, nav, header, footer")) n.remove();
  const parts = [];
  for (const p of ctx.css(sel, "p")) {
    if (p.text) parts.push("<p>" + p.text + "</p>");
  }
  return parts.join("\n");
}

// "" once the last page is reached: there the control is a disabled span
// rather than a link.
function nextPageURL(ctx, doc, currentURL) {
  const a = ctx.css1(doc, "a[aria-label='Next Page']");
  if (!a) return "";
  const href = a.attr("href");
  if (href === null || href === "") return "";
  return ctx.resolveUrl(currentURL, href);
}

module.exports = {
  name: "literotica",
  apiVersion: 1,
  requiresBrowser: false,

  probe: (url) => host(url),

  toc: (ctx, url) => {
    const doc = ctx.get(url);
    const title = pageTitle(ctx, doc);

    let chapters = extractChapters(ctx, doc, url);
    // A single story page used directly as a novel: treat it as one chapter.
    if (chapters.length === 0 && STORY_PATH.test(url)) {
      chapters = [{ title: title, url: url }];
    }

    return {
      novel: {
        title: title,
        author: authorName(ctx, doc),
        // Literotica serves a site-wide generic social image that is not
        // specific to the series, so no cover URL is extracted.
        description: seriesDescription(ctx, doc).trim(),
        coverUrl: "",
        language: "",
        tags: []
      },
      chapters: chapters
    };
  },

  chapter: (ctx, url) => {
    let doc = ctx.get(url);
    const title = pageTitle(ctx, doc);
    let content = storyContent(ctx, doc);

    const visited = new Set([url]);
    let current = url;
    for (;;) {
      const next = nextPageURL(ctx, doc, current);
      if (next === "" || visited.has(next)) break;
      visited.add(next);
      try {
        doc = ctx.get(next);
      } catch (e) {
        break;
      }
      const pageContent = storyContent(ctx, doc);
      if (pageContent !== "") content = content.trim() + "\n" + pageContent;
      current = next;
    }

    return { title: title, contentHtml: content };
  }
};
