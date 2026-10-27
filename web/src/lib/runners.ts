import type { Runner } from './types';

/** The controller is written in Go, where an empty list is often a nil slice and reaches us as `null`. The Runner type
 *  promises arrays, so every list is made one here, once, rather than at each place that reads it. */
export function normalizeRunner(r: Runner): Runner {
  const c = r.capabilities;
  return {
    ...r,
    projects: r.projects ?? [],
    capabilities: {
      ...c,
      agents: c?.agents ?? [],
      options: c?.options ?? [],
      repositories: c?.repositories ?? [],
    },
  };
}

export function normalizeRunners(runners: readonly Runner[] | null | undefined): Runner[] {
  return (runners ?? []).map(normalizeRunner);
}

/** Runner kind is relative to the controller, not to the screen viewing it. */
export function runnerLabel(r: Runner, remoteController = false, localRunnerId = ''): string {
  if (remoteController && r.id === localRunnerId) return `${r.name} (this computer)`;
  if (r.kind === 'local') return remoteController ? `${r.name} (controller)` : 'this computer';
  return r.name;
}
