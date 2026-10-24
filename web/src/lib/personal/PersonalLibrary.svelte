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
  let allWorkspaces = $state(false); let allCollections = $state(false);
  const recentWorkspaces = $derived([...workspaces].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt) || a.name.localeCompare(b.name)));
  const recentCollections = $derived([...collections].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt) || a.name.localeCompare(b.name)));
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
    <div class="library-columns">
      <section class="pn" aria-label="Saved workspaces">
        <h2 class="band">Saved workspaces <span class="total">{workspaces.length}</span></h2>
        <ul class="card-list" id="library-workspaces">{#each recentWorkspaces.slice(0, allWorkspaces ? undefined : 4) as w (w.id)}<li class="card-row workspace-row"><header><h3 class="title">{w.name}</h3></header><div class="actions"><button class="btn open" onclick={() => onopen(w)}>Open</button></div></li>{:else}<li class="muted">No workspaces are saved.</li>{/each}</ul>
        {#if workspaces.length > 4}<button class="btn quiet more" aria-controls="library-workspaces" aria-expanded={allWorkspaces} onclick={() => allWorkspaces = !allWorkspaces}>{allWorkspaces ? 'Show fewer workspaces' : `Show all ${workspaces.length} workspaces`}</button>{/if}
      </section>
    <section class="pn" aria-label="Personal collections">
      <h2 class="band">Personal collections <span class="total">{collections.length}</span></h2>
      <ul class="card-list" id="library-collections">
        {#each recentCollections.slice(0, allCollections ? undefined : 4) as c (c.id)}
          <li class="card-row" aria-label={c.name}>
            <header><h3 class="title">{c.name}</h3><span class="tag">{usages[c.id]?.referenceCount === 0 ? 'Retained data — no workspace' : `${usages[c.id]?.referenceCount ?? 'Unknown'} workspace ${usages[c.id]?.referenceCount === 1 ? 'reference' : 'references'}`}</span></header>
            <p class="count">{usages[c.id]?.recordCount ?? 'Unknown'} records</p>
            <div class="actions"><button class="btn" onclick={() => recover(c, false)}>Inspect records</button><button class="btn" onclick={() => recover(c, true)}>Use in new workspace</button></div>
          </li>
        {:else}<li class="muted">No personal collections are saved.</li>{/each}
      </ul>
      {#if collections.length > 4}<button class="btn quiet more" aria-controls="library-collections" aria-expanded={allCollections} onclick={() => allCollections = !allCollections}>{allCollections ? 'Show fewer collections' : `Show all ${collections.length} collections`}</button>{/if}
    </section>
    </div>
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
  .library-columns { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 20px; align-items: start; }
  .library-columns > section { min-width: 0; }
  .band { display: flex; align-items: center; gap: 12px; }
  .total { margin-left: auto; color: var(--text-2); font-family: var(--mono); }
  .more { margin-top: 16px; }
  .title { overflow-wrap: anywhere; }
  .workspace-row { grid-template-columns: minmax(0, 1fr) auto; align-items: center; }
  .workspace-row .actions { border: 0; padding: 0; }
  @media (max-width: 1000px) { .library-columns { grid-template-columns: minmax(0, 1fr); } }
  @media (max-width: 600px) { .library { padding: 16px; } button { min-height: 44px; } }
</style>
