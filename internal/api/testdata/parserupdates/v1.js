module.exports = {
  name: 'update-test',
  apiVersion: 1,
  probe: (url) => /^https?:\/\/site\.test\/book\/$/.test(url),
  toc: (ctx, url) => ({ novel: { title: 'v1' }, chapters: [] }),
  chapter: (ctx, url) => ({ title: '', contentHtml: '<p>v1</p>' }),
};
