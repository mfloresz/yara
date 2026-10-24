// lector-mangas.lat — Lector manga / ZeroComics network. Series pages are
// server-rendered HTML: metadata in h1.info-title / og: tags, chapter catalog
// in #chapters-list ([data-chapter-num] cards linking to
// /comics/{slug}/capitulo-{n}). Chapter pages embed every page as
// <img class="reader-page-img">.
const BASE = "https://lector-mangas.lat";

const CHAPTER_SLUG_RE = /^(capitulo|capítulo|chapter)-([0-9]+(?:\.[0-9]+)?)$/i;

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

function seriesSlug(u) {
  const parts = pathParts(u);
  if (parts.length < 2) return "";
  if (parts[0] !== "comics" && parts[0] !== "comic") return "";
  return parts[1] || "";
}

function isChapterHref(abs) {
  const parts = pathParts(abs);
  if (parts.length < 3) return false;
  return CHAPTER_SLUG_RE.test(parts[parts.length - 1]);
}

function numberFromSlug(abs) {
  const parts = pathParts(abs);
  if (parts.length === 0) return -1;
  const m = CHAPTER_SLUG_RE.exec(parts[parts.length - 1]);
  if (!m) return -1;
  const n = parseFloat(m[2]);
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

function chapterPages(ctx, doc) {
  const pages = [];
  const seen = new Set();
  for (const img of ctx.css(doc, "img.reader-page-img")) {
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
  return pages;
}

module.exports = {
  name: "lector-mangas",
  apiVersion: 1,
  media: 'comic',
  requiresBrowser: false,

  probe: (url) => hostOf(url) === "lector-mangas.lat",

  toc: (ctx, url) => {
    const slug = seriesSlug(url);
    if (!slug) ctx.fail("not_my_site", "expected lector-mangas series URL like /comics/{slug}: " + url);
    const sourceURL = BASE + "/comics/" + slug;
    const doc = ctx.get(sourceURL);

    let title = clean(ctx.css1(doc, "h1.info-title") ? ctx.css1(doc, "h1.info-title").text : "");
    if (!title) title = clean(metaContent(ctx, doc, 'meta[property="og:title"]'));
    if (!title) title = slug.replace(/-/g, " ");

    let description = clean(ctx.css1(doc, ".info-desc-text") ? ctx.css1(doc, ".info-desc-text").text : "");
    if (!description) description = metaContent(ctx, doc, 'meta[property="og:description"]').trim();
    if (!description) description = metaContent(ctx, doc, 'meta[name="description"]').trim();
    const cover = metaContent(ctx, doc, 'meta[property="og:image"]');

    const refs = [];
    const seen = new Set();
    const collect = (a) => {
      const href = a.attr("href");
      if (href === null || href === undefined || String(href).trim() === "") return;
      const abs = ctx.resolveUrl(sourceURL, String(href).trim());
      if (!isChapterHref(abs)) return;
      if (seen.has(abs)) return;
      seen.add(abs);
      let number = -1;
      // data-chapter-num lives on an ancestor card; walk via selector on doc is
      // expensive, so prefer the slug and accept it as authoritative.
      number = numberFromSlug(abs);
      refs.push({ url: abs, number: number, order: refs.length });
    };
    for (const a of ctx.css(doc, "#chapters-list a[href]")) collect(a);
    if (refs.length === 0) {
      for (const a of ctx.css(doc, 'a[href*="/capitulo-"], a[href*="/chapter-"]')) collect(a);
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
    const chapters = refs.map((r, i) => ({
      title: r.number >= 0 ? titleFromNumber(r.number) : ("Capítulo " + (i + 1)),
      url: r.url,
    }));
    return { comic: { title, description, author: "", coverUrl: cover, tags: [] }, chapters };
  },

  chapter: (ctx, url) => {
    const parts = pathParts(url);
    const chapterSlug = parts.length >= 3 ? parts[parts.length - 1] : "";
    if (!CHAPTER_SLUG_RE.test(chapterSlug)) {
      ctx.fail("not_my_site", "expected lector-mangas chapter URL like /comics/{slug}/capitulo-{n}: " + url);
    }
    const doc = ctx.get(url);
    const pages = chapterPages(ctx, doc);
    if (pages.length === 0) ctx.fail("site_layout_changed", "chapter has no pages: " + url);
    let title = "";
    const h = ctx.css1(doc, ".reader-h1-cap");
    if (h) title = clean(h.text);
    if (!title) {
      const m = CHAPTER_SLUG_RE.exec(chapterSlug);
      title = m ? "Capítulo " + m[2] : chapterSlug;
    }
    return { title, pages };
  },

  chapterKey: (ch) => {
    // Canonical identity: origin + pathname without trailing slash, lowercase
    // host. Query strings (tracking) never identify a chapter.
    const m = /^([a-z][a-z0-9+.-]*:\/\/[^/?#]+)(\/[^?#]*)/i.exec(ch.url || "");
    if (!m) return ch.url;
    let path = m[2].replace(/\/+$/, "");
    if (path === "") path = "/";
    return m[1].toLowerCase() + path;
  },
};
