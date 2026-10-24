// manhwaweb.com — JSON API backed. Series metadata from
// /manhwa/see/{slug}, page images from /chapters/see/{chapter-slug}.
const BASE = "https://manhwaweb.com";
const API = "https://manhwawebbackend-production.up.railway.app";

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
  if (parts.length < 2 || parts[0] !== "manhwa" || !parts[1]) return "";
  return parts[1];
}

function formatNumber(n) {
  if (!isFinite(n)) return String(n);
  if (Math.trunc(n) === n) return String(n);
  return String(n).replace(/\.?0+$/, "");
}

function chapterLinkOf(c) {
  if (c.link && String(c.link).trim() !== "") return String(c.link).trim();
  if (c.link_raw && String(c.link_raw).trim() !== "") return String(c.link_raw).trim();
  if (Array.isArray(c.versions)) {
    for (const v of c.versions) {
      if (v && v.link && String(v.link).trim() !== "") return String(v.link).trim();
    }
  }
  return "";
}

function normalizeURL(u) {
  if (/^https?:\/\//i.test(u)) return u;
  if (u[0] === "/") return BASE + u;
  return BASE + "/" + u;
}

module.exports = {
  name: "manhwaweb",
  apiVersion: 1,
  media: 'comic',
  requiresBrowser: false,

  probe: (url) => hostOf(url) === "manhwaweb.com",

  toc: (ctx, url) => {
    const slug = seriesSlug(url);
    if (!slug) ctx.fail("not_my_site", "expected ManhwaWeb series URL like /manhwa/{slug}: " + url);
    const doc = ctx.get(API + "/manhwa/see/" + encodeURIComponent(slug));
    let payload;
    try {
      payload = JSON.parse(doc.body);
    } catch (e) {
      ctx.fail("site_layout_changed", "series API returned invalid JSON for " + slug);
    }
    const list = Array.isArray(payload.chapters) ? payload.chapters : [];
    const kept = [];
    for (const c of list) {
      const link = chapterLinkOf(c || {});
      if (!link) continue;
      const n = typeof c.chapter === "number" ? c.chapter : parseFloat(c.chapter);
      kept.push({ number: isNaN(n) ? -1 : n, link });
    }
    if (kept.length === 0) ctx.fail("site_layout_changed", "no chapters found for " + url);
    kept.sort((a, b) => a.number - b.number);
    let title = clean(payload.name_esp || payload.the_real_name || slug.replace(/-/g, " "));
    const chapters = kept.map((c) => ({
      title: c.number >= 0 ? "Capítulo " + formatNumber(c.number) : c.link,
      url: normalizeURL(c.link),
    }));
    return {
      comic: {
        title,
        description: clean(payload._sinopsis || ""),
        author: "",
        coverUrl: (payload._imagen || "").trim(),
        tags: [],
      },
      chapters,
    };
  },

  chapter: (ctx, url) => {
    // Chapter slug is the last path segment of the reader URL.
    const parts = pathParts(url);
    if (parts.length === 0) ctx.fail("not_my_site", "not a manhwaweb chapter URL: " + url);
    const chapterSlug = parts[parts.length - 1];
    const doc = ctx.get(API + "/chapters/see/" + encodeURIComponent(chapterSlug));
    let payload;
    try {
      payload = JSON.parse(doc.body);
    } catch (e) {
      ctx.fail("site_layout_changed", "chapter API returned invalid JSON: " + url);
    }
    const images = payload && payload.chapter && Array.isArray(payload.chapter.img) ? payload.chapter.img : [];
    const pages = [];
    const seen = new Set();
    for (const raw of images) {
      const u = String(raw || "").trim();
      if (!/^https?:\/\//i.test(u)) continue;
      if (seen.has(u)) continue;
      seen.add(u);
      pages.push(u);
    }
    if (pages.length === 0) ctx.fail("site_layout_changed", "chapter has no pages: " + url);
    // Derive "Capítulo N" from the trailing -{number} of the slug.
    const m = /-([0-9]+(?:[._][0-9]+)?(?:_[0-9]+)?)$/.exec(chapterSlug);
    const title = m ? "Capítulo " + m[1].replace(/_/g, ".") : chapterSlug;
    return { title, pages };
  },

  chapterKey: (ch) => {
    const m = /^([a-z][a-z0-9+.-]*:\/\/[^/?#]+)(\/[^?#]*)/i.exec(ch.url || "");
    if (!m) return ch.url;
    return m[1].toLowerCase() + m[2].replace(/\/+$/, "");
  },
};
