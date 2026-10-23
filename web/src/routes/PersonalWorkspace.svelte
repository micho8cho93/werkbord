<script lang="ts">
  import WorkspaceManagement from '../lib/personal/WorkspaceManagement.svelte';
  import PersonalLibrary from '../lib/personal/PersonalLibrary.svelte';
  import { onDestroy, untrack } from 'svelte';
  import { embedded } from '../lib/embed';
  import { personalWorkspaceDraft } from '../lib/personal/conversation';
  import { assistant } from '../lib/assistant/store.svelte';
  import { z } from 'zod/v4';
  import WorkspaceRenderer from '../lib/personal/WorkspaceRenderer.svelte';
  import { WorkspaceSession } from '../lib/personal/session.svelte';
  import { personalAPI } from '../lib/personal/api';
  import { todoCollection, todoWorkspace } from '../lib/personal/example';
  let { id = '' }: { id?: string } = $props();
  let library = $state(false);
  let notice = $state('');
  let session = $state<WorkspaceSession | null>(null);
  let loading = $state(false);
  let problem = $state('');
  let attempt = $state(0);
  // A failed example install retains its successfully created resources for retry.
  let exampleCollectionId = '';
  let alive = true;
  onDestroy(() => { alive = false; });
  const message = (err: unknown) => err instanceof z.ZodError
    ? 'This saved workspace contains an unsupported or malformed definition. Correct it, then reload. Your collection records are safe.'
    : err instanceof Error ? err.message : 'This workspace could not be loaded. Try again.';
  $effect(() => {
    const target = id; library = false;
    void attempt;
    let active = true;
    session = null; problem = '';
    loading = Boolean(target);
    if (!target) return;
    const next = new WorkspaceSession();
    untrack(() => { void next.load(target).then(() => {
      if (active) session = next;
    }).catch(err => {
      if (active) problem = message(err);
    }).finally(() => { if (active) loading = false; }); });
    return () => { active = false; };
  });
  function editWithSalie() {
    if (embedded()) {
      window.parent.postMessage({ type: 'werkbord.frame', event: 'personal_workspace_assistant', workspaceId: id, revision: session?.workspace?.revision }, '*');
      return;
    }
    assistant.personalEditing = id && session?.workspace ? { workspaceId: id, revision: session.workspace.revision } : undefined;
    assistant.draft = personalWorkspaceDraft(id);
    assistant.show();
  }
  async function installExample() {
    if (loading) return;
    loading = true; problem = '';
    try {
      if (!exampleCollectionId) exampleCollectionId = (await personalAPI.createCollection(todoCollection)).id;
      const workspace = await personalAPI.createWorkspace(todoWorkspace(exampleCollectionId));
      if (alive) location.hash = `#/personal?id=${workspace.id}`;
    } catch (err) { problem = message(err); }
    finally { loading = false; }
  }
</script>

<div class="personal-page">
  {#if notice}<p role="status">{notice}</p>{/if}
  <button class="btn" onclick={() => library = !library}>{library ? 'Back to workspace' : 'Personal data library'}</button>
  {#if library}<PersonalLibrary api={personalAPI} onopen={w => { library = false; location.hash = `#/personal?id=${w.id}`; }} />{:else}
  <header><div><h1>{session?.workspace?.name ?? 'Personal workspace'}</h1><p>Saved on this computer</p></div><button class="btn primary" disabled={loading || session?.busy} onclick={editWithSalie}>{id ? 'Edit with Salie' : 'Create with Salie'}</button>{#if id}<button class="btn" disabled={loading || session?.busy} onclick={() => attempt++}>Reload workspace</button>{/if}{#if session?.workspace && !session.busy}<WorkspaceManagement workspace={session.workspace} api={personalAPI} onupdated={w => { session!.workspace = w; attempt++; }} onlibrary={() => library = true} ondeleted={preview => { notice = preview.collections.some(c => !c.delete) ? 'Workspace deleted. Preserved personal records are in the data library.' : 'Workspace and its exclusively owned personal data deleted.'; session = null; library = true; }} />{/if}</header>
  {#if loading}<p role="status">Loading workspace…</p>
  {:else if problem}<section class="recovery" role="alert"><h2>Workspace unavailable</h2><p>{problem}</p><p>Your saved collection records have not been changed.</p><button class="btn" onclick={() => id ? attempt++ : installExample()}>Try again</button></section>
  {:else if session}
    <svelte:boundary>
      <WorkspaceRenderer {session} />
      {#snippet failed(error, reset)}<section class="recovery" role="alert"><h2>Interface unavailable</h2><p>This definition could not be rendered. Your records are safe.</p><!-- The boundary only exposes a plain recovery message. --><button class="btn" onclick={() => { void error; reset(); attempt++; }}>Reload workspace</button></section>{/snippet}
    </svelte:boundary>
  {:else}<section class="example"><h2>A personal to-do workspace</h2><p>Save a ready-made checklist, editable table, progress display and priority chart. All views share one personal collection. No model is needed.</p><button class="btn primary" disabled={loading} onclick={installExample}>Save to-do example</button><p class="hint">Keep the resulting workspace URL to reopen it after restarting Werkbord.</p></section>{/if}
  {/if}
</div>

<style>
  .personal-page { width: 100%; max-width: 1120px; margin: 0 auto; padding: 24px; min-width: 0; }
  header { display: flex; flex-wrap: wrap; align-items: start; justify-content: space-between; gap: 12px; margin-bottom: 20px; }
  h1 { margin: 0; font-size: 24px; font-weight: 600; overflow-wrap: anywhere; }
  header p, .hint { margin: 6px 0 0; color: var(--text-2); font-size: 12px; }
  h2 { margin: 0; font-size: 18px; }
  .example, .recovery { display: grid; justify-items: start; gap: 14px; max-width: 65ch; padding: 24px 0; }
  .example p, .recovery p { margin: 0; color: var(--text-2); line-height: 1.6; }
  @media (max-width: 600px) { .personal-page { padding: 16px; } button { min-height: 44px; } }
</style>
