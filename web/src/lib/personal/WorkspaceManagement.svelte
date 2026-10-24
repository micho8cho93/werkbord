<script lang="ts">
  import Icon from '../Icon.svelte';
  import WorkspaceIcon from './WorkspaceIcon.svelte';
  import { newWorkspaceIcon } from './icons';
  import { workspaceSpecSchema, type Workspace } from './schema';
  import type { PersonalAPI, Deletion } from './api';
  let { workspace, api, teams = [], context = '', onupdated, ondeleted, onlibrary }: {
    workspace: Workspace; api: PersonalAPI; teams?: {id: string; name: string}[]; context?: string;
    onupdated: (workspace: Workspace) => void | Promise<void>; ondeleted: (preview: Deletion) => void | Promise<void>; onlibrary: () => void;
  } = $props();
  let open = $state(false);
  let panel = $state('');
  let busy = $state(false);
  let problem = $state('');
  let name = $state(''); let icon = $state<Workspace['icon']>('overview');
  let universal = $state(false); let individual = $state(false); let selectedTeams = $state<string[]>([]);
  let preview = $state<Deletion | null>(null);
  let confirmed = $state(false);
  let history = $state<Awaited<ReturnType<PersonalAPI['history']>>>([]);
  let target = $state(0);
  let historical = $state<Workspace | null>(null);
  const currentAssigned = $derived(context === 'universal' ? workspace.visibility.universal : context === 'personal' ? workspace.visibility.individual : (workspace.visibility.teamWorkspaceIds ?? []).includes(context));
  function settings() {
    open = false; panel = 'settings'; problem = '';
    name = workspace.name; icon = workspace.icon; universal = workspace.visibility.universal; individual = workspace.visibility.individual; selectedTeams = [...(workspace.visibility.teamWorkspaceIds ?? [])];
  }
  async function act(run: () => Promise<void>) {
    if (busy) return; busy = true; problem = '';
    try { await run(); } catch (err) { problem = `${err instanceof Error ? err.message : 'The change could not be confirmed.'} Reload to check the current state before retrying.`; preview = null; confirmed = false; }
    finally { busy = false; }
  }
  async function save(remove = false) {
    await act(async () => {
      const spec = workspaceSpecSchema.parse(workspaceSpec(workspace));
      if (remove) {
        if (context === 'universal') spec.visibility.universal = false;
        else if (context === 'personal') spec.visibility.individual = false;
        else spec.visibility.teamWorkspaceIds = (spec.visibility.teamWorkspaceIds ?? []).filter(id => id !== context);
      } else { spec.name = name; spec.icon = icon; spec.visibility = {universal, individual, teamWorkspaceIds: selectedTeams}; }
      await onupdated(await api.updateWorkspace(workspace.id, workspace.revision, spec)); panel = '';
    });
  }
  function workspaceSpec(w: Workspace) { const {id, revision, createdAt, updatedAt, ...spec} = w; void id; void revision; void createdAt; void updatedAt; return spec; }
  async function deletion(mode: 'keep_data' | 'delete_data') {
    confirmed = false;
    await act(async () => { preview = await api.previewDeletion(workspace.id, workspace.revision, mode); });
  }
  async function revisions() {
    open = false; panel = 'history'; historical = null;
    await act(async () => { history = await api.history(workspace.id); target = history.find(h => h.revision < workspace.revision)?.revision ?? 0; });
  }
</script>
<details class="overflow" bind:open><summary class="btn" aria-label="Manage workspace" title="Manage workspace"><Icon name="more" /></summary><div class="menu">
  <button class="btn" onclick={settings} disabled={busy}>Name, icon & locations</button>
  {#if context && currentAssigned}<button class="btn" onclick={() => { open = false; void save(true); }} disabled={busy}>Remove from this location</button>{/if}
  <button class="btn" onclick={revisions} disabled={busy}>Definition history</button>
  <button class="btn" onclick={onlibrary}>Data library</button>
  <button class="btn" onclick={() => { open = false; panel = 'delete'; preview = null; problem = ''; }} disabled={busy}>Delete workspace…</button>
</div></details>
{#if panel || problem}<section class="management" aria-label="Workspace management" aria-busy={busy}>
  {#if problem}<p role="alert">{problem}</p>{/if}
  {#if panel === 'settings'}
    <form onsubmit={e => { e.preventDefault(); void save(); }}>
      <label>Name <input class="input" bind:value={name} required maxlength="120" disabled={busy} /></label>
      <div class="icon-choice"><WorkspaceIcon {icon} name={name} size={24} /><span>Workspace icon</span><button class="btn" type="button" disabled={busy} onclick={() => icon = newWorkspaceIcon()}>New icon</button></div>
      <p>Each workspace has its own icon, distinct from the app’s navigation and action icons.</p>
      <fieldset disabled={busy}><legend>Show in</legend>
        <label><input type="checkbox" bind:checked={universal} /> Universal</label><label><input type="checkbox" bind:checked={individual} /> Individual</label>
        {#each teams as team (team.id)}<label><input type="checkbox" bind:group={selectedTeams} value={team.id} /> {team.name}</label>{/each}
        {#each selectedTeams.filter(id => !teams.some(t => t.id === id)) as id (id)}<label><input type="checkbox" bind:group={selectedTeams} value={id} /> Unavailable Team ({id})</label>{/each}
      </fieldset>
      <p>Locations control navigation only. Your workspace and personal records stay saved, even with no locations selected. Team display keeps them private to you.</p>
      <button class="btn primary" disabled={busy || !name.trim()}>Save changes</button>
    </form>
  {:else if panel === 'delete'}
    <h2>Delete “{workspace.name}”</h2>
    <div class="actions"><button class="btn" disabled={busy} onclick={() => deletion('keep_data')}>Delete workspace only</button><button class="btn" disabled={busy} onclick={() => deletion('delete_data')}>Delete workspace and its data</button></div>
    <p>Deleting only the workspace keeps all personal collections in the data library for recovery and reuse.</p>
    {#if preview}
      <h3>{preview.mode === 'keep_data' ? 'Keep personal records' : 'Review personal data deletion'}</h3>
      <p>The workspace definition, its revision history and all navigation entries will be deleted.</p>
      {#each preview.collections as c (c.collectionId)}<div class="collection"><strong>{c.delete ? 'Delete' : 'Keep'} {c.name}</strong><p>{c.recordCount} personal records. {c.referenceCount} workspace references.</p>
        {#if !c.delete && preview.mode === 'delete_data'}<p>Protected: this collection is shared or is not owned by this workspace.</p>{/if}
        {#if c.referenceCount > 1}<p>Used by: {c.workspaces.map(w => w.name).join(', ')}{c.referenceCount > 20 ? ' (first 20 shown)' : ''}.</p>{/if}
      </div>{/each}
      <p>Existing board tasks, projects, Git information, Team records and shared memory are unaffected. Retained collections remain in the data library.</p>
      <label><input type="checkbox" bind:checked={confirmed} disabled={busy} /> I confirm this deletion and the personal data choices above.</label>
      <button class="btn" disabled={busy || !confirmed} onclick={() => act(async () => { if (preview) await ondeleted(await api.deleteWorkspace(preview)); })}>Confirm deletion</button>
    {/if}
  {:else if panel === 'history'}
    <h2>Definition history</h2><p>Restore a saved interface as a new revision. Personal records and collection schemas stay current.</p>
    <label>Saved revision <select class="select" bind:value={target} disabled={busy} onchange={() => historical = null}>{#each history.filter(h => h.revision < workspace.revision) as h (h.revision)}<option value={h.revision}>Revision {h.revision} · {new Date(h.updatedAt).toLocaleString()}</option>{/each}</select></label>
    {#if !target}<p>No earlier revisions.</p>{:else}<button class="btn" disabled={busy} onclick={() => act(async () => { historical = (await api.snapshot(workspace.id, target)).workspace; })}>Review revision</button>{/if}
    {#if historical}<p>Restore revision {historical.revision}: “{historical.name}”, icon {historical.icon}, {historical.definition.views.length} views. Its saved display locations will also be restored.</p><button class="btn" disabled={busy} onclick={() => act(async () => { await onupdated(await api.rollback(workspace.id, workspace.revision, historical!.revision)); panel = ''; })}>Restore this definition</button>{/if}
    <p>The latest 100 revisions are listed.</p>
  {/if}
  <button class="btn" disabled={busy} onclick={() => { panel = ''; problem = ''; }}>Cancel</button>
</section>{/if}
<style>
  .icon-choice { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; }
  .overflow { position: relative; } summary { cursor: pointer; list-style: none; } summary::before { content: none; } summary::-webkit-details-marker { display: none; }
  .menu { position: absolute; z-index: 5; right: 0; top: 100%; display: grid; gap: 4px; width: min(270px, 80vw); padding: 8px; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-sm); }
  .management { flex-basis: 100%; display: grid; gap: 14px; padding: 16px; border: 1px solid var(--border); border-radius: var(--radius-sm); }
  form { display: grid; gap: 14px; } form > label { display: grid; gap: 6px; }
  fieldset, .actions { display: flex; flex-wrap: wrap; gap: 12px; } fieldset { border: 1px solid var(--border); }
  h2, h3, p { margin: 0; } h2 { font-family: inherit; font-size: 18px; text-transform: none; letter-spacing: normal; } h3 { font-size: 14px; } p { color: var(--text-2); line-height: 1.6; max-width: 72ch; }
  .collection { padding-block: 10px; border-top: 1px solid var(--border); }
  button { justify-self: start; } @media (max-width: 600px) { button { min-height: 44px; } }
</style>
