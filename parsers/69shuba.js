// 69shuba.com — chapter pages sit behind a Cloudflare challenge and the full
// catalog requires a logged-in session, so plain HTTP usually cannot complete
// an import. Charset decoding (GBK -> UTF-8) happens in the host Fetcher, not
// here: these parsers only ever see the decoded body.
const BASE = "https://www.69shuba.com";

// Below this many chapters the direct HTTP fetch did not reach the real
// catalog (the info page only links the ~5 most recent chapters), so the
// import is reported as blocked rather than silently truncating the novel.
const MIN_CHAPTERS = 20;

// The site indents paragraphs with an em space.
const EM_SPACE = String.fromCharCode(0x2003);

// Strict host check: only this site's domains (and their subdomains) are
// claimed, never a URL that merely mentions the domain in a query string.
function hostOf(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(u || "");
  if (!m) return "";
  return m[1].toLowerCase().replace(/^www\./, "");
}

function isSiteHost(u) {
  const h = hostOf(u);
  return h === "69shuba.com" || h.endsWith(".69shuba.com");
}

function bookIDOf(u) {
  const bare = /69shuba\.com\/book\/(\d+)\/?$/.exec(u);
  if (bare) return bare[1];
  const info = /69shuba\.com\/book\/(\d+)\.htm/.exec(u);
  return info ? info[1] : "";
}

// 69shuba dropped the .html extension from /txt/ chapter URLs; the
// extensionless form is canonical.
function canonicalChapterURL(url) {
  if (url.indexOf("/txt/") < 0) return url;
  return url.replace(/\.html$/, "");
}

function absolute(href) {
  if (href.indexOf("http") === 0) return href;
  return BASE + href;
}

function fromInlineJS(body, key) {
  const m = new RegExp(key + "\\s*:\\s*'([^']+)'").exec(body);
  return m ? m[1] : "";
}

function fetchChapterList(ctx, infoURL) {
  let doc;
  try {
    doc = ctx.get(infoURL).body;
  } catch (e) {
    return null;
  }

  // The site answers 200 with a "page not found" body when the catalog needs
  // a login.
  const titleNode = ctx.css1(doc, "title");
  const pageText = titleNode ? titleNode.text : "";
  if (pageText.indexOf("404") >= 0 || pageText.indexOf("页面目不存在") >= 0 || pageText.indexOf("页面不存在") >= 0) {
    return null;
  }

  let chapters = [];
  const selectors = [
    "#catalog ul li a",
    "div.catalog ul li a",
    "ul.chapter-list li a",
    ".listmain li a",
    "#list li a",
    ".booklist li a",
    ".volume li a",
    ".qustime li a"
  ];
  for (const sel of selectors) {
    for (const a of ctx.css(doc, sel)) {
      const href = a.attr("href");
      if (href === null) continue;
      chapters.push({ title: a.text, url: canonicalChapterURL(absolute(href)) });
    }
    if (chapters.length > 0) break;
  }

  // Fallback: any link that looks like a chapter URL.
  if (chapters.length === 0) {
    for (const a of ctx.css(doc, "a")) {
      const href = a.attr("href");
      if (href === null) continue;
      const text = a.text;
      if (text === "") continue;
      if (href.indexOf("/txt/") >= 0 || href.indexOf("/chapter/") >= 0 || href.indexOf("/read/") >= 0) {
        chapters.push({ title: text, url: canonicalChapterURL(absolute(href)) });
      }
    }
  }

  // The catalog lists chapters newest-first; the app expects chronological.
  chapters.reverse();
  return chapters;
}

// The ~5 most recent chapters the info page links in its sidebar.
function chaptersFromInfoPage(ctx, doc) {
  const chapters = [];
  for (const sel of ctx.css(doc, ".qustime ul li a")) {
    const href = sel.attr("href");
    if (href === null) continue;
    let title = "";
    const span = ctx.css1(sel, "span");
    if (span && span.text) title = span.text;
    if (!title) title = sel.text;
    const small = ctx.css1(sel, "small");
    if (small && small.text) title = title.replace(small.text, "").trim();
    chapters.push({ title: title, url: canonicalChapterURL(absolute(href)) });
  }
  return chapters;
}

function attrOf(ctx, doc, selector) {
  const n = ctx.css1(doc, selector);
  return n ? n.attr("content") || "" : "";
}

module.exports = {
  name: "69shuba",
  apiVersion: 1,
  requiresBrowser: true,

  probe: isSiteHost,

  // The Go parser had separate info-page and chapter-page entry points that
  // differed only in whether they read the description/cover metas; from a
  // /txt/ URL neither yields a usable catalog, so both collapse here.
  toc: (ctx, url) => {
    const doc = ctx.get(url).body;

    let title = fromInlineJS(doc, "articlename");
    let author = fromInlineJS(doc, "author");
    if (!title) {
      const n = ctx.css1(doc, "meta[property='og:novel:book_name']");
      if (n) title = n.attr("content") || "";
    }
    if (!title) {
      const n = ctx.css1(doc, ".booknav2 h1");
      if (n) title = n.text;
    }
    if (!author) {
      const n = ctx.css1(doc, "meta[property='og:novel:author']");
      if (n) author = n.attr("content") || "";
    }
    if (!author) {
      const n = ctx.css1(doc, ".booknav2 p");
      if (n) author = n.text.replace(/^作者：/, "");
    }

    let description = attrOf(ctx, doc, "meta[property='og:description']");
    if (!description) description = attrOf(ctx, doc, "meta[name='description']");

    // Build the catalog URL from the book id; /book/{id}/ holds the full list
    // but needs a logged-in session (or the browser proxy).
    let bookID = bookIDOf(url);
    if (bookID === "") {
      const m = /articleid:\s*'(\d+)'/.exec(doc);
      if (m) bookID = m[1];
    }

    let chapters = null;
    if (bookID !== "") {
      const fromCatalog = fetchChapterList(ctx, BASE + "/book/" + bookID + "/");
      if (fromCatalog && fromCatalog.length >= MIN_CHAPTERS) chapters = fromCatalog;
    }
    if (chapters === null) chapters = chaptersFromInfoPage(ctx, doc);

    if (chapters.length < MIN_CHAPTERS) {
      ctx.fail(
        "blocked",
        "only got " + chapters.length + "/" + MIN_CHAPTERS +
          " chapters via direct HTTP (needs the browser proxy with a logged-in session for the full catalog)"
      );
    }

    return {
      novel: {
        title: title,
        author: author,
        description: description,
        coverUrl: attrOf(ctx, doc, "meta[property='og:image']"),
        language: "",
        tags: []
      },
      chapters: chapters
    };
  },

  chapter: (ctx, url) => {
    const doc = ctx.get(url).body;

    const sel = ctx.css1(doc, ".txtnav") || ctx.css1(doc, "#content");
    if (!sel) ctx.fail("site_layout_changed", "no content found at " + url);

    const h1 = ctx.css1(sel, "h1");
    const title = h1 ? h1.text : "";

    for (const n of ctx.css(sel, "h1")) n.remove();
    for (const n of ctx.css(sel, "div.txtinfo")) n.remove();
    for (const n of ctx.css(sel, "#txtright")) n.remove();
    for (const n of ctx.css(sel, "div.txtright")) n.remove();
    for (const n of ctx.css(sel, "script, style, noscript, iframe, ins, .ad, .ads, .advert")) n.remove();
    for (const n of ctx.css(sel, "*")) {
      const style = n.attr("style");
      if (style === null) continue;
      const lower = style.toLowerCase();
      if (lower.indexOf("display:none") >= 0 || lower.indexOf("display: none") >= 0) n.remove();
    }

    const content = sel.html.split(EM_SPACE).join(" ").trim();
    if (content === "") ctx.fail("site_layout_changed", "empty content at " + url);

    return { title: title, contentHtml: content };
  }
};
