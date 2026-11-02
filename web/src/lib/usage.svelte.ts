import { api } from './api';
import type { UsageSettings, UsageView } from './usage';

/** The usage meter, read from this computer's controller. Shared, so the Agents strip and Settings never disagree or read twice. */
class UsageStore {
  view = $state<UsageView | null>(null);
  error = $state('');
  private last = 0;
  private inflight: Promise<void> | null = null;

  /** Reads the meter, at most every 15 seconds unless forced: runs report many times a second and the meter does not need that. */
  refresh(force = false): Promise<void> {
    if (this.inflight) return this.inflight;
    if (!force && Date.now() - this.last < 15000) return Promise.resolve();
    this.last = Date.now();
    this.inflight = api.usageMeter().then(
      v => { this.view = v; this.error = ''; },
      e => { this.error = e instanceof Error ? e.message : String(e); },
    ).finally(() => { this.inflight = null; });
    return this.inflight;
  }

  async save(settings: UsageSettings): Promise<void> {
    this.view = await api.setUsageSettings(settings);
    this.last = Date.now();
  }
}

export const usageStore = new UsageStore();
