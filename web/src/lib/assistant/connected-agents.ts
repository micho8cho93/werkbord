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

// Intentionally empty until a reviewed native interactive transport is implemented. A server
// capability alone must never expose a button this frontend cannot actually launch.
export const nativeCLILaunchers = new Map<string, (device: string) => void>();
