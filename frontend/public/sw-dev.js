// Service Worker para desarrollo - versión simple
const CACHE = 'yara-dev-v1';

self.addEventListener('install', (e) => {
  e.waitUntil(
    caches.open(CACHE).then((cache) => cache.addAll(['/', '/index.html', '/manifest.json']))
  );
});

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url);
  
  // Servir index.html desde caché si estamos offline
  if ((url.pathname === '/' || url.pathname === '/index.html') && !navigator.onLine) {
    e.respondWith(
      caches.match('/index.html').then((res) => {
        return res || caches.match('/') || new Response('', { status: 200 });
      })
    );
    return;
  }
  
  // Para otros assets, intentar caché primero, luego red
  e.respondWith(
    caches.match(e.request).then((res) => {
      return res || fetch(e.request).then((r) => {
        if (r.ok && (e.request.method === 'GET')) {
          const clone = r.clone();
          caches.open(CACHE).then((c) => c.put(e.request, clone));
        }
        return r;
      });
    })
  );
});
