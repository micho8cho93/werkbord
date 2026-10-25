<script lang="ts">
  import WorkspaceManagement from '../lib/personal/WorkspaceManagement.svelte';
  import PersonalLibrary from '../lib/personal/PersonalLibrary.svelte';
  import { onDestroy, untrack } from 'svelte';
  import PhoneBack from '../lib/mobile/PhoneBack.svelte';
  import { phone } from '../lib/mobile/phone.svelte';
  import { deckApi, isWorkspaceButton, newSlot, place } from '../lib/mobile/layout';
  import { isPhone } from '../lib/phones/capability';
  import { app } from '../lib/state.svelte';
  import { embedded } from '../lib/embed';
  import { contextualWorkspaceDraft, personalWorkspaceDraft } from '../lib/personal/conversation';
  import { assistant } from '../lib/assistant/sessions.svelte';
  import { z } from 'zod/v4';
  import WorkspaceRenderer from '../lib/personal/WorkspaceRenderer.svelte';
  import { WorkspaceSession } from '../lib/personal/session.svelte';
  import { personalAPI } from '../lib/personal/api';
  import { todoCollection, todoWorkspace } from '../lib/personal/example';
  import { pageAddresses } from '../lib/personal/shortcuts';
  import type { ShortcutTarget } from '../lib/personal/schema';
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
  // A phone works with the workspace's records. What a workspace is (its name, history, deletion, the data library's
  // housekeeping) is the owner's at the computer, so the phone is not shown those controls (api/phone.go).
  const restricted = isPhone();
  // Another device changed a workspace or its data: show it without a reload. Our own saves also come back as events,
  // so changes are coalesced and never read over a save that is still going.
  let seenChange = app.personalRevision;
  $effect(() => {
    const change = app.personalRevision;
    if (change === seenChange) return;
    seenChange = change;
    const timer = setTimeout(() => {
      const current = session;
      if (!id || !current || current.busy || loading) return;
      void current.load(id).catch(() => { if (alive) attempt++; });
    }, 400);
    return () => clearTimeout(timer);
  });
  // The Control Deck button for this workspace. The deck belongs to whoever is signed in here: a phone has its own.
  let onDeck = $state<boolean | null>(null);
  let deckNotice = $state('');
  $effect(() => {
    const target = id; void app.personalRevision;
    onDeck = null; deckNotice = '';
    if (!isWorkspaceButton(target)) return;
    let live = true;
    deckApi.load().then((layout) => { if (live) onDeck = layout.slots.some((slot) => slot?.button === target); }, () => { /* the deck is not offered */ });
    return () => { live = false; };
  });
  async function toggleDeck() {
    if (!isWorkspaceButton(id) || onDeck === null) return;
    deckNotice = '';
    try {
      const layout = await deckApi.load();
      const at = layout.slots.findIndex((slot) => slot?.button === id);
      let slots = layout.slots;
      if (at >= 0) slots = place(slots, at, null);
      else {
        const free = slots.findIndex((slot) => !slot);
        if (free < 0) { deckNotice = 'Your Control Deck is full. Remove a button first.'; return; }
        slots = place(slots, free, newSlot(crypto.randomUUID(), id));
      }
      await deckApi.save({ ...layout, slots });
      onDeck = at < 0;
      deckNotice = at < 0 ? 'Added to the Control Deck.' : 'Removed from the Control Deck.';
    } catch (err) { deckNotice = err instanceof Error ? err.message : 'The Control Deck could not be saved. Try again.'; }
  }
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
    // A shortcut here opens one of the app's own pages, by the address the page has.
    if (!embedded()) next.assist = (prompt) => {assistant.draft = contextualWorkspaceDraft(target,prompt); assistant.show();};
    next.navigable = (Object.keys(pageAddresses) as ShortcutTarget[]);
    next.navigate = (target) => { const address = pageAddresses[target]; if (address) location.hash = address; };
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
  {#if phone.on}<div class="phone-top"><PhoneBack /></div>{/if}
  {#if notice}<p role="status">{notice}</p>{/if}
  {#if !restricted}<button class="btn open" onclick={() => library = !library}>{library ? 'Back to workspace' : 'Data library'}</button>{/if}
  {#if library}<PersonalLibrary api={personalAPI} onopen={w => { library = false; location.hash = `#/personal?id=${w.id}`; }} />{:else}
  <header><div><h1>{session?.workspace?.name ?? 'Personal workspace'}</h1><p>{restricted ? 'Saved on your computer' : 'Saved on this computer'}</p></div><div class="head-actions"><button class="btn primary" disabled={loading || session?.busy} onclick={editWithSalie}>{id ? 'Edit with Salie' : 'Create with Salie'}</button>{#if id}<button class="btn quiet" disabled={loading || session?.busy} onclick={() => attempt++}>Reload workspace</button>{/if}{#if onDeck !== null}<button class="btn {onDeck ? 'warn' : 'add'}" onclick={toggleDeck} data-testid="workspace-deck-button">{onDeck ? 'Remove from Control Deck' : 'Add to Control Deck'}</button>{/if}{#if session?.workspace && !session.busy && !restricted}<WorkspaceManagement workspace={session.workspace} api={personalAPI} onupdated={w => { session!.workspace = w; attempt++; }} onlibrary={() => library = true} ondeleted={preview => { notice = preview.collections.some(c => !c.delete) ? 'Workspace deleted. Preserved personal records are in the data library.' : 'Workspace and its exclusively owned personal data deleted.'; session = null; library = true; }} />{/if}</div></header>
  {#if deckNotice}<p role="status" class="hint">{deckNotice}</p>{/if}
  {#if loading}<p role="status">Loading workspace…</p>
  {:else if problem}<section class="recovery" role="alert"><h2>Workspace unavailable</h2><p>{problem}</p><p>Your saved collection records have not been changed.</p><button class="btn quiet" onclick={() => id ? attempt++ : installExample()}>Try again</button></section>
  {:else if session}
    <svelte:boundary>
      <WorkspaceRenderer {session} />
      {#snippet failed(error, reset)}<section class="recovery" role="alert"><h2>Interface unavailable</h2><p>This definition could not be rendered. Your records are safe.</p><!-- The boundary only exposes a plain recovery message. --><button class="btn quiet" onclick={() => { void error; reset(); attempt++; }}>Reload workspace</button></section>{/snippet}
    </svelte:boundary>
  {:else}<section class="example"><h2>A personal to-do workspace</h2><p>Save a ready-made checklist, editable table, progress display and priority chart. All views share one personal collection. No model is needed.</p><button class="btn primary" disabled={loading} onclick={installExample}>Save to-do example</button><p class="hint">Keep the resulting workspace URL to reopen it after restarting Werkbord.</p></section>{/if}
  {/if}
</div>

<style>
  .personal-page { width: 100%; max-width: 1120px; margin: 0 auto; padding: 24px; min-width: 0; }
  header { display: flex; flex-wrap: wrap; align-items: start; justify-content: space-between; gap: 12px 20px; margin-bottom: 20px; }
  .head-actions { display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: 8px; }
  h1 { margin: 0; font-size: 24px; font-weight: 600; overflow-wrap: anywhere; }
  header p, .hint { margin: 6px 0 0; color: var(--text-2); font-size: 12px; }
  h2 { margin: 0; font-size: 18px; }
  .example, .recovery { display: grid; justify-items: start; gap: 14px; max-width: 65ch; padding: 24px 0; }
  .example p, .recovery p { margin: 0; color: var(--text-2); line-height: 1.6; }
  .phone-top { margin: -6px 0 4px; }
  @media (max-width: 600px) { .personal-page { padding: 16px; } button { min-height: 44px; } header { display: grid; } .head-actions { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); justify-content: stretch; } .head-actions > :global(.primary), .head-actions > :global(section) { grid-column: 1 / -1; } .head-actions > :global(.btn) { width: 100%; justify-content: center; } }
</style>
