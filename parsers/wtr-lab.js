// wtr-lab.com — a Next.js site serving novels in several reading modes. The
// metadata lives in the __NEXT_DATA__ JSON of the novel page and the catalog
// comes from GET /api/chapters/{raw_id}; both port cleanly.
//
// NOT PORTED: chapter bodies. The "web" reader service (the raw source text,
// which is what the translation pipeline consumes) is only reachable via
// POST /api/reader/get with a JSON body, and its payload is AES-256-GCM
// encrypted. ctx.get issues GETs with no request body or header control and
// goja exposes no crypto primitive, so chapter() fails loudly rather than
// returning wrong text. The "AI" mode is plaintext but quota- and
// Turnstile-limited, which the Go parser also avoided.
const NOVEL_PATH = /^\/([a-z]{2}(?:-[a-z]{2})?)\/novel\/(\d+)\/([^/]+)(?:\/chapter-(\d+))?\/?$/;

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

function parsePath(rawURL) {
  if (host(rawURL) !== "wtr-lab.com") return null;
  const m = NOVEL_PATH.exec(pathOf(rawURL));
  if (!m) return null;
  return {
    locale: m[1],
    rawID: parseInt(m[2], 10),
    slug: m[3],
    chapter: m[4] ? parseInt(m[4], 10) : 0
  };
}

function chapterURL(pageURL, parts, order) {
  return (
    baseURL(pageURL) + "/" + parts.locale + "/novel/" + parts.rawID + "/" + parts.slug + "/chapter-" + order
  );
}

function fetchChapters(ctx, pageURL, parts) {
  const resp = JSON.parse(ctx.get(baseURL(pageURL) + "/api/chapters/" + parts.rawID).body);
  const chapters = [];
  for (const ch of (resp && resp.chapters) || []) {
    const title = ch.title !== undefined && ch.title !== "" ? ch.title : ch.name;
    chapters.push({ title: clean(title), url: chapterURL(pageURL, parts, ch.order) });
  }
  return chapters;
}

module.exports = {
  name: "wtr-lab",
  apiVersion: 1,
  requiresBrowser: false,

  probe: (url) => parsePath(url) !== null,

  toc: (ctx, url) => {
    const parts = parsePath(url);
    if (!parts) ctx.fail("not_my_site", "invalid wtr-lab URL: " + url);

    const doc = ctx.get(url);
    const raw = ctx.css1(doc, "script#__NEXT_DATA__");
    const payload = raw ? raw.text.trim() : "";
    if (payload === "") ctx.fail("site_layout_changed", "__NEXT_DATA__ not found at " + url);

    let serie;
    try {
      const nextData = JSON.parse(payload);
      serie = nextData.props.pageProps.serie.serie_data.data;
    } catch (e) {
      ctx.fail("site_layout_changed", "cannot parse __NEXT_DATA__ at " + url + ": " + e.message);
    }
    if (!serie || !serie.title) ctx.fail("site_layout_changed", "no serie metadata in __NEXT_DATA__ at " + url);

    return {
      novel: {
        title: serie.title,
        author: serie.author || "",
        description: (serie.description || "").trim(),
        coverUrl: serie.image || "",
        language: parts.locale,
        tags: []
      },
      chapters: fetchChapters(ctx, url, parts)
    };
  },

  chapter: (ctx, url) => {
    const parts = parsePath(url);
    if (!parts) ctx.fail("not_my_site", "invalid wtr-lab URL: " + url);
    ctx.fail(
      "site_layout_changed",
      "wtr-lab chapter bodies are only served by POST /api/reader/get as AES-256-GCM ciphertext; " +
        "the parser script API has no POST and no crypto, so chapter text cannot be retrieved"
    );
  }
};
