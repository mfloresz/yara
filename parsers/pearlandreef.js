// pearlandreef.com — JormunTL WordPress theme. The full chapter list ships in
// the initial novel HTML (#novel-toc-list .chapter-item-improved, in reading
// order) and each chapter body lives in div.chapter-content. Direct fetches
// return 200 with everything inline, so no browser worker is needed.
function hostOf(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(u || "");
  return m ? m[1].toLowerCase().replace(/^www\./, "") : "";
}

function pathOf(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/[^/?#]*(\/[^?#]*)/i.exec(u || "");
  return m ? m[1] : "/";
}

// { slug, chapter } — chapter is "" for the novel info page.
function parsePath(rawURL) {
  if (hostOf(rawURL) !== "pearlandreef.com") return null;
  const novel = /^\/novels\/([^/]+)\/?$/.exec(pathOf(rawURL));
  if (novel) return { slug: novel[1], chapter: "" };
  const chapter = /^\/novels\/([^/]+)\/([^/]+)\/?$/.exec(pathOf(rawURL));
  if (chapter) return { slug: chapter[1], chapter: chapter[2] };
  return null;
}

function metaContent(ctx, doc, selector) {
  const n = ctx.css1(doc, selector);
  if (!n) return "";
  const c = n.attr("content");
  return c === null ? "" : c.trim();
}

function languageOf(doc) {
  // The theme renders no language <meta>; the value sits next to the
  // "Original language" label, so read it from the raw HTML.
  const m = /Original language<\/span>\s*<span[^>]*>([^<]+)</.exec(doc.body || "");
  return m ? m[1].trim() : "";
}

function descriptionOf(ctx, doc) {
  const synopsis = ctx.css1(doc, ".jt-hero-synopsis");
  if (synopsis) {
    const parts = [];
    for (const p of ctx.css(synopsis, "p")) {
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
  const hero = ctx.css1(doc, ".jt-novel-cover-col img");
  if (hero) {
    const src = hero.attr("src");
    if (src) return src;
  }
  return (
    metaContent(ctx, doc, "meta[property='og:image']") ||
    metaContent(ctx, doc, "meta[name='twitter:image']")
  );
}

function paragraphsOf(ctx, sel) {
  const out = [];
  for (const p of ctx.css(sel, "p")) {
    // The theme pads chapters with empty <p class="wp-block-paragraph"></p>
    // spacers; only paragraphs carrying text are story content.
    if (p.text) out.push("<p>" + p.text + "</p>");
  }
  return out;
}

module.exports = {
  name: "pearlandreef",
  apiVersion: 1,
  requiresBrowser: false,

  probe: (url) => parsePath(url) !== null,

  toc: (ctx, url) => {
    const parts = parsePath(url);
    if (!parts) ctx.fail("not_my_site", "invalid pearlandreef URL: " + url);
    if (parts.chapter !== "") ctx.fail("not_my_site", "not a novel page URL: " + url);

    const doc = ctx.get(url);

    let title = "";
    const h = ctx.css1(doc, "h1");
    if (h && h.text) title = h.text;
    if (!title) title = metaContent(ctx, doc, "meta[property='og:title']");
    if (!title) ctx.fail("site_layout_changed", "no novel title at " + url);

    const chapters = [];
    const seen = {};
    for (const a of ctx.css(doc, "#novel-toc-list .chapter-item-improved a")) {
      const href = a.attr("href");
      if (href === null || href === "") continue;
      const key = href.replace(/\/$/, "");
      if (seen[key]) continue;
      seen[key] = true;
      const t = ctx.css1(a, "h4");
      chapters.push({ title: t ? t.text.trim() : a.text.trim(), url: href });
    }
    if (chapters.length === 0) ctx.fail("site_layout_changed", "no chapter list at " + url);

    const tocNode = ctx.css1(doc, "#novel-toc");
    if (tocNode) {
      const total = parseInt(tocNode.attr("data-total") || "0", 10);
      if (total > chapters.length) {
        ctx.log("pearlandreef: page lists " + chapters.length + " of " + total + " chapters; the rest need JS pagination");
      }
    }

    const tags = [];
    for (const g of ctx.css(doc, "a.genre-tag")) {
      if (g.text) tags.push(g.text);
    }

    return {
      novel: {
        title: title.trim(),
        author: "",
        description: descriptionOf(ctx, doc).trim(),
        coverUrl: coverOf(ctx, doc),
        language: languageOf(doc),
        tags: tags,
      },
      chapters: chapters,
    };
  },

  chapter: (ctx, url) => {
    const parts = parsePath(url);
    if (!parts) ctx.fail("not_my_site", "invalid pearlandreef URL: " + url);
    if (parts.chapter === "") ctx.fail("not_my_site", "not a chapter URL: " + url);

    const doc = ctx.get(url);

    let title = "";
    const h = ctx.css1(doc, "h1");
    if (h && h.text) title = h.text;
    if (!title) title = metaContent(ctx, doc, "meta[property='og:title']");

    const sel = ctx.css1(doc, "div.chapter-content");
    if (!sel) ctx.fail("site_layout_changed", "no chapter content container at " + url);

    for (const n of ctx.css(sel, "script, style, noscript, iframe")) n.remove();

    let partsHtml = paragraphsOf(ctx, sel);
    if (partsHtml.length === 0) {
      for (const line of sel.text.split("\n")) {
        const t = line.trim();
        if (t) partsHtml.push("<p>" + t + "</p>");
      }
    }
    if (partsHtml.length === 0) ctx.fail("site_layout_changed", "empty chapter content at " + url);

    return { title: title.trim(), contentHtml: partsHtml.join("\n") };
  },
};
