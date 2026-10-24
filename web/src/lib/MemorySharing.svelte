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
  const nameOf = (g: Grant) => app.projects.find(p => p.id === g.localProjectId)?.name ?? g.repository;
  const stateWord = (state: string) => ({ awaiting_approval: 'Waiting for your review', private: 'Private', shared: 'Shared with the team', revoked: 'Revoked', revocation_pending: 'Withdrawing', deleted: 'Deleted', superseded: 'Replaced' } as Record<string, string>)[state] ?? state.replaceAll('_', ' ');
  async function change(g: Grant) { await request('PUT', base + '/grants', g); }
  function revoke(p: Publication) {
    return act(() => request('POST', base + '/review', {workspaceId: p.grant.workspaceId, projectId: p.grant.projectId, memoryId: p.memoryId, revoke: true}));
  }
  async function grantToken(w: string) {
    token = '';
    const result = await request<{token: string}>('POST', '/api/local-access', {name: 'Memory publication', scope: 'memory-v1', workspaceId: w});
    token = result.token;
    notice = 'Paste this grant into that workspace’s Team memory settings. To undo it, turn sharing off, wait for the withdrawals, then Disconnect.';
  }
</script>

<section class="pn sharing" aria-labelledby="sharing-title">
  <h2 id="sharing-title" class="band">Individual memory</h2>
  <p class="intro">Choose which projects share short notes with a Team. Nothing leaves this computer until you turn it on for a project, and a note about decisions is only shared after you approve its exact words.</p>
  <details class="about"><summary>What is shared</summary>
    <p class="muted">Git metadata means validated commit and pull request identities. Summaries and architectural decisions are separate choices. Personal preferences, private sessions, transcripts, source code, credentials, environment variables and local paths are never exported. Turning a project off withdraws its shared notes once the Team acknowledges it; copies already read or backed up may remain.</p>
  </details>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if notice}<p role="status">{notice}</p>{/if}
  {#if token}<label>Workspace-bound grant <input readonly value={token} onfocus={e => e.currentTarget.select()} /></label>{/if}
  {#if loading}<p class="muted">Loading sharing choices…</p>
  {:else}
    <ul class="card-list">
      {#each grants as g (g.workspaceId + ':' + g.projectId)}
        {@const mine = records.filter(r => r.grant.workspaceId === g.workspaceId && r.grant.projectId === g.projectId)}
        <li class="card-row">
          <header><h3 class="title">{nameOf(g)}</h3><span class="tag">{g.enabled ? 'Sharing on' : 'Sharing off'}</span></header>
          <p class="muted repo">{g.repository}</p>
          <div class="choices">
            <label class="check"><input type="checkbox" checked={g.enabled} disabled={busy} onchange={e => void act(() => change({...g, enabled: e.currentTarget.checked}))} /> Share Git metadata</label>
            <label class="check"><input type="checkbox" checked={g.semantic} disabled={busy || !g.enabled} onchange={e => void act(() => change({...g, semantic: e.currentTarget.checked}))} /> Propose summaries and decisions for review</label>
          </div>
          {#if mine.length}
            <div class="notes">
              <p class="kicker">{mine.length} {mine.length === 1 ? 'note' : 'notes'}</p>
              {#each mine as p (p.memoryId)}
                <div class="note">
                  <p class="summary">{p.receipt?.record.content.summary || p.summary || 'Implementation summary or architectural decision'}</p>
                  <div class="note-foot"><span class="tag">{stateWord(p.state)}</span>
                    {#if p.state === 'awaiting_approval'}<button class="btn small" disabled={busy} onclick={() => { review = p; summary = ''; }}>Review text</button>{/if}
                    {#if !['private', 'revoked', 'revocation_pending', 'deleted', 'superseded'].includes(p.state)}<button class="btn small danger" disabled={busy} onclick={() => void revoke(p)}>Revoke</button>{/if}
                  </div>
                </div>
              {/each}
            </div>
          {:else}<p class="muted">No notes from this project yet.</p>{/if}
          <div class="actions"><button class="btn" disabled={busy} onclick={() => void act(() => grantToken(g.workspaceId))}>Connect this workspace</button></div>
          <details><summary>Identifiers</summary><p class="muted identity">{g.workspaceId} · {g.projectId}</p></details>
        </li>
      {:else}<li class="muted">No project is shared yet. Add one below.</li>{/each}
      {#each access as entry (entry.id)}
        <li class="card-row">
          <header><h3 class="title">Connection</h3><span class="tag">Memory publication</span></header>
          <p class="muted identity">{entry.memoryWorkspaceId}</p>
          <div class="actions"><button class="btn" disabled={busy} onclick={() => void act(() => requestNoContent('DELETE', '/api/local-access/' + encodeURIComponent(entry.id)))}>Disconnect</button></div>
        </li>
      {/each}
    </ul>
    {#if review}
      {@const chosen = review}
      <form class="card-row" onsubmit={e => { e.preventDefault(); void act(async () => { await request('POST', base + '/review', {workspaceId: chosen.grant.workspaceId, projectId: chosen.grant.projectId, memoryId: chosen.memoryId, version: chosen.revision, summary}); review = null; }); }}>
        <label>Statement to share for this project <textarea bind:value={summary} maxlength="2048" required rows="4"></textarea></label>
        <p class="muted">Write only the conclusion or decision you mean to share. Another person on the Team still has to approve it.</p>
        <div class="actions"><button class="btn primary" disabled={busy}>Approve this text</button><button type="button" class="btn" onclick={() => { review = null; }}>Cancel</button></div>
      </form>
    {/if}
    <details class="add">
      <summary>Add a project to share</summary>
      <form onsubmit={e => { e.preventDefault(); void act(() => change({workspaceId: workspace, projectId: project, localProjectId: local, repository, enabled: true, semantic: false})); }}>
        <label>Local project <select bind:value={local} required><option value="">Choose a project</option>{#each app.projects as p (p.id)}<option value={p.id}>{p.name}</option>{/each}</select></label>
        <label>Workspace ID <input bind:value={workspace} required /></label>
        <label>Team project ID <input bind:value={project} required /></label>
        <label>Repository <input bind:value={repository} placeholder="github.com/organisation/repository" required /></label>
        <button class="btn primary" disabled={busy}>Turn on sharing</button>
      </form>
    </details>
    <div class="actions"><button class="btn" disabled={busy} onclick={() => void act(async () => {})}>Refresh status</button></div>
  {/if}
</section>

<style>
  .sharing { display: grid; gap: 14px; }
  .sharing p { margin: 0; max-width: 75ch; }
  .intro { line-height: 1.5; }
  .muted { color: var(--text-2); }
  .identity, .repo { overflow-wrap: anywhere; font-size: 12.5px; }
  .choices { display: grid; gap: 8px; }
  .notes { display: grid; gap: 8px; }
  .kicker { font: 500 11.5px var(--mono); letter-spacing: 0.1em; text-transform: uppercase; color: var(--text-2); }
  .note { display: grid; gap: 8px; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); }
  .note-foot { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
  .summary { line-height: 1.5; overflow-wrap: anywhere; }
  .add { padding-top: 12px; border-top: 1px solid var(--border); }
  form { display: grid; gap: 12px; padding-block: 12px; }
  form.card-row { padding: 14px 16px; }
  label { display: grid; gap: 5px; }
  label.check { display: flex; align-items: center; gap: 8px; }
  button { justify-self: start; }
</style>
