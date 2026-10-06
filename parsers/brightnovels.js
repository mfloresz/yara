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
    let payload;
    try {
      payload = JSON.parse(resp.body);
    } catch (e) {
      ctx.fail("site_layout_changed", "chapters endpoint did not return JSON at " + chaptersUrl);
    }
    const list = payload.chapters || [];
    if (list.length === 0) {
      ctx.fail("site_layout_changed", "no chapters found at " + chaptersUrl);
    }

    // Premium chapters are paywalled: the endpoint still lists them, but their
    // content is only a first-paragraph preview, so they are skipped at TOC
    // time (same policy as foxaholic/flyonthewalls). unlocked_at and the
    // top-level unlockedChapterIds keep anything a logged-in reader already
    // paid for — their browser worker can still fetch it.
    const unlockedIds = new Set(payload.unlockedChapterIds || []);
    const now = new Date();
    const free = list.filter((c) => {
      if (!c.is_premium) return true;
      if (unlockedIds.has(c.id)) return true;
      if (!c.unlocked_at) return false;
      // unlocked_at may be a future release date; only count as unlocked
      // if the date has already passed.
      const unlockedDate = new Date(c.unlocked_at);
      return unlockedDate <= now;
    });
    if (free.length === 0) {
      ctx.fail("site_layout_changed", "all " + list.length + " listed chapters are premium at " + chaptersUrl);
    }

    // Newest-first (descending index); the array order is the reading order.
    const chapters = free
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
    // A locked premium chapter still ships a first-paragraph preview as its
    // content, so gate on the access flags, not on emptiness.
    if (ch.is_premium && !ch.isUnlocked) {
      ctx.fail("blocked", "chapter is premium-locked at " + url + " (a logged-in browser worker can fetch it)");
    }
    const content = (ch.content || "").trim();
    if (!content) {
      ctx.fail("site_layout_changed", "no chapter content at " + url);
    }
    return {
      title: ch.title || ch.name || "Chapter " + ch.number,
      contentHtml: content
    };
  }
};
