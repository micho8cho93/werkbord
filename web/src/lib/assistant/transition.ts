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

export const OPEN_MS = 440;
/** Closing is a little quicker than opening: leaving should not make anyone wait. */
export const CLOSE_RATE = 1.35;
export const EASE = 'cubic-bezier(0.16, 1, 0.3, 1)';

export interface Plan {
  /** No movement: show or hide at once. */
  instant: boolean;
  keyframes: Keyframe[];
  duration: number;
  easing: string;
}

/** What the opening does. `from` and `to` are the background colours, resolved, from the sidebar's to the surface's own. */
export function plan(o: Origin, w: number, h: number, reduced: boolean, from: string, to: string): Plan {
  const r = coverRadius(o, w, h);
  return {
    instant: reduced,
    duration: reduced ? 0 : OPEN_MS,
    easing: EASE,
    keyframes: [
      { clipPath: circle(o, 0), backgroundColor: from },
      { clipPath: circle(o, r), backgroundColor: to },
    ],
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

/**
 * Drives the surface. `open` grows it; `close` runs the same animation backwards from wherever it is, so closing in the middle of
 * opening turns round instead of jumping. `done` is called when the surface has finished closing.
 */
export class Reveal {
  private anim: Animation | null = null;
  private closing = false;

  constructor(
    readonly el: HTMLElement,
    private p: Plan,
  ) {}

  /** Starts opening (or turns round a closing that has not finished). Resolves when it has finished; at once if there is no movement. */
  open(): Promise<void> {
    this.closing = false;
    if (this.p.instant || typeof this.el.animate !== 'function') return Promise.resolve();
    if (this.anim) {
      this.anim.playbackRate = 1;
      this.anim.play();
    } else {
      this.anim = this.el.animate(this.p.keyframes, { duration: this.p.duration, easing: this.p.easing, fill: 'both' });
    }
    const a = this.anim;
    return a.finished.then(
      () => {
        // Fully open: nothing to clip any more, and the surface falls back to its own style.
        if (this.anim === a && !this.closing) {
          a.cancel();
          this.anim = null;
        }
      },
      () => {},
    );
  }

  /** Starts closing. Resolves when the surface is gone from sight. */
  close(): Promise<void> {
    this.closing = true;
    if (this.p.instant || typeof this.el.animate !== 'function') return Promise.resolve();
    let a = this.anim;
    if (!a) {
      const back = [...this.p.keyframes].reverse();
      a = this.el.animate(back, { duration: this.p.duration / CLOSE_RATE, easing: this.p.easing, fill: 'both' });
      this.anim = a;
    } else {
      a.playbackRate = -CLOSE_RATE;
      a.play();
    }
    return a.finished.then(
      () => {},
      () => {},
    );
  }

  destroy(): void {
    this.anim?.cancel();
    this.anim = null;
  }
}
