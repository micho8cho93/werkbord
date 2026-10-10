// The assistant opens out of the button that was pressed and closes back into it. The surface is clipped to a circle that
// grows from the button's centre until it covers the window, while its background moves from the sidebar's colour to its own.
// The workspace underneath is never touched, only covered. Under "reduce motion" there is no movement at all: the surface is
// simply there, or gone.

export interface Origin {
  x: number;
  y: number;
}

export interface Box {
  left: number;
  top: number;
  width: number;
  height: number;
}

/** Where the button is, as the point the circle grows from. */
export const originOf = (b: Box): Origin => ({ x: b.left + b.width / 2, y: b.top + b.height / 2 });

/** The origin to fall back on when no button is known (a keyboard shortcut, a link): the top left, where the sidebar is. */
export const FALLBACK_ORIGIN: Origin = { x: 24, y: 24 };

/** A radius that reaches the farthest corner of a w×h window from the origin, however far outside the window the origin is. */
export function coverRadius(o: Origin, w: number, h: number): number {
  const dx = Math.max(Math.abs(o.x), Math.abs(w - o.x));
  const dy = Math.max(Math.abs(o.y), Math.abs(h - o.y));
  return Math.ceil(Math.hypot(dx, dy)) + 2;
}

export const circle = (o: Origin, r: number): string => `circle(${Math.max(0, Math.round(r * 10) / 10)}px at ${Math.round(o.x * 10) / 10}px ${Math.round(o.y * 10) / 10}px)`;

/**
 * How long the surface takes to open. It is meant to be seen: slow enough to feel chosen rather than flicked, with a slow start so
 * the eye finds the button it comes out of, a quick middle, and a soft landing.
 */
export const OPEN_MS = 950;
/** Closing is a little quicker than opening: leaving should not make anyone wait. */
export const CLOSE_RATE = 1.3;
export const EASE = 'cubic-bezier(0.72, 0, 0.18, 1)';

/**
 * The light that travels with the circle's edge, as keyframes over the same duration, so that it runs (and runs back) with the circle.
 * Each is one layer the size of the window whose gradients are drawn from `--r`, the circle's radius, which is animated as a number
 * of pixels (it is a registered property: app.css, `@property --r`). Where that is not supported there is no light, only the circle.
 */
export interface Fx {
  /** A bright line on the edge itself, with a glow either side of it. */
  edge: Keyframe[];
  /** A soft glow around the button the circle comes from, which spreads and fades as the circle takes over. */
  bloom: Keyframe[];
}

export interface Plan {
  /** No movement: show or hide at once. */
  instant: boolean;
  keyframes: Keyframe[];
  duration: number;
  easing: string;
  origin: Origin;
  /** How far the circle goes: to the farthest corner. */
  radius: number;
  /** Null where the light cannot be drawn. */
  fx: Fx | null;
}

/** Whether the light that travels with the edge can be drawn here: it needs a registered, animatable length (Safari 16.4, Chrome 85). */
export function canDrawLight(): boolean {
  return typeof CSS !== 'undefined' && typeof (CSS as { registerProperty?: unknown }).registerProperty === 'function';
}

/**
 * What the opening does. `from` and `to` are the background colours, resolved, from the sidebar's to the surface's own. The light is
 * drawn from the same radius, animated from nothing to the full distance with the same easing as the clip, so it is the edge exactly.
 */
export function plan(o: Origin, w: number, h: number, reduced: boolean, from: string, to: string, light = canDrawLight()): Plan {
  const r = coverRadius(o, w, h);
  const px = (n: number) => `${n}px`;
  return {
    instant: reduced,
    duration: reduced ? 0 : OPEN_MS,
    easing: EASE,
    origin: o,
    radius: r,
    keyframes: [
      { clipPath: circle(o, 0), backgroundColor: from },
      { clipPath: circle(o, r), backgroundColor: to },
    ],
    fx: light
      ? {
          edge: [
            { offset: 0, '--r': px(0), opacity: 0 },
            { offset: 0.06, opacity: 1 },
            { offset: 0.72, opacity: 1 },
            { offset: 1, '--r': px(r), opacity: 0 },
          ],
          bloom: [
            { offset: 0, '--r': px(0), opacity: 0 },
            { offset: 0.2, opacity: 1 },
            { offset: 1, '--r': px(r), opacity: 0 },
          ],
        }
      : null,
  };
}

export function prefersReducedMotion(): boolean {
  try {
    return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
  } catch {
    return false;
  }
}

/** Reads a colour token as the colour it is right now (the theme may have changed it). */
export function colourOf(el: Element, token: string, fallback: string): string {
  const v = getComputedStyle(el).getPropertyValue(token).trim();
  return v || fallback;
}

/** The layers of light that travel with the edge: each the size of the window, and invisible until they move. */
export interface FxElements {
  edge?: HTMLElement | null;
  bloom?: HTMLElement | null;
}

/**
 * Drives the surface. `open` grows it; `close` runs the same animation backwards from wherever it is, so closing in the middle of
 * opening turns round instead of jumping. `done` is called when the surface has finished closing.
 */
export class Reveal {
  private anims: Animation[] = [];
  private closing = false;

  constructor(
    readonly el: HTMLElement,
    private p: Plan,
    private fx: FxElements = {},
  ) {}

  /** The elements and what each does; the light is told where the circle comes from. */
  private layers(): [HTMLElement, Keyframe[]][] {
    const out: [HTMLElement, Keyframe[]][] = [[this.el, this.p.keyframes]];
    const fx = this.p.fx;
    if (!fx) return out;
    for (const [name, frames] of [['edge', fx.edge], ['bloom', fx.bloom]] as const) {
      const node = this.fx[name];
      if (!node) continue;
      node.style.setProperty('--ox', `${this.p.origin.x}px`);
      node.style.setProperty('--oy', `${this.p.origin.y}px`);
      out.push([node, frames]);
    }
    return out;
  }

  private settled(): Promise<void> {
    return Promise.all(this.anims.map((a) => a.finished)).then(
      () => {},
      () => {},
    );
  }

  /** Starts opening (or turns round a closing that has not finished). Resolves when it has finished; at once if there is no movement. */
  open(): Promise<void> {
    this.closing = false;
    if (this.p.instant || typeof this.el.animate !== 'function') return Promise.resolve();
    if (this.anims.length) {
      for (const a of this.anims) {
        a.playbackRate = 1;
        a.play();
      }
    } else {
      this.anims = this.layers().map(([node, frames]) => node.animate(frames, { duration: this.p.duration, easing: this.p.easing, fill: 'both' }));
    }
    const mine = this.anims;
    return this.settled().then(() => {
      // Fully open: nothing to clip any more, and the surface falls back to its own style.
      if (this.anims === mine && !this.closing) this.clear();
    });
  }

  /** Starts closing. Resolves when the surface is gone from sight. */
  close(): Promise<void> {
    this.closing = true;
    if (this.p.instant || typeof this.el.animate !== 'function') return Promise.resolve();
    if (!this.anims.length) {
      this.anims = this.layers().map(([node, frames]) => {
        const a = node.animate(frames, { duration: this.p.duration, easing: this.p.easing, fill: 'both' });
        a.currentTime = this.p.duration;
        return a;
      });
    }
    for (const a of this.anims) {
      a.playbackRate = -CLOSE_RATE;
      a.play();
    }
    return this.settled();
  }

  private clear(): void {
    for (const a of this.anims) a.cancel();
    this.anims = [];
  }

  destroy(): void {
    this.clear();
  }
}
