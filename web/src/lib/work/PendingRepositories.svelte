<script lang="ts">
  import FolderPicker from '../FolderPicker.svelte';
  import type { Overview, PendingRepository } from '../../shell/types';
  import type { WorkAction } from './actions';
  let { overview, act, chooseFolder }: { overview: Overview | null; act: (workspace: string, action: WorkAction) => Promise<void>; chooseFolder?: () => Promise<string> } = $props();
  const rows = $derived((overview?.entries ?? []).filter(e => !e.error && e.workspace.state === 'ready' && e.workspace.kind === 'personal').flatMap(e => (e.summary?.pendingRepositories ?? []).map(item => ({ workspace: e.workspace.id, item }))));
  let paths = $state<Record<string, string>>({});
  let busy = $state('');
  let error = $state('');
  let linked = $state<Record<string, boolean>>({});
  async function change(workspace: string, item: PendingRepository, operation: 'link' | 'clone', path?: string) {
    if (busy || linked[item.sourceRef]) return;
    busy = item.sourceRef; error = '';
    try { await act(workspace, { operation, sourceRef: item.sourceRef, path }); linked[item.sourceRef] = true; }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = ''; paths[item.sourceRef] = ''; }
  }
  $effect(() => {
    for (const r of rows) { const path = paths[r.item.sourceRef]; if (path && !busy && !linked[r.item.sourceRef]) void change(r.workspace, r.item, 'link', path); }
  });
</script>
{#if rows.length}
<section class="incoming" aria-labelledby="incoming-h">
  <h2 id="incoming-h" class="section-title">Team tickets waiting for a repository<span>{rows.length}</span></h2>
  <ul>{#each rows as r (r.workspace + r.item.sourceRef)}<li data-testid="waiting-repository">
    <strong>{r.item.title}</strong><p class="muted">From {r.item.from || 'Team'} · needs <code>{r.item.repository}</code></p>
    {#if linked[r.item.sourceRef]}<p role="status">Repository added. The ticket appears in Work after its source checks again.</p>
    {:else}<p class="muted">Choose its local clone{r.item.github ? ', or clone it from GitHub' : ''}. Linking creates a task; it does not start an agent.</p>
      <div class="actions"><FolderPicker bind:value={() => paths[r.item.sourceRef] ?? '', path => { paths[r.item.sourceRef] = path; }} disabled={!!busy} chooseWith={chooseFolder} />
      {#if r.item.github}<button class="btn" disabled={!!busy} onclick={() => change(r.workspace, r.item, 'clone')}>Clone from GitHub</button>{/if}</div>
    {/if}
  </li>{/each}</ul>
  {#if error}<p role="alert">{error} Check current projects before trying again.</p>{/if}
</section>
{/if}
<style>
  .incoming { border: 1px solid var(--tray-ask-edge); background: var(--tray-ask); border-radius: var(--radius); padding: 16px; }
  h2 { margin: 0 0 12px; } h2 span { margin-left: 8px; color: var(--text-2); font-size: 12px; }
  ul { margin: 0; padding: 0; list-style: none; } li + li { border-top: 1px solid var(--border); margin-top: 14px; padding-top: 14px; }
  strong { font-size: 14px; font-weight: 500; } p { margin: 6px 0; font-size: 13px; overflow-wrap: anywhere; }
  .muted { color: var(--text-2); } .actions { display: flex; flex-wrap: wrap; gap: 8px; }
</style>
