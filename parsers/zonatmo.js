// zonatmo.org — JSON API at zonatmo.to for /manga/ URLs (metadata +
// paginated chapter catalog + JIT-token page images), library HTML scraping
// for /library/ URLs (one .upload-link row per chapter with /view_uploads/
// links).
const API = "https://zonatmo.to/wp-api/api";
const PUBLIC = "https://zonatmo.org";
const UPLOADS = "https://zonatmo.to/wp-content/uploads";
const CDN = "https://cdn.zonatmo.to";
const PER_PAGE = 50;

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

function seriesKind(u) {
  const parts = pathParts(u);
  if (parts.length >= 2 && parts[0] === "manga" && parts[1]) return { kind: "manga", slug: parts[1] };
  if (parts.length >= 4 && parts[0] === "library" && parts[1] && parts[2] && parts[3]) {
    return { kind: "library", type: parts[1].toLowerCase(), id: parts[2], slug: parts[3] };
  }
  return null;
}

function envelope(body) {
  let env;
  try {
    env = JSON.parse(body);
  } catch (e) {
    return null;
  }
  return env;
}

function isObj(v) {
  return v !== null && typeof v === "object" && !Array.isArray(v);
}

function normalizeCover(raw) {
  raw = (raw || "").trim();
  if (!raw) return "";
  if (/^https?:\/\//i.test(raw)) return raw;
  return UPLOADS.replace(/\/+$/, "") + "/" + raw.replace(/^\/+/, "");
}

function fmtNum(n) {
  if (!isFinite(n)) return "";
  if (Math.trunc(n) === n) return String(n);
  return String(n).replace(/\.?0+$/, "");
}

function numOf(raw) {
  const n = parseFloat(String(raw || "").trim());
  return isNaN(n) ? -1 : n;
}

function chapterTitle(numRaw, title) {
  numRaw = (numRaw || "").trim();
  title = (title || "").trim();
  const n = numOf(numRaw);
  const disp = n >= 0 ? fmtNum(n) : numRaw;
  if (!title) return disp ? "Capítulo " + disp : "Capítulo";
  if (!disp) return title;
  return "Capítulo " + disp + " - " + title;
}

function metaContent(ctx, doc, sel) {
  const n = ctx.css1(doc, sel);
  if (!n) return "";
  const c = n.attr("content");
  return c === null || c === undefined ? "" : String(c).trim();
}

function fetchJSON(ctx, url) {
  const doc = ctx.get(url);
  const env = envelope(doc.body);
  if (!env) ctx.fail("site_layout_changed", "API returned invalid JSON: " + url);
  if (env.error) ctx.fail("site_layout_changed", "zonatmo API error: " + (env.message || url));
  return env.data;
}

module.exports = {
  name: "zonatmo",
  apiVersion: 1,
  media: 'comic',
  // Cloudflare + JS-rendered reader: reliable fetching needs the browser worker.
  requiresBrowser: true,

  probe: (url) => {
    const h = hostOf(url);
    return h === "zonatmo.org" || h.endsWith(".zonatmo.org") || h === "zonatmo.to" || h.endsWith(".zonatmo.to");
  },

  toc: (ctx, url) => {
    const ref = seriesKind(url);
    if (!ref) ctx.fail("not_my_site", "expected ZonaTMO series URL like /manga/{slug} or /library/{type}/{id}/{slug}: " + url);

    // Library URLs: chapter list from the library HTML itself.
    if (ref.kind === "library") {
      const sourceURL = PUBLIC + "/library/" + ref.type + "/" + ref.id + "/" + ref.slug;
      const doc = ctx.get(sourceURL);
      let rows = ctx.css(doc, "ul.list-chapters li.upload-link");
      if (rows.length === 0) rows = ctx.css(doc, "li.upload-link");
      const refs = [];
      const seen = new Set();
      for (let order = 0; order < rows.length; order++) {
        const row = rows[order];
        let link = ctx.css1(row, ".chapter-detail a[href*='view_uploads/']");
        if (!link) link = ctx.css1(row, "a[href*='view_uploads/']");
        if (!link) continue;
        const href = String(link.attr("href") || "").trim();
        if (!href || seen.has(href)) continue;
        seen.add(href);
        let titleNode = ctx.css1(row, ".chapter-number");
        let title = titleNode ? clean(titleNode.text) : "";
        let m = /cap[ií]tulo\s+([0-9]+(?:\.[0-9]+)?)/i.exec(title);
        let num = m ? numOf(m[1]) : -1;
        if (!title) title = num >= 0 ? "Capítulo " + fmtNum(num) : href;
        refs.push({ url: ctx.resolveUrl(sourceURL, href), title, number: num, order });
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
      let title = clean(ctx.css1(doc, ".element-title") ? ctx.css1(doc, ".element-title").text : "");
      if (!title) {
        title = metaContent(ctx, doc, 'meta[property="og:title"]')
          .replace(/^Ver\s+/i, "")
          .replace(/\s*\|\s*ZonaTMO.*$/i, "")
          .trim();
      }
      if (!title) title = ref.slug.replace(/-/g, " ");
      const description = metaContent(ctx, doc, 'meta[property="og:description"]');
      const cover = metaContent(ctx, doc, 'meta[property="og:image"]');
      return { comic: { title, description, author: "", coverUrl: cover, tags: [] }, chapters: refs.map((r) => ({ title: r.title, url: r.url })) };
    }

    // Canonical /manga/ URLs: metadata + paginated catalog from the JSON API.
    const slug = ref.slug;
    const details = fetchJSON(ctx, API + "/single/manga/" + encodeURIComponent(slug));
    if (!isObj(details)) ctx.fail("site_layout_changed", "unexpected series payload for " + slug);
    let title = clean(details.title || slug.replace(/-/g, " "));
    const description = clean(details.overview || "");
    const cover = normalizeCover(details.cover || "");

    const refs = [];
    let page = 1;
    for (;;) {
      const data = fetchJSON(
        ctx,
        API + "/single/manga/" + encodeURIComponent(slug) + "/chapters?page=" + page + "&postsPerPage=" + PER_PAGE + "&order=asc"
      );
      if (!isObj(data) || !Array.isArray(data.items)) ctx.fail("site_layout_changed", "unexpected chapter list payload");
      for (const item of data.items) {
        const cslug = (item.slug || "").trim();
        if (!cslug) continue;
        refs.push({
          url: PUBLIC + "/manga/" + slug + "/" + cslug,
          title: chapterTitle(item.chapter_number || "", item.title || ""),
          number: numOf(item.chapter_number || ""),
          order: refs.length,
        });
      }
      const totalPages = data.pagination && data.pagination.total_pages ? data.pagination.total_pages : 0;
      if (!totalPages || page >= totalPages) break;
      page++;
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
    return {
      comic: { title, description, author: "", coverUrl: cover, tags: [] },
      chapters: refs.map((r) => ({ title: r.title, url: r.url })),
    };
  },

  chapter: (ctx, url) => {
    // Legacy /view_uploads/{id} reader pages are server-rendered HTML.
    if (/\/view_uploads\/\d+/.test(url)) {
      const doc = ctx.get(url);
      const pages = [];
      const seen = new Set();
      for (const img of ctx.css(doc, "img[src], img[data-src]")) {
        let raw = img.attr("data-src");
        if (raw === null || raw === undefined || String(raw).trim() === "") raw = img.attr("src");
        if (raw === null || raw === undefined) continue;
        raw = String(raw).trim();
        if (!raw || /favicon/i.test(raw)) continue;
        const abs = ctx.resolveUrl(url, raw);
        if (!/\.(jpg|jpeg|png|webp|avif|jxl)(\?|#|$)/i.test(abs)) continue;
        if (seen.has(abs)) continue;
        seen.add(abs);
        pages.push(abs);
      }
      if (pages.length === 0) ctx.fail("site_layout_changed", "chapter has no pages: " + url);
      let title = "";
      const t = ctx.css1(doc, "title");
      if (t) title = clean(t.text);
      if (!title) title = url;
      return { title, pages };
    }
    const parts = pathParts(url);
    if (parts.length < 3 || parts[0] !== "manga" || !parts[1] || !parts[2]) {
      ctx.fail("not_my_site", "expected ZonaTMO chapter URL like /manga/{slug}/{chapter-slug}: " + url);
    }
    const data = fetchJSON(
      ctx,
      API + "/single/manga/" + encodeURIComponent(parts[1]) + "/" + encodeURIComponent(parts[2])
    );
    if (!isObj(data) || !isObj(data.chapter)) ctx.fail("site_layout_changed", "unexpected chapter payload: " + url);
    const ch = data.chapter;
    const images = Array.isArray(ch.images) ? ch.images.slice() : [];
    images.sort((a, b) => (a.page_number || 0) - (b.page_number || 0));
    const jit = String(ch.jit || "").replace(/^\/+|\/+$/g, "");
    const pages = [];
    for (const img of images) {
      const name = String((img && img.image_url) || "").trim().replace(/^\/+/, "");
      if (!jit || !name) continue;
      pages.push(CDN + "/manga/" + jit + "/" + name);
    }
    if (pages.length === 0) ctx.fail("site_layout_changed", "chapter has no pages: " + url);
    let title = chapterTitle(ch.chapter_number || "", ch.title || "");
    if (!title) title = parts[2];
    return { title, pages };
  },

  chapterKey: (ch) => {
    // Prefer the canonical /manga/{series}/{chapter} path; fall back to the
    // full URL for /view_uploads/ reader links (stable numeric id).
    const m = /^([a-z][a-z0-9+.-]*:\/\/[^/?#]+)(\/[^?#]*)/i.exec(ch.url || "");
    if (!m) return ch.url;
    return m[1].toLowerCase() + m[2].replace(/\/+$/, "");
  },
};
