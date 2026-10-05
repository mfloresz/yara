// foxaholic.com (incl. 18.foxaholic.com) — WordPress "Madara"/wp-manga theme.
// Only free chapters are downloaded: premium entries carry class "premium"
// and their link is "#", so they are skipped at TOC time; when the site
// later unlocks one it gains a real URL and shows up as a new chapter.
const HOST_RE = /(^|\.)foxaholic\.com$/i;

function isSiteURL(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^\/?#]+)([^?#]*)/i.exec(u || "");
  if (!m) return false;
  const host = m[1].toLowerCase().replace(/^www\./, "");
  return HOST_RE.test(host) && /^\/novel\/[^\/]+/.test(m[2]);
}

function metaContent(ctx, doc, selector) {
  const n = ctx.css1(doc, selector);
  if (!n) return "";
  const c = n.attr("content");
  return c === null ? "" : c.trim();
}

// The listing renders newest-first and repeats entries in the "Free" tab
// panel, so: document order + dedupe by URL, then reverse into reading order.
function extractChapters(ctx, doc) {
  const chapters = [];
  const seen = new Set();

  for (const li of ctx.css(doc, "li.wp-manga-chapter")) {
    const cls = li.attr("class");
    if (cls !== null && cls.split(/\s+/).indexOf("premium") >= 0) continue;
    const link = ctx.css1(li, "a");
    if (!link) continue;
    const href = link.attr("href");
    if (href === null || href === "" || href === "#") continue;
    if (seen.has(href)) continue;
    seen.add(href);
    const title = (link.text || "").trim();
    chapters.push({ title: title, url: href });
  }

  chapters.reverse();
  return chapters;
}

function paragraphsOf(ctx, container) {
  const out = [];
  for (const p of ctx.css(container, "p")) {
    let html = p.html;
    if (html === null) continue;
    html = html.replace(/<\/?span[^>]*>/g, "").trim();
    if (html === "" || html === "&nbsp;" || html === "\u00a0") continue;
    out.push("<p>" + html + "</p>");
  }
  return out;
}

module.exports = {
  name: "foxaholic",
  apiVersion: 1,
  requiresBrowser: true,

  probe: isSiteURL,

  toc: (ctx, url) => {
    const doc = ctx.get(url);

    let title = metaContent(ctx, doc, "meta[property='og:title']");
    const h = ctx.css1(doc, "h1");
    if (h && h.text) title = h.text;
    if (!title) ctx.fail("site_layout_changed", "no novel title at " + url);

    const tags = [];
    for (const a of ctx.css(doc, ".genres-content a")) {
      const t = (a.text || "").trim();
      if (t) tags.push(t);
    }

    let description = metaContent(ctx, doc, "meta[property='og:description']");
    if (!description) {
      const s = ctx.css1(doc, ".summary__content");
      if (s) description = s.text;
    }

    const lis = ctx.css(doc, "li.wp-manga-chapter");
    if (lis.length === 0) {
      ctx.fail("site_layout_changed", "no chapter list at " + url);
    }
    const chapters = extractChapters(ctx, doc);
    if (chapters.length === 0) {
      ctx.log("all " + lis.length + " listed chapters are premium; nothing free to download");
    }

    return {
      novel: {
        title: title.trim(),
        author: (function () {
          const a = ctx.css1(doc, ".author-content");
          return a ? a.text.trim() : "";
        })(),
        description: (description || "").trim(),
        coverUrl: metaContent(ctx, doc, "meta[property='og:image']"),
        language: "",
        tags: tags,
      },
      chapters: chapters,
    };
  },

  chapter: (ctx, url) => {
    const doc = ctx.get(url);

    let title = "";
    const h = ctx.css1(doc, "h1");
    if (h && h.text) {
      // h1 is "Novel Name - 6.2"; the TOC already carries the short number.
      title = h.text.trim().replace(/^.*\s-\s/, "");
    }
    if (!title) title = metaContent(ctx, doc, "meta[property='og:title']");

    const body =
      ctx.css1(doc, ".reading-content .text-left") ||
      ctx.css1(doc, ".reading-content");
    if (!body) ctx.fail("site_layout_changed", "no chapter content at " + url);

    for (const n of ctx.css(body, "script, style, noscript, iframe, ins, form")) n.remove();
    for (const n of ctx.css(body, ".chapter-warning, .google-auto-placed, .adsbygoogle, .sharedaddy")) n.remove();
    for (const n of ctx.css(body, "div[id^='foxah'], div[class^='foxah-']")) n.remove();

    const parts = paragraphsOf(ctx, body);
    if (parts.length === 0) {
      ctx.fail("site_layout_changed", "no paragraphs in chapter content at " + url);
    }

    return { title: title.trim(), contentHtml: parts.join("\n") };
  },
};
