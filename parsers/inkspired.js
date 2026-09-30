// getinkspired.com — Cloudflare-protected (403 to plain HTTP), so pages
// arrive through the browser worker. Everything needed is server-rendered as
// JSON-LD plus plain HTML.
const clean = (s) => (s || "").replace(/\s+/g, " ").trim();

function host(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(u);
  return m ? m[1].toLowerCase().replace(/^www\./, "") : "";
}

function pathOf(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/[^/?#]*(\/[^?#]*)/i.exec(u);
  return m ? m[1] : "/";
}

// /{lang}/story/{id}/chapter/{slug}/ — the language segment is optional
// because the site's own share links drop it.
const CHAPTER_PATH = /^\/(?:[a-z]{2}\/)?story\/(\d+)\/chapter\/[^/]+\/?$/;
const STORY_PATH = /^\/(?:[a-z]{2}\/)?story\/(\d+)(?:\/[^/]+)?\/?$/;

function parseURL(rawURL) {
  if (host(rawURL) !== "getinkspired.com") return null;
  const path = pathOf(rawURL);
  const chapter = CHAPTER_PATH.exec(path);
  if (chapter) return { storyID: chapter[1], kind: "chapter" };
  // A /chapter/ segment with no chapter slug is a malformed reading URL, not a
  // story page whose slug happens to be "chapter".
  if (path.indexOf("/chapter/") >= 0) return null;
  const story = STORY_PATH.exec(path);
  if (story) return { storyID: story[1], kind: "story" };
  return null;
}

function metaContent(ctx, doc, selector) {
  const n = ctx.css1(doc, selector);
  if (!n) return "";
  const c = n.attr("content");
  return c === null ? "" : c.trim();
}

// Malformed blocks are skipped rather than failing the parse, so a site-side
// schema change degrades to the HTML fallbacks instead of erroring.
function findJSONLD(ctx, doc, jsonType) {
  for (const s of ctx.css(doc, "script[type='application/ld+json']")) {
    const raw = s.text.trim();
    if (raw === "") continue;
    let probe;
    try {
      probe = JSON.parse(raw);
    } catch (e) {
      continue;
    }
    if (!probe || probe["@type"] !== jsonType) continue;
    return probe;
  }
  return null;
}

function chaptersFromBook(book) {
  // Titles in the Book JSON-LD are already clean (no "1.- " position prefix).
  const chapters = [];
  for (const part of book.hasPart || []) {
    const url = (part.url || "").trim();
    if (url === "") continue;
    let title = clean(part.name);
    if (title === "") title = "Chapter " + (chapters.length + 1);
    chapters.push({ title: title, url: url });
  }
  return chapters;
}

// The table of contents the story page renders inside #chapterModal. The list's
// own "1.- " position prefix duplicates the order the parser assigns.
function chaptersFromModal(ctx, doc, pageURL) {
  const chapters = [];
  const seen = new Set();
  for (const a of ctx.css(doc, "#chapterModal a.reader-chapter-link")) {
    const href = (a.attr("href") || "").trim();
    if (href === "") continue;
    const url = ctx.resolveUrl(pageURL, href);
    if (seen.has(url)) continue;
    seen.add(url);
    let title = clean(a.text).replace(/^\d+\.\s*-\s*/, "");
    if (title === "") title = "Chapter " + (chapters.length + 1);
    chapters.push({ title: title, url: url });
  }
  return chapters;
}

// Selecting per-paragraph keeps the comment bubbles, scripts and the paginated
// reader out of the result, because those all live outside div.paragraph.
function chapterHTML(ctx, doc) {
  let sel = ctx.css1(doc, "#chapter_block div[id^='chapter-scroll-content-']");
  if (!sel) sel = ctx.css1(doc, "div[id^='chapter-scroll-content-']");
  if (!sel) return "";

  const parts = [];
  const append = (html, text) => {
    if (text.trim() === "") return;
    parts.push("<p>" + html.trim() + "</p>");
  };

  for (const para of ctx.css(sel, ".paragraph")) {
    const paragraphs = ctx.css(para, "p");
    if (paragraphs.length > 0) {
      for (const p of paragraphs) append(p.html, p.text);
      continue;
    }
    // Some chapters separate paragraphs with <br> instead of <p>.
    append(para.html, para.text);
  }
  return parts.join("\n");
}

function htmlTitle(ctx, doc) {
  const h = ctx.css1(doc, "h1.crx-h2");
  if (h && h.text) return clean(h.text);
  const og = metaContent(ctx, doc, "meta[property='og:title']").replace(/^Inkspired - /, "");
  if (clean(og) !== "") return clean(og);
  const t = ctx.css1(doc, "title");
  return t ? clean(clean(t.text).replace(/ \| Inkspired$/, "")) : "";
}

// capitulo-1-huracan-2647835 -> "capitulo 1 huracan"
function chapterSlugTitle(chapterURL) {
  const path = pathOf(chapterURL);
  const parts = path.replace(/^\/+|\/+$/g, "").split("/");
  if (parts.length === 0) return "";
  const slug = parts[parts.length - 1].replace(/-\d+$/, "");
  return clean(slug.split("-").join(" "));
}

module.exports = {
  name: "inkspired",
  apiVersion: 1,
  requiresBrowser: true,

  probe: (url) => parseURL(url) !== null,

  toc: (ctx, url) => {
    if (!parseURL(url)) ctx.fail("not_my_site", "invalid inkspired URL: " + url);

    let doc = ctx.get(url);
    let storyURL = url;
    let book = findJSONLD(ctx, doc, "Book");

    if (!book) {
      // A reading page carries no Book block; partOf points at the story.
      const chapter = findJSONLD(ctx, doc, "Chapter");
      if (chapter && chapter.partOf && chapter.partOf.url) {
        storyURL = chapter.partOf.url;
        doc = ctx.get(storyURL);
        book = findJSONLD(ctx, doc, "Book");
      }
    }

    let title = "";
    let author = "";
    let description = "";
    let chapters = [];
    let sourceURL = storyURL;

    if (book) {
      sourceURL = (book.url || "").trim() || storyURL;
      title = clean(book.name);
      author = book.author ? (book.author.name || "").trim() : "";
      description = (book.description || "").trim();
      chapters = chaptersFromBook(book);
    }

    // HTML fallbacks: the JSON-LD block carries no cover, and a schema change
    // could leave the other fields empty even though the page renders them.
    const coverUrl = metaContent(ctx, doc, "meta[property='og:image']");
    if (title === "") title = htmlTitle(ctx, doc);
    if (author === "") {
      const n = ctx.css1(doc, "a.crx-authorrow .crx-authorrow-name");
      if (n) author = n.text.trim();
    }
    // The rendered blurb is the full synopsis; the JSON-LD and og:description
    // copies of it are capped. Scoped to the page header because
    // .formating-space is also used for the reader comments further down.
    const blurb = ctx.css1(doc, "header span.formating-space");
    if (blurb && blurb.text.trim() !== "") description = blurb.text.trim();
    if (description === "") description = metaContent(ctx, doc, "meta[property='og:description']");
    if (chapters.length === 0) chapters = chaptersFromModal(ctx, doc, sourceURL);

    if (title === "") ctx.fail("site_layout_changed", "no story metadata found at " + url);

    return {
      novel: {
        title: title,
        author: author,
        description: description,
        coverUrl: coverUrl,
        language: "",
        tags: []
      },
      chapters: chapters
    };
  },

  chapter: (ctx, url) => {
    const parsed = parseURL(url);
    if (!parsed || parsed.kind !== "chapter") ctx.fail("not_my_site", "not an inkspired chapter URL: " + url);

    const doc = ctx.get(url);

    const h = ctx.css1(doc, "#chapter_block h2.content_chapter_title_reader");
    let title = h ? clean(h.text) : "";
    if (title === "") title = chapterSlugTitle(url);

    const content = chapterHTML(ctx, doc);
    if (content.trim() === "") ctx.fail("site_layout_changed", "inkspired chapter page has no content: " + url);

    return { title: title, contentHtml: content };
  }
};
