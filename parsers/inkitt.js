// inkitt.com — the public JSON API the LNReader plugin uses:
// /api/stories/{id} for metadata and the full chapter list, plus the story
// page HTML for the author and the summary (the API carries no summary).
// Chapters past the free preview are served folded to anonymous readers, so a
// logged-in browser session is required to unfold them.
const clean = (s) => (s || "").replace(/\s+/g, " ").trim();

function host(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(u);
  return m ? m[1].toLowerCase().replace(/^www\./, "") : "";
}

function baseURL(u) {
  const m = /^(https?:\/\/[^/?#]+)/i.exec(u);
  return m ? m[1] : "";
}

function pathOf(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/[^/?#]*(\/[^?#]*)/i.exec(u);
  return m ? m[1] : "/";
}

// /stories/{id} with an optional genre segment or slug suffix.
const STORY_PATH = /^\/stories\/(?:[a-z0-9-]+\/)?(\d+)\/?$/;
// A reading page /stories/{id}/chapters/{n}.
const CHAPTER_PATH = /^\/stories\/\d+\/chapters\/(\d+)\/?$/;

function parseURL(rawURL) {
  if (host(rawURL) !== "inkitt.com") return null;
  const path = pathOf(rawURL);
  const chapter = CHAPTER_PATH.exec(path);
  if (chapter) {
    const parts = path.replace(/^\/+|\/+$/g, "").split("/");
    if (parts.length === 4 && /^\d+$/.test(parts[1])) {
      return { storyID: parts[1], kind: "chapter", chapter: chapter[1] };
    }
    return null;
  }
  const story = STORY_PATH.exec(path);
  if (story) return { storyID: story[1], kind: "story", chapter: "0" };
  return null;
}

function chapterNumber(rawURL) {
  const m = CHAPTER_PATH.exec(pathOf(rawURL));
  return m ? parseInt(m[1], 10) : 0;
}

function metaContent(ctx, doc, selector) {
  const n = ctx.css1(doc, selector);
  if (!n) return "";
  const c = n.attr("content");
  return c === null ? "" : c.trim();
}

function authorOf(ctx, doc) {
  const a = ctx.css1(doc, "#storyAuthor");
  if (a && a.text) return a.text;
  const l = ctx.css1(doc, "a.author-link");
  if (l && l.text) return l.text;
  return metaContent(ctx, doc, "meta[name='author']");
}

function summaryOf(ctx, doc) {
  const s = ctx.css1(doc, "p.story-summary");
  if (s && s.text) return s.text;
  return metaContent(ctx, doc, "meta[property='og:description']");
}

function extractChapters(ctx, doc, pageURL) {
  const chapters = [];
  const seen = new Set();
  for (const a of ctx.css(doc, "a.chapter-link")) {
    const href = a.attr("href");
    if (href === null || href === "") continue;
    const url = ctx.resolveUrl(pageURL, href);
    if (seen.has(url)) continue;
    seen.add(url);

    const t = ctx.css1(a, "span.chapter-title");
    let title = t && t.text ? t.text : a.text;
    let order = chapterNumber(url);
    if (order <= 0) order = chapters.length + 1;
    chapters.push({ title: clean(title), url: url, order: order });
  }
  return chapters.map((c) => ({ title: c.title, url: c.url }));
}

// Preserving the inline markup keeps the emphasis the source carries.
function chapterContent(ctx, doc) {
  const sel = ctx.css1(doc, "div#chapterText");
  if (!sel) return "";

  for (const n of ctx.css(sel, "script, style, noscript, iframe")) n.remove();

  const parts = [];
  for (const p of ctx.css(sel, "p")) {
    if (p.text) parts.push("<p>" + p.html.trim() + "</p>");
  }
  if (parts.length > 0) return parts.join("\n");

  // Fallback: some chapters carry no <p> tags and separate paragraphs with
  // <br><br> instead.
  for (const chunk of sel.html.split(/<br\s*\/?>/i)) {
    const c = chunk.trim();
    if (c === "") continue;
    const visible = c.replace(/<[^>]+>/g, "").split("&nbsp;").join("").trim();
    if (visible === "") continue;
    parts.push("<p>" + c + "</p>");
  }
  return parts.join("\n");
}

module.exports = {
  name: "inkitt",
  apiVersion: 1,
  requiresBrowser: true,

  probe: (url) => parseURL(url) !== null,

  toc: (ctx, url) => {
    const parsed = parseURL(url);
    if (!parsed) ctx.fail("not_my_site", "invalid inkitt URL: " + url);

    const base = baseURL(url);
    const story = JSON.parse(ctx.get(base + "/api/stories/" + parsed.storyID).body);
    if (!story.title) ctx.fail("site_layout_changed", "story " + parsed.storyID + " not found or has no title");

    const storyURL = base + "/stories/" + story.id;
    let chapters = (story.chapters || [])
      .filter((ch) => ch.chapter_number > 0)
      .map((ch) => ({
        title: clean(ch.name),
        url: base + "/stories/" + story.id + "/chapters/" + ch.chapter_number
      }));

    // The API does not always carry chapters (e.g. drafts), so the story page
    // also supplies the author and the summary.
    let author = story.user ? story.user.name || "" : "";
    let summary = "";
    let doc;
    try {
      doc = ctx.get(storyURL);
    } catch (e) {
      doc = null;
    }
    if (doc) {
      if (chapters.length === 0) chapters = extractChapters(ctx, doc, storyURL);
      const a = authorOf(ctx, doc);
      if (a) author = a;
      summary = summaryOf(ctx, doc);
    } else if (chapters.length === 0) {
      ctx.fail("site_layout_changed", "cannot fetch the story page at " + storyURL);
    }

    return {
      novel: {
        title: clean(story.title),
        author: author.trim(),
        description: summary.trim(),
        coverUrl: (story.vertical_cover && story.vertical_cover.url) || story.cover_url || "",
        language: "",
        tags: []
      },
      chapters: chapters
    };
  },

  chapter: (ctx, url) => {
    const parsed = parseURL(url);
    if (!parsed || parsed.kind !== "chapter") ctx.fail("not_my_site", "not an inkitt chapter URL: " + url);

    const doc = ctx.get(url);

    let title = "";
    const t = ctx.css1(doc, "h2.chapter-head-title");
    if (t) title = t.text;
    if (!title && parsed.chapter !== "0") title = "Chapter " + parseInt(parsed.chapter, 10);

    const content = chapterContent(ctx, doc);
    if (content.trim() === "") {
      ctx.fail(
        "blocked",
        "story " + parsed.storyID + " chapter page is folded (only the free preview is public): " +
          "connect the browser worker and log in to inkitt.com in that browser, then retry"
      );
    }

    return { title: clean(title), contentHtml: content };
  }
};
