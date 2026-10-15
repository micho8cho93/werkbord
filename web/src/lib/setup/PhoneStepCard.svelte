<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../api';
  import PhoneEnroll from '../phones/PhoneEnroll.svelte';
  import type { RelayStatus } from '../types';
  import SetupCard from './SetupCard.svelte';

  /** Pairing a phone: it reaches this computer through the relay in your own Cloudflare account, so there is no app or VPN to install on it. */
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

  const status = $derived(paired ? 'done' : skipped ? 'off' : relay?.enabled && !relay.connected ? 'wait' : 'todo');
  const summary = $derived(
    paired ? 'Phone connected' : skipped ? 'Skipped' : !relay ? '' : !relay.enabled ? 'Relay not set up' : relay.connected ? 'Ready to pair' : 'Connecting to the relay…',
  );
</script>

<SetupCard title="Connect your phone" {status} {summary}>
  {#if !relay && unreachable}
    <p class="muted" role="status">The relay could not be checked just now ({unreachable}).</p>
    <div class="row"><button class="btn" onclick={refresh}>Try again</button></div>
  {:else if !relay}
    <p class="muted">Checking…</p>
  {:else if !relay.enabled}
    <p>
      Your phone reaches this computer through a small relay that runs in your own Cloudflare account. It only passes encrypted bytes
      along, and there is no app to install on the phone: you scan a code and it opens in the browser.
    </p>
    <p>The relay is not set up on this computer yet. Deploy it once, then add its two addresses to <code>config.json</code> as <code>relay.url</code> and <code>relay.connectorUrl</code> and restart Werkbord.</p>
    <p class="muted small">The steps are in <code>docs/RELAY.md</code> under “Deploying the relay”. You can skip this and pair a phone later in Settings → Phones.</p>
  {:else if !relay.connected}
    <p role="status" aria-live="polite">Connecting to your relay…</p>
    {#if relay.error}<p class="error" role="alert">{relay.error}{relay.retryInSeconds ? ` Trying again in ${relay.retryInSeconds} seconds.` : ''}</p>{/if}
    <div class="row"><button class="btn" onclick={refresh}>Check again</button></div>
  {:else}
    <p>Scan this code with your phone’s camera. It opens Werkbord in the phone’s browser, signed in to this computer only.</p>
    <PhoneEnroll {onenrolled} />
    <p class="muted small">Each phone has its own sign-in. You can see and revoke them in <a href="#/settings?phone">Settings → Phones</a>.</p>
  {/if}
</SetupCard>

<style>
  .row { display: flex; gap: 8px; flex-wrap: wrap; }
  .small { font-size: 0.82rem; }
</style>
