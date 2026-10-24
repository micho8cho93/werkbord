<script lang="ts">
  import { untrack } from 'svelte';
  import { z } from 'zod/v4';
  import WorkspaceIcon from './WorkspaceIcon.svelte';
  import WorkspaceRenderer from './WorkspaceRenderer.svelte';
  import { WorkspaceSession } from './session.svelte';
  import { personalAPI } from './api';
  import { collectionSchema, collectionSpecSchema, workspaceSpecSchema, workspaceID, validateWorkspace, type Workspace } from './schema';

  let { args, onready }: { args: Record<string, unknown>; onready: (ready: boolean) => void } = $props();
  let preview = $state<WorkspaceSession | null>(null);
  let previous = $state<Workspace | null>(null);
  let problem = $state('');
  const proposalSchema = z.strictObject({
    workspaceId: workspaceID.optional(), revision: z.number().int().positive().optional(),
    workspace: workspaceSpecSchema,
    collections: z.array(z.strictObject({ id: z.string(), revision: z.number().int().nonnegative(), spec: collectionSpecSchema })).max(32),
  });
  const proposal = $derived(proposalSchema.safeParse(args));
  $effect(() => {
    const parsed = proposal;
    let active = true;
    preview = null; previous = null; problem = '';
    untrack(() => onready(false));
    if (!parsed.success) { problem = 'This preview could not be read. Ask Salie to prepare it again.'; return; }
    const change = parsed.data;
    void (async () => {
      try {
        const meta = { revision: 1, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() };
        const collections = change.collections.map(c => collectionSchema.parse({ ...meta, ...c.spec, id: c.id }));
        const workspace = validateWorkspace({ ...meta, ...change.workspace, id: change.workspaceId ?? 'pws_aaaaaaaaaaaaaaaa' }, collections);
        const session = new WorkspaceSession();
        session.readOnly = true;
        session.collections = collections;
        if (change.workspaceId) {
          const current = await personalAPI.workspace(change.workspaceId);
          if (current.revision !== change.revision) throw new Error('This workspace changed after the proposal was prepared. Ask Salie for a fresh proposal.');
          if (active) previous = current;
        }
        {
          const entries = await Promise.all(change.collections.filter(c => c.revision > 0).map(async c => {
            const saved = await personalAPI.collection(c.id);
            if (saved.revision !== c.revision) throw new Error('A collection schema changed. Ask Salie for a fresh proposal.');
            return [c.id, await personalAPI.records(c.id)] as const;
          }));
          session.records = Object.fromEntries(entries);
        }
        // Preview uses the real registry and saved records with all writes disabled.
        session.workspace = { ...workspace, definition: { ...workspace.definition, views: workspace.definition.views.map(v => ({ ...v, operations: [] })) } };
        if (active) { preview = session; onready(true); }
      } catch (err) {
        if (active) problem = `${err instanceof Error ? err.message : 'Preview unavailable.'} Nothing has been saved by this proposal.`;
      }
    })();
    return () => { active = false; };
  });
</script>

<section class="workspace-preview" aria-label="Proposed workspace">
  {#if problem}<p role="alert">{problem}</p>
  {:else if preview?.workspace && proposal.success}
    <h3><WorkspaceIcon icon={preview.workspace.icon} name={preview.workspace.name} size={18} />{preview.workspace.name}</h3>
    {#if previous}<p>Editing {previous.name} · revision {proposal.data.revision} · {previous.id}</p>{:else}<p>New workspace · {proposal.data.collections.some(c => c.revision > 0) ? 'reuses saved personal data' : 'collections start empty'}</p>{/if}
    <p>Show in: {[preview.workspace.visibility.universal ? 'Universal' : '', preview.workspace.visibility.individual ? 'Individual' : '', ...(preview.workspace.visibility.teamWorkspaceIds ?? [])].filter(Boolean).join(', ')}</p>
    {#if previous}<details><summary>Previous definition and locations</summary><pre>{JSON.stringify(previous, null, 2)}</pre></details>{/if}
    <details><summary>Review fields and record controls</summary>
      {#each proposal.data.collections as c (c.id)}
        <h4>{c.spec.name}</h4>
        <ul>{#each c.spec.fields as field (field.key)}<li>{field.name} ({field.key}) · {field.type}{field.required ? ' · required' : ' · optional'}{field.options?.length ? ` · ${field.options.join(', ')}` : ''}</li>{/each}</ul>
      {/each}
      {#each proposal.data.workspace.definition.views as view (view.id)}<p>{view.name}: {view.operations?.join(', ') || 'read only'}{view.filters?.length ? ` · ${view.filters.map(f => `${f.field} = ${f.value}`).join(', ')}` : ''}</p>{/each}
    </details>
    <p>Preview only. Confirm below to save this definition. Existing records keep their IDs and values.</p>
    <svelte:boundary>
      {#key preview}<WorkspaceRenderer session={preview} />{/key}
      {#snippet failed(error)}<p role="alert">Preview unavailable: {error instanceof Error ? error.message : 'unsupported interface'}</p>{/snippet}
    </svelte:boundary>
  {:else}<p role="status">Preparing workspace preview…</p>{/if}
</section>

<style>
  .workspace-preview { display: grid; gap: 12px; min-width: 0; padding-block: 12px; border-block: 1px solid var(--border); }
  h3 { display: flex; align-items: center; gap: 8px; margin: 0; font-size: 16px; overflow-wrap: anywhere; }
  h4 { margin-block: 12px 6px; font-size: 13px; }
  p, li, summary { margin: 0; font-size: 12.5px; line-height: 1.6; overflow-wrap: anywhere; }
  p { color: var(--text-2); }
  summary { cursor: pointer; }
  summary:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
  ul { padding-left: 20px; }
  pre { max-height: 240px; overflow: auto; font-size: 12px; white-space: pre-wrap; overflow-wrap: anywhere; }
</style>
