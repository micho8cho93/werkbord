// Saves what a person types a moment after they stop, one save at a time, and says honestly where it stands.

export type DraftPatch = Record<string, string>;
export type DraftStatus = 'saved' | 'unsaved' | 'saving' | 'failed';

export class DraftSaver {
  status = $state<DraftStatus>('saved');
  private pending: DraftPatch = {};
  private timer: ReturnType<typeof setTimeout> | undefined;
  private running: Promise<void> | null = null;

  /** commit returns whether the whole patch was stored; a refusal keeps the text so it can be tried again. */
  constructor(private commit: (patch: DraftPatch) => Promise<boolean>, private delay = 800) {}

  get dirty(): boolean { return Object.keys(this.pending).length > 0; }

  push(patch: DraftPatch): void {
    Object.assign(this.pending, patch);
    this.status = 'unsaved';
    clearTimeout(this.timer);
    this.timer = setTimeout(() => { void this.flush(); }, this.delay);
  }

  /** Saves whatever is waiting and resolves when nothing is: used before leaving a document. */
  async flush(): Promise<boolean> {
    clearTimeout(this.timer);
    while (this.running) await this.running;
    if (!this.dirty) return this.status !== 'failed';
    const patch = this.pending;
    this.pending = {};
    this.status = 'saving';
    this.running = (async () => {
      let stored: boolean;
      try { stored = await this.commit(patch); } catch { stored = false; }
      // Text typed during the save is newer than the failed patch.
      if (!stored) this.pending = { ...patch, ...this.pending };
      this.status = !stored ? 'failed' : this.dirty ? 'unsaved' : 'saved';
    })();
    try { await this.running; } finally { this.running = null; }
    const after = this.status as DraftStatus; // It changed while the save ran.
    if (after === 'unsaved') return this.flush();
    return after === 'saved';
  }

  dispose(): void { clearTimeout(this.timer); }
}
