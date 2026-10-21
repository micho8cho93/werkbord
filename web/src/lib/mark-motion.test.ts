import { describe, expect, it } from 'vitest';
import { MARK_MOTIONS, markFrame } from './mark-motion';

describe('streaming mark identity and footprint', () => {
  it('keeps all 48 original pixels and fits the activity line throughout every approved loop', () => {
    expect(MARK_MOTIONS).toHaveLength(6);
    for (const mode of MARK_MOTIONS) for (let t = 0; t < 2; t += .025) {
      const cells = markFrame(mode, t);
      expect(new Set(cells.map(c => c.index)).size).toBe(48);
      for (const cell of cells) {
        expect(Object.values(cell).every(Number.isFinite)).toBe(true);
        expect(cell.x).toBeGreaterThan(-15); expect(cell.x).toBeLessThan(145);
        expect(cell.y).toBeGreaterThan(-10); expect(cell.y).toBeLessThan(105);
        expect(cell.opacity).toBeGreaterThan(0); expect(cell.opacity).toBeLessThanOrEqual(1);
      }
    }
  });
  it('returns to the original logo and closes each two-second loop without jumping', () => {
    for (const mode of MARK_MOTIONS) {
      expect(markFrame(mode, 0, 0)).toEqual(markFrame('helix', 1, 0));
      const start = markFrame(mode, 0).sort((a,b) => a.index-b.index);
      const end = markFrame(mode, 2).sort((a,b) => a.index-b.index);
      start.forEach((cell, i) => {
        expect(end[i].x).toBeCloseTo(cell.x); expect(end[i].y).toBeCloseTo(cell.y);
        expect(end[i].opacity).toBeCloseTo(cell.opacity);
      });
    }
  });
});
