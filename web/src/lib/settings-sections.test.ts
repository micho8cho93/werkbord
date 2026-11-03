import { describe, expect, it } from 'vitest';
import { GENERAL, PERSONAL, settingsQuery, visibleGroups } from './settings-sections';

const labels = (on: { app: boolean; window: boolean }) => visibleGroups([GENERAL, ...PERSONAL], on).flatMap((g) => g.sections.map((s) => s.label));

describe('the sections of Settings, for each place they are drawn', () => {
  it('shows the Mac-only sections only where the app is present', () => {
    expect(labels({ app: false, window: false })).not.toContain('Updates & about');
    expect(labels({ app: false, window: false })).not.toContain('Plan');
    expect(labels({ app: true, window: false })).toContain('Updates & about');
    expect(labels({ app: true, window: false })).not.toContain('Plan');
    expect(labels({ app: true, window: true })).toEqual(expect.arrayContaining(['Updates & about', 'Plan']));
  });

  it('puts Appearance, Alerts and Keyboard shortcuts everywhere, so a browser has them too', () => {
    expect(labels({ app: false, window: false }).slice(0, 3)).toEqual(['Appearance', 'Alerts', 'Keyboard shortcuts']);
  });

  it('has no section twice, and the first of Work is the page itself', () => {
    const keys = [GENERAL, ...PERSONAL].flatMap((g) => g.sections.map((s) => s.key));
    expect(new Set(keys).size).toBe(keys.length);
    expect(settingsQuery('defaults')).toBe('');
    expect(settingsQuery('runners')).toBe('?runners');
  });
});
