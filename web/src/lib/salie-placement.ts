export const SALIE_PLACEMENTS = ['sidebar', 'floating', 'both'] as const;
export type SaliePlacement = typeof SALIE_PLACEMENTS[number];
/** One choice for the whole app, kept as the two scopes the window already stores and the frames already exchange. */
export interface SaliePlacements { universal: SaliePlacement; workspace: SaliePlacement }
export const everywhere = (placement: SaliePlacement): SaliePlacements => ({ universal: placement, workspace: placement });
/** Workspaces were designed with Salie in the sidebar and no floating button over the work; that is now the one default everywhere. */
export const defaultSaliePlacements = (): SaliePlacements => everywhere('sidebar');
export function isSaliePlacement(value: unknown): value is SaliePlacement {
  return SALIE_PLACEMENTS.some(placement => placement === value);
}
export function isSaliePlacements(value: unknown): value is SaliePlacements {
  if (!value || typeof value !== 'object') return false;
  const placements = value as Record<string, unknown>;
  return isSaliePlacement(placements.universal) && isSaliePlacement(placements.workspace);
}
/** The one choice a saved pair stands for. A pair saved before there was one choice (different on Home and in projects) offered Salie in
 *  both ways somewhere, so it becomes "Both" and nothing that was reachable stops being so. */
export function unifiedPlacement(value: SaliePlacements): SaliePlacement {
  return value.universal === value.workspace ? value.universal : 'both';
}
