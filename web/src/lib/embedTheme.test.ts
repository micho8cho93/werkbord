import { describe, expect, it, vi } from 'vitest';
import { onStyle, onTheme } from '../../../internal/nativebridge/bridge.js';

// A page framed by the window: it believes only its parent, and only the words the window may say about its look.
function framed() {
  const listeners = new Set<(e: { source: unknown; data: unknown }) => void>();
  const parent = {};
  const w = {
    parent,
    addEventListener: (_: string, l: (e: { source: unknown; data: unknown }) => void) => listeners.add(l),
    removeEventListener: (_: string, l: (e: { source: unknown; data: unknown }) => void) => listeners.delete(l),
  };
  const say = (data: unknown, source: unknown = parent) => listeners.forEach((l) => l({ source, data }));
  return { w, say, listeners };
}

describe('the window telling a page the theme', () => {
  it('passes Light, Dark and Device, so choosing Device in Settings reaches an open page', () => {
    const { w, say } = framed();
    const seen = vi.fn();
    onTheme(seen, w);
    for (const theme of ['dark', 'light', '']) say({ type: 'werkbord.theme', theme });
    expect(seen.mock.calls).toEqual([['dark'], ['light'], ['']]);
  });
  it('ignores any other value, any other sender and any other message', () => {
    const { w, say } = framed();
    const seen = vi.fn();
    onTheme(seen, w);
    for (const theme of ['blue', 1, null, undefined]) say({ type: 'werkbord.theme', theme });
    say({ type: 'werkbord.theme', theme: 'dark' }, {});
    say({ type: 'werkbord.style', style: 'graphite' });
    expect(seen).not.toHaveBeenCalled();
  });
  it('stops listening when asked, and does nothing at all outside a window', () => {
    const { w, listeners } = framed();
    onTheme(() => {}, w)();
    expect(listeners.size).toBe(0);
    const top = { addEventListener: vi.fn() } as { parent?: unknown; addEventListener: () => void };
    top.parent = top;
    onTheme(() => {}, top)();
    expect(top.addEventListener).not.toHaveBeenCalled();
  });
});

describe('the window telling a page the style', () => {
  it('passes Paper, Graphite and Classic', () => {
    const { w, say } = framed();
    const seen = vi.fn();
    onStyle(seen, w);
    for (const style of ['paper', 'graphite', 'classic']) say({ type: 'werkbord.style', style });
    expect(seen.mock.calls).toEqual([['paper'], ['graphite'], ['classic']]);
  });
  it('ignores any other value, any other sender and any other message', () => {
    const { w, say } = framed();
    const seen = vi.fn();
    onStyle(seen, w);
    for (const style of ['', 'neon', 1, null, undefined]) say({ type: 'werkbord.style', style });
    say({ type: 'werkbord.style', style: 'graphite' }, {});
    say({ type: 'werkbord.theme', theme: 'dark' });
    expect(seen).not.toHaveBeenCalled();
  });
});
