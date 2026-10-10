import { describe, expect, it, vi } from 'vitest';
import { circle, CLOSE_RATE, coverRadius, FALLBACK_ORIGIN, originOf, OPEN_MS, plan, Reveal } from './transition';

describe('where the surface grows from', () => {
  it('is the middle of the button', () => {
    expect(originOf({ left: 10, top: 20, width: 100, height: 40 })).toEqual({ x: 60, y: 40 });
  });

  it('reaches the farthest corner of the window, from inside it', () => {
    expect(coverRadius({ x: 0, y: 0 }, 300, 400)).toBeGreaterThanOrEqual(500);
    expect(coverRadius({ x: 150, y: 200 }, 300, 400)).toBeGreaterThanOrEqual(Math.hypot(150, 200));
    // From the corner it must still cover the whole window, not just a quarter.
    const r = coverRadius({ x: 20, y: 30 }, 1000, 800);
    for (const [cx, cy] of [[0, 0], [1000, 0], [0, 800], [1000, 800]]) expect(Math.hypot(cx - 20, cy - 30)).toBeLessThanOrEqual(r);
  });

  it('still covers the window when the button is outside it (the desktop sidebar is beside the frame)', () => {
    const o = { x: -140, y: 90 };
    const r = coverRadius(o, 900, 700);
    for (const [cx, cy] of [[0, 0], [900, 0], [0, 700], [900, 700]]) expect(Math.hypot(cx - o.x, cy - o.y)).toBeLessThanOrEqual(r);
  });

  it('is written as a circle at that point', () => {
    expect(circle({ x: 10.04, y: 20 }, 0)).toBe('circle(0px at 10px 20px)');
    expect(circle({ x: -30, y: 5 }, 400.26)).toBe('circle(400.3px at -30px 5px)');
    expect(circle({ x: 0, y: 0 }, -5)).toBe('circle(0px at 0px 0px)');
  });
});

describe('the plan', () => {
  it('grows a circle and moves the background from the sidebar’s colour to its own', () => {
    const p = plan({ x: 30, y: 40 }, 800, 600, false, 'rgb(255, 255, 255)', 'rgb(245, 245, 242)');
    expect(p.instant).toBe(false);
    expect(p.duration).toBe(OPEN_MS);
    expect(p.keyframes[0]).toMatchObject({ clipPath: 'circle(0px at 30px 40px)', backgroundColor: 'rgb(255, 255, 255)' });
    expect(p.keyframes[1].backgroundColor).toBe('rgb(245, 245, 242)');
    expect(String(p.keyframes[1].clipPath)).toMatch(/^circle\(\d+(\.\d)?px at 30px 40px\)$/);
  });

  it('has no movement at all under reduced motion', () => {
    const p = plan(FALLBACK_ORIGIN, 800, 600, true, 'white', 'black');
    expect(p.instant).toBe(true);
    expect(p.duration).toBe(0);
  });
});

/** The parts of the Web Animations API the surface uses, enough to see what it asks of it. */
function fakeEl() {
  const made: FakeAnimation[] = [];
  class FakeAnimation {
    playbackRate = 1;
    played = 0;
    cancelled = false;
    private done!: () => void;
    finished: Promise<unknown>;
    constructor(
      public keyframes: Keyframe[],
      public opts: KeyframeAnimationOptions,
    ) {
      this.finished = new Promise((r) => (this.done = () => r(this)));
    }
    play() {
      this.played++;
    }
    cancel() {
      this.cancelled = true;
    }
    finish() {
      this.done();
    }
  }
  const el = {
    animate: vi.fn((k: Keyframe[], o: KeyframeAnimationOptions) => {
      const a = new FakeAnimation(k, o);
      made.push(a);
      return a;
    }),
  } as unknown as HTMLElement;
  return { el, made };
}
const flush = () => new Promise((r) => setTimeout(r, 0));

describe('opening and closing', () => {
  const p = plan({ x: 10, y: 10 }, 800, 600, false, 'a', 'b');

  it('opens, then takes the clip off once it is open so nothing is left to cut the window', async () => {
    const { el, made } = fakeEl();
    const r = new Reveal(el, p);
    const done = r.open();
    expect(made).toHaveLength(1);
    expect(made[0].opts).toMatchObject({ duration: OPEN_MS, fill: 'both' });
    made[0].finish();
    await done;
    expect(made[0].cancelled).toBe(true);
  });

  it('closes by running the same movement backwards, a little faster', async () => {
    const { el, made } = fakeEl();
    const r = new Reveal(el, p);
    const open = r.open();
    made[0].finish();
    await open;
    const closing = r.close();
    expect(made).toHaveLength(2);
    expect(made[1].keyframes).toEqual([...p.keyframes].reverse());
    expect(made[1].opts.duration).toBeCloseTo(OPEN_MS / CLOSE_RATE);
    made[1].finish();
    await closing;
  });

  it('turns round in the middle of opening instead of jumping', async () => {
    const { el, made } = fakeEl();
    const r = new Reveal(el, p);
    void r.open();
    const closing = r.close();
    expect(made).toHaveLength(1); // the same animation, run backwards
    expect(made[0].playbackRate).toBe(-CLOSE_RATE);
    expect(made[0].played).toBe(1);
    made[0].finish();
    await closing;
    const again = r.open(); // and opens again from there
    expect(made[0].playbackRate).toBe(1);
    expect(made).toHaveLength(1);
    made[0].finish();
    await again;
    await flush();
  });

  it('does nothing at all when motion is reduced: it is simply there, and simply gone', async () => {
    const { el, made } = fakeEl();
    const r = new Reveal(el, plan({ x: 0, y: 0 }, 800, 600, true, 'a', 'b'));
    await r.open();
    await r.close();
    expect(made).toHaveLength(0);
    expect(el.animate).not.toHaveBeenCalled();
  });

  it('copes with a browser that cannot animate', async () => {
    const r = new Reveal({} as HTMLElement, p);
    await r.open();
    await r.close();
  });
});
