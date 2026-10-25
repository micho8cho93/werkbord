import type { AgentPreference, CatalogAgent } from '../selection';

/** Every gate is independent. ACP coding and restricted chat do not imply a native terminal. */
export function connectedAgents(catalog: CatalogAgent[], preferences: Record<string, AgentPreference>): CatalogAgent[] {
  return catalog.filter(a => {
    const p = preferences[a.id];
    const cli = a.interactive;
    return p?.directCLI === true && a.installed && a.available && cli?.supported === true
      && cli.authorized === true && !!cli.device
      && (cli.authentication === 'verified' || (cli.authentication === 'unknown' && p.cliSignedIn === true));
  });
}

// Only these native entry points have a matching reviewed frontend launch flow.
export const nativeCLILaunchers = new Set(['claude-code', 'codex']);
