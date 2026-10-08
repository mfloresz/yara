module.exports = {
  name: 'asianovel',
  apiVersion: 1,
  requiresBrowser: false,
  probe: (url) => /^https?:\/\/(www\.)?asianovel\.net\/story\/\d+\/?$/.test(url),
  toc: (ctx, url) => {
    const doc = ctx.get(url);
    const section = ctx.css1(doc, 'section.story__chapters');
    if (section === null) ctx.fail('site_layout_changed', 'no chapter section');
    const ol = ctx.css1(section, 'ol.chapter-group__list');
    if (ol === null) ctx.fail('site_layout_changed', 'no chapter list');
    const chapters = ctx.css(ol, 'li.chapter-group__list-item a').map((a) => ({
      title: a.text.trim(),
      url: ctx.resolveUrl(url, a.href),
    }));
    // Extract novel metadata
    const title = ctx.css1(doc, 'meta[property="og:title"]')?.attr('content') || '';
    const coverUrl = ctx.css1(doc, 'meta[property="og:image"]')?.attr('content') || '';
    const description = ctx.css1(doc, 'meta[property="og:description"]')?.attr('content') || '';
    // Author from link
    let author = '';
    const authorLinks = ctx.css(doc, 'a[href*="/author/"]');
    if (authorLinks.length > 0) {
      author = authorLinks[0].text.trim();
    }
    // Tags from meta tags
    const tags = ctx.css(doc, 'meta[property="article:tag"]').map((m) => m.attr('content'));
    return {
      novel: {
        title: title || ctx.css1(doc, 'title')?.text?.replace(/ - Asianovel$/, '').trim() || '',
        description: description,
        author: author,
        coverUrl: coverUrl,
        language: 'en',
        tags: tags,
      },
      chapters: chapters,
    };
  },
  chapter: (ctx, url) => {
    const doc = ctx.get(url);
    // Title is in h1
    const h1 = ctx.css1(doc, 'h1');
    const title = h1 ? h1.text.trim() : '';
    // Content is in the div with formatting classes
    const contentDiv = ctx.css1(doc, 'div.resize-font');
    if (contentDiv === null) ctx.fail('site_layout_changed', 'no content block');
    // Remove ad blocks if present
    const ads = ctx.css(contentDiv, 'div.asian-ads-top-content, div.asian-ads-bottom-content');
    for (const ad of ads) {
      ad.remove();
    }
    return { title: title, contentHtml: contentDiv.html };
  },
};
