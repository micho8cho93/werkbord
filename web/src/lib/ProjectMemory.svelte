<script lang="ts">
  import { untrack } from 'svelte';
  import { api, ApiError } from './api';
  import { memoryLabel, verificationLabel } from './memory';
  import type { Memory, MemoryEvidence } from './memory';

  let { projectId }: { projectId: string } = $props();
  let records = $state<Memory[]>([]);
  let query = $state('');
  let status = $state('');
  let loading = $state(true);
  let busy = $state(false);
  let error = $state('');
  let conflict = $state(false);
  let message = $state('');
  let bounded = $state(false);
  let editing = $state<Memory | null>(null);
  let showEditor = $state(false);
  let summary = $state('');
  let kind = $state('decision');
  let sourceId = $state('');
  let sources = $state<MemoryEvidence[]>([]);
  let deleteId = $state('');
  let generation = 0;

  $effect(() => {
    const id = projectId;
    untrack(() => {
      query = ''; status = ''; records = []; showEditor = false;
      sourceId = ''; deleteId = ''; message = ''; error = '';
      void load(id);
    });
    return () => { generation++; };
  });
  async function load(id = projectId) {
    const ticket = ++generation;
    loading = true; error = ''; conflict = false;
    try {
      const page = await api.memories(id, query, status);
      if (ticket === generation && projectId === id) { records = page.records; bounded = page.bounded; }
    } catch (e) { if (ticket === generation) error = e instanceof Error ? e.message : String(e); }
    finally { if (ticket === generation) loading = false; }
  }
  function edit(memory: Memory | null) {
    editing = memory; summary = memory?.summary ?? ''; showEditor = true;
    kind = memory?.authority === 'inferred_preference' ? 'preference' : memory?.authority === 'approved_policy' ? 'policy' : 'decision';
    message = ''; deleteId = '';
  }
  async function mutate(action: () => Promise<unknown>, success: string) {
    if (busy) return;
    const id = projectId;
    busy = true; error = ''; message = ''; conflict = false;
    try {
      await action();
      if (id === projectId) { message = success; showEditor = false; deleteId = ''; sourceId = ''; await load(id); }
    } catch (e) { if (id === projectId) { error = e instanceof Error ? e.message : String(e); conflict = e instanceof ApiError && e.status === 409; } }
    finally { busy = false; }
  }
  async function inspect(memory: Memory) {
    if (sourceId === memory.id) { sourceId = ''; return; }
    const id = projectId;
    sourceId = memory.id; sources = []; error = '';
    try { const result = await api.memorySources(memory); if (projectId === id && sourceId === memory.id) sources = result; }
    catch (e) { if (projectId === id) error = e instanceof Error ? e.message : String(e); }
  }
  function save() {
    const original = editing;
    void mutate(() => api.saveMemory(projectId, { summary, kind, ...(original ? {corrects: original.id, version: original.version, repository: original.scope.repository} : {}) }), original ? 'Correction saved. The previous record is in History.' : 'Memory saved.');
  }
</script>

<section class="pn memory" aria-labelledby="memory-title">
  <h3 id="memory-title" class="band">Memory</h3>
  <p class="muted">What this project remembers: decisions, preferences and facts. Private to you on this computer; share them with a Team under Settings → Personal memory.</p>
  <details class="about"><summary>Good to know</summary><p class="muted">Agent reports stay unverified until you confirm them. Saved policies are context only and never change what an agent may do. Do not save passwords or keys.</p></details>
  <form class="tools" onsubmit={(event) => { event.preventDefault(); void load(); }}>
    <label class="search">Search memories<input class="input" type="search" maxlength="256" bind:value={query} placeholder="Search words in a memory" /></label>
    <label>Status<select class="input" bind:value={status}><option value="">All records</option><option value="current">Current</option><option value="needs_verification">Unverified</option><option value="historical">History</option><option value="superseded">Superseded</option></select></label>
    <button class="btn" disabled={loading || busy}>Search</button>
    <button class="btn primary" type="button" disabled={busy} onclick={() => edit(null)}>Save a memory</button>
  </form>
  {#if error}<p class="error" role="alert">{error}{conflict ? ' Refresh the list before retrying a change.' : ''}</p>{/if}
  {#if message}<p role="status">{message}</p>{/if}
  {#if showEditor}
    <form class="editor" onsubmit={(event) => { event.preventDefault(); save(); }}>
      <h4>{editing ? 'Correct and confirm memory' : 'Save a memory'}</h4>
      <label>Kind<select class="input" bind:value={kind}><option value="decision">Decision</option><option value="preference">Preference</option><option value="fact">Fact</option><option value="policy">Policy</option></select></label>
      <label>What should this project remember?<textarea class="input" rows="3" maxlength="4096" bind:value={summary} required></textarea></label>
      <p class="muted small">Saving records your confirmation. A correction keeps the original evidence in History.</p>
      <div class="actions"><button class="btn primary" disabled={busy || !summary.trim()}>Confirm and save</button><button type="button" class="btn quiet" disabled={busy} onclick={() => showEditor = false}>Cancel</button></div>
    </form>
  {/if}
  {#if loading}<p class="muted" role="status">Loading memories…</p>
  {:else if records.length === 0}<p class="muted">No memories match this view. Save a decision or preference, or finish a run to collect its evidence.</p>
  {:else}
    <ul class="records card-list">
      {#each records as memory (memory.id)}
        <li class="card-row">
          <header><strong class="title">{memoryLabel(memory)}</strong><span class="tag">{verificationLabel(memory)}</span><span class="tag">{memory.type.replaceAll('_', ' ')}</span>{#if memory.pinned}<span class="tag">Pinned</span>{/if}</header>
          <p class="claim">{memory.summary}</p>
          <div class="actions">
            <button class="btn quiet" disabled={busy} onclick={() => edit(memory)}>Correct</button>
            <button class="btn quiet" disabled={busy} onclick={() => mutate(() => api.changeMemory(memory, {pinned: !memory.pinned}), memory.pinned ? 'Memory unpinned.' : 'Memory pinned.')}>{memory.pinned ? 'Unpin' : 'Pin'}</button>
            {#if memory.state !== 'historical' && memory.state !== 'superseded'}<button class="btn quiet" disabled={busy} onclick={() => mutate(() => api.changeMemory(memory, {archive: true}), 'Memory archived.')}>Archive</button>{/if}
            <button class="btn quiet" aria-expanded={sourceId === memory.id} disabled={busy} onclick={() => inspect(memory)}>Sources</button>
            <button class="btn quiet" disabled={busy} onclick={() => deleteId = memory.id}>Delete</button>
          </div>
          {#if deleteId === memory.id}<div class="confirm"><p>Delete this memory and its saved source excerpts? Run and Git history will remain.</p><div class="actions"><button class="btn danger" disabled={busy} onclick={() => mutate(() => api.deleteMemory(memory), 'Memory deleted.')}>Delete memory</button><button class="btn ok" disabled={busy} onclick={() => deleteId = ''}>Keep memory</button></div></div>{/if}
          {#if sourceId === memory.id}
            <div class="sources">
              <p class="muted small">Original source: <span class="mono">{memory.source.reference}</span></p>
              {#if memory.source.gitSha}<p class="mono">{memory.source.gitSha} {memory.source.branch ?? ''}</p>{/if}
              {#if memory.source.runId}<p class="mono">Run {memory.source.runId}</p>{/if}
              {#if memory.scope.repository}<p class="mono">{memory.scope.repository}</p>{/if}
              <ul>{#each sources as source (source.key)}<li><p class="mono">{source.source.reference}{source.source.approvalRef ? ` · ${source.source.approvalRef}` : ''}</p><p>{source.excerpt}</p><time datetime={source.observedAt}>{new Date(source.observedAt).toLocaleString()}</time></li>{/each}</ul>
              <p class="muted small">Showing up to 64 saved sources per record.</p>
            </div>
          {/if}
        </li>
      {/each}
    </ul>
    {#if bounded}<p class="muted">Showing the first 100 matches. Refine the search or status to find other records.</p>{/if}
  {/if}
</section>

<style>
  .memory { max-width: 900px; }
  .tools, .actions { display: flex; flex-wrap: wrap; align-items: end; gap: 8px; }
  .tools { margin: 20px 0; gap: 12px; }
  label { display: grid; gap: 6px; }
  .search { flex: 1; min-width: 180px; }
  .editor { display: grid; gap: 12px; padding: 16px 0; border-block: 1px solid var(--border); }
  h4, .claim { margin: 0; }
  .records { margin-top: 4px; }
  .claim { white-space: pre-wrap; overflow-wrap: anywhere; max-width: 75ch; margin-bottom: 12px; }
  .sources, .confirm { margin-top: 12px; padding: 12px; background: var(--surface-2); overflow-wrap: anywhere; }
  .sources ul { padding-left: 20px; }
  .sources li + li { margin-top: 12px; }
  .sources p { margin: 4px 0; }
  time { color: var(--text-2); font-size: 12px; }
  textarea { width: 100%; resize: vertical; }
</style>
