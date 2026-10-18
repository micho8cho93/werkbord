// The Runners screen's cards, from what each runner reports about itself. A runner is a machine of the person's own; the card says what
// it is and whether it is connected, using only what it reported. No framework code.

import { runnerConnection } from '../format';
import type { Runner } from '../types';

export type RunnerFilter = 'all' | 'online' | 'offline';

export interface RunnerCard {
  id: string;
  name: string;
  /** "darwin/arm64 · Werkbord 4.32", what it reports of itself. */
  spec: string;
  state: string;
  tone: 'work' | 'ok' | 'neutral';
  online: boolean;
  cpu: string;
  /** Free memory, when the runner reports it. */
  ram: string;
  storage: string;
  /** What the runner says is wrong with it, if anything. */
  note: string;
}

const gb = (n?: number): string => (n === undefined ? '' : `${(n / 1024 ** 3).toFixed(n >= 10 * 1024 ** 3 ? 0 : 1)} GB`);

export function runnerCard(r: Runner, live = true): RunnerCard {
  const state = runnerConnection(r, live);
  const up = live && r.online && !r.disabled;
  const caps = r.capabilities;
  return {
    id: r.id,
    name: r.kind === 'local' ? `${r.name} (this computer)` : r.name,
    spec: [r.os && r.arch ? `${r.os}/${r.arch}` : (r.os ?? ''), r.version ? `Werkbord ${r.version}` : ''].filter(Boolean).join(' · '),
    state,
    tone: !up ? 'neutral' : r.currentRuns > 0 ? 'work' : 'ok',
    online: up,
    cpu: up && caps?.cpu ? `${caps.cpu} ${caps.cpu === 1 ? 'core' : 'cores'}` : '',
    ram: up && caps?.availableRamBytes !== undefined ? `${gb(caps.availableRamBytes)} free` : '',
    storage: up && caps?.storageBytes !== undefined ? `${gb(caps.storageBytes)}` : '',
    note: caps?.diagnostics ?? '',
  };
}

export function matchesRunner(r: Pick<Runner, 'online' | 'disabled'>, f: RunnerFilter): boolean {
  const up = r.online && !r.disabled;
  return f === 'all' || (f === 'online' && up) || (f === 'offline' && !up);
}

