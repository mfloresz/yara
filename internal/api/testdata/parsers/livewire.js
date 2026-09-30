// Test parser for the Livewire/Alpine catalog shape: the chapter list is not in
// the DOM at all, it is a JSON string inside an x-data attribute that the
// browser would normally evaluate. Exercises the JSON path of ctx.get
// (doc.body is the raw body, so the attribute is extracted and JSON.parse'd).
module.exports = {
  name: 'test-livewire-catalog',
  apiVersion: 1,
  requiresBrowser: false,

  probe: function (url) {
    return /^https?:\/\/(www\.)?skydemonorder\.com\/projects\//i.test(String(url));
  },

  toc: function (ctx, url) {
    var doc = ctx.get(url);
    if (doc.status >= 400) {
      ctx.fail('blocked', 'project page returned status ' + doc.status);
    }
    var node = ctx.css1(doc, '[x-data]');
    if (node === null) {
      ctx.fail('site_layout_changed', 'no x-data catalog container on the project page');
    }
    var raw = node.attr('x-data') || '';
    var start = raw.indexOf('JSON.parse(\'');
    if (start < 0) {
      ctx.fail('site_layout_changed', 'x-data carries no JSON.parse catalog');
    }
    var from = start + 'JSON.parse(\''.length;
    var end = raw.indexOf('\'', from);
    if (end < 0) {
      ctx.fail('site_layout_changed', 'unterminated JSON.parse catalog');
    }
    // The page escapes inner quotes as the literal sequence ², which the
    // browser's JSON.parse would decode; do the same.
    var catalog = JSON.parse(raw.slice(from, end).replace(/\\u0022/g, '"'));

    var chapters = [];
    for (var i = 0; i < catalog.length; i++) {
      var entry = catalog[i];
      var slug = String(entry.slug || '');
      if (slug === '') {
        continue;
      }
      chapters.push({
        // The catalog title carries no episode number, so it is prefixed here:
        // the host derives chapter order from the leading number in the title.
        title: entry.episode + '. ' + String(entry.title || ''),
        url: ctx.resolveUrl(doc.url, '/chapters/' + slug),
      });
    }
    if (chapters.length === 0) {
      ctx.fail('site_layout_changed', 'catalog contained no chapters');
    }

    var heading = ctx.css1(doc, 'h1.font-title');
    return {
      novel: {
        title: heading === null ? '' : heading.text,
        author: '',
        description: '',
        coverUrl: '',
        language: 'en',
        tags: [],
      },
      chapters: chapters,
    };
  },

  chapter: function (ctx, url) {
    var doc = ctx.get(url);
    var body = ctx.css1(doc, 'div.chapter-content');
    if (body === null) {
      ctx.fail('site_layout_changed', 'no div.chapter-content on the chapter page');
    }
    body.remove();
    return { title: '', contentHtml: body.html };
  },
};
