module.exports = {
  name: 'update-test',
  apiVersion: 1,
  probe: (url) => /^https?:\/\/site\.test\/(book|magazine)\/$/.test(url),
  toc: (ctx, url) => ({ novel: { title: 'v2' }, chapters: [] }),
  chapter: (ctx, url) => ({ title: '', contentHtml: '<p>v2</p>' }),
};
