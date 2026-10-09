import { describe, expect, it, vi } from 'vitest';
import source from '../../public/sw.js?raw';

// public/sw.js is a plain script, so it is run here against a stand-in for the worker's global scope.
type FetchHandler = (event: { request: Record<string, string>; respondWith: (p: unknown) => void }) => void;

function load(): FetchHandler {
  const handlers: Record<string, FetchHandler> = {};
  const self = { location: { origin: 'http://127.0.0.1:7499' }, addEventListener: (type: string, h: FetchHandler) => (handlers[type] = h) };
  const caches = { open: vi.fn(() => Promise.resolve({ put: vi.fn() })), match: vi.fn(() => Promise.resolve(undefined)), keys: vi.fn(() => Promise.resolve([])) };
  const fetch = vi.fn(() => Promise.resolve({ clone: () => ({}) }));
  new Function('self', 'caches', 'fetch', source)(self, caches, fetch);
  return handlers.fetch;
}

function answered(request: Record<string, string>): boolean {
  const respondWith = vi.fn();
  load()({ request: { method: 'GET', mode: 'cors', destination: '', ...request }, respondWith });
  return respondWith.mock.calls.length > 0;
}

describe('service worker', () => {
  const page = 'http://127.0.0.1:7499/';

  it('leaves a navigation in a frame to the network: WebKit never completes one a worker answers (the desktop window)', () => {
    expect(answered({ url: page, mode: 'navigate', destination: 'iframe' })).toBe(false);
    expect(answered({ url: page, mode: 'navigate', destination: 'frame' })).toBe(false);
  });

  it('still answers a page opened in a browser tab, and the assets it loads', () => {
    expect(answered({ url: page, mode: 'navigate', destination: 'document' })).toBe(true);
    expect(answered({ url: page + 'assets/index-abc.js', destination: 'script' })).toBe(true);
  });

  it('never answers the API, another origin, or anything but a GET', () => {
    expect(answered({ url: page + 'api/projects' })).toBe(false);
    expect(answered({ url: 'http://example.com/', mode: 'navigate', destination: 'document' })).toBe(false);
    expect(answered({ url: page, method: 'POST', mode: 'navigate', destination: 'document' })).toBe(false);
  });
});
