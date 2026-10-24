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
  <header><h1>Personal data library</h1><button class="btn" disabled={busy} onclick={() => attempt++}>Reload library</button></header>
  <p>Collections kept after deleting a workspace remain here. Inspect their records or use them in a new workspace. All data stays on this computer.</p>
  {#if notice}<p role="status">{notice}</p>{/if}
  {#if problem}<p role="alert">{problem}</p>{/if}
  {#if busy}<p role="status">Loading personal data…</p>{:else}
    {#if workspaces.length}<h2>Saved workspaces</h2><p>Includes workspaces with no display locations.</p><div class="rows">{#each workspaces as w (w.id)}<button class="btn" onclick={() => onopen(w)}>{w.name}</button>{/each}</div>{/if}
    <h2>Personal collections</h2>
    {#each collections as c (c.id)}<section class="row" aria-label={c.name}><div><h3>{c.name}</h3><p>{usages[c.id]?.recordCount ?? 'Unknown'} records · {usages[c.id]?.referenceCount === 0 ? 'Retained data — no workspace' : `${usages[c.id]?.referenceCount} workspace references`}</p></div><div class="rows"><button class="btn" onclick={() => recover(c, false)}>Inspect records</button><button class="btn" onclick={() => recover(c, true)}>Use in new workspace</button></div></section>{:else}<p>No personal collections are saved.</p>{/each}
  {/if}
  {#if session}<h2>Collection records</h2><WorkspaceRenderer {session} />{/if}
</div>
<style>
  .library { max-width: 1120px; margin-inline: auto; padding: 24px; }
  header, .row, .rows { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; } header, .row { justify-content: space-between; }
  h1 { font-size: 20px; margin: 0; } h2 { margin: 24px 0 12px; font-family: inherit; font-size: 18px; text-transform: none; letter-spacing: normal; } h3 { font-size: 14px; margin: 0; }
  p { color: var(--text-2); line-height: 1.6; max-width: 72ch; } .row { padding-block: 16px; border-bottom: 1px solid var(--border); } .row p { margin-bottom: 0; }
  @media (max-width: 600px) { .library { padding: 16px; } button { min-height: 44px; } }
</style>
