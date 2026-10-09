// dmxs.org — Chinese BL novel site (GBK charset; GBK -> UTF-8 decoding happens
// in the host Fetcher, not here: this script only ever sees the decoded body).
//
// Book pages live at /<category>/<id>.html (e.g. /BLTR/25084.html) and list
// every slice in div.book_list. Chapter pages live at
// /view/<classid>-<bookid>-<n>.html and hold a fixed-size slice of the novel
// (~100 <p>), NOT exactly one chapter: a slice starts/ends mid-chapter and
// chapter headers (第N章) fall mid-page. Slices are contiguous, so a 1:1
// mapping of book_list entry -> view page covers the novel with no overlap.
// The only cleanup needed is dropping the book-intro block (title/author/
// blurb) that precedes the first header on page 1.
function hostOf(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(u || "");
  if (!m) return "";
  return m[1].toLowerCase().replace(/^www\./, "");
}

function isSiteHost(u) {
  const h = hostOf(u);
  return h === "dmxs.org" || h.endsWith(".dmxs.org");
}

// Book pages are /<category>/<id>.html. Chapter (/view/<a>-<b>-<n>.html),
// txt-download (/txt/...) and author pages never match: their second segment
// is not a bare number.
function isBookPage(u) {
  return (
    isSiteHost(u) && /dmxs\.org\/[A-Za-z]+\/\d+\.html\/?(?:[?#]|$)/.test(u)
  );
}

function isChapterHeader(t) {
  return /^第\d+章$/.test((t || "").trim());
}

module.exports = {
  name: "dmxs",
  apiVersion: 1,
  requiresBrowser: false,
  probe: isBookPage,
  toc: (ctx, url) => {
    const doc = ctx.get(url);
    const info = ctx.css1(doc, "div.book_info div.infos");
    if (info === null) ctx.fail("site_layout_changed", "no book info block");
    const h1 = ctx.css1(info, "h1");
    if (h1 === null) ctx.fail("site_layout_changed", "no book title");

    let author = "";
    const authorLink = ctx.css1(info, "div.date a");
    if (authorLink !== null) author = authorLink.text.trim();

    let description = "";
    const desc = ctx.css1(info, "p");
    if (desc !== null) description = desc.text.trim();

    const tags = ctx.css(info, "div.tags a").map((a) => a.text.trim());

    const list = ctx.css1(doc, "div.book_list");
    if (list === null) ctx.fail("site_layout_changed", "no chapter list");
    const chapters = ctx
      .css(list, "ul li a")
      .map((a) => ({
        title: a.text.trim(),
        url: ctx.resolveUrl(url, a.href),
      }))
      .filter((c) => c.title !== "" && c.url !== "");
    if (chapters.length === 0)
      ctx.fail("site_layout_changed", "empty chapter list");

    return {
      novel: {
        title: h1.text.trim(),
        description: description,
        author: author,
        coverUrl: "",
        language: "",
        tags: tags,
      },
      chapters: chapters,
    };
  },
  chapter: (ctx, url) => {
    const doc = ctx.get(url);
    const body = ctx.css1(doc, "div.read_chapterDetail");
    if (body === null) ctx.fail("site_layout_changed", "no content block");

    const h1 = ctx.css1(doc, "div.read_chapterName h1");
    const title = h1 ? h1.text.trim() : "";

    // Page 1 starts with the book-intro block (title/author/blurb/tags)
    // before the first 第N章 header; later pages start mid-story. Drop the
    // intro only when the lead-in actually looks like one.
    const paras = ctx.css(body, "p");
    let start = 0;
    for (let i = 0; i < paras.length; i++) {
      if (isChapterHeader(paras[i].text)) {
        let intro = false;
        for (let j = 0; j < i; j++) {
          const t = paras[j].text;
          if (t.indexOf("作者：") >= 0 || t.indexOf("简介：") >= 0) {
            intro = true;
            break;
          }
        }
        if (intro) start = i;
        break;
      }
    }
    for (let i = 0; i < start; i++) paras[i].remove();

    // The site uses byte 0x80 as a middle dot (e.g. in transliterated
    // names); GBK decoding turns it into U+20AC. A literal euro sign
    // never appears in these novels, so normalize it to · (U+00B7),
    // matching the middle dots the site encodes properly elsewhere.
    const contentHtml = body.html.split("€").join("·").trim();
    if (contentHtml === "")
      ctx.fail("site_layout_changed", "empty content block");
    return { title: title, contentHtml: contentHtml };
  },
};
