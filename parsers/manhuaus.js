// manhuaus.com — Madara WordPress theme. Series HTML with
// li.wp-manga-chapter links to /manga/{slug}/chapter-{n}/. Chapter pages embed
// <img class="wp-manga-chapter-img"> inside .reading-content.
const BASE = "https://manhuaus.com";
const CHAPTER_SLUG_RE = /^chapter-([0-9]+(?:[.-][0-9]+)?)$/i;

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

function numberFromSlug(abs) {
  const parts = pathParts(abs);
  if (parts.length === 0) return -1;
  const m = CHAPTER_SLUG_RE.exec(parts[parts.length - 1]);
  if (!m) return -1;
  const n = parseFloat(m[1].replace("-", "."));
  return isNaN(n) ? -1 : n;
}

function titleFromNumber(n) {
  if (n < 0) return "";
  if (Number.isInteger(n)) return "Chapter " + n;
  return "Chapter " + String(n);
}

function metaContent(ctx, doc, sel) {
  const n = ctx.css1(doc, sel);
  if (!n) return "";
  const c = n.attr("content");
  return c === null || c === undefined ? "" : String(c).trim();
}

module.exports = {
  name: "manhuaus",
  apiVersion: 1,
  media: 'comic',
  // Cloudflare + aggressive popunders: reliable fetching needs the browser worker.
  requiresBrowser: true,

  probe: (url) => hostOf(url) === "manhuaus.com",

  toc: (ctx, url) => {
    const parts = pathParts(url);
    if (parts.length < 2 || parts[0] !== "manga" || !parts[1]) {
      ctx.fail("not_my_site", "expected manhuaus series URL like /manga/{slug}: " + url);
    }
    const slug = parts[1];
    const sourceURL = BASE + "/manga/" + slug + "/";
    const doc = ctx.get(sourceURL);

    let title = clean(ctx.css1(doc, ".post-title h1, .post-title h3") ? ctx.css1(doc, ".post-title h1, .post-title h3").text : "");
    if (!title) title = clean(metaContent(ctx, doc, 'meta[property="og:title"]'));
    if (!title) title = slug.replace(/-/g, " ");
    let description = clean(ctx.css1(doc, ".description-summary .summary__content") ? ctx.css1(doc, ".description-summary .summary__content").text : "");
    if (!description) description = metaContent(ctx, doc, 'meta[property="og:description"]').trim();
    const cover = metaContent(ctx, doc, 'meta[property="og:image"]');

    const refs = [];
    const seen = new Set();
    for (const a of ctx.css(doc, "li.wp-manga-chapter a[href]")) {
      const href = a.attr("href");
      if (href === null || href === undefined || String(href).trim() === "") continue;
      const abs = ctx.resolveUrl(sourceURL, String(href).trim());
      const lp = pathParts(abs);
      if (lp.length < 3 || !CHAPTER_SLUG_RE.test(lp[lp.length - 1])) continue;
      if (seen.has(abs)) continue;
      seen.add(abs);
      const num = numberFromSlug(abs);
      let t = clean(a.text);
      if (!t) t = titleFromNumber(num);
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
    const slug = parts.length >= 1 ? parts[parts.length - 1] : "";
    if (!CHAPTER_SLUG_RE.test(slug)) {
      ctx.fail("not_my_site", "expected manhuaus chapter URL like /manga/{slug}/chapter-{n}: " + url);
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
    let title = "";
    const h = ctx.css1(doc, "h1#chapter-heading");
    if (h) title = clean(h.text);
    if (!title) {
      const m = CHAPTER_SLUG_RE.exec(slug);
      title = m ? "Chapter " + m[1].replace("-", ".") : slug;
    }
    return { title, pages };
  },

  chapterKey: (ch) => {
    const m = /^([a-z][a-z0-9+.-]*:\/\/[^/?#]+)(\/[^?#]*)/i.exec(ch.url || "");
    if (!m) return ch.url;
    return m[1].toLowerCase() + m[2].replace(/\/+$/, "");
  },
};
