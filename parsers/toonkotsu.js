// toonkotsu.com — SvelteKit site with a JSON API. Series catalog from
// /cdn/v1/series/{hid} (chapters with numeric ids, ascending reading order
// derived from the `n` field). Chapter pages from /api/manga/chapter/{id}
// whose `pages` entries carry AES-CTR key material ({src, k, n, t}): page
// bytes are fetched from /cdn/e/*.bin and decrypted by the Go downloader,
// so the key rides along as a `#tira-enc=` URL fragment (never sent to the
// server — see internal/comicdownloader/decrypt.go).
const BASE = "https://toonkotsu.com";

// Chapter page URLs look like
// /title/{hid}-{slug}/{chapterId}-chapter-{n} (e.g. /title/dvzyd-foo/1030705-chapter-1).
const CHAPTER_HREF_RE = /\/([0-9]+)-chapter-([0-9]+)\/?(?:[?#]|$)/i;
const LABEL_RE = /^chap\.?\s*0*([0-9]+)$/i;

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

// Series URLs are /title/{hid}-{slug}; chapter URLs append
// /{chapterId}-chapter-{n}. Both yield the hid (the part before the first
// dash of the title segment).
function hidOf(u) {
  const parts = pathParts(u);
  if (parts.length < 1 || parts[0] !== "title" || !parts[1]) return "";
  const seg = parts[1];
  const dash = seg.indexOf("-");
  const hid = dash < 0 ? seg : seg.slice(0, dash);
  if (!/^[a-z0-9]+$/i.test(hid)) return "";
  return hid;
}

function chapterIdOf(u) {
  const parts = pathParts(u);
  if (parts.length < 3) return "";
  const m = /^([0-9]+)-chapter-[0-9]+$/i.exec(parts[parts.length - 1]);
  if (!m) return "";
  return m[1];
}

function numberOf(u) {
  const m = CHAPTER_HREF_RE.exec(u || "");
  if (!m) return -1;
  const n = parseInt(m[2], 10);
  return isNaN(n) ? -1 : n;
}

function titleFromLabel(label, n) {
  const t = clean(label);
  const m = LABEL_RE.exec(t);
  if (m) return "Capítulo " + parseInt(m[1], 10);
  if (t) return t;
  return n >= 0 ? "Capítulo " + n : "";
}

// The chapter JSON API is served as application/json, but when the fetch
// goes through the browser worker's challenge-tab fallback the body comes
// back wrapped in HTML (<pre>JSON</pre>). Accept both shapes.
function parseJsonBody(ctx, doc) {
  const raw = doc.body || "";
  try {
    return JSON.parse(raw);
  } catch (e) {
    // Fall through to the <pre> extraction below.
  }
  const pres = ctx.css(doc, "pre");
  for (const pre of pres) {
    const t = (pre.text || "").trim();
    if (!t) continue;
    try {
      return JSON.parse(t);
    } catch (e2) {
      // Keep trying other <pre> nodes.
    }
  }
  ctx.fail("site_layout_changed", "chapter API returned invalid JSON for " + doc.url);
}

function metaContent(ctx, doc, sel) {
  const n = ctx.css1(doc, sel);
  if (!n) return "";
  const c = n.attr("content");
  return c === null || c === undefined ? "" : String(c).trim();
}

function absUrl(src) {
  const s = String(src || "").trim();
  if (/^https?:\/\//i.test(s)) return s;
  if (s[0] === "/") return BASE + s;
  return BASE + "/" + s;
}

function pageUrl(p) {
  const url = absUrl(p.src);
  const k = String((p && p.k) || "").trim();
  const n = String((p && p.n) || "").trim();
  // Pages without key material are plain images; the rest need decryption
  // by the downloader (fragment carries k/n, never sent over the wire).
  if (!k || !n) return url;
  return url + "#tira-enc=aes-256-ctr&k=" + k + "&n=" + n;
}

function tocFromSeriesJson(hid, payload, url) {
  const series = payload && payload.series ? payload.series : null;
  const list = payload && Array.isArray(payload.chapters) ? payload.chapters : [];
  if (!series || list.length === 0) return null;
  const kept = [];
  for (const c of list) {
    if (!c || typeof c.id === "undefined" || c.id === null) continue;
    const n = typeof c.n === "number" ? c.n : parseInt(c.n, 10);
    kept.push({ id: String(c.id), n: isNaN(n) ? -1 : n, label: c.label || "" });
  }
  if (kept.length === 0) return null;
  kept.sort((a, b) => {
    if (a.n >= 0 && b.n >= 0 && a.n !== b.n) return a.n - b.n;
    if (a.n >= 0 && b.n < 0) return -1;
    if (b.n >= 0 && a.n < 0) return 1;
    return 0;
  });
  const title = clean(series.title) || hid;
  const authors = Array.isArray(series.authors) ? series.authors.filter((a) => clean(a) !== "") : [];
  const tags = Array.isArray(series.tags) ? series.tags.map((t) => clean(t)).filter((t) => t !== "") : [];
  const chapters = kept.map((c) => ({
    title: titleFromLabel(c.label, c.n),
    url: BASE + "/title/" + hid + "-" + (series.slug || hid) + "/" + c.id + "-chapter-" + (c.n >= 0 ? c.n : "0"),
  }));
  return {
    comic: {
      title,
      description: clean(series.description),
      author: authors.join(", "),
      coverUrl: series.cover ? absUrl(series.cover) : "",
      tags,
    },
    chapters,
  };
}

function tocFromHtml(ctx, doc, hid, sourceURL) {
  let title = clean(ctx.css1(doc, "h1") ? ctx.css1(doc, "h1").text : "");
  if (!title) title = clean(metaContent(ctx, doc, 'meta[property="og:title"]'));
  if (!title) title = hid;
  let description = clean(metaContent(ctx, doc, 'meta[property="og:description"]'));
  if (!description) description = clean(metaContent(ctx, doc, 'meta[name="description"]'));
  const cover = metaContent(ctx, doc, 'meta[property="og:image"]');

  const refs = [];
  const seen = new Set();
  for (const a of ctx.css(doc, 'a[href*="-chapter-"]')) {
    const href = a.attr("href");
    if (href === null || href === undefined || String(href).trim() === "") continue;
    const abs = ctx.resolveUrl(sourceURL, String(href).trim());
    const m = CHAPTER_HREF_RE.exec(abs);
    if (!m) continue;
    const id = m[1];
    if (seen.has(id)) continue;
    seen.add(id);
    const n = numberOf(abs);
    refs.push({ id, n, order: refs.length });
  }
  if (refs.length === 0) return null;
  refs.sort((a, b) => {
    if (a.n >= 0 && b.n >= 0 && a.n !== b.n) return a.n - b.n;
    if (a.n >= 0 && b.n < 0) return -1;
    if (b.n >= 0 && a.n < 0) return 1;
    return a.order - b.order;
  });
  return {
    comic: { title, description, author: "", coverUrl: cover, tags: [] },
    chapters: refs.map((r) => ({
      title: r.n >= 0 ? "Capítulo " + r.n : "Capítulo",
      url: BASE + "/title/" + hid + "/" + r.id + "-chapter-" + (r.n >= 0 ? r.n : "0"),
    })),
  };
}

module.exports = {
  name: "toonkotsu",
  apiVersion: 1,
  media: 'comic',
  // The chapter JSON API (/api/manga/chapter/{id}) sits behind a
  // human-verification wall (HTTP 429 + {"code":"manga-limit"}) for plain
  // HTTP, so chapter fetches must go through the user's browser worker,
  // which reuses the cleared session. The series catalog endpoint and page
  // images are reachable directly.
  requiresBrowser: true,

  probe: (url) => hostOf(url) === "toonkotsu.com",

  toc: (ctx, url) => {
    const hid = hidOf(url);
    if (!hid) ctx.fail("not_my_site", "expected toonkotsu series URL like /title/{hid}-{slug}: " + url);
    const sourceURL = BASE + "/title/" + pathParts(url)[1];
    let payload = null;
    try {
      payload = parseJsonBody(ctx, ctx.get(BASE + "/cdn/v1/series/" + hid));
    } catch (e) {
      payload = null;
    }
    if (payload) {
      const snap = tocFromSeriesJson(hid, payload, url);
      if (snap) return snap;
    }
    const doc = ctx.get(sourceURL);
    const snap = tocFromHtml(ctx, doc, hid, sourceURL);
    if (!snap) ctx.fail("site_layout_changed", "no chapters found for " + url);
    return snap;
  },

  chapter: (ctx, url) => {
    const hid = hidOf(url);
    const id = chapterIdOf(url);
    if (!hid || !id) {
      ctx.fail("not_my_site", "expected toonkotsu chapter URL like /title/{hid}-{slug}/{id}-chapter-{n}: " + url);
    }
    const payload = parseJsonBody(ctx, ctx.get(BASE + "/api/manga/chapter/" + id));
    if (payload && payload.code === "manga-limit") {
      ctx.fail("blocked", "toonkotsu asked for human verification for " + url);
    }
    const ch = payload && payload.chapter ? payload.chapter : null;
    const rawPages = ch && Array.isArray(ch.pages) ? ch.pages : [];
    const pages = [];
    const seen = new Set();
    for (const p of rawPages) {
      if (!p || !p.src || String(p.src).trim() === "") continue;
      const u = pageUrl(p);
      if (seen.has(u)) continue;
      seen.add(u);
      pages.push(u);
    }
    if (pages.length === 0) ctx.fail("site_layout_changed", "chapter has no pages: " + url);
    const n = ch && typeof ch.n === "number" ? ch.n : numberOf(url);
    return { title: titleFromLabel(ch && ch.label, n), pages };
  },

  chapterKey: (ch) => {
    // Numeric chapter ids are the durable identity: series slugs can be
    // renamed, but the id never changes.
    const hid = hidOf(ch.url || "");
    const id = chapterIdOf(ch.url || "");
    if (!hid || !id) return ch.url;
    return "toonkotsu/" + hid + "/" + id;
  },
};
