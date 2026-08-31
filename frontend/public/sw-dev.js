// Service Worker para desarrollo. NO cachea JS/CSS/HMR del dev server de Vite
// porque Vite ya gestiona el versionado de módulos y el HMR; cachearlos aquí
// provoca que se sirva código obsoleto y rompa handlers (paginación, HMR, etc).
// Solo se cachea lo necesario para servir la app offline.
const CACHE = 'yara-dev-v1';
const PRECACHE = ['/', '/index.html', '/manifest.json'];

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(CACHE).then((cache) => cache.addAll(PRECACHE)));
});

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))),
    ),
  );
});

self.addEventListener('fetch', (e) => {
  if (e.request.method !== 'GET') return;
  const url = new URL(e.request.url);

  // Nunca interceptar el dev server de Vite, el backend, ni el HMR websocket:
  // son streams con versionado por query/hash y el navegador los maneja mejor
  // sin intermediarios.
  if (url.origin !== self.location.origin) return;
  if (url.pathname.startsWith('/@')) return;          // Vite source modules
  if (url.pathname.startsWith('/node_modules/')) return;
  if (url.pathname.startsWith('/src/')) return;
  if (url.pathname.startsWith('/api') || url.pathname.startsWith('/ai')) return;
  if (url.pathname === '/sw-dev.js' || url.pathname === '/sw.js') return;

  // Network-first para todo lo demás: si falla (offline), recurrimos a la
  // caché precargada para que la app abra sin red.
  e.respondWith(
    fetch(e.request)
      .then((res) => {
        if (res.ok && url.pathname.match(/\.(html|svg|png|ico|woff2?|ttf)$/)) {
          const clone = res.clone();
          caches.open(CACHE).then((c) => c.put(e.request, clone));
        }
        return res;
      })
      .catch(() => caches.match(e.request).then((r) => r || caches.match('/index.html'))),
  );
});
