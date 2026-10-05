// novelping.com — server-rendered Bootstrap site, no framework and no
// Cloudflare. The info page only ships the first 30 chapters in a <template>,
// so the full catalog comes from the same XHR the page uses when the reader
// opens the chapter-archive tab: GET /ajax/chapter-archive?novelId=<slug>,
// which answers with the complete list in one shot.
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

// { slug, chapter } — chapter is "" for the novel info page.
function parsePath(rawURL) {
  if (host(rawURL) !== "novelping.com") return null;
  const path = pathOf(rawURL);
  const novel = /^\/book\/([^/]+)\/?$/.exec(path);
  if (novel) return { slug: novel[1], chapter: "" };
  const chapter = /^\/book\/([^/]+)\/(chapter-[^/]+)\/?$/.exec(path);
  if (chapter) return { slug: chapter[1], chapter: chapter[2] };
  return null;
}

function metaContent(ctx, doc, property) {
  const n = ctx.css1(doc, 'meta[property="' + property + '"]');
  if (!n) return "";
  const c = n.attr("content");
  return c === null ? "" : clean(c);
}

function fetchChapters(ctx, pageURL, slug) {
  const url = baseURL(pageURL) + "/ajax/chapter-archive?novelId=" + encodeURIComponent(slug);
  const arch = ctx.get(url);
  const nodes = ctx.css(arch, "[data-chapter-item] a");
  const chapters = [];
  for (const a of nodes) {
    const title = ctx.css1(a, ".chapter-title");
    chapters.push({ title: clean(title ? title.text : a.text), url: a.href });
  }
  if (chapters.length === 0) ctx.fail("site_layout_changed", "no chapters in chapter-archive response for " + slug);
  return chapters;
}

module.exports = {
  name: "novelping",
  apiVersion: 1,
  requiresBrowser: false,

  probe: (url) => parsePath(url) !== null,

  toc: (ctx, url) => {
    const parts = parsePath(url);
    if (!parts) ctx.fail("not_my_site", "invalid novelping URL: " + url);
    if (parts.chapter !== "") ctx.fail("not_my_site", "not a novel page URL: " + url);

    const doc = ctx.get(url);

    const title = metaContent(ctx, doc, "og:novel:novel_name");
    if (!title) ctx.fail("site_layout_changed", "no novel title found on page");

    // The description meta is truncated with an ellipsis; the full synopsis is
    // in the collapsed block, one paragraph per beat.
    let description = "";
    const descBlock = ctx.css1(doc, "#novel-description-content");
    if (descBlock) {
      const paras = [];
      for (const p of ctx.css(descBlock, "p")) {
        if (p.text) paras.push(p.text);
      }
      if (paras.length > 0) description = paras.join("\n\n");
    }
    if (!description) description = metaContent(ctx, doc, "og:description");

    const tags = [];
    const genre = metaContent(ctx, doc, "og:novel:genre");
    if (genre) {
      for (const g of genre.split(",")) {
        const t = clean(g);
        if (t) tags.push(t);
      }
    }

    return {
      novel: {
        title: title,
        author: metaContent(ctx, doc, "og:novel:author"),
        description: description,
        coverUrl: metaContent(ctx, doc, "og:image"),
        language: "",
        tags: tags
      },
      chapters: fetchChapters(ctx, url, parts.slug)
    };
  },

  chapter: (ctx, url) => {
    const parts = parsePath(url);
    if (!parts) ctx.fail("not_my_site", "invalid novelping URL: " + url);
    if (parts.chapter === "") ctx.fail("not_my_site", "not a chapter URL: " + url);

    const doc = ctx.get(url);

    const titleNode = ctx.css1(doc, ".chr-text");
    if (!titleNode) ctx.fail("site_layout_changed", "no chapter title found at " + url);

    // #chr-content wraps the story body between the two ad slots; the ads are
    // the only other children, so dropping them leaves the text alone.
    const body = ctx.css1(doc, "#chr-content");
    if (!body) ctx.fail("site_layout_changed", "no chapter content block at " + url);
    for (const ad of ctx.css(body, ".js-ad-slot")) ad.remove();

    return { title: clean(titleNode.text), contentHtml: body.html };
  }
};
