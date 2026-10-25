// The person's plan: Free, or Premium once they have given the app an unlock key (docs/PLANS.md). The key is checked by the app, and
// by every Team workspace it is given to; this only shows what the app says and sends what the person pastes. The key is never kept
// here, and never comes back from the app.

import { ask } from './native';
import type { PremiumStatus } from './types';

const FREE: PremiumStatus = { active: false, canCheck: true, buyUrl: '' };

class Plan {
  status = $state<PremiumStatus>(FREE);
  /** The app has answered at least once, so the sidebar does not say Free for a moment to a person who is on Premium. */
  loaded = $state(false);
  busy = $state(false);
  /** Why the last unlock did not work, in the app's words. */
  error = $state('');

  get premium(): boolean {
    return this.status.active;
  }

  async refresh(): Promise<void> {
    try {
      this.status = await ask((a) => a.Premium());
    } catch {
      // A window with no app behind it (a browser test) has no plan to show: it is Free.
      this.status = FREE;
    }
    this.loaded = true;
  }

  /** Gives the app the pasted key. Resolves to whether it was accepted; the reason it was not is in `error`. */
  async unlock(key: string): Promise<boolean> {
    this.error = '';
    this.busy = true;
    try {
      this.status = await ask((a) => a.UnlockPremium(key));
      this.loaded = true;
      return true;
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e);
      return false;
    } finally {
      this.busy = false;
    }
  }
}

export const plan = new Plan();
