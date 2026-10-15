import { describe, expect, it } from 'vitest';
import { TOURS, tourDue, type TourStep, type TourSurface } from './tours';

const tips: readonly TourStep[] = [{ title: 'Hello', body: 'World' }];
const tours: Record<TourSurface, readonly TourStep[]> = { desktop: tips, phone: tips, settings: tips, analytics: [] };
const never = () => false;

describe('tourDue', () => {
  it('waits for the onboarding record for the tours kept on the computer', () => {
    expect(tourDue(null, 'desktop', tours, never)).toBe(false);
  });
  it('is due until this device has shown it', () => {
    expect(tourDue({}, 'desktop', tours, never)).toBe(true);
    expect(tourDue({ tours: { desktop: '2026-10-15T00:00:00Z' } }, 'desktop', tours, never)).toBe(false);
  });
  it('tracks each surface on its own', () => {
    const ob = { tours: { settings: '2026-10-15T00:00:00Z' } };
    expect(tourDue(ob, 'settings', tours, never)).toBe(false);
    expect(tourDue(ob, 'desktop', tours, never)).toBe(true);
  });
  it('keeps the phone tour on the phone, whatever the computer says', () => {
    expect(tourDue(null, 'phone', tours, never)).toBe(true);
    expect(tourDue({ tours: { phone: '2026-10-15T00:00:00Z' } }, 'phone', tours, never)).toBe(true);
    expect(tourDue({}, 'phone', tours, () => true)).toBe(false);
  });
  it('never shows an empty tour', () => {
    expect(tourDue({}, 'analytics', tours, never)).toBe(false);
  });
});

describe('the tours themselves', () => {
  it('are short and say something in every tip', () => {
    for (const [surface, steps] of Object.entries(TOURS)) {
      expect(steps.length, surface).toBeGreaterThan(0);
      expect(steps.length, surface).toBeLessThanOrEqual(5);
      for (const s of steps) { expect(s.title.trim(), surface).not.toBe(''); expect(s.body.trim(), surface).not.toBe(''); }
    }
  });
});
