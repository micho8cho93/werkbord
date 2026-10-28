<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from './api';
  import { confirmAction } from './confirm.svelte';
  import { formatDateTime } from './clock.svelte';
  let clients = $state<Awaited<ReturnType<typeof api.clients>>['clients']>([]);
  let code = $state<Awaited<ReturnType<typeof api.clientCode>> | null>(null);
  let error = $state('');
  let busy = $state(false);
  async function refresh() { try { clients = (await api.clients()).clients; } catch (e) { error = (e as Error).message; } }
  onMount(() => { void refresh(); });
  async function pair() {
    busy = true; error = ''; code = null;
    try { code = await api.clientCode(); } catch (e) { error = (e as Error).message; } finally { busy = false; }
  }
  async function revoke(id: string, name: string) {
    if (!(await confirmAction({
      title: `Revoke desktop control for ${name}?`,
      message: 'Its open connections will close. Its separate runner access is unchanged.',
      confirmLabel: 'Revoke desktop control',
      danger: true,
    }))) return;
    busy = true; error = '';
    try { await api.revokeClient(id); await refresh(); } catch (e) { error = (e as Error).message; } finally { busy = false; }
  }
</script>
<section class="pn" aria-labelledby="desktop-clients-title">
  <div class="ph"><h3 id="desktop-clients-title">Desktop control</h3><button class="btn" disabled={busy} onclick={pair}>Pair desktop</button></div>
  <div class="body">
    <p class="muted">Paired desktops can edit tasks and control managed runs. Runner permissions are paired and revoked separately.</p>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    {#if code}
      <div class="well"><p>Controller: {code.controller}</p><p>On the second computer, run <code>werkbord device connect {code.controller}</code>, then enter this code:</p><p class="code">{code.code}</p><p>Expires {formatDateTime(code.expiresAt)}. Pair the runner first and add <code>--runner</code> to also execute on that computer.</p></div>
    {/if}
    {#each clients as client (client.id)}
      <div class="device"><span>{client.name}</span><button class="btn danger" disabled={busy} onclick={() => revoke(client.id, client.name)}>Revoke control</button></div>
    {:else}<p class="muted">No desktops paired for control.</p>{/each}
  </div>
</section>
<style>
  .body { padding: 16px; display: grid; gap: 12px; }
  .device { display: flex; align-items: center; justify-content: space-between; gap: 12px; border-top: 1px solid var(--border); padding-top: 12px; }
  .well { overflow-wrap: anywhere; }
  .code { font-family: var(--mono); user-select: all; }
</style>
