import { describe, expect, it, vi } from 'vitest';
import { circle, CLOSE_RATE, coverRadius, FALLBACK_ORIGIN, originOf, OPEN_MS, plan, Reveal, type FxElements } from './transition';

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

  it('takes its time: long enough to be seen as chosen, short enough not to be waited for', () => {
    expect(OPEN_MS).toBeGreaterThanOrEqual(800);
    expect(OPEN_MS).toBeLessThanOrEqual(1200);
    expect(OPEN_MS / CLOSE_RATE).toBeGreaterThanOrEqual(550);
    expect(OPEN_MS / CLOSE_RATE).toBeLessThan(OPEN_MS);
  });

  it('has a light that is the edge itself: its radius goes from nothing to the full distance, as the circle’s does, and it is only shown and hidden in between', () => {
    const p = plan({ x: 30, y: 40 }, 800, 600, false, 'a', 'b', true);
    const edge = p.fx!.edge;
    expect(edge[0]).toMatchObject({ offset: 0, '--r': '0px' });
    expect(edge[edge.length - 1]).toMatchObject({ offset: 1, '--r': `${p.radius}px` });
    for (const k of edge.slice(1, -1)) expect(k['--r']).toBeUndefined();
    expect(p.radius).toBe(coverRadius({ x: 30, y: 40 }, 800, 600));
  });

  it('keeps every offset in order, which the browser demands of keyframes', () => {
    const p = plan({ x: 0, y: 0 }, 800, 600, false, 'a', 'b', true);
    for (const frames of [p.fx!.edge, p.fx!.bloom]) {
      const offsets = frames.map((f) => f.offset as number);
      expect(offsets).toEqual([...offsets].sort((x, y) => x - y));
      expect(offsets[0]).toBe(0);
      expect(offsets[offsets.length - 1]).toBe(1);
    }
  });

  it('has no light where the browser cannot animate a registered length, only the circle', () => {
    expect(plan({ x: 0, y: 0 }, 800, 600, false, 'a', 'b', false).fx).toBeNull();
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
      public node: unknown,
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
  const node = () => {
    const vars: Record<string, string> = {};
    const n: { style: { vars: Record<string, string>; setProperty(k: string, v: string): void }; animate: ReturnType<typeof vi.fn> } = {
      style: { vars, setProperty: (k, v) => (vars[k] = v) },
      animate: vi.fn((k: Keyframe[], o: KeyframeAnimationOptions) => {
        const a = new FakeAnimation(n, k, o);
        made.push(a);
        return a;
      }),
    };
    return n;
  };
  const el = node();
  const fx = { edge: node(), bloom: node() };
  const finishAll = () => made.forEach((a) => a.finish());
  return { el: el as unknown as HTMLElement, fx: fx as unknown as FxElements, rawFx: fx, made, finishAll, animate: el.animate };
}
const flush = () => new Promise((r) => setTimeout(r, 0));

describe('opening and closing', () => {
  const p = plan({ x: 10, y: 10 }, 800, 600, false, 'a', 'b', true);

  it('opens the surface and the rings together, and takes the clip off once it is open so nothing is left to cut the window', async () => {
    const { el, fx, made, finishAll } = fakeEl();
    const r = new Reveal(el, p, fx);
    const done = r.open();
    expect(made).toHaveLength(3); // the surface, the edge’s light and the glow
    for (const a of made) expect(a.opts).toMatchObject({ duration: OPEN_MS, easing: p.easing, fill: 'both' });
    finishAll();
    await done;
    expect(made.every((a) => a.cancelled)).toBe(true);
  });

  it('tells the light where the circle comes from', () => {
    const { el, fx, rawFx } = fakeEl();
    new Reveal(el, p, fx).open();
    for (const n of [rawFx.edge, rawFx.bloom]) expect(n.style.vars).toEqual({ '--ox': '10px', '--oy': '10px' });
  });

  it('closes by running the same movement backwards, a little faster', async () => {
    const { el, fx, made, finishAll } = fakeEl();
    const r = new Reveal(el, p, fx);
    const open = r.open();
    finishAll();
    await open;
    const closing = r.close();
    expect(made).toHaveLength(6);
    const back = made.slice(3);
    expect(back[0].keyframes).toEqual(p.keyframes);
    expect(back[1].keyframes).toEqual(p.fx!.edge);
    for (const a of back) {
      expect(a.opts.duration).toBe(OPEN_MS);
      expect(a.playbackRate).toBe(-CLOSE_RATE);
      expect((a as unknown as Animation).currentTime).toBe(OPEN_MS);
    }
    back.forEach((a) => a.finish());
    await closing;
  });

  it('turns round in the middle of opening instead of jumping', async () => {
    const { el, fx, made, finishAll } = fakeEl();
    const r = new Reveal(el, p, fx);
    void r.open();
    const closing = r.close();
    expect(made).toHaveLength(3); // the same animations, run backwards
    for (const a of made) {
      expect(a.playbackRate).toBe(-CLOSE_RATE);
      expect(a.played).toBe(1);
    }
    finishAll();
    await closing;
    const again = r.open(); // and opens again from there
    for (const a of made) expect(a.playbackRate).toBe(1);
    expect(made).toHaveLength(3);
    finishAll();
    await again;
    await flush();
  });

  it('can reopen during a close that started from the fully open surface', async () => {
    const { el, fx, made, finishAll } = fakeEl();
    const r = new Reveal(el, p, fx);
    const first = r.open();
    finishAll();
    await first;
    void r.close();
    const back = made.slice(3);
    const again = r.open();
    expect(made).toHaveLength(6);
    for (const a of back) {
      expect(a.playbackRate).toBe(1);
    }
    expect(back[0].keyframes).toEqual(p.keyframes);
    finishAll();
    await again;
  });

  it('works with only the surface, if the light is not there', async () => {
    const { el, made, finishAll } = fakeEl();
    const r = new Reveal(el, p);
    const done = r.open();
    expect(made).toHaveLength(1);
    finishAll();
    await done;
  });

  it('does nothing at all when motion is reduced: it is simply there, and simply gone', async () => {
    const { el, fx, made, animate } = fakeEl();
    const r = new Reveal(el, plan({ x: 0, y: 0 }, 800, 600, true, 'a', 'b'), fx);
    await r.open();
    await r.close();
    expect(made).toHaveLength(0);
    expect(animate).not.toHaveBeenCalled();
  });

  it('copes with a browser that cannot animate', async () => {
    const r = new Reveal({} as HTMLElement, p);
    await r.open();
    await r.close();
  });
});
