// gaydemon.com — story pages are server-rendered and fetch fine with a plain
// HTTP GET. The story body mixes <p> paragraphs with <h1>-<h6> POV/scene
// headings (the "Julian"/"Alexis" markers that switch narrators) and <hr> scene
// breaks, so the body is walked once in document order and switched on the
// element's nodeName rather than collected selector by selector.
const clean = (s) => (s || "").replace(/\s+/g, " ").trim();
const HEADINGS = ["h1", "h2", "h3", "h4", "h5", "h6"];
const BOILERPLATE = "To get in touch with the author";

function host(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(u);
  if (!m) return (u || "").indexOf("gaydemon.com") >= 0;
  const h = m[1].toLowerCase();
  return h === "gaydemon.com" || h.endsWith(".gaydemon.com");
}

function pathOf(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/[^/?#]*(\/[^?#]*)/i.exec(u);
  return m ? m[1] : "/";
}

function metaContent(ctx, doc, selector) {
  const n = ctx.css1(doc, selector);
  if (!n) return "";
  const c = n.attr("content");
  return c === null ? "" : c.trim();
}

function novelTitle(ctx, doc) {
  const t = ctx.css1(doc, "article#story h1[itemprop='name']");
  if (t && t.text) return clean(t.text);
  const h = ctx.css1(doc, "h1");
  if (h && h.text) return clean(h.text);
  return clean(metaContent(ctx, doc, "meta[property='og:title']"));
}

// The h1 holds the story title, identical on every chapter page, so it cannot
// be used as the chapter title. The toggle button embeds an
// <svg><title>Show all chapters</title></svg> whose text is dropped so it does
// not leak into the label.
function chapterLabel(ctx, doc) {
  const btn = ctx.css1(doc, "#chapters button.story-page-chapt");
  if (!btn) return "";
  for (const n of ctx.css(btn, "svg")) n.remove();
  return clean(btn.text);
}

function authorName(ctx, doc) {
  const a = ctx.css1(doc, "a.story-page-author");
  if (a && a.text) return a.text;
  return metaContent(ctx, doc, "meta[name='author']") || metaContent(ctx, doc, "meta[property='article:author']");
}

function novelDescription(ctx, doc) {
  let desc = "";
  const standfirst = ctx.css1(doc, "header.story-page p.textify");
  if (standfirst) desc = standfirst.text;
  if (desc === "") desc = metaContent(ctx, doc, "meta[name='description']");
  if (desc === "") desc = metaContent(ctx, doc, "meta[property='og:description']");

  const tags = [];
  for (const t of ctx.css(doc, "section.tags a.story-page-tag")) {
    if (t.text) tags.push(t.text);
  }
  if (tags.length > 0) desc += "\n\nTags: " + tags.join(", ");

  return desc.trim();
}

// The current page is a <span> (not a link) in the nav, so it resolves to the
// page URL.
function extractChapters(ctx, doc, pageURL) {
  const chapters = [];
  const seen = new Set();
  for (const li of ctx.css(doc, "nav#chapter-nav ul li")) {
    const a = ctx.css1(li, "a");
    if (a) {
      const href = a.attr("href");
      if (href === null || href === "") continue;
      const url = ctx.resolveUrl(pageURL, href);
      if (seen.has(url)) continue;
      seen.add(url);
      chapters.push({ title: clean(a.text), url: url });
      continue;
    }
    const span = ctx.css1(li, "span");
    if (span) {
      if (seen.has(pageURL)) continue;
      seen.add(pageURL);
      chapters.push({ title: clean(span.text), url: pageURL });
    }
  }
  return chapters;
}

function contentRoot(ctx, doc) {
  return ctx.css1(doc, "div.story-text[itemprop='articleBody']") || ctx.css1(doc, "div.textify.story-text");
}

function storyContent(ctx, doc) {
  const sel = contentRoot(ctx, doc);
  if (!sel) return "";
  for (const n of ctx.css(sel, "script, style, noscript, iframe, nav, header, footer")) n.remove();

  const BLOCKS = HEADINGS.concat(["p", "blockquote", "li", "pre", "hr", "div", "section", "article"]);
  const parts = [];
  const push = (tag, text) => {
    if (text === "" || text.indexOf(BOILERPLATE) === 0) return;
    parts.push("<" + tag + ">" + text + "</" + tag + ">");
  };

  // One pass in document order: a wrapper that holds blocks is skipped so its
  // own blocks are emitted in place; a wrapper without blocks is unwrapped
  // whole.
  for (const n of ctx.css(sel, BLOCKS.join(", "))) {
    const tag = n.nodeName;
    if (tag === "hr") {
      parts.push("<hr>");
    } else if (HEADINGS.indexOf(tag) >= 0) {
      push(tag, n.text);
    } else if (tag === "p" || tag === "blockquote" || tag === "li" || tag === "pre") {
      push("p", n.text);
    } else if (tag === "div" || tag === "section" || tag === "article") {
      if (ctx.css(n, "p, " + HEADINGS.join(", ") + ", blockquote, li").length > 0) continue;
      if (ctx.css(n, "div, section, article").length > 0) continue;
      push("p", n.text);
    }
  }

  if (parts.length === 0) {
    // Fallback for markup without element children (bare text nodes).
    for (const line of sel.text.split("\n")) {
      const t = line.trim();
      if (t !== "") parts.push("<p>" + t + "</p>");
    }
  }
  return parts.join("\n");
}

module.exports = {
  name: "gaydemon",
  apiVersion: 1,
  requiresBrowser: false,

  probe: (url) => host(url) && pathOf(url).toLowerCase().indexOf("/stories/") === 0,

  toc: (ctx, url) => {
    const doc = ctx.get(url);
    const title = novelTitle(ctx, doc);

    let chapters = extractChapters(ctx, doc, url);
    // A standalone story page without a chapter nav is a single chapter.
    if (chapters.length === 0 && storyContent(ctx, doc) !== "") {
      let chapterTitle = chapterLabel(ctx, doc);
      if (chapterTitle === "") chapterTitle = title;
      chapters = [{ title: chapterTitle, url: url }];
    }

    return {
      novel: {
        title: title,
        author: authorName(ctx, doc),
        description: novelDescription(ctx, doc),
        coverUrl: "",
        language: "",
        tags: []
      },
      chapters: chapters
    };
  },

  chapter: (ctx, url) => {
    const doc = ctx.get(url);
    let title = chapterLabel(ctx, doc);
    if (title === "") title = novelTitle(ctx, doc);
    return { title: title, contentHtml: storyContent(ctx, doc) };
  }
};
