<script lang="ts">
  import { untrack } from 'svelte';
  import type { PersonalAPI, Usage } from './api';
  import type { Collection, Workspace } from './schema';
  import WorkspaceRenderer from './WorkspaceRenderer.svelte';
  import { WorkspaceSession } from './session.svelte';
  let { api, onopen }: {api: PersonalAPI; onopen: (workspace: Workspace) => void | Promise<void>} = $props();
  let collections = $state<Collection[]>([]); let workspaces = $state<Workspace[]>([]); let usages = $state<Record<string, Usage>>({});
  let session = $state<WorkspaceSession | null>(null);
  let busy = $state(false); let problem = $state(''); let notice = $state(''); let attempt = $state(0);
  $effect(() => {
    void attempt; let active = true; busy = true; problem = ''; session = null;
    untrack(() => { void Promise.all([api.collections(), api.workspaces()]).then(async ([cs, ws]) => {
      const usage = await Promise.all(cs.map(c => api.usage(c.id)));
      if (active) { collections = cs; workspaces = ws; usages = Object.fromEntries(usage.map(u => [u.collectionId, u])); }
    }).catch(err => { if (active) problem = err instanceof Error ? err.message : 'The data library could not be loaded. Try again.'; }).finally(() => { if (active) busy = false; }); });
    return () => { active = false; };
  });
  async function recover(c: Collection, save: boolean) {
    if (busy) return; busy = true; problem = '';
    try {
      const spec = {name: c.name, icon: 'auto' as const, uiSchemaVersion: 1 as const, collectionIds: [c.id], readBindings: [], visibility: {universal: true, individual: true, teamWorkspaceIds: []}, definition: {views: [{id: 'records', name: c.name, kind: 'table' as const, collectionId: c.id, fields: c.fields.map(f => f.key), operations: ['create' as const, 'update' as const, 'delete' as const]}], layout: {type: 'view' as const, viewId: 'records'}}};
      if (save) {
        const workspace = await api.createWorkspace(spec);
        notice = 'Recovered records in a new personal workspace.';
        await onopen(workspace);
      } else {
        // A fixed, local table is only an inspection surface; no definition is saved.
        const records = await api.records(c.id);
        const next = new WorkspaceSession(api);
        next.readOnly = true;
        next.workspace = {...spec, id: 'pws_aaaaaaaaaaaaaaaa', revision: 1, createdAt: c.createdAt, updatedAt: c.updatedAt};
        next.collections = [c];
        next.records = {[c.id]: records};
        session = next;
      }
    } catch (err) { problem = `${err instanceof Error ? err.message : 'Recovery could not be confirmed.'} Reload to check the library before retrying.`; }
    finally { busy = false; }
  }
</script>
<div class="library">
  <header><h1>Data library</h1><button class="btn quiet" disabled={busy} onclick={() => attempt++}>Reload library</button></header>
  <p class="intro">Everything your workspaces keep, stored on this computer. Collections left behind by a deleted workspace stay here to inspect or reuse.</p>
  {#if notice}<p role="status">{notice}</p>{/if}
  {#if problem}<p role="alert">{problem}</p>{/if}
  {#if busy}<p role="status">Loading personal data…</p>{:else}
    {#if workspaces.length}
      <section class="pn" aria-label="Saved workspaces">
        <h2 class="band">Saved workspaces</h2>
        <ul class="card-list">{#each workspaces as w (w.id)}<li class="card-row"><header><h3 class="title">{w.name}</h3></header><div class="actions"><button class="btn open" onclick={() => onopen(w)}>Open</button></div></li>{/each}</ul>
      </section>
    {/if}
    <section class="pn" aria-label="Personal collections">
      <h2 class="band">Personal collections</h2>
      <ul class="card-list">
        {#each collections as c (c.id)}
          <li class="card-row" aria-label={c.name}>
            <header><h3 class="title">{c.name}</h3><span class="tag">{usages[c.id]?.referenceCount === 0 ? 'Retained data — no workspace' : `${usages[c.id]?.referenceCount} workspace references`}</span></header>
            <p class="count">{usages[c.id]?.recordCount ?? 'Unknown'} records</p>
            <div class="actions"><button class="btn" onclick={() => recover(c, false)}>Inspect records</button><button class="btn" onclick={() => recover(c, true)}>Use in new workspace</button></div>
          </li>
        {:else}<li class="muted">No personal collections are saved.</li>{/each}
      </ul>
    </section>
  {/if}
  {#if session}<section class="pn" aria-label="Collection records"><h2 class="band">Collection records</h2><WorkspaceRenderer {session} /></section>{/if}
</div>
<style>
  .library { display: grid; gap: 20px; max-width: 1800px; margin-inline: auto; padding: 24px clamp(16px, 2.5vw, 40px); }
  header { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; }
  .card-row > header { justify-content: flex-start; }
  h1 { font-size: 20px; margin: 0; }
  p { margin: 0; color: var(--text-2); line-height: 1.6; max-width: 72ch; }
  .count { font-size: 13px; }
  @media (max-width: 600px) { .library { padding: 16px; } button { min-height: 44px; } }
</style>
