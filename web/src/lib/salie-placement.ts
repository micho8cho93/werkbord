export const SALIE_PLACEMENTS = ['sidebar', 'floating', 'both'] as const;
export type SaliePlacement = typeof SALIE_PLACEMENTS[number];
export interface SaliePlacements { universal: SaliePlacement; workspace: SaliePlacement }
export const defaultSaliePlacements = (): SaliePlacements => ({ universal: 'floating', workspace: 'sidebar' });
export function isSaliePlacement(value: unknown): value is SaliePlacement {
  return SALIE_PLACEMENTS.some(placement => placement === value);
}
export function isSaliePlacements(value: unknown): value is SaliePlacements {
  if (!value || typeof value !== 'object') return false;
  const placements = value as Record<string, unknown>;
  return isSaliePlacement(placements.universal) && isSaliePlacement(placements.workspace);
}
