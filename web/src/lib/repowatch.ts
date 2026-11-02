// Noticing a push to a team's repository. Each repository is looked at about once a minute: where every branch is (one cheap call).
// The first look is only the baseline; after it, a branch that has moved, or is new, is a change. Nothing here talks to GitHub itself.

export interface Head { name: string; sha: string }
export interface RepoChange {
  repo: string;
  branch: string;
  sha: string;
  previous?: string;
  /** The branch is new, not moved. */
  created?: boolean;
}

/** What changed between the heads last seen (undefined: none yet, so this look is the baseline) and the heads now. */
export function changesBetween(repo: string, before: Record<string, string> | undefined, now: readonly Head[]): RepoChange[] {
  if (!before) return [];
  const out: RepoChange[] = [];
  for (const h of now) {
    const was = before[h.name];
    if (was === undefined) out.push({ repo, branch: h.name, sha: h.sha, created: true });
    else if (was !== h.sha) out.push({ repo, branch: h.name, sha: h.sha, previous: was });
  }
  return out;
}

/** owner/name on github.com, or host/owner/name on another GitHub host. */
export const REPO_NAME = /^(?:[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)+\/)?[A-Za-z0-9][A-Za-z0-9._-]{0,99}\/[A-Za-z0-9._][A-Za-z0-9._-]{0,99}$/;
export const MAX_WATCHED = 20;

export interface Watcher {
  /** The repositories to watch. A repository that stays on the list keeps its baseline. */
  set(repos: readonly string[]): void;
  /** One look at every watched repository. */
  tick(): Promise<void>;
  stop(): void;
}

export function createRepoWatcher(opts: { heads(repo: string): Promise<Head[]>; report(change: RepoChange): void; /** Every look that found branches, so the team can be told where they are. */ seen?(repo: string, heads: Head[]): void; every?: number }): Watcher {
  const seen = new Map<string, Record<string, string>>();
  let repos: string[] = [];
  let busy = false;
  const timer = setInterval(() => void watcher.tick(), opts.every ?? 60000);
  const watcher: Watcher = {
    set(next) {
      repos = next.filter((r) => REPO_NAME.test(r)).slice(0, MAX_WATCHED);
      for (const r of [...seen.keys()]) if (!repos.includes(r)) seen.delete(r);
    },
    async tick() {
      if (busy) return;
      busy = true;
      try {
        for (const repo of repos) {
          let heads: Head[];
          try { heads = await opts.heads(repo); } catch { continue; }
          // GitHub answering with nothing (signed out, no access) is no news, and must not make every branch look new later.
          if (!heads.length) continue;
          for (const c of changesBetween(repo, seen.get(repo), heads)) opts.report(c);
          opts.seen?.(repo, heads);
          seen.set(repo, Object.fromEntries(heads.map((h) => [h.name, h.sha])));
        }
      } finally { busy = false; }
    },
    stop() { clearInterval(timer); },
  };
  return watcher;
}
