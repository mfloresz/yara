// fenrirealm.com — served entirely by a JSON API under /api/new/v2.
const API = "https://fenrirealm.com/api/new/v2";

const clean = (s) => (s || "").replace(/\s+/g, " ").trim();

function host(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(u);
  return m ? m[1].toLowerCase().replace(/^www\./, "") : "";
}

function pathParts(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/[^/?#]*(\/[^?#]*)/i.exec(u);
  const raw = m ? m[1] : u;
  return raw.replace(/^\/+|\/+$/g, "").split("/").filter((p) => p !== "");
}

function seriesSlug(ctx, pageURL) {
  const parts = pathParts(pageURL);
  if (parts.length < 2 || parts[0] !== "series") {
    ctx.fail("not_my_site", "unexpected URL path: " + pageURL);
  }
  return parts[1];
}

function chapterSlugs(ctx, chapterURL) {
  const parts = pathParts(chapterURL);
  if (parts.length < 3 || parts[0] !== "series") {
    ctx.fail("not_my_site", "unexpected chapter URL path: " + chapterURL);
  }
  return [parts[1], parts[2]];
}

function isPremium(ch) {
  return !!ch.locked && ch.locked.price > 0;
}

function chapterRefs(slug, items) {
  const refs = [];
  for (const ch of items) {
    // Premium chapters sit behind the paywall; the API returns only a preview.
    if (isPremium(ch)) continue;
    const title = ch.name !== undefined && ch.name !== "" ? ch.name : ch.title;
    refs.push({ title: clean(title), url: "https://fenrirealm.com/series/" + slug + "/" + ch.slug });
  }
  return refs;
}

function fetchJSON(ctx, url) {
  return JSON.parse(ctx.get(url).body);
}

function descriptionOf(ctx, rawHTML) {
  if (!rawHTML || rawHTML.trim() === "") return "";
  const blocks = ctx.css(rawHTML, "p, div, li");
  if (blocks.length === 0) {
    const body = ctx.css1(rawHTML, "body");
    return body ? clean(body.text) : "";
  }
  const parts = [];
  for (const b of blocks) {
    const t = clean(b.text);
    if (t) parts.push(t);
  }
  return parts.join("\n\n");
}

function escapeHTML(s) {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&#34;")
    .replace(/'/g, "&#39;");
}

function tiptapToHTML(node) {
  if (!node) return "";
  if (node.type === "text") return escapeHTML(node.text || "");
  if (node.type === "paragraph") {
    if (!node.content || node.content.length === 0) return "<p></p>\n";
    return "<p>" + (node.content || []).map(tiptapToHTML).join("") + "</p>\n";
  }
  return (node.content || []).map(tiptapToHTML).join("");
}

// Cloudflare obfuscates some series served as "html" with invisible Unicode
// characters and hidden style/div noise. The strip is a code-point loop rather
// than a regex: several of these characters (U+2028/U+2029) terminate a line in
// JS source and the variation selectors need a surrogate pair, so a character
// class would have to be escaped away from readability.
function isInvisible(code) {
  return (
    code === 0x00ad ||
    (code >= 0x200b && code <= 0x200f) ||
    (code >= 0x2028 && code <= 0x202f) ||
    (code >= 0x2060 && code <= 0x206f) ||
    code === 0xfeff ||
    (code >= 0xe0100 && code <= 0xe01ef)
  );
}

function stripInvisible(s) {
  let out = "";
  for (let i = 0; i < s.length; ) {
    const code = s.codePointAt(i);
    if (!isInvisible(code)) out += s[i];
    i += code > 0xffff ? 2 : 1;
  }
  return out;
}

function stripCFObfuscation(raw) {
  return stripInvisible(
    raw
      .replace(/<style>[^<]*\.cf[0-9a-f]+\{[^<]*<\/style>/gs, "")
      .replace(/<div class="[^"]*" aria-hidden="true">[^<]*<\/div>/gs, "")
  ).trim();
}

module.exports = {
  name: "fenrirealm",
  apiVersion: 1,
  requiresBrowser: false,

  probe: (url) => host(url) === "fenrirealm.com",

  toc: (ctx, url) => {
    const slug = seriesSlug(ctx, url);
    const meta = fetchJSON(ctx, API + "/series/" + encodeURIComponent(slug));
    const items = fetchJSON(ctx, API + "/series/" + encodeURIComponent(slug) + "/chapters");

    return {
      novel: {
        title: meta.title || "",
        author: meta.user ? meta.user.username || "" : "",
        description: descriptionOf(ctx, meta.description),
        coverUrl: meta.cover ? "https://fenrirealm.com/" + meta.cover.replace(/^\//, "") : "",
        language: "",
        tags: []
      },
      chapters: chapterRefs(slug, Array.isArray(items) ? items : [])
    };
  },

  chapter: (ctx, url) => {
    const parts = chapterSlugs(ctx, url);
    const slug = parts[0];
    const chSlug = parts[1];
    const ch = fetchJSON(ctx, API + "/series/" + encodeURIComponent(slug) + "/chapters/" + encodeURIComponent(chSlug));

    if (isPremium(ch)) {
      ctx.fail("blocked", "chapter " + chSlug + " is premium/locked (price " + ch.locked.price + ") and cannot be downloaded");
    }

    let contentHtml;
    if (ch.content_format === "html") {
      contentHtml = stripCFObfuscation(ch.content || "");
    } else {
      try {
        contentHtml = tiptapToHTML(JSON.parse(ch.content));
      } catch (e) {
        ctx.fail("site_layout_changed", "cannot parse chapter content (format " + ch.content_format + "): " + e.message);
      }
    }

    const title = ch.name !== undefined && ch.name !== "" ? ch.name : ch.title;
    return { title: clean(title), contentHtml: contentHtml.trim() };
  }
};
