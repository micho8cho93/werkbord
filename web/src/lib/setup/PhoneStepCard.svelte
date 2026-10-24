<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../api';
  import PhoneEnroll from '../phones/PhoneEnroll.svelte';
  import type { RelayStatus } from '../types';
  import SetupCard from './SetupCard.svelte';

  /** Pairing a phone: it reaches this computer through Werkbord's relay, so there is no app or VPN to install on it. */
  let { paired = false, skipped = false, onenrolled }: { paired?: boolean; skipped?: boolean; onenrolled?: () => void } = $props();

  let relay = $state<RelayStatus | null>(null);
  let unreachable = $state('');

  async function refresh() {
    try {
      relay = await api.relay();
      unreachable = '';
    } catch (err) {
      unreachable = err instanceof Error ? err.message : String(err);
    }
  }

  onMount(() => {
    void refresh();
    // Connecting to the relay takes a moment, and its state changes by itself.
    const t = setInterval(() => { if (document.visibilityState === 'visible' && !relay?.connected) void refresh(); }, 3000);
    return () => clearInterval(t);
  });

  const status = $derived(paired ? 'done' : skipped ? 'off' : relay?.enabled && !relay.connected ? 'todo' : 'todo');
  const summary = $derived(
    paired ? 'Phone connected' : skipped ? 'Skipped' : !relay ? '' : !relay.enabled ? 'Relay turned off' : relay.connected ? 'Ready to pair' : 'Connecting…',
  );
</script>

<SetupCard title="Connect your phone" {status} {summary}>
  {#if !relay && unreachable}
    <p class="muted" role="status">The relay could not be checked just now ({unreachable}).</p>
    <div class="row"><button class="btn quiet" onclick={refresh}>Try again</button></div>
  {:else if !relay}
    <p class="muted">Checking…</p>
  {:else if !relay.enabled}
    <p>
      Your phone reaches this computer through a small relay that only passes encrypted bytes along. There is no app to install on the
      phone: you scan a code and it opens in the browser.
    </p>
    <p>The relay is turned off on this computer (<code>relay.disabled</code> in <code>config.json</code>). Remove that setting and restart Werkbord to use it.</p>
    <p class="muted small">You can skip this and pair a phone later in Settings → Phones.</p>
  {:else}
    <p>Scan this code with your phone’s camera. It opens Werkbord in the phone’s browser, signed in to this computer only.</p>
    <PhoneEnroll {onenrolled} />
    {#if !relay.connected}
      <p class="muted small" role="status" aria-live="polite">Reaching the relay… {relay.error ? `${relay.error} ` : ''}A code made now still works as soon as this computer is connected.</p>
    {/if}
    <p class="muted small">Each phone has its own sign-in. You can see and revoke them in <a href="#/settings?phones">Settings → Phones</a>.</p>
  {/if}
</SetupCard>

<style>
  .row { display: flex; gap: 8px; flex-wrap: wrap; }
  .small { font-size: 0.82rem; }
</style>
