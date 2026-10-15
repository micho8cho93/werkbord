import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

// Exercise the real worker routing with a transport double; crypto and the real relay
// are independently covered by the browser tunnel and Team encrypted-session tests.
function worker(pairings = new Map([['pairing', 'individual'], ['pairing:team', 'team']])) {
  const handlers = new Map();
  const connections = [];
  class LinkError extends Error { constructor(reason, message) { super(message); this.reason = reason; } }
  class Link {
    constructor(pairing) { this.pairing = pairing; this.alive = false; connections.push(this); }
    async open() { this.alive = true; }
    close() { this.alive = false; }
    async request(request) {
      return { status: 200, headers: { 'content-type': 'application/json' }, body: JSON.stringify({ pairing: this.pairing, path: request.path }) };
    }
  }
  const context = vm.createContext({
    self: { location: { origin: 'https://connect.example' }, addEventListener: (name, fn) => handlers.set(name, fn) },
    Link, LinkError, decodePairing: (raw) => raw, kvGet: async (key) => pairings.get(key),
    URL, Headers, Response, Date, Uint8Array,
    caches: { match: async () => null },
  });
  const source = readFileSync(new URL('../src/sw.js', import.meta.url), 'utf8').replace(/^import .*;$/gm, '');
  vm.runInContext(source, context);
  return {
    connections, pairings,
    async fetch(path, mode = 'cors') {
      let response;
      handlers.get('fetch')({ request: { url: 'https://connect.example' + path, method: 'GET', mode, headers: new Headers() }, respondWith: (p) => { response = p; }, waitUntil: () => {} });
      return response;
    },
    async reset(lane) {
      let result;
      await new Promise((resolve) => handlers.get('message')({ data: { type: 'reset', lane }, ports: [{ postMessage: (r) => { result = r; } }], waitUntil: (p) => p.then(resolve) }));
      return result;
    },
  };
}

test('Team and Individual requests keep independent pairings and connections', async () => {
  const w = worker();
  assert.deepEqual(await (await w.fetch('/api/projects')).json(), { pairing: 'individual', path: '/api/projects' });
  assert.deepEqual(await (await w.fetch('/team/api/summary?x=1')).json(), { pairing: 'team', path: '/api/summary?x=1' });
  assert.deepEqual(await (await w.fetch('/api/projects')).json(), { pairing: 'individual', path: '/api/projects' });
  assert.equal(w.connections.length, 2);
});

test('re-pairing Team never resets Individual', async () => {
  const w = worker();
  await w.fetch('/api/projects'); await w.fetch('/team/api/summary');
  w.pairings.set('pairing:team', 'new-team');
  assert.equal((await w.reset('team')).ok, true);
  assert.equal(w.connections[0].alive, true);
  assert.equal(w.connections[1].alive, false);
  assert.deepEqual(await (await w.fetch('/team/api/summary')).json(), { pairing: 'new-team', path: '/api/summary' });
  assert.equal(w.connections.length, 3);
});

test('an unconfigured Team lane explains how to get access', async () => {
  const w = worker(new Map([['pairing', 'individual']]));
  const response = await w.fetch('/team/', 'navigate');
  assert.match(await response.text(), /Ask your admin to turn on phone access/);
  assert.equal(w.connections.length, 0);
});
