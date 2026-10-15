// The pages visited since the app opened, so that Back on the phone goes where the person came from: from My Work into a task and
// back to My Work, from Projects into a board and back to Projects. History itself is the browser's; this only knows whether there is
// somewhere to go back to, which the browser does not say.

import { untrack } from 'svelte';
import { hrefOf } from '../location';
import { router } from '../router.svelte';
import { advance, parentHref } from './nav';

class Trail {
  stack = $state<string[]>([hrefOf(router.location)]);
  private settling = false;

  constructor() {
    // Settling an address (App.svelte) replaces the current entry rather than adding one; the router says so before it changes.
    window.addEventListener('werkbord:place', () => (this.settling = true));
  }

  /** Records where the page is now. Called when the address changes. */
  note(): void {
    const href = hrefOf(router.location);
    const settling = this.settling;
    this.settling = false;
    // Read without depending on it: this runs in an effect that must not wake itself.
    const before = untrack(() => this.stack);
    const next = advance(before, href, settling);
    if (next.length !== before.length || next.some((h, i) => h !== before[i])) this.stack = next;
  }

  get canGoBack(): boolean {
    return this.stack.length > 1;
  }

  /** Back to the page before this one, or to the parent when this is where the person started. */
  back(): void {
    if (this.canGoBack) history.back();
    else location.hash = parentHref(router.location);
  }
}

export const trail = new Trail();
