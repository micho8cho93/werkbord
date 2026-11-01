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
  <div class="connection-note status" data-state={app.connection === 'offline' ? 'offline' : 'working'} role="status" data-testid="connection-status">
    <div>
      <strong>{app.connection === 'syncing' ? 'Synchronizing…' : app.connection === 'connecting' ? 'Connecting…' : 'Live updates disconnected'}</strong>
      <p>Displayed status may be outdated.{app.lastSyncedAt ? ` Last synchronized ${timeAgo(app.lastSyncedAt, app.now)}.` : ''}{app.connection === 'offline' ? ' Reconnect before acting.' : ''}</p>
      <details>
        <summary>More</summary>
        <p>Agents may still be running on their machines. Reconnect to refresh status before acting; keep this tab open to retain drafts.{app.remote ? ' Work changes, new runs and Git need the controller, so they wait until it is reachable; this computer never switches to a board of its own.' : ''}</p>
      </details>
    </div>
    {#if app.connection === 'offline'}<button class="btn small primary" type="button" onclick={() => app.reconnect()}>Reconnect</button>{/if}
    {#if app.connection !== 'offline'}<span class="bar" aria-hidden="true"></span>{/if}
  </div>
{/if}

<style>
  .connection-note { flex-wrap: wrap; overflow-wrap: anywhere; display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 16px; background: var(--surface-2); border-bottom: 1px solid var(--border); flex: none; }
  strong { font-size: 13px; color: var(--warn-text); }
  p { font-size: 12px; color: var(--text-2); }
  button { flex: none; min-height: 44px; }
  details { margin-top: 2px; }
  summary { font-size: 12px; color: var(--text-2); cursor: pointer; }
  .status { position: relative; }
  /* Offline is a stop sign, not a note: a red edge, and the one way out is the one filled button. Working is a thin current along the foot. */
  .status[data-state='offline'] { border-left: 3px solid var(--danger); background: color-mix(in srgb, var(--danger) 8%, var(--surface-2)); }
  .status[data-state='offline'] strong { color: var(--danger-text); }
  .bar { position: absolute; left: 0; right: 0; bottom: 0; height: 2px; background: color-mix(in srgb, var(--accent) 18%, transparent); overflow: hidden; }
  .bar::after { content: ''; position: absolute; inset: 0; width: 35%; background: var(--accent); transform: translateX(-100%); }
  @media (prefers-reduced-motion: no-preference) { .bar::after { animation: sweep 1.3s var(--ease) infinite; } }
  @media (prefers-reduced-motion: reduce) { .bar::after { transform: none; width: 100%; opacity: .5; } }
  @keyframes sweep { to { transform: translateX(300%); } }
</style>
