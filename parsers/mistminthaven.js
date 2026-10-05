// mistminthaven.com — a Next.js App Router site. The catalog comes from a
// public JSON API (api.mistminthaven.com) and the chapter body ships inside
// the RSC flight stream of the server-rendered chapter page as a text record
// ("id:T<len>," followed by exactly <len> characters). No Cloudflare; plain
// HTTP fetches work.
const FLIGHT_PREFIX = "self.__next_f.push([1,";
const API_BASE = "https://api.mistminthaven.com";

const clean = (s) => (s || "").replace(/[\u200b\u200c\u200d\ufeff]/g, "").replace(/\s+/g, " ").trim();

function host(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(u);
  return m ? m[1].toLowerCase().replace(/^www\./, "") : "";
}

function pathOf(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/[^/?#]*(\/[^?#]*)/i.exec(u);
  return m ? m[1] : "/";
}

// { slug, chapter } — chapter is "" for the novel info page.
function parsePath(rawURL) {
  if (host(rawURL) !== "mistminthaven.com") return null;
  const novel = /^\/novels\/([^/]+)\/?$/.exec(pathOf(rawURL));
  if (novel) return { slug: novel[1], chapter: "" };
  const chapter = /^\/novels\/([^/]+)\/([^/]+)\/?$/.exec(pathOf(rawURL));
  if (chapter) return { slug: chapter[1], chapter: chapter[2] };
  return null;
}

function chapterURL(novelSlug, chapterSlug) {
  return "https://www.mistminthaven.com/novels/" + novelSlug + "/" + chapterSlug;
}

// Scans the payload after the opening quote of one flight chunk and returns the
// raw JS string body with escapes intact. The terminator is a single unescaped
// quote followed by "]).
function flightBody(rest) {
  let body = "";
  for (let i = 1; i < rest.length; ) {
    const c = rest[i];
    if (c === "\\") {
      if (i + 1 >= rest.length) return null;
      body += c + rest[i + 1];
      i += 2;
      continue;
    }
    if (c === '"' && rest.slice(i, i + 3) === '"])') return { body: body, consumed: i + 3 };
    body += c;
    i++;
  }
  return null;
}

function hex4(raw, i) {
  return parseInt(raw.slice(i, i + 4), 16);
}

// The flight chunks are JS string literals: < > & are \uXXXX-escaped,
// apostrophes may be written as \' (which JSON rejects, so a plain JSON decode
// does not apply) and newlines as \n.
function decodeJSString(raw) {
  let out = "";
  const n = raw.length;
  for (let i = 0; i < n; ) {
    const c = raw[i];
    if (c !== "\\") {
      out += c;
      i++;
      continue;
    }
    if (i + 1 >= n) throw new Error("trailing backslash in flight string");
    i++;
    const e = raw[i];
    i++;
    switch (e) {
      case "n": out += "\n"; break;
      case "r": out += "\r"; break;
      case "t": out += "\t"; break;
      case "b": out += "\b"; break;
      case "f": out += "\f"; break;
      case "v": out += "\v"; break;
      case "0": out += "\0"; break;
      case "x": {
        out += String.fromCharCode(parseInt(raw.slice(i, i + 2), 16));
        i += 2;
        break;
      }
      case "u": {
        if (raw[i] === "{") {
          const end = raw.indexOf("}", i);
          if (end < 0) throw new Error("unterminated \\u{...} escape");
          out += String.fromCodePoint(parseInt(raw.slice(i + 1, end), 16));
          i = end + 1;
          break;
        }
        if (i + 4 > n) throw new Error("truncated \\u escape");
        const hi = hex4(raw, i);
        // Two consecutive escapes can form a surrogate pair.
        if (hi >= 0xd800 && hi <= 0xdbff && i + 10 <= n && raw.slice(i + 4, i + 6) === "\\u") {
          const lo = hex4(raw, i + 6);
          if (lo >= 0xdc00 && lo <= 0xdfff) {
            out += String.fromCharCode(hi, lo);
            i += 10;
            break;
          }
        }
        out += String.fromCharCode(hi);
        i += 4;
        break;
      }
      default:
        // \' \" \\ \/ and any other escaped char map to itself.
        out += e;
    }
  }
  return out;
}

// Concatenates every flight chunk of the page into the single RSC stream the
// site's React tree was serialized into.
function flightStream(ctx, page) {
  let stream = "";
  for (;;) {
    const idx = page.indexOf(FLIGHT_PREFIX);
    if (idx < 0) return stream;
    const rest = page.slice(idx + FLIGHT_PREFIX.length);
    if (rest[0] !== '"') {
      page = rest;
      continue;
    }
    const chunk = flightBody(rest);
    if (!chunk) ctx.fail("site_layout_changed", "malformed flight chunk");
    try {
      stream += decodeJSString(chunk.body);
    } catch (e) {
      ctx.fail("site_layout_changed", "decoding flight chunk: " + e.message);
    }
    page = rest.slice(chunk.consumed);
  }
}

// The chapter body is the longest streamed text record ("id:T<len>," followed
// by exactly <len> characters) that starts with a paragraph — the other
// records are smaller UI strings.
function extractContent(ctx, page) {
  const stream = flightStream(ctx, page);
  const re = /:T([0-9a-f]+),/g;
  let best = "";
  let m;
  while ((m = re.exec(stream)) !== null) {
    const len = parseInt(m[1], 16);
    const text = stream.slice(m.index + m[0].length, m.index + m[0].length + len);
    if (text.slice(0, 3) === "<p>" && text.length > best.length) best = text;
  }
  if (!best) ctx.fail("site_layout_changed", "no chapter content found in flight stream");
  return best;
}

module.exports = {
  name: "mistminthaven",
  apiVersion: 1,
  requiresBrowser: false,

  probe: (url) => parsePath(url) !== null,

  toc: (ctx, url) => {
    const parts = parsePath(url);
    if (!parts) ctx.fail("not_my_site", "invalid mistminthaven URL: " + url);
    if (parts.chapter !== "") ctx.fail("not_my_site", "not a novel page URL: " + url);

    const novel = JSON.parse(ctx.get(API_BASE + "/api/novel/slug/" + parts.slug).body).data;
    if (!novel) ctx.fail("site_layout_changed", "novel not found in mistminthaven API: " + parts.slug);

    const list = JSON.parse(ctx.get(API_BASE + "/api/novels/slug/" + parts.slug + "/chapters").body).data || [];
    const chapters = [];
    for (const vol of list) {
      for (const ch of vol.chapters || []) {
        // Premium chapters sit behind a paywall the anonymous API cannot read;
        // hidden ones are unpublished drafts.
        if (!ch.isFree || ch.isHidden) continue;
        chapters.push({
          title: clean(ch.title),
          url: chapterURL(parts.slug, ch.slug),
        });
      }
    }
    if (chapters.length === 0) ctx.fail("site_layout_changed", "no free chapters returned by the mistminthaven API for " + parts.slug);

    const tags = (novel.genres || []).map((g) => g.name).filter(Boolean);

    return {
      novel: {
        title: clean(novel.title),
        author: clean(novel.author),
        description: (novel.description || "").trim(),
        coverUrl: novel.avatarUrl || "",
        language: novel.nativeLanguage || "",
        tags: tags,
      },
      chapters: chapters,
    };
  },

  chapter: (ctx, url) => {
    const parts = parsePath(url);
    if (!parts) ctx.fail("not_my_site", "invalid mistminthaven URL: " + url);
    if (parts.chapter === "") ctx.fail("not_my_site", "not a chapter URL: " + url);

    const doc = ctx.get(url);
    const content = extractContent(ctx, doc.body).replace(/[\u200b\u200c\u200d\ufeff]/g, "").trim();

    // The SSR <title> carries "Chapter Title | Mistmint Haven".
    const titleNode = ctx.css1(doc, "title");
    let title = titleNode ? titleNode.text : "";
    title = title.replace(/\s*\|\s*Mistmint Haven\s*$/, "");

    return { title: clean(title), contentHtml: content };
  },
};
