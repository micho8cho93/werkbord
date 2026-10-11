import { describe, expect, it } from 'vitest';

// The design tokens, read from app.css, against the surfaces text sits on: WCAG AA (4.5:1) for every token meant as text.
// scripts/measure-legibility.cjs measures the rendered screens; this keeps a token edit from undoing it.

// @ts-expect-error node's own types are not installed in this package; the test runs under node.
const { readFileSync } = await import('node:fs');
const css: string = readFileSync(new URL('../app.css', import.meta.url), 'utf8');

function block(selector: string): Record<string, string> {
  const start = css.indexOf(selector + ' {');
  const body = css.slice(css.indexOf('{', start) + 1, css.indexOf('\n}', start));
  const out: Record<string, string> = {};
  for (const m of body.matchAll(/--([a-z0-9-]+):\s*(#[0-9a-fA-F]{6})\s*;/g)) out[m[1]] = m[2].toLowerCase();
  return out;
}


const light = block(':root');
const dark = { ...light, ...block(":root[data-theme='dark']") };

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

describe.each([
  ['light', light],
  ['dark', dark],
])('%s tokens', (_name, tokens) => {
  it('read from app.css', () => {
    expect(tokens.text).toMatch(/^#/);
    expect(tokens['ok-text']).toMatch(/^#/);
  });
  for (const fg of TEXT)
    for (const bg of GROUND)
      it(`${fg} reads on ${bg}`, () => {
        expect(ratio(tokens[fg], tokens[bg])).toBeGreaterThanOrEqual(4.5);
      });
});

describe('what a status word must not use on paper', () => {
  it('the bare green is below 4.5:1 on a tray in light, which is why status words use --ok-text', () => {
    expect(ratio(light.ok, light['surface-2'])).toBeLessThan(4.5);
    expect(ratio(light['ok-text'], light['surface-2'])).toBeGreaterThanOrEqual(4.5);
  });
});
