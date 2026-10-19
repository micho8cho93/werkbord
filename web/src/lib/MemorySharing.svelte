<script lang="ts">
  import { onMount } from 'svelte';
  import { request, requestNoContent } from './api';
  import { app } from './state.svelte';
  type Grant = { workspaceId: string; projectId: string; localProjectId: string; repository: string; enabled: boolean; semantic: boolean };
  type Publication = { grant: Grant; memoryId: string; revision: number; state: string; summary: string; receipt?: {record: {id: string; state: string; content: {summary: string}}} };
  let grants = $state<Grant[]>([]);
  let access = $state<{id: string; memoryWorkspaceId?: string; scope?: string}[]>([]);
  let records = $state<Publication[]>([]);
  let workspace = $state('');
  let project = $state('');
  let local = $state('');
  let repository = $state('');
  let busy = $state(false);
  let loading = $state(true);
  let error = $state('');
  let notice = $state('');
  let review = $state<Publication | null>(null);
  let summary = $state('');
  let token = $state('');
  const base = '/api/memory/v1/publication';
  onMount(() => { void act(async () => {}); });
  async function reload() {
    grants = await request<Grant[]>('GET', base + '/grants');
    access = (await request<typeof access>('GET', '/api/local-access')).filter(e => e.scope === 'memory-v1');
    const pages = await Promise.all([...new Set(grants.map(g => g.workspaceId))].map(w => request<Publication[]>('GET', base + '?workspaceId=' + encodeURIComponent(w))));
    records = pages.flat(); loading = false;
  }
  async function act(work: () => Promise<unknown>) {
    busy = true; error = ''; notice = '';
    try { await work(); await reload(); }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = false; loading = false; }
  }
  async function change(g: Grant) { await request('PUT', base + '/grants', g); }
  function revoke(p: Publication) {
    return act(() => request('POST', base + '/review', {workspaceId: p.grant.workspaceId, projectId: p.grant.projectId, memoryId: p.memoryId, revoke: true}));
  }
  async function grantToken(w: string) {
    token = '';
    const result = await request<{token: string}>('POST', '/api/local-access', {name: 'Memory publication', scope: 'memory-v1', workspaceId: w});
    token = result.token;
    notice = 'Paste this separate grant into that workspace’s Team Memory settings. Use Disconnect below to revoke the connection. Disable publication first and wait for withdrawals before disconnecting.';
  }
</script>

<section class="pn sharing" aria-labelledby="sharing-title">
  <h2 id="sharing-title">Team Memory sharing</h2>
  <p>Choose each project and destination explicitly. Eligible Git metadata contains only validated commit and pull request identities. Semantic implementation summaries and architectural decisions need a separate choice and a review of the exact text.</p>
  <p class="muted">Personal preferences, private sessions, transcripts, source code, credentials, environment variables and local paths are never exported automatically. Other knowledge stays private in Individual.</p>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if notice}<p role="status">{notice}</p>{/if}
  {#if token}<label>Workspace-bound grant <input readonly value={token} onfocus={e => e.currentTarget.select()} /></label>{/if}
  {#if loading}<p class="muted">Loading sharing choices…</p>
  {:else}
    {#each grants as g (g.workspaceId + ':' + g.projectId)}
      <div class="sharing-row">
        <div><strong>{g.repository}</strong><p class="muted identity">{g.workspaceId} · {g.projectId}</p></div>
        <label class="check"><input type="checkbox" checked={g.enabled} disabled={busy} onchange={e => void act(() => change({...g, enabled: e.currentTarget.checked}))} /> Publish Git metadata</label>
        <label class="check"><input type="checkbox" checked={g.semantic} disabled={busy || !g.enabled} onchange={e => void act(() => change({...g, semantic: e.currentTarget.checked}))} /> Propose reviewed summaries and decisions</label>
        <button class="btn" disabled={busy} onclick={() => void act(() => grantToken(g.workspaceId))}>Connect this workspace</button>
      </div>
    {:else}<p class="muted">No project is shared yet. Add an authorised repository below.</p>{/each}
    {#each access as entry (entry.id)}
      <div class="sharing-row"><span class="identity">Publication connection · {entry.memoryWorkspaceId}</span><button class="btn" disabled={busy} onclick={() => void act(() => requestNoContent('DELETE', '/api/local-access/' + encodeURIComponent(entry.id)))}>Disconnect this grant</button></div>
    {/each}
    <details>
      <summary>Add a sharing destination</summary>
      <form onsubmit={e => { e.preventDefault(); void act(() => change({workspaceId: workspace, projectId: project, localProjectId: local, repository, enabled: true, semantic: false})); }}>
        <label>Local project <select bind:value={local} required><option value="">Choose a project</option>{#each app.projects as p (p.id)}<option value={p.id}>{p.name}</option>{/each}</select></label>
        <label>Workspace ID <input bind:value={workspace} required /></label>
        <label>Team project ID <input bind:value={project} required /></label>
        <label>Canonical repository <input bind:value={repository} placeholder="github.com/organisation/repository" required /></label>
        <button class="btn primary" disabled={busy}>Enable Git metadata publication</button>
      </form>
    </details>
    <h3>Proposed and shared memories</h3>
    <button class="btn" disabled={busy} onclick={() => void act(async () => {})}>Refresh delivery status</button>
    {#each records as p (p.grant.workspaceId + ':' + p.grant.projectId + ':' + p.memoryId)}
      <div class="sharing-row">
        <strong>{p.summary || 'Implementation summary or architectural decision'}</strong>
        <p class="muted identity">{p.grant.workspaceId} · {p.grant.projectId} · {p.state.replaceAll('_', ' ')}</p>
        {#if p.receipt}<p>{p.receipt.record.content.summary || 'The shared record was withdrawn.'}</p>{/if}
        <div class="actions">
          {#if p.state === 'awaiting_approval'}<button class="btn" disabled={busy} onclick={() => { review = p; summary = ''; }}>Review proposed text</button>{/if}
          {#if !['private', 'revoked', 'revocation_pending', 'deleted', 'superseded'].includes(p.state)}<button class="btn danger" disabled={busy} onclick={() => void revoke(p)}>Revoke publication</button>{/if}
        </div>
      </div>
    {:else}<p class="muted">No eligible memories in the selected repositories yet.</p>{/each}
    {#if review}
      {@const chosen = review}
      <form onsubmit={e => { e.preventDefault(); void act(async () => { await request('POST', base + '/review', {workspaceId: chosen.grant.workspaceId, projectId: chosen.grant.projectId, memoryId: chosen.memoryId, version: chosen.revision, summary}); review = null; }); }}>
        <label>Sanitised statement for this project <textarea bind:value={summary} maxlength="2048" required rows="4"></textarea></label>
        <p class="muted">Write only the implementation conclusion or architectural decision you intend to share. Team still requires another authorised person to approve publication.</p>
        <div class="actions"><button class="btn primary" disabled={busy}>Approve this exact text for sharing</button><button type="button" class="btn" onclick={() => { review = null; }}>Cancel</button></div>
      </form>
    {/if}
  {/if}
  <p class="muted">Disabling a project queues withdrawal of its shared records. Offline changes wait for reconnection. Revocation removes active Team access when acknowledged; it cannot erase copies already read, exported, backed up or incorporated into other work.</p>
</section>

<style>
  .sharing { display: grid; gap: 12px; }
  .sharing p { margin: 0; max-width: 75ch; }
  .muted { color: var(--text-2); }
  .identity { overflow-wrap: anywhere; }
  .sharing-row { display: grid; gap: 8px; padding-block: 14px; border-top: 1px solid var(--border); }
  form { display: grid; gap: 12px; padding-block: 12px; }
  label { display: grid; gap: 5px; }
  label.check { display: flex; align-items: center; gap: 8px; }
  .actions { display: flex; flex-wrap: wrap; gap: 8px; }
  button { justify-self: start; }
</style>
