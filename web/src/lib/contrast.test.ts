import { describe, expect, it } from 'vitest';

// The design tokens, read from shared tokens, against the surfaces text sits on: WCAG AA (4.5:1) for every token meant as text.
// scripts/measure-legibility.cjs measures the rendered screens; this keeps a token edit from undoing it.

// @ts-expect-error node's own types are not installed in this package; the test runs under node.
const { readFileSync } = await import('node:fs');
const css: string = readFileSync(new URL('../../../internal/uipresentation/static/tokens.css', import.meta.url), 'utf8');

function block(selector: string): Record<string, string> {
  const start = css.indexOf(selector + ' {');
  const body = css.slice(css.indexOf('{', start) + 1, css.indexOf('\n}', start));
  const out: Record<string, string> = {};
  for (const m of body.matchAll(/--([a-z0-9-]+):\s*(#[0-9a-fA-F]{6})\s*;/g)) out[m[1]] = m[2].toLowerCase();
  return out;
}


const baseLight = block(':root');
const baseDark = { ...baseLight, ...block(":root[data-theme='dark']") };
const names = ['paper', 'graphite', 'classic', 'cloud', 'moss', 'dune', 'plum'];
const palettes = names.flatMap(name => {
  const selector = name === 'classic' ? ':root:not([data-style])' : `:root[data-style='${name}']`;
  return [
    [name + ' light', { ...baseLight, ...block(selector) }],
    [name + ' dark', { ...baseDark, ...block(selector + "[data-theme='dark']") }],
  ] as const;
});

const channel = (v: number) => {
  const c = v / 255;
  return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
};
const luminance = (hex: string) => 0.2126 * channel(parseInt(hex.slice(1, 3), 16)) + 0.7152 * channel(parseInt(hex.slice(3, 5), 16)) + 0.0722 * channel(parseInt(hex.slice(5, 7), 16));
const ratio = (a: string, b: string) => {
  const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
};

const TEXT = ['text', 'text-2', 'accent', 'ok-text', 'warn-text', 'danger-text'];
const GROUND = ['bg', 'surface', 'surface-2'];

describe.each(palettes)('%s tokens', (_name, tokens) => {
  it('read from shared tokens', () => {
    expect(tokens.text).toMatch(/^#/);
    expect(tokens['ok-text']).toMatch(/^#/);
  });
  for (const fg of TEXT)
    for (const bg of GROUND)
      it(`${fg} reads on ${bg}`, () => {
        expect(ratio(tokens[fg], tokens[bg])).toBeGreaterThanOrEqual(4.5);
      });
});

describe('theme parity and primary controls', () => {
  for (const name of names) {
    const selector = name === 'classic' ? ':root:not([data-style])' : `:root[data-style='${name}']`;
    it(`${name} has identical device and explicit dark palettes`, () => {
      const dark = block(selector + "[data-theme='dark']");
      expect(Object.keys(dark).length).toBeGreaterThan(5);
      expect(block(selector + ":not([data-theme='light'])")).toEqual(dark);
      expect(Object.keys(block(selector)).sort()).toEqual(Object.keys(dark).sort());
    });
  }
  for (const [name, tokens] of palettes) it(`${name} primary label reads on its fill`, () => {
    expect(ratio(tokens['pri-text'], tokens['pri-fill'])).toBeGreaterThanOrEqual(4.5);
  });
  it('Classic is the default and its explicit palette matches', () => {
    expect(block(':root:not([data-style])')).toEqual(block(":root[data-style='classic']"));
  });
});

// The surface ladder (frame, tray and the tinted trays) is derived in CSS with color-mix; mix it the same way here.
const depths = [...css.matchAll(/--depth-(frame|tray):\s*([\d.]+)%/g)].map(m => [m[1], Number(m[2])] as const);
const depth = (kind: string, dark: boolean) => depths.filter(d => d[0] === kind).map(d => d[1])[dark ? 1 : 0];
const rgb = (hex: string) => [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16));
const mix = (a: string, b: string, share: number) => '#' + rgb(a).map((v, i) => Math.round(v * share + rgb(b)[i] * (1 - share)).toString(16).padStart(2, '0')).join('');

describe.each(palettes)('%s surface ladder', (name, tokens) => {
  const dark = name.endsWith('dark');
  const shade = dark ? '#000000' : tokens.text;
  const frame = mix(tokens.bg, shade, depth('frame', dark) / 100);
  const tray = mix(tokens.bg, shade, depth('tray', dark) / 100);
  const tint = (kind: string, from: string) => {
    const share = Number(new RegExp(`--tray-${kind}: color-mix\\(in srgb, var\\(--[a-z]+\\) ([\\d.]+)%, var\\(--bg\\)\\)`).exec(css)?.[1]);
    return mix(from, tokens.bg, share / 100);
  };
  const tinted = { work: tint('work', tokens.accent), ask: tint('ask', tokens.amber), block: tint('block', tokens.block), ok: tint('ok', tokens.ok) };
  const grounds: Record<string, string> = { frame, tray, ...Object.fromEntries(Object.entries(tinted).map(([k, v]) => ['tray-' + k, v])) };
  for (const [ground, hex] of Object.entries(grounds))
    for (const fg of ['text', 'text-2'])
      it(`${fg} reads on the ${ground} ground`, () => {
        expect(ratio(tokens[fg], hex)).toBeGreaterThanOrEqual(4.5);
      });
  for (const fg of ['ok-text', 'warn-text', 'danger-text'])
    it(`${fg} reads on a plain tray`, () => {
      expect(ratio(tokens[fg], tray)).toBeGreaterThanOrEqual(4.5);
    });
  // Without a step between page, tray and frame there is nothing to tell groups apart: the ladder has to be seen.
  it('the page, the tray and the frame are three distinct steps', () => {
    expect(ratio(tokens.bg, tray)).toBeGreaterThan(1.02);
    expect(ratio(tokens.bg, frame)).toBeGreaterThan(ratio(tokens.bg, tray));
  });
});

// Identity (identity.css): the ring and its arc are graphics, which need 3:1 against the page (WCAG 1.4.11); the initials on an avatar
// and the letter on a neutral tile are text, which need 4.5:1; a white logo tile has to stand apart from the ground in the dark styles.
describe.each(palettes)('%s identity', (_name, tokens) => {
  const RING = ['accent', 'warn', 'block', 'text-2'];
  for (const fg of RING)
    for (const bg of GROUND)
      it(`the ${fg} ring reads on ${bg}`, () => {
        expect(ratio(tokens[fg], tokens[bg])).toBeGreaterThanOrEqual(3);
      });
  it('the completed, failed and stopped badges carry a surface-coloured glyph', () => {
    for (const fill of ['ok-text', 'danger-text', 'text-2']) expect(ratio(tokens.surface, tokens[fill])).toBeGreaterThanOrEqual(3);
  });
  it('the waiting dot reads against the surface it sits on', () => {
    expect(ratio(tokens.warn, tokens.surface)).toBeGreaterThanOrEqual(3);
  });
  it('initials read on every avatar tone', () => {
    const accentBlock = mix(tokens.accent, tokens.block, 0.5);
    const tones = [tokens.ok, tokens.warn, tokens.block, tokens['text-2'], accentBlock, mix(tokens.ok, tokens.accent, 0.5)];
    for (const tone of tones) for (const ground of GROUND) expect(ratio(tokens.text, mix(tone, tokens[ground], 0.16))).toBeGreaterThanOrEqual(4.5);
  });
  it('the viewer\'s own avatar reads, accent on its tint', () => {
    expect(ratio(tokens.accent, tokens.tint)).toBeGreaterThanOrEqual(4.5);
  });
  it('the letter on a neutral tile reads on surface-2', () => {
    expect(ratio(tokens.text, tokens['surface-2'])).toBeGreaterThanOrEqual(4.5);
  });
  it('the white logo tile is distinct from a dark ground', () => {
    if (_name.endsWith('dark')) expect(ratio('#ffffff', tokens.surface)).toBeGreaterThanOrEqual(3);
  });
});
