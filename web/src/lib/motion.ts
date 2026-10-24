import '../../../internal/uipresentation/static/motion.js';
import { cubicOut } from 'svelte/easing';
import type { TransitionConfig } from 'svelte/transition';

interface Snapshot { items: Map<string, { node: HTMLElement; rect: DOMRect; parent: HTMLElement | null; scroll: string }>; width: number; scroll: string }
interface Motion {
  reduced(): boolean;
  animate(node: Element, frames: Keyframe[], duration?: number): Animation | null;
  snapshot(root: HTMLElement, selector?: string): Snapshot;
  layout(root: HTMLElement, before: Snapshot | undefined, selector?: string, entries?: boolean): Snapshot;
  cancel(): void;
  dispose(root: HTMLElement): void;
}
declare global { interface Window { WerkbordMotion: Motion } }
export const reducedMotion = () => window.WerkbordMotion.reduced();
export const animate = (node: Element, frames: Keyframe[], duration = 180) => window.WerkbordMotion.animate(node, frames, duration);

/** Only the existing mark moves; its shape, placement and colours remain unchanged. */
export function milestone(root: ParentNode = document) {
  root.querySelectorAll('.mark').forEach(mark => {
    if (!mark.getClientRects().length) return;
    mark.querySelectorAll('rect').forEach((cell, index) => {
      const animation = animate(cell, [{ opacity: 0, transform: 'translateY(6px)' }, { opacity: 1, transform: 'none' }], 500);
      animation?.effect?.updateTiming({ delay: Math.floor(index/16)*35 });
    });
  });
}

/** The existing surface supplies its geometry; motion does not replace layout. */
export function surface(node: Element, options: { x?: number; y?: number; duration?: number } = {}, context?: { direction: 'in' | 'out' | 'both' }): TransitionConfig {
  const transform = getComputedStyle(node).transform;
  return {
    duration: reducedMotion() ? 0 : context?.direction === 'out' ? 180 : (options.duration ?? 220), easing: cubicOut,
    css: t => `opacity:${t};transform:${transform === 'none' ? '' : transform} translate(${(options.x ?? 0) * (1-t)}px,${(options.y ?? 6) * (1-t)}px)`,
  };
}

export interface LayoutOptions { revision: string; enabled?: boolean; selector?: string; entries?: boolean }
/** Initial loads, filter changes and reconnects only establish a new baseline. */
export function layout(node: HTMLElement, initial: LayoutOptions) {
  let previous: Snapshot | undefined;
  let options = initial;
  let disposed = false;
  let generation = 0;
  const capture = (next: LayoutOptions, mayAnimate: boolean) => {
    const current = ++generation;
    queueMicrotask(() => {
      if (disposed || current !== generation) return;
      previous = mayAnimate ? window.WerkbordMotion.layout(node, previous, next.selector, next.entries) : window.WerkbordMotion.snapshot(node, next.selector);
    });
  };
  capture(initial, false);
  return {
    update(next: LayoutOptions) {
      const changed = next.revision !== options.revision;
      capture(next, changed && !!options.enabled && !!next.enabled);
      options = next;
    },
    destroy() { disposed = true; window.WerkbordMotion.dispose(node); previous = undefined; },
  };
}

export function acknowledge(node: HTMLElement, initial: { value: unknown; success: boolean; fresh?: boolean }) {
  let value = initial.value;
  function show() {
    const background = getComputedStyle(node).backgroundColor;
    animate(node, [{ backgroundColor: 'color-mix(in srgb, var(--ok) 12%, var(--surface))' }, { backgroundColor: background }], 350);
    node.querySelectorAll('svg').forEach(icon => animate(icon, [{ opacity: .3 }, { opacity: 1 }], 180));
  }
  if (initial.fresh && initial.success) show();
  return { update(next: typeof initial) { if (next.value !== value && next.success) show(); value = next.value; } };
}

/** Animate a chart's picture while its exact numbers are already available. */
export function chartBar(node: SVGRectElement | HTMLElement, initial: { value: number; vertical?: boolean; entrance?: boolean; scope: string }) {
  let previous = initial;
  let visible = false;
  const show = () => {
    if (visible) return;
    visible = true;
    if (initial.entrance !== false) run(0, initial.value, 320);
  };
  function run(from: number, to: number, duration: number) {
    const axis = previous.vertical ? 'Y' : 'X';
    const ratio = to > 0 ? Math.max(0, from / to) : 0;
    animate(node, [{ transform: `scale${axis}(${ratio})` }, { transform: `scale${axis}(1)` }], duration);
  }
  const observer = new IntersectionObserver(entries => { if (entries.some(e => e.isIntersecting)) { show(); observer.disconnect(); } });
  observer.observe(node);
  return {
    update(next: typeof initial) {
      // A new set of figures settles in with a short fade: bars that stretch from one length to another read as noise.
      if (visible && next.value !== previous.value && next.scope !== previous.scope) animate(node, [{ opacity: 0.35 }, { opacity: 1 }], 200);
      previous = next;
    },
    destroy() { observer.disconnect(); },
  };
}

/** Pause existing local activity while it is outside the viewport. */
export function visibility(node: HTMLElement) {
  const observer = new IntersectionObserver(entries => {
    node.toggleAttribute('data-motion-offscreen', !entries.some(e => e.isIntersecting));
  });
  observer.observe(node);
  return { destroy() { observer.disconnect(); } };
}

/** A closing guide yields its existing space; it introduces no replacement layout. */
export function collapse(node: HTMLElement): TransitionConfig {
  const height = node.getBoundingClientRect().height;
  const style = getComputedStyle(node);
  return { duration: reducedMotion() ? 0 : 180, easing: cubicOut, css: t => `height:${height*t}px;min-height:0;opacity:${t};overflow:hidden;padding-top:${parseFloat(style.paddingTop)*t}px;padding-bottom:${parseFloat(style.paddingBottom)*t}px` };
}

/** Runs `show` once, the first time the node is on screen, so a chart plays its entrance when it is looked at, not when it is built. */
function onceVisible(node: Element, show: () => void) {
  if (typeof IntersectionObserver !== 'function') { show(); return { destroy() {} }; }
  const observer = new IntersectionObserver(entries => { if (entries.some(e => e.isIntersecting)) { show(); observer.disconnect(); } });
  observer.observe(node);
  return { destroy() { observer.disconnect(); } };
}

/** A line draws itself from its first point to its last. */
export function chartDraw(node: SVGGeometryElement, options: { delay?: number; duration?: number } = {}) {
  const watch = onceVisible(node, () => {
    const length = node.getTotalLength?.() ?? 0;
    if (!length) return;
    const animation = animate(node, [{ strokeDasharray: `${length} ${length}`, strokeDashoffset: length }, { strokeDasharray: `${length} ${length}`, strokeDashoffset: 0 }], options.duration ?? 900);
    animation?.effect?.updateTiming({ delay: options.delay ?? 0, fill: 'backwards' });
  });
  return { destroy: watch.destroy };
}

/** A shape grows out of one edge (an area from its baseline, a bar from its foot), one after another when given a delay. */
export function chartGrow(node: SVGElement | HTMLElement, options: { from?: 'bottom' | 'left'; delay?: number; duration?: number } = {}) {
  const horizontal = options.from === 'left';
  const watch = onceVisible(node, () => {
    const animation = animate(node, [{ transformOrigin: horizontal ? 'left center' : 'center bottom', transform: horizontal ? 'scaleX(0)' : 'scaleY(0)', opacity: 0.2 }, { transformOrigin: horizontal ? 'left center' : 'center bottom', transform: 'none', opacity: 1 }], options.duration ?? 560);
    animation?.effect?.updateTiming({ delay: options.delay ?? 0, fill: 'backwards' });
  });
  return { destroy: watch.destroy };
}

/** A ring segment sweeps round from where it starts to where it ends. */
export function chartSweep(node: SVGCircleElement, options: { length: number; circumference: number; delay?: number }) {
  const watch = onceVisible(node, () => {
    const animation = animate(node, [{ strokeDasharray: `0 ${options.circumference}` }, { strokeDasharray: `${options.length} ${options.circumference - options.length}` }], 760);
    animation?.effect?.updateTiming({ delay: options.delay ?? 0, fill: 'backwards' });
  });
  return { destroy: watch.destroy };
}
