// novelarrow.com — a Next.js App Router site that renders server-side. The
// catalog comes from a small JSON API; the chapter body ships inside the RSC
// flight stream as a JS-string-escaped HTML fragment.
const FLIGHT_PREFIX = "self.__next_f.push([1,";

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

// { slug, chapter } — chapter is "" for the novel info page.
function parsePath(rawURL) {
  if (host(rawURL) !== "novelarrow.com") return null;
  const path = pathOf(rawURL);
  const novel = /^\/novel\/([^/]+)\/?$/.exec(path);
  if (novel) return { slug: novel[1], chapter: "" };
  const chapter = /^\/chapter\/([^/]+)\/([^/]+)\/?$/.exec(path);
  if (chapter) return { slug: chapter[1], chapter: chapter[2] };
  return null;
}

function chapterURL(pageURL, slug, chapterID) {
  return baseURL(pageURL) + "/chapter/" + slug + "/" + chapterID;
}

function fetchChapters(ctx, pageURL, slug) {
  const resp = JSON.parse(ctx.get(baseURL(pageURL) + "/api-web/novels/" + slug + "/chapters?sort=asc").body);
  const items = (resp && resp.items) || [];
  const chapters = [];
  for (const ch of items) {
    if (!ch.chapter_id) continue;
    chapters.push({ title: clean(ch.chapter_name), url: chapterURL(pageURL, slug, ch.chapter_id) });
  }
  if (chapters.length === 0) ctx.fail("site_layout_changed", "no chapters returned by the novelarrow API for " + slug);
  return chapters;
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

// The chapter HTML is escaped as a JS string: < > & become < > &,
// apostrophes are written as \' (which JSON rejects, so a plain JSON decode
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

// Every other flight chunk carries JSON bookkeeping and never starts with "<"
// (encoded as <), so the first match is the rendered reading pane.
function extractContent(ctx, page) {
  for (;;) {
    const idx = page.indexOf(FLIGHT_PREFIX);
    if (idx < 0) ctx.fail("site_layout_changed", "no chapter content found in flight stream");
    const rest = page.slice(idx + FLIGHT_PREFIX.length);
    if (rest[0] !== '"') {
      page = rest;
      continue;
    }
    const chunk = flightBody(rest);
    if (!chunk) ctx.fail("site_layout_changed", "malformed flight chunk");
    if (chunk.body.slice(0, 6) !== "\\u003c") {
      page = rest;
      continue;
    }
    try {
      return decodeJSString(chunk.body);
    } catch (e) {
      ctx.fail("site_layout_changed", "decoding chapter content: " + e.message);
    }
  }
}

function metaContent(ctx, doc, selector) {
  const n = ctx.css1(doc, selector);
  if (!n) return "";
  const c = n.attr("content");
  return c === null ? "" : c.trim();
}

module.exports = {
  name: "novelarrow",
  apiVersion: 1,
  requiresBrowser: false,

  probe: (url) => parsePath(url) !== null,

  toc: (ctx, url) => {
    const parts = parsePath(url);
    if (!parts) ctx.fail("not_my_site", "invalid novelarrow URL: " + url);
    if (parts.chapter !== "") ctx.fail("not_my_site", "not a novel page URL: " + url);

    const doc = ctx.get(url);

    const h = ctx.css1(doc, "h1");
    const title = h ? h.text : "";
    if (!title) ctx.fail("site_layout_changed", "no novel title found on page");

    const authorNode = ctx.css1(doc, 'a[href*="/author/"]');
    const author = authorNode ? authorNode.text : "";

    // The synopsis renders inside the .site-reading-copy block, one <p> per
    // paragraph.
    const reading = ctx.css1(doc, ".site-reading-copy");
    const paras = [];
    if (reading) {
      for (const p of ctx.css(reading, "p")) {
        if (p.text) paras.push(p.text);
      }
    }

    const img = ctx.css1(doc, ".novel-cover-frame img");
    const coverUrl = img && img.attr("src") ? img.attr("src").trim() : "";

    return {
      novel: {
        title: clean(title),
        author: clean(author),
        description: paras.join("\n\n").trim(),
        coverUrl: coverUrl,
        language: "",
        tags: []
      },
      chapters: fetchChapters(ctx, url, parts.slug)
    };
  },

  chapter: (ctx, url) => {
    const parts = parsePath(url);
    if (!parts) ctx.fail("not_my_site", "invalid novelarrow URL: " + url);
    if (parts.chapter === "") ctx.fail("not_my_site", "not a chapter URL: " + url);

    const doc = ctx.get(url);
    const content = extractContent(ctx, doc.body).trim();

    // The chapter title is the leading heading of the content fragment; fall
    // back to the og:novel:chapter_name meta when the site omits it.
    let title = "";
    const heading = ctx.css(content, "h1, h2, h3, h4, h5, h6");
    if (heading.length > 0) title = heading[0].text;
    if (!title) title = metaContent(ctx, doc, 'meta[name="og:novel:chapter_name"]');
    if (!title) ctx.fail("site_layout_changed", "no chapter title found at " + url);

    return { title: clean(title), contentHtml: content };
  }
};
