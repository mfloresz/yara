// onfmangas.com — ONF MANGAS. Series pages (/manga/{id}/{slug}) embed the
// full chapter catalog as hex-encoded JSON (const _hex = "..." holding
// chaptersData with {id, numero, titulo, url}). Reader pages
// (/lector/{id}/{slug}) embed page images the same way
// (const _hexP = "..." holding pagesData with {src, fallback}).
// The reader is behind Cloudflare, so fetching needs the browser worker.
const BASE = "https://onfmangas.com";

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

function fmtNum(n) {
  if (!isFinite(n)) return "";
  if (Math.trunc(n) === n) return String(n);
  return String(n).replace(/\.?0+$/, "");
}

function numOf(raw) {
  const n = parseFloat(String(raw || "").trim());
  return isNaN(n) ? -1 : n;
}

// Same decoding the site itself uses:
// decodeURIComponent(_hex.replace(/(..)/g, '%$1'))
function decodeHex(ctx, hex, url) {
  let str;
  try {
    str = decodeURIComponent(hex.replace(/(..)/g, "%$1"));
  } catch (e) {
    ctx.fail("site_layout_changed", "chapter catalog is not valid hex JSON: " + url);
  }
  try {
    return JSON.parse(str);
  } catch (e) {
    ctx.fail("site_layout_changed", "chapter catalog is not valid JSON: " + url);
  }
  return null;
}

function metaContent(ctx, doc, sel) {
  const n = ctx.css1(doc, sel);
  if (!n) return "";
  const c = n.attr("content");
  return c === null || c === undefined ? "" : String(c).trim();
}

function scanNameOf(v) {
  if (v && Array.isArray(v.grupos_list) && v.grupos_list.length > 0 && v.grupos_list[0].nombre) {
    return clean(v.grupos_list[0].nombre);
  }
  return "";
}

function baseTitleOf(c) {
  const t = clean((c && (c.titulo || c.titulo_str)) || "");
  if (t) return t;
  const n = numOf(c && c.numero);
  return n >= 0 ? "Capítulo " + fmtNum(n) : "";
}

module.exports = {
  name: "onfmangas",
  apiVersion: 1,
  media: 'comic',
  // Reader pages (/lector/...) sit behind Cloudflare: reliable fetching
  // needs the browser worker.
  requiresBrowser: true,

  probe: (url) => hostOf(url) === "onfmangas.com",

  toc: (ctx, url) => {
    const parts = pathParts(url);
    if (parts.length < 2 || parts[0] !== "manga" || !parts[1]) {
      ctx.fail("not_my_site", "expected ONF MANGAS series URL like /manga/{id}/{slug}: " + url);
    }
    const doc = ctx.get(url);
    const m = /const _hex\s*=\s*"([0-9a-fA-F]+)"/.exec(doc.body || "");
    if (!m) ctx.fail("site_layout_changed", "no embedded chapter catalog found: " + url);
    const data = decodeHex(ctx, m[1], url);
    if (!Array.isArray(data) || data.length === 0) {
      ctx.fail("site_layout_changed", "no chapters found for " + url);
    }

    const refs = [];
    const seen = new Set();
    for (const c of data) {
      if (!c || !c.url) continue;
      const versions = [c].concat(
        Array.isArray(c.otras_versiones) ? c.otras_versiones.filter((v) => v && v.url) : []
      );
      const base = baseTitleOf(c);
      for (const v of versions) {
        const abs = ctx.resolveUrl(url, String(v.url).trim());
        if (seen.has(abs)) continue;
        seen.add(abs);
        let title = baseTitleOf(v) || base;
        if (versions.length > 1) {
          const scan = scanNameOf(v);
          if (scan) title = (title || base || "Capítulo") + " (" + scan + ")";
        }
        if (!title) title = abs;
        refs.push({ url: abs, title, number: numOf(v.numero !== undefined ? v.numero : c.numero), order: refs.length });
      }
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

    let title = clean(ctx.css1(doc, "h1.manga-title") ? ctx.css1(doc, "h1.manga-title").text : "");
    if (!title) title = clean(metaContent(ctx, doc, 'meta[property="og:title"]').replace(/\s*-\s*Leer Online.*$/i, ""));
    if (!title) title = clean(metaContent(ctx, doc, 'meta[name="title"]'));
    if (!title) title = parts[2] ? parts[2].replace(/-/g, " ") : parts[1];

    let description = clean(ctx.css1(doc, ".manga-description") ? ctx.css1(doc, ".manga-description").text : "");
    if (!description) description = clean(metaContent(ctx, doc, 'meta[property="og:description"]'));

    let author = clean(ctx.css1(doc, ".author-link") ? ctx.css1(doc, ".author-link").text : "");

    let cover = "";
    const poster = ctx.css1(doc, "img.manga-poster");
    if (poster) {
      const src = poster.attr("src");
      if (src !== null && src !== undefined && String(src).trim() !== "") {
        cover = ctx.resolveUrl(url, String(src).trim());
      }
    }
    if (!cover) cover = metaContent(ctx, doc, 'meta[property="og:image"]');

    const tags = [];
    const seenTags = new Set();
    for (const a of ctx.css(doc, "a.genre-tag")) {
      const t = clean(a.text);
      if (!t || seenTags.has(t.toLowerCase())) continue;
      seenTags.add(t.toLowerCase());
      tags.push(t);
    }

    return {
      comic: { title, description, author, coverUrl: cover, tags },
      chapters: refs.map((r) => ({ title: r.title, url: r.url })),
    };
  },

  chapter: (ctx, url) => {
    const parts = pathParts(url);
    if (parts.length < 2 || parts[0] !== "lector" || !parts[1]) {
      ctx.fail("not_my_site", "expected ONF MANGAS chapter URL like /lector/{id}/{slug}: " + url);
    }
    const doc = ctx.get(url);
    const body = doc.body || "";
    const pages = [];
    const seen = new Set();
    const push = (raw) => {
      const u = String(raw || "").trim();
      if (!/^https?:\/\//i.test(u)) return;
      if (seen.has(u)) return;
      seen.add(u);
      pages.push(u);
    };

    // Primary: the hex-encoded pagesData the reader hydrates from. Each
    // entry carries src (a MangaDex at-home node URL, which rots once the
    // node drops the image from cache) plus fallback (the canonical
    // uploads.mangadex.org URL). Prefer the fallback: it is the stable
    // long-term address, while src 404s on older chapters.
    // Referer: uploads.mangadex.org answers a foreign Referer with a 200
    // MangaDex placeholder instead of the image (no status code to retry
    // on), so every page carries #tira-referer=https://mangadex.org/ and the
    // downloader sends exactly that instead of the onfmangas origin.
    const REFERER_FRAG = "#tira-referer=https://mangadex.org/";
    const withReferer = (u) => {
      u = String(u || "").trim();
      if (u.indexOf("#") >= 0) return u + "&tira-referer=https://mangadex.org/";
      return u + REFERER_FRAG;
    };
    const m = /const _hexP\s*=\s*"([0-9a-fA-F]+)"/.exec(body);
    if (m) {
      const data = decodeHex(ctx, m[1], url);
      if (!Array.isArray(data) || data.length === 0) {
        ctx.fail("site_layout_changed", "chapter has no pages: " + url);
      }
      for (const pag of data) {
        if (!pag) continue;
        push(withReferer(pag.fallback || pag.src));
      }
    } else {
      // Fallback: server-rendered <img class="manga-page"> (noscript).
      for (const img of ctx.css(doc, "img.manga-page")) {
        let raw = img.attr("src");
        if (raw === null || raw === undefined || String(raw).trim() === "") raw = img.attr("data-src");
        if (raw === null || raw === undefined) continue;
        push(raw);
      }
    }
    if (pages.length === 0) ctx.fail("site_layout_changed", "chapter has no pages: " + url);

    let title = "";
    const t = ctx.css1(doc, "title");
    if (t) {
      title = clean(t.text)
        .replace(/^Leer\s+/i, "")
        .replace(/\s*Online en Español.*$/i, "")
        .trim();
    }
    if (!title) {
      const slug = parts[parts.length - 1] || "";
      const cm = /capitulo-([0-9]+(?:[._][0-9]+)?)/i.exec(slug);
      title = cm ? "Capítulo " + cm[1].replace(/_/g, ".") : slug;
    }
    return { title, pages };
  },

  chapterKey: (ch) => {
    // The /lector/{numeric-id} is stable while the trailing slug can change.
    const m = /^([a-z][a-z0-9+.-]*:\/\/[^/?#]+)\/lector\/(\d+)/i.exec(ch.url || "");
    if (m) return m[1].toLowerCase() + "/lector/" + m[2];
    const f = /^([a-z][a-z0-9+.-]*:\/\/[^/?#]+)(\/[^?#]*)/i.exec(ch.url || "");
    if (!f) return ch.url;
    return f[1].toLowerCase() + f[2].replace(/\/+$/, "");
  },
};
