// A run's activity as the task page shows it: loaded in pages over HTTP and
// kept current by the event stream.

import { api } from './api';
import { FeedBuilder, type FeedItem } from './feed';
import type { ControllerEvent } from './types';

export class RunFeed {
  items = $state.raw<FeedItem[]>([]);
  hasMore = $state(false);
  loading = $state(true);
  loadingOlder = $state(false);
  error = $state('');

  private builder = new FeedBuilder();

  constructor(readonly runId: string) {}

  /** Loads the newest page. Safe to call again after a reconnect: events already shown are skipped. */
  async load(): Promise<void> {
    try {
      const page = await api.runEvents(this.runId);
      if (this.builder.apply(page.events)) this.items = this.builder.items;
      if (this.builder.firstSeq === 0 || this.loading) this.hasMore = page.hasMore;
      this.error = '';
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err);
    } finally {
      this.loading = false;
    }
  }

  async loadOlder(): Promise<void> {
    if (this.loadingOlder || !this.hasMore) return;
    this.loadingOlder = true;
    try {
      const page = await api.runEvents(this.runId, this.builder.firstSeq);
      if (this.builder.apply(page.events)) this.items = this.builder.items;
      this.hasMore = page.hasMore;
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err);
    } finally {
      this.loadingOlder = false;
    }
  }

  /** Applies an event from the stream. */
  push(ev: ControllerEvent): void {
    if (this.builder.apply([ev])) this.items = this.builder.items;
  }
}
