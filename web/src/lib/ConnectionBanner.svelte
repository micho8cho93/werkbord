<script lang="ts">
  import { app } from './state.svelte';
  import { timeAgo } from './format';
</script>

{#if app.connection !== 'live' && app.connection !== 'unauthorized'}
  <div class="connection-note" role="status" data-testid="connection-status">
    <div>
      <strong>{app.connection === 'syncing' ? 'Synchronizing…' : app.connection === 'connecting' ? 'Connecting…' : 'Live updates disconnected'}</strong>
      <p>Displayed status may be outdated.{app.lastSyncedAt ? ` Last synchronized ${timeAgo(app.lastSyncedAt, app.now)}.` : ''} Agents may still be running on their machines.</p>
    </div>
    {#if app.connection === 'offline'}<button class="btn small" type="button" onclick={() => app.reconnect()}>Reconnect</button>{/if}
  </div>
{/if}

<style>
  .connection-note { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 16px; background: var(--surface-2); border-bottom: 1px solid var(--border); flex: none; }
  strong { font-size: 13px; color: var(--warn-text); }
  p { font-size: 12px; color: var(--text-2); }
  button { flex: none; min-height: 44px; }
</style>
