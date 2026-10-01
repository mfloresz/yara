// Test parser: reproduces the markup shape the api package's mock servers
// serve for novelfire.net, so the engine-driven import/update/redownload flows
// are exercised end-to-end without depending on the retired Go parsers.
module.exports = {
  name: 'test-novelfire',
  apiVersion: 1,
  requiresBrowser: false,

  probe: function (url) {
    return /^https?:\/\/(www\.)?novelfire\.net(\/|$)/i.test(String(url));
  },

  toc: function (ctx, url) {
    var doc = ctx.get(url);
    if (doc.status >= 400) {
      ctx.fail('blocked', 'index returned status ' + doc.status);
    }
    var list = ctx.css1(doc, 'ul.chapter-list');
    if (list === null) {
      ctx.fail('site_layout_changed', 'no ul.chapter-list on the index page');
    }

    var chapters = [];
    var nodes = ctx.css(list, 'li a');
    for (var i = 0; i < nodes.length; i++) {
      var title = ctx.css1(nodes[i], 'span.chapter-title');
      chapters.push({
        title: title === null ? nodes[i].text : title.text,
        url: nodes[i].href,
      });
    }

    var heading = ctx.css1(doc, 'div.main-head h1');
    var author = ctx.css1(doc, 'span[itemprop="author"]');
    var description = ctx.css1(doc, 'meta[itemprop="description"]');
    var cover = ctx.css1(doc, 'meta[property="og:image"]');

    return {
      novel: {
        title: heading === null ? '' : heading.text,
        author: author === null ? '' : author.text,
        description: description === null ? '' : (description.attr('content') || ''),
        coverUrl: cover === null ? '' : ctx.resolveUrl(doc.url, cover.attr('content') || ''),
        language: 'en',
        tags: [],
      },
      chapters: chapters,
    };
  },

  chapter: function (ctx, url) {
    var doc = ctx.get(url);
    if (doc.status >= 400) {
      ctx.fail('blocked', 'chapter returned status ' + doc.status);
    }
    var body = ctx.css1(doc, 'div.chapter-content');
    if (body === null) {
      ctx.fail('site_layout_changed', 'no div.chapter-content on the chapter page');
    }
    body.remove();
    var heading = ctx.css1(doc, 'span.chapter-title');
    return {
      title: heading === null ? '' : heading.text,
      contentHtml: body.html,
    };
  },
};
