<script lang="ts">
  import { untrack } from 'svelte';
  import { z } from 'zod/v4';
  import { personalAPI, type Deletion } from './api';
  import { workspaceID } from './schema';
  let {args, onready}: {args: Record<string, unknown>; onready: (ready: boolean) => void} = $props();
  let preview = $state<Deletion | null>(null); let problem = $state('');
  $effect(() => {
    const raw = args; let active = true; preview = null; problem = ''; untrack(() => onready(false));
    const parsed = z.strictObject({workspaceId: workspaceID, revision: z.number().int().positive(), mode: z.enum(['keep_data', 'delete_data']), fingerprint: z.string().regex(/^[a-f0-9]{64}$/)}).safeParse(raw);
    if (!parsed.success) { problem = 'Deletion preview unavailable. Ask Salie to prepare it again.'; return; }
    const p = parsed.data;
    void personalAPI.previewDeletion(p.workspaceId, p.revision, p.mode).then(current => {
      if (!active) return;
      if (current.fingerprint !== p.fingerprint) throw new Error('Data or workspace dependencies changed. Ask Salie for a fresh deletion proposal.');
      preview = current; onready(true);
    }).catch(err => { if (active) problem = err instanceof Error ? err.message : 'Deletion preview unavailable. Nothing has changed.'; });
    return () => { active = false; };
  });
</script>
<section class="deletion-preview" aria-label="Personal workspace deletion preview">
  {#if problem}<p role="alert">{problem}</p>
  {:else if preview}
    <h3>{preview.mode === 'keep_data' ? 'Delete workspace only' : 'Delete workspace and its data'}: {preview.name}</h3>
    <p>Deletes the definition, revision history and all navigation entries.</p>
    {#each preview.collections as c (c.collectionId)}<div><strong>{c.delete ? 'Permanently delete' : 'Keep'} {c.name}</strong><p>{c.recordCount} personal records · {c.referenceCount} workspace references.</p>
      {#if !c.delete && preview.mode === 'delete_data'}<p>Protected because it is shared or is not owned by this workspace.</p>{/if}
      {#if c.referenceCount > 1}<p>Used by: {c.workspaces.map(w => w.name).join(', ')}{c.referenceCount > 20 ? ' (first 20 shown)' : ''}.</p>{/if}
    </div>{/each}
    <p>Retained collections remain in the Data library for recovery and reuse. Existing board tasks, projects, Git information, Team records and shared memory are unaffected.</p>
    <p>Confirm below to authorise this exact deletion. A change to data or dependencies requires a new review.</p>
  {:else}<p role="status">Checking personal data and workspace dependencies…</p>{/if}
</section>
<style>
  .deletion-preview { display: grid; gap: 12px; padding-block: 12px; border-block: 1px solid var(--border); min-width: 0; }
  h3 { font-size: 16px; margin: 0; overflow-wrap: anywhere; } p { margin: 0; color: var(--text-2); line-height: 1.6; max-width: 72ch; overflow-wrap: anywhere; }
</style>
