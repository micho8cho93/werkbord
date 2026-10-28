<script lang="ts">
  import { app } from './state.svelte';
  import { timeAgo } from './format';
</script>

{#if app.remote}
  <div class="connection-note" role="status" data-testid="controller-location">
    <div><strong>Connected controller</strong><p>{app.device.controller} · {app.device.runnerId ? 'This computer also runs agents.' : 'Desktop control only.'} Each run shows its execution machine.</p></div>
  </div>
{/if}
{#if app.connection !== 'live' && app.connection !== 'unauthorized'}
  <div class="connection-note" role="status" data-testid="connection-status">
    <div>
      <strong>{app.connection === 'syncing' ? 'Synchronizing…' : app.connection === 'connecting' ? 'Connecting…' : 'Live updates disconnected'}</strong>
      <p>Displayed status may be outdated.{app.lastSyncedAt ? ` Last synchronized ${timeAgo(app.lastSyncedAt, app.now)}.` : ''} Agents may still be running on their machines. Reconnect to refresh status before acting; keep this tab open to retain drafts.{app.remote ? ' Board changes, new runs and Git need the controller, so they wait until it is reachable; this computer never switches to a board of its own.' : ''}</p>
    </div>
    {#if app.connection === 'offline'}<button class="btn small" type="button" onclick={() => app.reconnect()}>Reconnect</button>{/if}
  </div>
{/if}

<style>
  .connection-note { flex-wrap: wrap; overflow-wrap: anywhere; display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 16px; background: var(--surface-2); border-bottom: 1px solid var(--border); flex: none; }
  strong { font-size: 13px; color: var(--warn-text); }
  p { font-size: 12px; color: var(--text-2); }
  button { flex: none; min-height: 44px; }
</style>
