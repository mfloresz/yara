// wattpad.com — the public JSON APIs the reference WattpadDownloader uses:
// /api/v3/stories/{id} for metadata and the full part list, /api/v3/story_parts/
// {id} to resolve a part page back to its story, and /apiv2/storytext?id={id}
// for one part's HTML.
const STORY_FIELDS =
  "tags,id,title,createDate,modifyDate,language(name),description,completed,mature,url,isPaywalled," +
  "user(username,avatar,description),parts(id,title),cover,copyright";

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

// kind: "story" | "part"
function parseURL(rawURL) {
  if (host(rawURL) !== "wattpad.com") return null;
  const path = pathOf(rawURL);
  const story = /^\/story\/(\d+)(?:-.*)?\/?$/.exec(path);
  if (story) return { id: story[1], kind: "story" };
  const part = /^\/(\d+)(?:-.*)?\/?$/.exec(path);
  if (part) return { id: part[1], kind: "part" };
  return null;
}

function slugify(title) {
  const lower = (title || "").trim().toLowerCase();
  let out = "";
  let prevDash = true; // trims leading dashes without a special case
  for (const ch of lower) {
    const code = ch.charCodeAt(0);
    const alnum = (code >= 97 && code <= 122) || (code >= 48 && code <= 57);
    if (alnum) {
      out += ch;
      prevDash = false;
    } else if (ch === " " || ch === "-" || ch === "_") {
      if (!prevDash) {
        out += "-";
        prevDash = true;
      }
    }
  }
  return out.replace(/^-+|-+$/g, "");
}

function partURL(pageURL, partID, title) {
  const slug = slugify(title);
  if (slug === "") return baseURL(pageURL) + "/" + partID;
  return baseURL(pageURL) + "/" + partID + "-" + slug;
}

function fetchJSON(ctx, url) {
  return JSON.parse(ctx.get(url).body);
}

function fetchPart(ctx, pageURL, partID) {
  const part = fetchJSON(ctx, baseURL(pageURL) + "/api/v3/story_parts/" + partID + "?fields=id,title,groupId");
  if (!part.groupId) ctx.fail("site_layout_changed", "part " + partID + " not found or has no parent story");
  return part;
}

function fetchStory(ctx, pageURL) {
  const parsed = parseURL(pageURL);
  if (!parsed) ctx.fail("not_my_site", "invalid wattpad URL: " + pageURL);

  let storyID = parsed.id;
  if (parsed.kind === "part") {
    const part = fetchPart(ctx, pageURL, parsed.id);
    storyID = part.groupId;
    if (!storyID || storyID === "0") ctx.fail("site_layout_changed", "part " + parsed.id + " has no parent story");
  }

  const story = fetchJSON(ctx, baseURL(pageURL) + "/api/v3/stories/" + storyID + "?fields=" + STORY_FIELDS);
  if (!story.title) ctx.fail("site_layout_changed", "story " + storyID + " not found or has no title");
  return story;
}

function chapterRefs(pageURL, story) {
  const parts = story.parts || [];
  return parts.map((p, i) => ({ title: clean(p.title), url: partURL(pageURL, p.id, p.title) }));
}

// The API returns the 256px thumbnail; upgrade to the 512px variant.
function coverURL(cover) {
  if (!cover) return "";
  return cover.replace("-256-", "-512-");
}

module.exports = {
  name: "wattpad",
  apiVersion: 1,
  requiresBrowser: false,

  probe: (url) => parseURL(url) !== null,

  toc: (ctx, url) => {
    const story = fetchStory(ctx, url);
    return {
      novel: {
        title: clean(story.title),
        author: (story.user ? story.user.username || "" : "").trim(),
        description: (story.description || "").trim(),
        coverUrl: coverURL(story.cover),
        language: "",
        tags: []
      },
      chapters: chapterRefs(url, story)
    };
  },

  chapter: (ctx, url) => {
    const parsed = parseURL(url);
    if (!parsed || parsed.kind !== "part") ctx.fail("not_my_site", "not a wattpad chapter URL: " + url);

    // Resolve the part to its parent story and canonical title first.
    const part = fetchPart(ctx, url, parsed.id);

    const raw = ctx.get(baseURL(url) + "/apiv2/storytext?id=" + parsed.id).body;
    const content = raw.trim();
    if (content === "") {
      ctx.fail("blocked", "part " + parsed.id + " is empty (it may be paywalled or deleted)");
    }

    return { title: clean(part.title), contentHtml: content };
  }
};
