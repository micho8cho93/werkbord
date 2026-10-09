// Service worker: makes the shell installable and opens instantly.
// API responses are never cached; state always comes from the controller.
const CACHE = 'werkbord-shell-v2';
const SHELL = ['/', '/manifest.webmanifest', '/theme.js', '/favicon.svg', '/icon.svg', '/icon-192.png', '/icon-512.png'];

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  const url = new URL(req.url);
  if (req.method !== 'GET' || url.origin !== self.location.origin || url.pathname.startsWith('/api/')) {
    return;
  }

  // A page shown in a frame, as the desktop app's window shows each workspace, is left to the network: WebKit never
  // completes a framed navigation that a worker answers, so the frame stayed blank from its second load on. (The page does
  // not register this worker when it is framed, and removes one that an earlier version registered.)
  if (req.destination === 'iframe' || req.destination === 'frame') {
    return;
  }

  // Navigations: network first so a new build is picked up, cached shell offline.
  if (req.mode === 'navigate') {
    event.respondWith(
      fetch(req)
        .then((res) => {
          const copy = res.clone();
          caches.open(CACHE).then((c) => c.put('/', copy));
          return res;
        })
        .catch(() => caches.match('/')),
    );
    return;
  }

  // Content-hashed assets never change, so cache first.
  if (url.pathname.startsWith('/assets/')) {
    event.respondWith(
      caches.match(req).then(
        (hit) =>
          hit ||
          fetch(req).then((res) => {
            const copy = res.clone();
            caches.open(CACHE).then((c) => c.put(req, copy));
            return res;
          }),
      ),
    );
  }
});
