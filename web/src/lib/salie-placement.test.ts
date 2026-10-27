import { describe, expect, it } from 'vitest';
import { defaultSaliePlacements, everywhere, isSaliePlacements, unifiedPlacement } from './salie-placement';

describe('Salie placement is one choice for the whole app', () => {
  it('stands for the same choice on Home and in projects', () => {
    expect(everywhere('floating')).toEqual({ universal: 'floating', workspace: 'floating' });
    expect(defaultSaliePlacements()).toEqual({ universal: 'sidebar', workspace: 'sidebar' });
    expect(isSaliePlacements(everywhere('sidebar'))).toBe(true);
  });

  it('reads an equal pair as that choice', () => {
    for (const placement of ['sidebar', 'floating', 'both'] as const) expect(unifiedPlacement(everywhere(placement))).toBe(placement);
  });

  it('reads a pair saved before there was one choice as Both, so nothing that was reachable disappears', () => {
    // The old default: floating on Home, sidebar in projects.
    expect(unifiedPlacement({ universal: 'floating', workspace: 'sidebar' })).toBe('both');
    expect(unifiedPlacement({ universal: 'sidebar', workspace: 'floating' })).toBe('both');
  });
});
