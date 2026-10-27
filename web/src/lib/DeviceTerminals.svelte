<script lang="ts">
  import { onMount } from 'svelte';
  import { api, request } from './api';
  import { app } from './state.svelte';
  import { isPhone } from './phones/capability';
  import { terminalPath, type TerminalSession as Session } from './assistant/terminal';
  import TerminalSession from './assistant/TerminalSession.svelte';
  let sessions = $state<Session[]>([]);
  let clients = $state<Awaited<ReturnType<typeof api.clients>>['clients']>([]);
  let selected = $state<Session | null>(null);
  let projectId = $state('');
  let runnerId = $state('');
  let agentId = $state('codex');
  let clientId = $state('');
  let error = $state('');
  let notice = $state('');
  let busy = $state(false);
  let requestId = $state('');
  const runners = $derived(app.runners.filter(r => r.capabilities?.nativeTerminals && (r.kind === 'local' || r.projects.includes(projectId))));
  async function refresh() { try { sessions = await request<Session[]>('GET','/api/device/terminals'); } catch(e) { error = (e as Error).message; } }
  onMount(() => {
    void refresh();
    try {
      const raw = sessionStorage.getItem('werkbord:terminal-create');
      if (raw) { const saved = JSON.parse(raw); projectId = saved.projectId; runnerId = saved.runnerId; agentId = saved.agentId; requestId = saved.requestId; }
    } catch { /* The request identity remains visible until this view closes. */ }
    if (!app.remote) void api.clients().then(r => { clients = r.clients; clientId = r.clients[0]?.id ?? ''; }).catch(e => { error = (e as Error).message; });
    const timer = setInterval(() => { if (!busy) void refresh(); },10000);
    return () => clearInterval(timer);
  });
  async function create() {
    busy = true; error = ''; notice = '';
    // Reuse the request after an uncertain response instead of creating another process.
    if (!requestId) requestId = crypto.randomUUID();
    try { sessionStorage.setItem('werkbord:terminal-create',JSON.stringify({projectId,runnerId,agentId,requestId})); } catch { /* The in-memory request remains stable. */ }
    try {
      const session = await request<Session>('POST',`/api/projects/${encodeURIComponent(projectId)}/terminals`,{agentId,device:runnerId,cols:100,rows:30,requestId});
      selected = session; requestId = ''; try { sessionStorage.removeItem('werkbord:terminal-create'); } catch { /* Optional storage. */ } await refresh();
    } catch(e) { error = `${(e as Error).message} Retry keeps this creation request; inspect the machine before changing its target.`; }
    finally { busy = false; }
  }
  async function grant(enabled: boolean) {
    if (!selected || !clientId) return;
    busy = true; error = '';
    try { await request('PUT',`${terminalPath(selected)}/clients/${encodeURIComponent(clientId)}`,{runnerId:selected.device,enabled}); notice = `${enabled ? 'Granted' : 'Revoked'} access for ${clients.find(c=>c.id===clientId)?.name} to this session.`; }
    catch(e) { error = (e as Error).message; } finally { busy = false; }
  }
</script>
{#if !isPhone()}
<section class="pn terminals" aria-labelledby="device-terminals-title">
  <div class="heading"><h3 id="device-terminals-title">Native agent terminals</h3><button class="btn small" disabled={busy} onclick={refresh}>Refresh sessions</button></div>
  <p class="muted">Processes run on the named machine and continue through a connection loss. Desktop access is granted per session on the controller.</p>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if notice}<p role="status">{notice}</p>{/if}
  {#if !app.remote}
    <details><summary>Start a terminal on a machine</summary><div class="create">
      <label>Project<select class="input" bind:value={projectId} disabled={busy || !!requestId}><option value="">Select project</option>{#each app.projects.filter(p=>p.kind!=='work') as p (p.id)}<option value={p.id}>{p.name}</option>{/each}</select></label>
      <label>Machine<select class="input" bind:value={runnerId} disabled={busy || !!requestId}><option value="">Select machine</option>{#each runners as r (r.id)}<option value={r.id}>{r.name}{r.online ? '' : ' · offline'}</option>{/each}</select></label>
      <label>Agent<select class="input" bind:value={agentId} disabled={busy || !!requestId}><option value="codex">Codex</option><option value="claude-code">Claude Code</option></select></label>
      <button class="btn" disabled={busy || !projectId || !runnerId} onclick={create}>{requestId ? 'Check creation request' : 'Start terminal'}</button>
      <p class="muted">The machine must have this agent installed, signed in, and explicitly allowed for native terminal use.</p>
    </div></details>
  {/if}
  <div class="sessions">{#each sessions as session (session.id)}<button class="btn" class:active={selected?.id===session.id} onclick={() => { selected = session; notice = ''; }}>{session.repository} · {session.agentId} · {app.runners.find(r=>r.id===session.device)?.name ?? session.device} · {session.state}</button>{:else}<p class="muted">{app.remote ? 'No terminal sessions have been shared with this desktop.' : 'No native terminal sessions.'}</p>{/each}</div>
  {#if selected}
    {#if !app.remote && clients.length}<div class="sharing"><label>Desktop access for this session<select class="input" bind:value={clientId}>{#each clients as client (client.id)}<option value={client.id}>{client.name}</option>{/each}</select></label><button class="btn" disabled={busy || !clientId} onclick={() => grant(true)}>Grant access</button><button class="btn danger" disabled={busy || !clientId} onclick={() => grant(false)}>Revoke access</button></div>{/if}
    {#key selected.id}<TerminalSession session={selected} />{/key}
    <button class="btn small" onclick={() => selected=null}>Close viewer</button>
  {/if}
</section>
{/if}
<style>
  .terminals { margin-top: 20px; padding: 16px; display: grid; gap: 16px; } .heading,.create,.sharing,.sessions { display: flex; gap: 12px; flex-wrap: wrap; align-items: end; } .heading { justify-content: space-between; align-items: center; } h3,p { margin: 0; } h3 { font-size: 16px; } p { max-width: 70ch; } label { display: grid; gap: 6px; } .create { padding-top: 12px; } .create p { flex-basis: 100%; } summary { cursor: pointer; } .sessions { align-items: stretch; } .sessions .btn { white-space: normal; text-align: left; overflow-wrap: anywhere; } .active { border-color: var(--text); } .error { color: var(--red); }
</style>
