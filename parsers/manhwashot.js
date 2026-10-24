// manhwashot.lat — custom Next.js site (Spanish). Series HTML with
// h1.series-title and a.ch-row links to /manga/{slug}/capitulo-{n}/. Chapter
// images carry no distinctive class; page images are identified by URL
// (/WP-manga/data/ on img.manhwashot.lat).
const BASE = "https://manhwashot.lat";
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

function isPageImage(raw) {
  if (!/^https?:\/\//i.test(raw)) return false;
  if (raw.indexOf("/WP-manga/data/") < 0) return false;
  return /\.(jpg|jpeg|png|webp|avif|gif)(\?|#|$)/i.test(raw);
}

module.exports = {
  name: "manhwashot",
  apiVersion: 1,
  media: 'comic',
  requiresBrowser: false,

  probe: (url) => hostOf(url) === "manhwashot.lat",

  toc: (ctx, url) => {
    const parts = pathParts(url);
    if (parts.length < 2 || parts[0] !== "manga" || !parts[1]) {
      ctx.fail("not_my_site", "expected manhwashot series URL like /manga/{slug}: " + url);
    }
    const slug = parts[1];
    const sourceURL = BASE + "/manga/" + slug + "/";
    const doc = ctx.get(sourceURL);

    let title = clean(ctx.css1(doc, "h1.series-title") ? ctx.css1(doc, "h1.series-title").text : "");
    if (!title) {
      title = metaContent(ctx, doc, 'meta[property="og:title"]');
      const idx = title.indexOf(" - Leer ");
      if (idx > 0) title = title.slice(0, idx).trim();
      title = clean(title);
    }
    if (!title) title = slug.replace(/-/g, " ");
    let description = "";
    for (const d of ctx.css(doc, ".series-info .series-desc, .series-desc")) {
      if (d.text && d.text.trim().length > description.length) description = d.text.trim();
    }
    if (!description) description = metaContent(ctx, doc, 'meta[property="og:description"]');
    const cover = metaContent(ctx, doc, 'meta[property="og:image"]');

    const refs = [];
    const seen = new Set();
    const collect = (a) => {
      const href = a.attr("href");
      if (href === null || href === undefined || String(href).trim() === "") return;
      const abs = ctx.resolveUrl(sourceURL, String(href).trim());
      const lp = pathParts(abs);
      if (lp.length < 3) return;
      if (!CHAPTER_SLUG_RE.test(lp[lp.length - 1])) return;
      if (seen.has(abs)) return;
      seen.add(abs);
      const num = numberFromSlug(abs);
      let t = "";
      const num_el = ctx.css1(a, ".ch-num");
      if (num_el) t = clean(num_el.text);
      if (!t) t = clean(a.text);
      if (!t) t = titleFromNumber(num);
      refs.push({ url: abs, title: t, number: num, order: refs.length });
    };
    for (const a of ctx.css(doc, "a.ch-row[href]")) collect(a);
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
    return { comic: { title, description: clean(description), author: "", coverUrl: cover, tags: [] }, chapters: refs.map((r) => ({ title: r.title, url: r.url })) };
  },

  chapter: (ctx, url) => {
    const parts = pathParts(url);
    const slug = parts.length >= 1 ? parts[parts.length - 1] : "";
    if (!CHAPTER_SLUG_RE.test(slug)) {
      ctx.fail("not_my_site", "expected manhwashot chapter URL like /manga/{slug}/capitulo-{n}: " + url);
    }
    const doc = ctx.get(url);
    const pages = [];
    const seen = new Set();
    for (const img of ctx.css(doc, "img")) {
      let raw = img.attr("data-src");
      if (raw === null || raw === undefined || String(raw).trim() === "") raw = img.attr("src");
      if (raw === null || raw === undefined) continue;
      raw = String(raw).trim();
      if (!isPageImage(raw)) continue;
      if (seen.has(raw)) continue;
      seen.add(raw);
      pages.push(raw);
    }
    if (pages.length === 0) ctx.fail("site_layout_changed", "chapter has no pages: " + url);
    let title = clean(ctx.css1(doc, "h1") ? ctx.css1(doc, "h1").text : "");
    if (!title) {
      const m = CHAPTER_SLUG_RE.exec(slug);
      title = m ? "Capítulo " + m[2] : slug;
    }
    return { title, pages };
  },

  chapterKey: (ch) => {
    const m = /^([a-z][a-z0-9+.-]*:\/\/[^/?#]+)(\/[^?#]*)/i.exec(ch.url || "");
    if (!m) return ch.url;
    return m[1].toLowerCase() + m[2].replace(/\/+$/, "");
  },
};
