import source from '../../../internal/uipresentation/static/motion.js?raw';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { layout } from './motion';

class Item {
  isConnected = true;
  dataset: Record<string, string>;
  style = {};
  constructor(public key: string, public parentElement: object, public left = 0, public top = 0) { this.dataset = { motionKey: key }; }
  getBoundingClientRect() { return { left: this.left, top: this.top, width: 180, height: 100 }; }
  getClientRects() { return [this.getBoundingClientRect()]; }
  querySelectorAll() { return []; }
  cloneNode() { return new Item(this.key, this.parentElement, this.left, this.top); }
  hasAttribute() { return false; }
  removeAttribute() { /* A ghost drops product identity. */ }
  setAttribute() { /* A ghost is presentation only. */ }
  remove() { this.isConnected = false; }
}
function harness() {
  let reduced = false;
  const changes: (() => void)[] = [];
  const animations: { cancelled: boolean; cancel(): void }[] = [];
  const ghosts: Item[] = [];
  const window = { WerkbordMotion: undefined as unknown as {
    snapshot(root: object, selector?: string): unknown;
    layout(root: object, before: unknown, selector?: string, entries?: boolean): void;
    animate(node: object, frames: object[], duration?: number): unknown;
    dispose(root: object): void;
  }, matchMedia: () => ({ get matches() { return reduced; }, addEventListener: (_: string, cb: () => void) => changes.push(cb) }) };
  const document = { hidden: false, documentElement: { toggleAttribute: vi.fn() }, addEventListener: vi.fn(), body: { append: (item: Item) => ghosts.push(item) } };
  class AnimatedItem extends Item {
    animate() {
      let reject: (error: Error) => void = () => {};
      const finished = new Promise<void>((_, no) => { reject = no; });
      const a = { finished, cancelled: false, cancel() { this.cancelled = true; reject(new Error('cancelled')); } };
      animations.push(a); return a;
    }
    cloneNode() { return new AnimatedItem(this.key, this.parentElement, this.left, this.top); }
  }
  const parent = {};
  const item = new AnimatedItem('task-1', parent);
  const root = { clientWidth: 1000, scrollLeft: 0, scrollTop: 0, items: [item], querySelectorAll() { return this.items; } };
  // Run the exact neutral browser script against isolated DOM stand-ins.
  new Function('window', 'document', 'IntersectionObserver', source)(window, document, class {});
  return { window, document, root, item, animations, ghosts, parent, preference(value: boolean) { reduced = value; changes.forEach(cb => cb()); } };
}
afterEach(() => vi.unstubAllGlobals());

describe('confirmed layout motion', () => {
  it('establishes initial data and reconnects without replaying card entrances', async () => {
    const h = harness(); vi.stubGlobal('window', h.window);
    const action = layout(h.root as unknown as HTMLElement, { revision: 'loading', enabled: false, entries: true });
    await Promise.resolve();
    action.update({ revision: 'loaded', enabled: true, entries: true });
    await Promise.resolve();
    expect(h.animations).toHaveLength(0);
    h.item.left = 200;
    action.update({ revision: 'saved', enabled: true, entries: true });
    await Promise.resolve();
    expect(h.animations).toHaveLength(1);
    action.update({ revision: 'disconnected', enabled: false, entries: true }); await Promise.resolve();
    h.item.left = 400;
    action.update({ revision: 'reconnected', enabled: true, entries: true }); await Promise.resolve();
    expect(h.animations).toHaveLength(1);
    action.destroy();
  });

  it('does not treat scrolling, resizing or unchanged data as a task move', () => {
    const h = harness(), api = h.window.WerkbordMotion;
    let before = api.snapshot(h.root);
    api.layout(h.root, before); expect(h.animations).toHaveLength(0);
    h.item.left = 200; h.root.scrollLeft = 200;
    api.layout(h.root, before); expect(h.animations).toHaveLength(0);
    before = api.snapshot(h.root); h.root.clientWidth = 700; h.item.left = 0;
    api.layout(h.root, before); expect(h.animations).toHaveLength(0);
  });

  it('replaces interrupted travel and removes copies immediately when the surface closes', () => {
    const h = harness(), api = h.window.WerkbordMotion;
    let before = api.snapshot(h.root);
    h.item.parentElement = {}; h.item.left = 200;
    api.layout(h.root, before);
    before = api.snapshot(h.root);
    h.item.parentElement = {}; h.item.left = 400;
    api.layout(h.root, before);
    expect(h.ghosts.filter(g => g.isConnected)).toHaveLength(1);
    api.dispose(h.root);
    expect(h.ghosts.filter(g => g.isConnected)).toHaveLength(0);
  });

  it('cancels active movement and removes inert ghosts when reduced motion changes', async () => {
    const h = harness(), api = h.window.WerkbordMotion;
    const before = api.snapshot(h.root);
    h.item.parentElement = {}; h.item.left = 250;
    api.layout(h.root, before);
    expect(h.ghosts).toHaveLength(1);
    expect(h.animations).toHaveLength(2);
    h.preference(true);
    for (let i=0; i<5; i++) await Promise.resolve();
    expect(h.animations.every(a => a.cancelled)).toBe(true);
    expect(h.ghosts[0].isConnected).toBe(false);
    api.animate(h.item, [{ opacity: 0 }, { opacity: 1 }]);
    expect(h.animations).toHaveLength(2);
  });
});
