// brightnovels.com — Laravel + Inertia (Vue SPA). The HTML is a shell; all
// data lives in a JSON blob inside the data-page attribute of the document.
//
// The series page only embeds the newest chapters, so toc() fetches the
// separate JSON endpoint /series/{slug}/chapters, which returns the full list
// newest-first (descending "index"). Chapter URLs are /series/{slug}/{slug},
// where the chapter slug is its stable number.

const clean = (s) => (s || "").replace(/\s+/g, " ").trim();

function host(url) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(url);
  return m ? m[1].toLowerCase().replace(/^www\./, "") : "";
}

// Attribute escaping, applied in reverse; &amp; last so it is not unescaped twice.
function decodeEntities(s) {
  return s
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#0?39;/g, "'")
    .replace(/&amp;/g, "&");
}

function inertiaProps(ctx, url) {
  const doc = ctx.get(url);
  const m = /data-page="([^"]*)"/.exec(doc.body);
  if (!m) ctx.fail("site_layout_changed", "no Inertia data-page payload at " + url);
  try {
    return JSON.parse(decodeEntities(m[1])).props;
  } catch (e) {
    ctx.fail("site_layout_changed", "unparseable data-page payload at " + url);
  }
}

function stripTags(s) {
  return clean(decodeEntities(String(s || "")).replace(/<[^>]*>/g, " "));
}

function novelOf(ctx, props, baseUrl) {
  const s = props.series || {};
  const tags = [].concat(s.genres || [], s.tags || [])
    .map((t) => clean(t.name))
    .filter(Boolean);

  let coverUrl = "";
  if (s.cover && s.cover.medium_url) coverUrl = ctx.resolveUrl(baseUrl, s.cover.medium_url);

  return {
    title: clean(s.title),
    author: s.user && s.user.name ? clean(s.user.name) : "",
    description: stripTags(s.description),
    coverUrl: coverUrl,
    language: "",
    tags: tags
  };
}

module.exports = {
  name: "brightnovels",
  apiVersion: 1,
  requiresBrowser: false,

  probe: (url) => host(url) === "brightnovels.com" && /\/series\/[^/?#]+\/?$/.test(url),

  toc: (ctx, url) => {
    const props = inertiaProps(ctx, url);
    const series = props.series || {};
    const novel = novelOf(ctx, props, url);

    const chaptersUrl = ctx.resolveUrl(url, "/series/" + series.slug + "/chapters");
    const resp = ctx.get(chaptersUrl);
    let list;
    try {
      list = JSON.parse(resp.body).chapters || [];
    } catch (e) {
      ctx.fail("site_layout_changed", "chapters endpoint did not return JSON at " + chaptersUrl);
    }
    if (list.length === 0) {
      ctx.fail("site_layout_changed", "no chapters found at " + chaptersUrl);
    }

    // Newest-first (descending index); the array order is the reading order.
    const chapters = list
      .slice()
      .reverse()
      .map((c) => ({
        title: c.name || c.title || "Chapter " + c.number,
        url: ctx.resolveUrl(url, "/series/" + series.slug + "/" + c.slug)
      }));

    return { novel: novel, chapters: chapters };
  },

  chapter: (ctx, url) => {
    const props = inertiaProps(ctx, url);
    const ch = props.chapter || {};
    const content = (ch.content || "").trim();
    if (!content) {
      ctx.fail("site_layout_changed", "no chapter content at " + url + " (locked or premium chapter?)");
    }
    return {
      title: ch.title || ch.name || "Chapter " + ch.number,
      contentHtml: content
    };
  }
};
