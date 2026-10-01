// chrysanthemumgarden.com — a WordPress site whose chapter bodies are guarded
// by the "cg-scrape-protection" plugin: random junk spans/paragraphs/headings
// are hidden with inline "height:1px … overflow:hidden" styles and carry random
// alphanumeric noise or a watermark. Those are dropped here.
//
// HYBRID PARSER: the plugin also renders real prose through per-page
// obfuscation fonts (52-letter Open Sans subsets with permuted glyph
// assignments). This script cannot decode them — ctx.get hands scripts a
// UTF-8 string and goja has no Brotli — so the server does it: the host-side
// helper in internal/api/parser_cgfont.go rewrites every protected span to
// its true letters before this script sees the HTML. If the protection is
// present but undecodable, the fetch fails loudly and the chapter is never
// stored with scrambled text.
const clean = (s) => (s || "").replace(/\s+/g, " ").trim();

function host(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(u || "");
  return m ? m[1].toLowerCase().replace(/^www\./, "") : "";
}

function pathOf(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/[^/?#]*(\/[^?#]*)/i.exec(u || "");
  return m ? m[1] : "/";
}

function metaContent(ctx, doc, selector) {
  const n = ctx.css1(doc, selector);
  if (!n) return "";
  const c = n.attr("content");
  return c === null ? "" : c.trim();
}

function isHiddenNoise(node) {
  const style = node.attr("style");
  return style !== null && /height\s*:\s*1px/.test(style.toLowerCase());
}

function titleOf(ctx, doc) {
  const h = ctx.css1(doc, "h1.novel-title");
  if (h) {
    // The h1 wraps the real title around a raw-title span; dropping the
    // children leaves the node's own text.
    for (const n of ctx.css(h, "*")) n.remove();
    if (h.text) return h.text;
  }
  const e = ctx.css1(doc, "h1.entry-title");
  if (e && e.text) return e.text;
  const og = metaContent(ctx, doc, "meta[property='og:title']");
  if (og) return og.replace(/ - Chrysanthemum Garden$/, "").trim();
  return "";
}

function authorOf(ctx, doc) {
  const article = ctx.css1(doc, "article");
  if (!article) return "";
  const html = article.html;
  const re = /(Author|Translators?)\s*:\s*([^<]+)/gi;
  let author = "";
  let translator = "";
  let m;
  while ((m = re.exec(html)) !== null) {
    const value = m[2].trim();
    if (m[1].toLowerCase().indexOf("author") === 0) {
      if (!author) author = value;
    } else if (!translator) {
      translator = value;
    }
  }
  return author || translator;
}

// The chapter list lives in a nested div, so only the entry-content's own
// paragraphs are the synopsis. The child combinator is anchored on the class,
// not left leading: cascadia silently matches nothing for a bare "> p".
function descriptionOf(ctx, doc) {
  const parts = [];
  for (const p of ctx.css(doc, ".entry-content > p")) {
    if (p.text) parts.push(p.text);
  }
  if (parts.length > 0) return parts.join("\n\n");
  return metaContent(ctx, doc, "meta[property='og:description']");
}

function chapterRefs(ctx, doc, pageURL) {
  const refs = [];
  for (const a of ctx.css(doc, "a.chapter-item")) {
    const href = a.attr("href");
    if (href === null || href.trim() === "") continue;
    const named = ctx.css1(a, ".chapter-item-name");
    let title = named ? named.text : "";
    if (!title) title = a.text;
    // The item text also carries "10 months ago • 1,337 words" details; the
    // chapter name is only the first line.
    const nl = title.indexOf("\n");
    if (nl >= 0) title = title.slice(0, nl);
    title = title.trim();
    if (title === "") continue;
    refs.push({ title: clean(title), url: ctx.resolveUrl(pageURL, href.trim()) });
  }
  return refs;
}

module.exports = {
  name: "chrysanthemumgarden",
  apiVersion: 1,
  requiresBrowser: false,

  probe: (url) => host(url) === "chrysanthemumgarden.com" && pathOf(url).toLowerCase().indexOf("/novel-tl/") === 0,

  toc: (ctx, url) => {
    const doc = ctx.get(url);
    return {
      novel: {
        title: titleOf(ctx, doc).trim(),
        author: authorOf(ctx, doc).trim(),
        description: descriptionOf(ctx, doc).trim(),
        coverUrl: metaContent(ctx, doc, "meta[property='og:image']"),
        language: "",
        tags: []
      },
      chapters: chapterRefs(ctx, doc, url)
    };
  },

  chapter: (ctx, url) => {
    const doc = ctx.get(url);

    let title = "";
    const t = ctx.css1(doc, ".chrys-post-title .chapter-title");
    if (t) title = t.text;
    if (!title) title = metaContent(ctx, doc, "meta[property='og:title']");
    if (!title) {
      const h = ctx.css1(doc, "h1");
      if (h) title = h.text;
    }

    const sel = ctx.css1(doc, "#novel-content") || ctx.css1(doc, ".entry-content");
    if (!sel) ctx.fail("site_layout_changed", "no chapter content container at " + url);

    // Drop the scrape-protection noise: hidden junk spans inside paragraphs,
    // watermark paragraphs and the hidden heading.
    for (const n of ctx.css(sel, "span, p, div, h1, h2, h3, h4")) {
      if (isHiddenNoise(n)) n.remove();
    }

    const parts = [];
    for (const p of ctx.css(sel, "p")) {
      if (p.text) parts.push("<p>" + p.text + "</p>");
    }

    return { title: clean(title), contentHtml: parts.join("\n") };
  }
};
