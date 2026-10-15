import { describe, expect, it } from 'vitest';
import { tourDue, type TourStep, type TourSurface } from './tours';

const tips: readonly TourStep[] = [{ title: 'Hello', body: 'World' }];
const tours: Record<TourSurface, readonly TourStep[]> = { desktop: tips, 'desktop-team': tips, phone: tips, 'phone-team': [], settings: tips, analytics: tips };

describe('tourDue', () => {
  it('waits for the onboarding record', () => {
    expect(tourDue(null, 'desktop', tours)).toBe(false);
  });
  it('is due until this device has shown it', () => {
    expect(tourDue({}, 'desktop', tours)).toBe(true);
    expect(tourDue({ tours: { desktop: '2026-10-15T00:00:00Z' } }, 'desktop', tours)).toBe(false);
  });
  it('tracks each surface on its own', () => {
    const ob = { tours: { phone: '2026-10-15T00:00:00Z' } };
    expect(tourDue(ob, 'phone', tours)).toBe(false);
    expect(tourDue(ob, 'desktop-team', tours)).toBe(true);
  });
  it('never shows an empty tour', () => {
    expect(tourDue({}, 'phone-team', tours)).toBe(false);
  });
});
