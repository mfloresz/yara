// epub.pub hosts complete EPUBs and serves them UNPACKED by path — that is
// how its online readers work, so no account is needed:
//   https://asset.epub.pub/epub/<slug>.epub/OEBPS/content.opf  — manifest + spine
//   https://asset.epub.pub/epub/<slug>.epub/<toc file>          — nav XHTML or NCX
//   https://asset.epub.pub/epub/<slug>.epub/OEBPS/<file>        — one spine document
// asset.epub.pub sits behind Cloudflare (www.epub.pub does not), so the asset
// fetches need the browser worker.
//
// The host HTML parser flattens self-closed XML tags (<item/>, <content/>),
// so the OPF/NCX are only read flat: find-all + attr, never structure.

const BOILERPLATE_TITLE = /^(cover|full.?page.?image|title page|copyright|contents|table of contents|content warning|about (this|the) book|about the author|dedication|acknowledg(e)?ments?|blurb|triggers?( include)?|epigraph|colophon|imprint|playlist|preview|excerpt|newsletter|also by\b)/i;
const BOILERPLATE_CONTAINS = /official playlist/i;

const slugOf = (url) => (url.match(/epub\.pub\/book\/([^/?#]+)/) || [])[1] || '';

module.exports = {
  name: 'epub-pub',
  apiVersion: 1,
  media: 'novel',
  requiresBrowser: true,
  probe: (url) => /^https?:\/\/(www\.)?epub\.pub\/book\/[^/?#]+/.test(url),
  toc: (ctx, url) => {
    const slug = slugOf(url);
    if (!slug) ctx.fail('site_layout_changed', 'URL does not carry a book slug');

    const page = ctx.get(url);
    const meta = (sel, attr) => {
      const n = ctx.css1(page, sel);
      return n ? (n.attr(attr) || '').trim() : '';
    };

    const opfUrl = 'https://asset.epub.pub/epub/' + slug + '.epub/OEBPS/content.opf';
    const opf = ctx.get(opfUrl);

    const items = {};
    for (const item of ctx.css(opf, 'item')) {
      const id = item.attr('id');
      if (id) {
        items[id] = {
          href: item.attr('href') || '',
          mediaType: item.attr('media-type') || '',
          properties: item.attr('properties') || '',
        };
      }
    }
    const spine = ctx.css(opf, 'itemref')
      .map((r) => r.attr('idref'))
      .filter((id) => id && items[id] && items[id].mediaType === 'application/xhtml+xml');
    if (spine.length === 0) ctx.fail('site_layout_changed', 'no xhtml spine items in content.opf');

    const absOf = (base, rel) => ctx.resolveUrl(base, rel).split('#')[0];
    const body = opf.body || '';

    // TOC flavors: EPUB3 nav.xhtml (properties="nav") or EPUB2 toc.ncx.
    let tocItem = null, ncxMode = false;
    for (const id of Object.keys(items)) {
      if (items[id].properties.split(/\s+/).indexOf('nav') !== -1) { tocItem = items[id]; break; }
    }
    if (tocItem === null) {
      const spineToc = (ctx.css1(opf, 'spine') || { attr: () => '' }).attr('toc');
      const ncxId = spineToc || Object.keys(items).find((id) => items[id].mediaType === 'application/x-dtbncx+xml');
      if (ncxId) { tocItem = items[ncxId]; ncxMode = true; }
    }
    if (tocItem === null) ctx.fail('site_layout_changed', 'no nav or ncx TOC in content.opf');

    const tocUrl = absOf(opfUrl, tocItem.href);
    const tocDoc = ctx.get(tocUrl);
    const titles = {};
    if (ncxMode) {
      for (const np of ctx.css(tocDoc, 'navpoint')) {
        const label = ctx.css1(np, 'navlabel text');
        const content = ctx.css1(np, 'content');
        const src = content ? content.attr('src') : '';
        if (label && src) titles[absOf(tocUrl, src)] = label.text.trim();
      }
    } else {
      let anchors = ctx.css(tocDoc, 'nav[role="doc-toc"] a');
      if (anchors.length === 0) anchors = ctx.css(tocDoc, 'nav li a');
      for (const a of anchors) {
        const href = a.attr('href');
        if (href && !titles[absOf(tocUrl, href)]) titles[absOf(tocUrl, href)] = a.text.trim();
      }
    }

    const chapters = [];
    for (const id of spine) {
      const href = absOf(opfUrl, items[id].href);
      const title = (titles[href] || '').replace(/\s*\|\s*\d+\s*$/, '').trim();
      if (!title || BOILERPLATE_TITLE.test(title) || BOILERPLATE_CONTAINS.test(title)) continue;
      chapters.push({ title: title, url: href });
    }
    if (chapters.length === 0) ctx.fail('site_layout_changed', 'no chapters after filtering spine/TOC');

    const dcre = (tag) => {
      const m = body.match(new RegExp('<' + tag + '[^>]*>([\\s\\S]*?)</' + tag + '>'));
      return m ? m[1].trim() : '';
    };
    let author = '';
    for (const a of ctx.css(page, 'a[rel="author"]')) {
      if (a.text.trim()) { author = a.text.trim(); break; }
    }
    if (!author) author = dcre('dc:creator');
    let title = dcre('dc:title') || (ctx.css1(page, 'h1') || { text: '' }).text.trim();

    return {
      novel: {
        title: title,
        description: meta('meta[name="description"]', 'content'),
        author: author,
        coverUrl: 'https://image.epub.pub/' + slug + '.jpg',
        language: dcre('dc:language') || 'en',
        tags: ctx.css(page, 'a[href^="/genres/"]')
          .map((a) => a.text.trim())
          .filter((t, i, all) => t && all.indexOf(t) === i),
      },
      chapters: chapters,
    };
  },
  chapter: (ctx, url) => {
    const doc = ctx.get(url);
    const content = ctx.css1(doc, 'body');
    if (content === null) ctx.fail('site_layout_changed', 'no body in spine document');
    // Spine documents are whole-file pages: body is the chapter. Vellum nests
    // it in a wrapper div, other producers hang <p> off body directly.
    const stem = decodeURIComponent((url.split('#')[0].split('/').pop() || '').replace(/\.xhtml$/i, '')).toLowerCase();
    const t = ctx.css1(doc, 'title');
    let title = t ? t.text.trim().replace(/\s*\|\s*\d+\s*$/, '') : '';
    if (title.toLowerCase() === stem) title = ''; // opaque producer names (c7H) → TOC title
    return { title: title, contentHtml: content.html };
  },
};
