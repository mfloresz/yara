// skynovels.net — metadata, catalog and chapter bodies all come from the JSON
// API under api.skynovels.net. The catalog is volume-major, so chapters are
// re-sorted by chapter number and then URL after collecting every volume.
//
// The API rejects requests without a Referer of https://www.skynovels.net/;
// ctx.get cannot set request headers, so the host Fetcher must supply it.
const API = "https://api.skynovels.net/api";
const IMAGE_BASE = "https://api.skynovels.net/api/get-image/";

const host = (u) => {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(u);
  return m ? m[1].toLowerCase().replace(/^www\./, "") : "";
};

function novelID(ctx, u) {
  const m = /\/novelas\/(\d+)/.exec(u);
  if (!m) ctx.fail("not_my_site", "no novel ID found in URL " + u);
  return parseInt(m[1], 10);
}

function fetchJSON(ctx, url) {
  return JSON.parse(ctx.get(url).body);
}

function fetchVolumeChapters(ctx, novel, volumeID) {
  const PAGE_SIZE = 100;
  const all = [];
  for (let page = 1; ; page++) {
    const url = API + "/novels/" + novel + "/volumes/" + volumeID + "/" + page + "/chapters?page=" + page + "&limit=" + PAGE_SIZE;
    const resp = fetchJSON(ctx, url);
    const items = (resp && resp.items) || [];
    for (const ch of items) {
      all.push({
        title: ch.chp_title,
        url: "https://www.skynovels.net/novelas/" + novel + "/chapter/" + ch.id,
        key: ch.chp_number
      });
    }
    const pagination = resp && resp.pagination;
    if (!pagination || !pagination.hasMore || items.length === 0) break;
  }
  return all;
}

function fetchAllChapters(ctx, novel) {
  const volumesResp = fetchJSON(ctx, API + "/novels/" + novel + "/volumes");
  const volumes = (volumesResp && volumesResp.volumes) || [];

  const all = [];
  for (const vol of volumes) {
    let chapters;
    try {
      chapters = fetchVolumeChapters(ctx, novel, vol.id);
    } catch (e) {
      continue;
    }
    for (const c of chapters) all.push(c);
  }

  all.sort((x, y) => x.key - y.key || (x.url < y.url ? -1 : x.url > y.url ? 1 : 0));
  return all.map((c) => ({ title: c.title, url: c.url }));
}

// Go's isZeroWidth covers unicode.Cf; the ranges below are that category.
function isFormatChar(code) {
  return (
    code === 0x00ad ||
    (code >= 0x0600 && code <= 0x0605) ||
    code === 0x061c ||
    code === 0x06dd ||
    code === 0x070f ||
    (code >= 0x0890 && code <= 0x0891) ||
    code === 0x08e2 ||
    code === 0x180e ||
    (code >= 0x200b && code <= 0x200f) ||
    (code >= 0x202a && code <= 0x202e) ||
    (code >= 0x2060 && code <= 0x2064) ||
    (code >= 0x2066 && code <= 0x206f) ||
    code === 0xfeff ||
    (code >= 0xfff9 && code <= 0xfffb) ||
    (code >= 0xe0100 && code <= 0xe01ef)
  );
}

// SkyNovels injects zero-width characters and separates paragraphs with
// blank lines; single newlines become <br> for dialogue/verse formatting.
function cleanContent(s) {
  let cleaned = "";
  for (let i = 0; i < s.length; ) {
    const code = s.codePointAt(i);
    if (!isFormatChar(code)) cleaned += s[i];
    i += code > 0xffff ? 2 : 1;
  }
  cleaned = cleaned.replace(/\r\n/g, "\n");

  const result = [];
  for (const raw of cleaned.split("\n\n")) {
    const p = raw.trim();
    if (p === "") continue;
    result.push("<p>" + p.split("\n").join("<br>") + "</p>");
  }
  return result.join("\n\n");
}

module.exports = {
  name: "skynovels",
  apiVersion: 1,
  requiresBrowser: false,

  probe: (url) => host(url) === "skynovels.net",

  toc: (ctx, url) => {
    const novel = novelID(ctx, url);
    const resp = fetchJSON(ctx, API + "/novels/" + novel + "/base");
    const n = (resp && resp.novel) || {};

    let author = n.nvl_writer || "";
    if (n.nvl_translator) {
      author = author ? author + " (trad. " + n.nvl_translator + ")" : n.nvl_translator;
    }

    return {
      novel: {
        title: n.nvl_title || "",
        author: author,
        description: n.nvl_content || "",
        coverUrl: n.image ? IMAGE_BASE + n.image + "/novels/false" : "",
        language: "",
        tags: []
      },
      chapters: fetchAllChapters(ctx, novel)
    };
  },

  chapter: (ctx, url) => {
    const m = /\/chapter\/(\d+)/.exec(url);
    if (!m) ctx.fail("not_my_site", "no chapter ID found in URL " + url);

    const resp = fetchJSON(ctx, API + "/chapters/" + m[1]);
    const ch = (resp && resp.chapter) || {};
    if (!ch.chp_content) ctx.fail("site_layout_changed", "empty chapter content at " + url);

    return { title: ch.chp_title || "", contentHtml: cleanContent(ch.chp_content) };
  }
};
