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
// Paper and Graphite each replace the neutrals and the accent; status colours and amber come from the base tokens in each mode.
// The base tokens above are Classic's.
const PAPER = ":root:not([data-style='classic']):not([data-style='graphite'])";
const paperLight = { ...light, ...block(PAPER) };
const paperDark = { ...dark, ...block(PAPER + "[data-theme='dark']") };
const paperDarkByDevice = block(PAPER + ":not([data-theme='light'])");
const graphiteLight = { ...light, ...block(":root[data-style='graphite']") };
const graphiteDark = { ...dark, ...block(":root[data-style='graphite'][data-theme='dark']") };
// The same dark palette is written again for a device that is dark without the person having chosen; the two must not drift apart.
const graphiteDarkByDevice = block(":root[data-style='graphite']:not([data-theme='light'])");

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
  ['classic light', light],
  ['classic dark', dark],
  ['paper light', paperLight],
  ['paper dark', paperDark],
  ['graphite light', graphiteLight],
  ['graphite dark', graphiteDark],
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

describe('the styles', () => {
  it('Graphite writes its dark palette the same way for a device that is dark and for a chosen dark', () => {
    const chosen = block(":root[data-style='graphite'][data-theme='dark']");
    expect(Object.keys(chosen).length).toBeGreaterThan(5);
    expect(graphiteDarkByDevice).toEqual(chosen);
  });
  it('Graphite sets the same tokens in light and in dark, so neither mode inherits the other\'s', () => {
    expect(Object.keys(block(":root[data-style='graphite']")).sort()).toEqual(Object.keys(block(":root[data-style='graphite'][data-theme='dark']")).sort());
  });
  it('Paper writes its dark palette the same way for a device that is dark and for a chosen dark', () => {
    const chosen = block(PAPER + "[data-theme='dark']");
    expect(Object.keys(chosen).length).toBeGreaterThan(5);
    expect(paperDarkByDevice).toEqual(chosen);
  });
  it('Paper sets the same tokens in light and in dark', () => {
    expect(Object.keys(block(PAPER)).sort()).toEqual(Object.keys(block(PAPER + "[data-theme='dark']")).sort());
  });
  it('the three styles do not share a page colour in either mode, so switching style is seen at once', () => {
    for (const mode of [[light, paperLight, graphiteLight], [dark, paperDark, graphiteDark]]) {
      const grounds = mode.map((t) => t.bg);
      expect(new Set(grounds).size).toBe(3);
      expect(new Set(mode.map((t) => t.accent)).size).toBeGreaterThanOrEqual(2);
    }
  });
  for (const [name, tokens] of [['Classic', light], ['Classic dark', dark], ['Paper', paperLight], ['Paper dark', paperDark], ['Graphite', graphiteLight], ['Graphite dark', graphiteDark]] as const)
    it(`${name}: the label on the primary button reads on its fill`, () => {
      const fill = tokens['pri-fill'] ?? tokens.accent;
      const label = tokens['pri-text'] ?? tokens['accent-text'];
      expect(ratio(label, fill)).toBeGreaterThanOrEqual(4.5);
    });
});
