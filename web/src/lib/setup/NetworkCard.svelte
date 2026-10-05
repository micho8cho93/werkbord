<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../api';
  import { copyText } from '../clipboard';
  import QrImage from '../QrImage.svelte';
  import type { NetworkStatus, PhoneLink } from '../types';
  import SetupCard from './SetupCard.svelte';

  let { skipped = false, onskip }: { skipped?: boolean; onskip?: () => void } = $props();

  let net = $state<NetworkStatus | null>(null);
  let phone = $state<PhoneLink | null>(null);
  let busy = $state(false);
  let error = $state('');
  let copied = $state(false);

  async function refresh() {
    try {
      net = await api.network();
      error = '';
      if (net.state === 'connected' && !phone) phone = await api.phoneLink().catch(() => null);
      if (net.state !== 'connected') phone = null;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  onMount(() => {
    void refresh();
    // While it is coming up the state changes by itself (the user signs in in another tab).
    const t = setInterval(() => {
      if (document.visibilityState === 'visible') void refresh();
    }, 2500);
    return () => clearInterval(t);
  });

  async function change(on: boolean) {
    busy = true;
    error = '';
    try {
      net = on ? await api.enableNetwork() : await api.disableNetwork();
      if (net.state !== 'connected') phone = null;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  async function restart() {
    await change(false);
    await change(true);
  }

  async function copy() {
    if (net?.url && (await copyText(net.url))) {
      copied = true;
      setTimeout(() => (copied = false), 1800);
    }
  }

  const status = $derived(
    net?.state === 'connected' ? 'done' : skipped ? 'off' : net && ['starting', 'needs_login', 'needs_approval'].includes(net.state) ? 'wait' : 'todo',
  );
  const summary = $derived(
    !net
      ? ''
      : ({
          off: skipped ? 'Skipped: this computer only' : 'Off',
          starting: 'Starting…',
          needs_login: 'Waiting for you to sign in',
          needs_approval: 'Waiting for approval',
          connected: net.hostname || 'Connected',
          error: 'Needs attention',
        } as const)[net.state],
  );
</script>

<SetupCard title="Phone access" {status} {summary}>
  {#if !net}
    <p class="muted">Checking…</p>
  {:else if net.state === 'connected'}
    <p>
      Open this on your phone or another computer on your Tailscale account:
    </p>
    <p class="url"><code class="mono">{net.url}</code></p>
    <div class="row">
      <button class="btn small" onclick={copy}>{copied ? 'Copied' : 'Copy address'}</button>
      <button class="btn small quiet" disabled={busy} onclick={() => change(false)}>Turn off</button>
    </div>
    {#if phone}
      <div class="qr">
        <QrImage svg={phone.qrSvg} size={176} label="QR code that opens Werkbord on your phone" />
        <p class="muted small">
          Scan this with your phone's camera to open Werkbord already signed in. It carries your access token: keep it to
          yourself. The phone needs the Tailscale app, signed in to the same account.
        </p>
      </div>
    {/if}
    {#if !net.https && net.httpsHint}
      <p class="hint">{net.httpsHint}</p>
    {/if}
    {#if net.health?.length}<p class="hint">Tailscale reports: {net.health.join('; ')}</p>{/if}
  {:else if net.state === 'needs_login'}
    <p>Sign in to reach Werkbord from your phone. A free Tailscale account is enough; Werkbord has no account of its own and runs no servers.</p>
    {#if net.authUrl}
      <a class="btn primary" href={net.authUrl} target="_blank" rel="noopener noreferrer">Sign in with Tailscale</a>
    {/if}
    <p class="muted small">This page notices when you are done.</p>
  {:else if net.state === 'needs_approval'}
    <p>Signed in. Your tailnet's admin has to approve this device before it can be reached.</p>
    <a class="btn" href="https://login.tailscale.com/admin/machines" target="_blank" rel="noopener noreferrer">Open the admin console</a>
  {:else if net.state === 'starting'}
    <p class="muted">Starting the private network…</p>
  {:else if net.state === 'error'}
    <p class="error" role="alert">{net.error || 'The private network could not start.'}</p>
    <div class="row"><button class="btn" disabled={busy} onclick={restart}>Try again</button></div>
  {:else}
    <p>
      Reach Werkbord from your phone, privately. It joins your own Tailscale network from inside Werkbord: nothing is exposed to the
      internet, and there is nothing to configure.
    </p>
    {#if net.choice === 'pinned_off'}
      <p class="hint">This is turned off in config.json (<code>network.enabled</code>).</p>
    {:else}
      <div class="row">
        <button class="btn primary" disabled={busy} onclick={() => change(true)}>Turn on phone access</button>
        {#if onskip && !skipped}<button class="btn quiet" onclick={onskip}>Skip: this computer only</button>{/if}
      </div>
    {/if}
  {/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}
</SetupCard>

<style>
  .row {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .url {
    overflow-wrap: anywhere;
  }

  .qr {
    display: grid;
    gap: 8px;
    justify-items: start;
  }

  .small {
    font-size: 0.8rem;
  }

  .hint {
    font-size: 0.82rem;
    color: var(--text-2);
  }
</style>
