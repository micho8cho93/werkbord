import { describe, expect, it } from 'vitest';
import allowedText from '../../../scripts/parity-allowed.json?raw';
import script from '../../../scripts/browser-parity.cjs?raw';

// scripts/parity-allowed.json lists the differences between the Mac window page and the web app that are allowed to stay, and why. A list
// nobody can read the reasons of is a list that only grows, so each entry must say which side, what, and why; the match must compile
// and the screen must be one the parity run draws (scripts/browser-parity.cjs).
const allowed = JSON.parse(allowedText) as Record<string, unknown>;
const screens = new Set([...script.matchAll(/\{ id: '([a-z-]+)'/g)].map((m) => m[1]));

describe('scripts/parity-allowed.json', () => {
  it('names only screens the parity run draws', () => {
    expect(screens.size).toBeGreaterThan(5);
    for (const id of Object.keys(allowed)) expect(id === '*' || screens.has(id), `${id} is not a screen in browser-parity.cjs`).toBe(true);
  });

  it('gives every entry a side, a match that compiles and a reason', () => {
    let entries = 0;
    for (const [id, list] of Object.entries(allowed)) {
      expect(Array.isArray(list), `${id} is a list`).toBe(true);
      for (const e of list as Record<string, unknown>[]) {
        entries++;
        expect(['window', 'web'], `${id}: side`).toContain(e.side);
        expect(typeof e.match === 'string' && e.match.length > 0, `${id}: match`).toBe(true);
        expect(() => new RegExp(e.match as string), `${id}: ${e.match} compiles`).not.toThrow();
        expect(typeof e.why === 'string' && (e.why as string).trim().length >= 40, `${id}: ${e.match} needs a reason of a sentence or more`).toBe(true);
        expect(Object.keys(e).sort(), `${id}: ${e.match} has only side, match and why`).toEqual(['match', 'side', 'why']);
        expect(e.match, `${id}: a match that allows everything allows nothing in particular`).not.toMatch(/^(\.\*|\^?\.\+?\$?)$/);
      }
    }
    expect(entries).toBeGreaterThan(0);
  });
});
