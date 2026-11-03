// Whether the Personal workspace has answered recently enough that an answer sent now would reach it. Pure: the window and the web app each
// say when they last heard from it.
import type { Overview } from './types';

/** How old a workspace's summary may be before its questions are no longer offered for answering. */
export const FRESH_FOR = 30_000;

export function reachable(overview: Overview | null, now: number, readAt?: number, id = 'personal'): boolean {
  const e = overview?.entries.find((x) => x.workspace.id === id);
  if (!e?.summary || e.error || e.workspace.state !== 'ready') return false;
  return now - (readAt ?? Date.parse(e.summary.at)) < FRESH_FOR;
}
