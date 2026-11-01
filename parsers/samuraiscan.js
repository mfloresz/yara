// samurai.j5z.xyz — Madara WordPress theme (Spanish). Series pages live at
// /son/{slug}/ and render an empty #manga-chapters-holder that the frontend
// fills with POST {series-url}ajax/chapters/ (no params). That endpoint
// returns li.wp-manga-chapter links to /son/{slug}/capitulo-{n}/. Chapter
// pages embed <img class="wp-manga-chapter-img"> inside .reading-content.
const CHAPTER_SLUG_RE = /^cap[ií]tulo[-_]?([0-9]+(?:[.\-_][0-9]+)?)$/i;

const clean = (s) => (s || "").replace(/\s+/g, " ").trim();

function hostOf(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(u || "");
  if (!m) return "";
  return m[1].toLowerCase().replace(/^www\./, "");
}

function pathParts(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/[^/?#]*\/([^?#]*)/i.exec(u || "");
  if (!m) return [];
  return m[1].split("/").filter((p) => p !== "");
}

function originOf(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/[^/?#]*/i.exec(u || "");
  return m ? m[0].toLowerCase() : "";
}

function numberFromSlug(abs) {
  const parts = pathParts(abs);
  if (parts.length === 0) return -1;
  const m = CHAPTER_SLUG_RE.exec(parts[parts.length - 1]);
  if (!m) return -1;
  const n = parseFloat(m[1].replace(/[-_]/g, "."));
  return isNaN(n) ? -1 : n;
}

function titleFromNumber(n) {
  if (n < 0) return "";
  if (Number.isInteger(n)) return "Capítulo " + n;
  return "Capítulo " + String(n);
}

function metaContent(ctx, doc, sel) {
  const n = ctx.css1(doc, sel);
  if (!n) return "";
  const c = n.attr("content");
  return c === null || c === undefined ? "" : String(c).trim();
}

module.exports = {
  name: "samuraiscan",
  apiVersion: 1,
  media: 'comic',
  // Direct HTTP works: no Cloudflare challenge observed on series, chapter
  // list or chapter pages, so plain fetching is enough (retryable failures
  // still fall back to the browser worker automatically).
  requiresBrowser: false,

  probe: (url) => hostOf(url) === "samurai.j5z.xyz",

  toc: (ctx, url) => {
    const parts = pathParts(url);
    if (parts.length < 2 || parts[0] !== "son" || !parts[1]) {
      ctx.fail("not_my_site", "expected samuraiscan series URL like /son/{slug}: " + url);
    }
    const origin = originOf(url) || "https://samurai.j5z.xyz";
    const slug = parts[1];
    const sourceURL = origin + "/son/" + slug + "/";
    const doc = ctx.get(sourceURL);

    let title = clean(ctx.css1(doc, ".post-title h1, .post-title h3") ? ctx.css1(doc, ".post-title h1, .post-title h3").text : "");
    if (!title) title = clean(metaContent(ctx, doc, 'meta[property="og:title"]'));
    if (!title) title = slug.replace(/-/g, " ");
    let description = clean(ctx.css1(doc, ".description-summary .summary__content") ? ctx.css1(doc, ".description-summary .summary__content").text : "");
    if (!description) description = clean(ctx.css1(doc, ".summary__content") ? ctx.css1(doc, ".summary__content").text : "");
    if (!description) description = clean(metaContent(ctx, doc, 'meta[property="og:description"]'));
    let cover = "";
    const coverImg = ctx.css1(doc, ".summary_image img");
    if (coverImg) {
      const s = coverImg.attr("src");
      if (s !== null && s !== undefined && String(s).trim() !== "") {
        cover = ctx.resolveUrl(sourceURL, String(s).trim());
      }
    }
    if (!cover) cover = metaContent(ctx, doc, 'meta[property="og:image"]');

    // The series HTML carries no chapters (JS-filled holder): the catalog
    // comes from the Madara ajax/chapters/ endpoint (POST, no params).
    const res = ctx.postForm(sourceURL + "ajax/chapters/", {});
    const listHtml = (res && res.body) || "";
    if (!listHtml) ctx.fail("site_layout_changed", "empty chapter list for " + url);

    const refs = [];
    const seen = new Set();
    for (const a of ctx.css(listHtml, "li.wp-manga-chapter a[href]")) {
      const href = a.attr("href");
      if (href === null || href === undefined || String(href).trim() === "") continue;
      const abs = ctx.resolveUrl(sourceURL, String(href).trim());
      if (hostOf(abs) !== "samurai.j5z.xyz") continue;
      if (abs.indexOf(sourceURL) !== 0) continue;
      if (seen.has(abs)) continue;
      seen.add(abs);
      const num = numberFromSlug(abs);
      let t = clean(a.text);
      if (!t) t = titleFromNumber(num) || abs;
      refs.push({ url: abs, title: t, number: num, order: refs.length });
    }
    if (refs.length === 0) ctx.fail("site_layout_changed", "no chapters found for " + url);
    refs.sort((a, b) => {
      const ah = a.number >= 0, bh = b.number >= 0;
      if (ah && bh) {
        if (a.number === b.number) return a.order - b.order;
        return a.number - b.number;
      }
      if (ah) return -1;
      if (bh) return 1;
      return a.order - b.order;
    });
    return { comic: { title, description, author: "", coverUrl: cover, tags: [] }, chapters: refs.map((r) => ({ title: r.title, url: r.url })) };
  },

  chapter: (ctx, url) => {
    const parts = pathParts(url);
    if (parts.length < 3 || parts[0] !== "son" || !parts[1] || !parts[2]) {
      ctx.fail("not_my_site", "expected samuraiscan chapter URL like /son/{slug}/capitulo-{n}: " + url);
    }
    const doc = ctx.get(url);
    const pages = [];
    const seen = new Set();
    for (const img of ctx.css(doc, ".reading-content img.wp-manga-chapter-img")) {
      let raw = img.attr("data-src");
      if (raw === null || raw === undefined || String(raw).trim() === "") raw = img.attr("src");
      if (raw === null || raw === undefined) continue;
      raw = String(raw).trim();
      if (!/^https?:\/\//i.test(raw)) continue;
      if (!/\.(jpg|jpeg|png|webp|avif|gif)(\?|#|$)/i.test(raw)) continue;
      if (seen.has(raw)) continue;
      seen.add(raw);
      pages.push(raw);
    }
    if (pages.length === 0) ctx.fail("site_layout_changed", "chapter has no pages: " + url);
    let title = clean(ctx.css1(doc, "h1#chapter-heading") ? ctx.css1(doc, "h1#chapter-heading").text : "");
    if (!title) title = clean(ctx.css1(doc, ".c-breadcrumb li:last-child, .breadcrumb li:last-child") ? ctx.css1(doc, ".c-breadcrumb li:last-child, .breadcrumb li:last-child").text : "");
    if (!title) {
      const t = ctx.css1(doc, "title");
      if (t) {
        const m = /-\s*(Cap[ií]tulo\s+[0-9]+(?:[.\-_][0-9]+)?)\s*-/i.exec(clean(t.text));
        if (m) title = m[1];
      }
    }
    if (!title) {
      const m = CHAPTER_SLUG_RE.exec(parts[parts.length - 1]);
      title = m ? "Capítulo " + m[1].replace(/[-_]/g, ".") : parts[parts.length - 1];
    }
    return { title, pages };
  },

  chapterKey: (ch) => {
    const m = /^([a-z][a-z0-9+.-]*:\/\/[^/?#]+)(\/[^?#]*)/i.exec(ch.url || "");
    if (!m) return ch.url;
    return m[1].toLowerCase() + m[2].replace(/\/+$/, "");
  },
};
