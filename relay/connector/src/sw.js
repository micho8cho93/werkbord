// The connector's service worker. It sits between the app and the network and answers every request the app makes of
// its own origin by asking the controller on your computer, through the relay, end-to-end encrypted. The app is not
// stored here: it comes from your computer, so it is always the version your computer runs.
//
// It must be served as /sw.js: the app registers /sw.js itself, and a worker that is already that script is left alone.
import { Link, LinkError, decodePairing } from "./link.js";
import { kvGet } from "./store.js";

const CACHE_ASSETS = "werkbord-assets-1";
const CACHE_PAGES = "werkbord-connector-2";
const STATUS_PAGE = "/connector/status.html";
const PAGES = [STATUS_PAGE, "/connector/style.css", "/connector/lanes.js", "/connector/lanes.css"];
const MAX_ASSETS = 120;
// Not tunnelled: the worker itself, the page that pairs, and the connector's own pages.
const STATIC = (p) => p === "/sw.js" || p.startsWith("/connect/") || p.startsWith("/connector/");
// Headers worth sending. The controller only reads these too (internal/relay/tunnel).
const FORWARD = ["accept", "accept-language", "authorization", "content-type", "if-none-match", "if-modified-since", "range"];

const WORDS = {
  team_unpaired: ["Ask your admin to turn on phone access", "If phone access is already enabled, open Team Settings on your member computer and pair this phone."],
  offline: ["Your computer is offline", "Werkbord cannot reach your computer. It may be asleep, shut down or without internet. This page tries again by itself."],
  unavailable: ["Temporarily unavailable", "The connection service is busy or cannot be reached right now. Nothing is wrong with your computer. This page tries again by itself."],
  removed: ["This phone was signed out", "Your computer no longer knows this phone. On your computer, open Werkbord, then Settings, then Phones, and scan a new code."],
  invalid: ["This link is out of date", "It does not match your computer any more. On your computer, open Werkbord, then Settings, then Phones, and scan a new code."],
};

self.addEventListener("install", (event) => {
  event.waitUntil(caches.open(CACHE_PAGES).then((c) => c.addAll(PAGES)).then(() => self.skipWaiting()));
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

// ---- the connection ----

const lanes = {
  individual: { link: null, opening: null, failure: null, generation: 0 },
  team: { link: null, opening: null, failure: null, generation: 0 },
};

function retryAfter(f) {
  if (f.error.reason === "removed" || f.error.reason === "invalid") return Infinity; // only a new code helps
  return Math.min(2000 * 2 ** (f.count - 1), 60_000);
}

// A person opening or reloading the app is asking now, and is owed a fresh answer rather than the last one; the app's own
// background requests are what the backoff is for. Nothing is retried for a phone that was removed: only a new code helps.
async function getLink(asked, lane = "individual") {
  const state = lanes[lane];
  if (state.link?.alive) return state.link;
  if (state.opening) return state.opening;
  if (state.failure && (!asked || state.failure.error.reason === "removed" || state.failure.error.reason === "invalid") && Date.now() - state.failure.at < retryAfter(state.failure)) throw state.failure.error;
  const raw = await kvGet(lane === "team" ? "pairing:team" : "pairing").catch(() => null);
  if (!raw) throw new LinkError(lane === "team" ? "team_unpaired" : "unpaired", "no phone pairing");
  const generation = state.generation;
  state.opening = (async () => {
    try {
      const l = new Link(decodePairing(raw));
      await l.open();
      if (state.generation !== generation) { l.close(); throw new LinkError("unavailable", "pairing changed"); }
      state.link = l;
      state.failure = null;
      return l;
    } catch (e) {
      const error = e instanceof LinkError ? e : new LinkError("unavailable", String(e));
      if (state.generation === generation) state.failure = { error, at: Date.now(), count: (state.failure?.count || 0) + 1 };
      throw error;
    } finally {
      if (state.generation === generation) state.opening = null;
    }
  })();
  return state.opening;
}

function forget(lane) {
  const state = lanes[lane];
  state.generation++;
  state.opening = null;
  state.link?.close();
  state.link = null;
  state.failure = null;
}

// The pairing page talks to this worker: "connect" checks the whole path and reports why it failed; "reset" is sent
// after a new pairing is saved, so nothing from the old one is kept.
self.addEventListener("message", (event) => {
  const port = event.ports[0];
  const data = event.data || {};
  const lane = data.lane === "team" ? "team" : "individual";
  if (data.type === "reset") forget(lane);
  if (data.type === "reset" || data.type === "connect") {
    event.waitUntil(
      getLink(false, lane).then(
        () => port?.postMessage({ ok: true }),
        (e) => port?.postMessage({ ok: false, reason: e.reason || "unavailable" }),
      ),
    );
  }
});

// ---- requests ----

self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);
  if (url.origin !== self.location.origin || STATIC(url.pathname)) return;
  event.respondWith(answer(event, url));
});

async function answer(event, url) {
  const req = event.request;
  const isAsset = req.method === "GET" && url.pathname.startsWith("/assets/");
  if (isAsset) {
    const hit = await caches.match(req);
    if (hit) return hit; // content-hashed: never changes, and costs the relay nothing
  }
  let res;
  try {
    const lane = url.pathname === "/team" || url.pathname.startsWith("/team/") ? "team" : "individual";
    res = await viaLink(req, url, lane);
    if (req.mode === "navigate" && res.ok && res.headers.get("content-type")?.includes("text/html")) {
      const html = await res.text();
      const headers = new Headers(res.headers); headers.delete("content-length");
      res = new Response(html.replace("</head>", '<script src="/connector/lanes.js" defer></script><link rel="stylesheet" href="/connector/lanes.css"></head>'), {status:res.status, headers});
    }
  } catch (e) {
    return failed(e, req);
  }
  if (isAsset && res.ok) {
    event.waitUntil(storeAsset(req, res.clone()).catch(() => {}));
  }
  return res;
}

async function storeAsset(req, res) {
  const cache = await caches.open(CACHE_ASSETS);
  await cache.put(req, res);
  const keys = await cache.keys();
  for (const k of keys.slice(0, Math.max(0, keys.length - MAX_ASSETS))) await cache.delete(k);
}

async function viaLink(req, url, lane) {
  const body = req.method === "GET" || req.method === "HEAD" ? null : new Uint8Array(await req.arrayBuffer());
  const headers = {};
  for (const name of FORWARD) {
    const v = req.headers.get(name);
    if (v) headers[name] = v;
  }
  const send = async () => {
    const l = await getLink(req.mode === "navigate", lane);
    return l.request({ method: req.method, path: (lane === "team" ? url.pathname.slice(5) || "/" : url.pathname) + url.search, headers, body, signal: req.signal });
  };
  let r;
  try {
    r = await send();
  } catch (e) {
    // The connection can be gone since the last request (a phone that slept). A read is safe to ask again, once.
    if (req.method !== "GET" || e?.reason !== "unavailable" || lanes[lane].link?.alive) throw e;
    r = await send();
  }
  const bodyless = req.method === "HEAD" || r.status === 204 || r.status === 205 || r.status === 304;
  return new Response(bodyless ? null : r.body, { status: r.status, headers: r.headers });
}

function failed(e, req) {
  const reason = e?.reason || "unavailable";
  if (reason === "unpaired") {
    // Nothing to connect to yet. The first page is the connector's own, which says to scan a code.
    return req.mode === "navigate" ? fetch(req) : json(401, "unpaired", "This phone is not paired yet.");
  }
  if (req.mode === "navigate") return statusPage(reason);
  const [title] = WORDS[reason] || WORDS.unavailable;
  return json(503, "relay_" + reason, title + ".");
}

function json(status, code, message) {
  return new Response(JSON.stringify({ error: { code, message } }), {
    status,
    headers: { "content-type": "application/json", "cache-control": "no-store", "x-werkbord-relay": code },
  });
}

async function statusPage(reason) {
  const [title, text] = WORDS[reason] || WORDS.unavailable;
  const cached = await caches.match(STATUS_PAGE);
  let html = cached ? await cached.text() : "<!doctype html><title>Werkbord</title><h1>__TITLE__</h1><p>__TEXT__</p>";
  html = html.replace("</head>", '<script src="/connector/lanes.js" defer></script><link rel="stylesheet" href="/connector/lanes.css"></head>');
  html = html.replace("__REASON__", reason).replace("__TITLE__", title).replace("__TEXT__", text);
  return new Response(html, { status: 200, headers: { "content-type": "text/html; charset=utf-8", "cache-control": "no-store" } });
}
