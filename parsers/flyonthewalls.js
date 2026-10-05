// flyonthewalls.blog — the Fictioneer WordPress theme, so the floraegarden
// parser's helpers apply: same chapter-group list, story-id discovery and an
// RSS chapter feed. Plain HTTP clients get a "Checking your browser..."
// challenge, so fetching must go through the browser worker.
const BASE = "https://flyonthewalls.blog";
const CHAPTER_RE = /flyonthewalls\.blog\/story\/[a-z0-9-]+\/[a-z0-9-]+\/?$/;

function metaContent(ctx, doc, selector) {
  const n = ctx.css1(doc, selector);
  if (!n) return "";
  const c = n.attr("content");
  return c === null ? "" : c.trim();
}

function titleCase(s) {
  return s
    .split(/[-_]/)
    .filter((w) => w !== "")
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(" ");
}

function authorOf(ctx, doc) {
  const a = ctx.css1(doc, ".story__identity-meta a.author");
  if (a && a.text) return a.text;
  const em = ctx.css1(doc, "em.chapter__author");
  if (em) {
    const text = em.text.replace(/^by /, "");
    if (text) return text;
  }
  const authorURL = metaContent(ctx, doc, "meta[property='article:author']");
  if (authorURL) {
    const i = authorURL.lastIndexOf("/author/");
    if (i >= 0) {
      const slug = authorURL.slice(i + 8).replace(/\/$/, "");
      const name = titleCase(slug);
      if (name) return name;
    }
  }
  return "";
}

function descriptionOf(ctx, doc) {
  const summary = ctx.css1(doc, "section.story__summary");
  if (summary) {
    const parts = [];
    for (const p of ctx.css(summary, "p")) {
      if (p.text) parts.push(p.text);
    }
    const joined = parts.join("\n\n");
    if (joined) return joined;
  }
  return (
    metaContent(ctx, doc, "meta[property='og:description']") ||
    metaContent(ctx, doc, "meta[name='description']")
  );
}

function coverOf(ctx, doc) {
  const og = metaContent(ctx, doc, "meta[property='og:image']");
  if (og) return og;
  const tw = metaContent(ctx, doc, "meta[name='twitter:image']");
  if (tw) return tw;
  const img = ctx.css1(doc, ".custom-cover-container img");
  if (img) {
    const src = img.attr("src");
    if (src) return src;
  }
  for (const i of ctx.css(doc, "img")) {
    let src = i.attr("src");
    if (src === null) src = i.attr("data-src");
    if (!src) continue;
    if (src.indexOf("wp-content/uploads") >= 0 && /\.(webp|jpg|jpeg|png)$/.test(src)) return src;
  }
  return "";
}

function extractChapters(ctx, doc) {
  const chapters = [];
  const seen = new Set();

  const add = (href, title) => {
    let url = href;
    if (url.indexOf("http") !== 0) url = BASE + url;
    url = url.replace(/\/$/, "");
    if (seen.has(url)) return;
    seen.add(url);
    chapters.push({ title: (title || "").trim(), url: url });
  };

  for (const li of ctx.css(doc, "li.chapter-group__list-item")) {
    // Premium/locked chapters are not downloadable.
    if (li.attr("class") !== null && li.attr("class").split(/\s+/).indexOf("_premium") >= 0) continue;
    const link = ctx.css1(li, "a.chapter-group__list-item-link");
    if (!link) continue;
    const href = link.attr("href");
    if (href !== null) add(href, link.text);
  }

  if (chapters.length === 0) {
    for (const a of ctx.css(doc, "a")) {
      const href = a.attr("href");
      if (href === null || !CHAPTER_RE.test(href)) continue;
      add(href, a.text);
    }
  }

  return chapters;
}

function storyID(ctx, doc) {
  const body = ctx.css1(doc, "body");
  if (body) {
    const id = body.attr("data-story-id");
    if (id !== null && id !== "") return id;
  }
  for (const n of ctx.css(doc, "[data-story-id]")) {
    const id = n.attr("data-story-id");
    if (id !== null && id !== "") return id;
  }
  for (const n of ctx.css(doc, "link[type='application/rss+xml']")) {
    const href = n.attr("href");
    if (href === null) continue;
    const i = href.indexOf("story_id=");
    if (i < 0) continue;
    let id = href.slice(i + 9);
    const amp = id.indexOf("&");
    if (amp >= 0) id = id.slice(0, amp);
    return id;
  }
  return "";
}

function unescapeXML(s) {
  return s
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&apos;/g, "'")
    .replace(/&#(\d+);/g, (_, d) => String.fromCodePoint(parseInt(d, 10)))
    .replace(/&#x([0-9a-fA-F]+);/g, (_, h) => String.fromCodePoint(parseInt(h, 16)))
    .replace(/&amp;/g, "&");
}

// The feed is XML and must not go through the HTML parser: an HTML parser
// treats <link> as a void element and silently drops every chapter URL.
function chaptersFromRSS(ctx, id) {
  const body = ctx.get(BASE + "/feed/rss-chapters?story_id=" + id).body;
  const chapters = [];
  for (const item of body.match(/<item>[\s\S]*?<\/item>/g) || []) {
    const link = /<link>([\s\S]*?)<\/link>/.exec(item);
    if (!link) continue;
    const url = link[1].trim();
    if (url === "") continue;
    const title = /<title>([\s\S]*?)<\/title>/.exec(item);
    chapters.push({ title: title ? unescapeXML(title[1]).trim() : "", url: url });
  }
  return chapters;
}

function chapterRefs(ctx, doc) {
  const chapters = extractChapters(ctx, doc);
  if (chapters.length > 0) return chapters;
  const id = storyID(ctx, doc);
  if (id !== "") return chaptersFromRSS(ctx, id);
  return [];
}

function paragraphsOf(ctx, sel) {
  const out = [];
  for (const p of ctx.css(sel, "p")) {
    if (p.text) out.push("<p>" + p.text + "</p>");
  }
  return out;
}

// Strict host check: only this site's domain (and its subdomains) is
// claimed, never a URL that merely mentions the domain in a query string.
function isSiteHost(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^\/?#]*)/i.exec(u || "");
  if (!m) return false;
  const h = m[1].toLowerCase().replace(/^www\./, "");
  return h === "flyonthewalls.blog" || h.endsWith(".flyonthewalls.blog");
}

module.exports = {
  name: "flyonthewalls",
  apiVersion: 1,
  requiresBrowser: true,

  probe: isSiteHost,

  toc: (ctx, url) => {
    const doc = ctx.get(url);

    let title = metaContent(ctx, doc, "meta[property='og:title']");
    if (!title) {
      const h = ctx.css1(doc, "h1.story__identity-title");
      if (h && h.text) title = h.text;
    }
    if (!title) {
      const h = ctx.css1(doc, "h1");
      if (h) title = h.text;
    }

    return {
      novel: {
        title: title.trim(),
        author: authorOf(ctx, doc).trim(),
        description: descriptionOf(ctx, doc).trim(),
        coverUrl: coverOf(ctx, doc),
        language: "",
        tags: []
      },
      chapters: chapterRefs(ctx, doc)
    };
  },

  chapter: (ctx, url) => {
    const doc = ctx.get(url);

    let title = "";
    const h = ctx.css1(doc, "h1.chapter__title");
    if (h && h.text) title = h.text;
    if (!title) title = metaContent(ctx, doc, "meta[property='og:title']");
    if (!title) {
      const f = ctx.css1(doc, "h1");
      if (f) title = f.text;
    }

    let sel =
      ctx.css1(doc, "#chapter-content") ||
      ctx.css1(doc, ".chapter__content") ||
      ctx.css1(doc, "section.chapter__content") ||
      ctx.css1(doc, "[data-fictioneer-chapter-target='content']");
    if (!sel) ctx.fail("site_layout_changed", "no chapter content container at " + url);

    for (const n of ctx.css(sel, "script, style, noscript, iframe, nav, header, footer")) n.remove();
    for (const n of ctx.css(sel, ".chapter-group__list-item-checkmark, .only-logged-in")) n.remove();
    for (const n of ctx.css(sel, "[style*='display:none'], [style*='display: none']")) n.remove();

    let parts = paragraphsOf(ctx, sel);
    if (parts.length === 0) {
      // Last resort: the container's text, one paragraph per line.
      for (const line of sel.text.split("\n")) {
        const t = line.trim();
        if (t) parts.push("<p>" + t + "</p>");
      }
    }

    return { title: title.trim(), contentHtml: parts.join("\n") };
  }
};
