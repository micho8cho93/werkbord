<script lang="ts">
  import Icon from '../Icon.svelte';
  import { embedded } from '../embed';
  import DeletionPreview from '../personal/DeletionPreview.svelte';
  import WorkspaceProposalPreview from '../personal/WorkspaceProposalPreview.svelte';
  import { assistant } from './store.svelte';
  import type { ConfirmItem } from './transcript';
  import { clock, operationTitle } from './words';
  import { taskHref } from '../router.svelte';
  import { acknowledge, surface } from '../motion';

  // A change the assistant prepared. Nothing has happened: the words below were written by Werkbord from the exact arguments
  // that will be used, not by the model, and only the button here carries the change out.
  let { item, motion = false }: { item: ConfirmItem; motion?: boolean } = $props();
  let workspaceReady = $state(false);
  const deletionArgs = $derived(item.operation === 'propose_delete_personal_workspace' ? item.args : undefined);
  let deletionReady = $state(false);
  const personalArgs = $derived(item.operation === 'propose_personal_workspace' ? item.args : undefined);
  const until = $derived(Date.parse(item.expiresAt));
  const taskArgs = $derived(item.operation === 'create_ticket' ? item.args : undefined);
  const taskTitle = $derived(typeof taskArgs?.title === 'string' ? taskArgs.title : 'New task');
  const taskDescription = $derived(typeof taskArgs?.description === 'string' ? taskArgs.description : '');
  function connectedDestination() {
    if (!item.ticket) return null;
    const project = /^workspace:([^/]{1,40})\/([^/]{1,120})$/.exec(item.ticket.projectId);
    const ticket = /^workspace:([^/]{1,40})\/([^/]{1,120})$/.exec(item.ticket.id);
    if (!project || !ticket || project[1] !== ticket[1] || !project[1].startsWith('team:')) return null;
    return { workspace: project[1], place: `?tab=board&project=${encodeURIComponent(project[2])}&ticket=${encodeURIComponent(ticket[2])}` };
  }
  const savedWorkspaceID = $derived(item.operation === 'propose_personal_workspace' && item.state === 'confirmed' ? /\bpws_[a-z2-7]{16}\b/.exec(item.outcome ?? '')?.[0] : undefined);
  const savedWorkspaceRevision = $derived(/\brevision ([1-9][0-9]*)\./.exec(item.outcome ?? '')?.[1]);
  const savedWorkspacePlace = $derived(savedWorkspaceID ? `#/personal?id=${savedWorkspaceID}${savedWorkspaceRevision ? `&revision=${savedWorkspaceRevision}` : ''}` : '');
  const connected = $derived(connectedDestination());
  function openSavedWorkspace(event: MouseEvent) {
    if (!savedWorkspaceID || !embedded()) return;
    event.preventDefault();
    window.parent.postMessage({ type: 'werkbord.frame', event: 'open', target: 'personal', place: savedWorkspacePlace }, '*');
  }
  function openConnectedTask() {
    if (!connected) return;
    window.parent.postMessage({ type: 'werkbord.frame', event: 'open', target: connected.workspace, place: connected.place }, '*');
  }
</script>

<section class="card confirm" data-state={item.state} use:acknowledge={{ value: item.state, success: item.state === 'confirmed' }} in:surface={{ duration: motion ? 180 : 0 }} aria-label="{operationTitle(item.operation)}: {item.state === 'waiting' ? 'waiting for you' : item.state}">
  <div class="lab">{operationTitle(item.operation)} <span class="status"><span class="status-dot" aria-hidden="true"></span>{item.state === 'waiting' ? 'Needs you' : item.state === 'working' ? 'Working' : item.state === 'confirmed' ? 'Done' : item.state === 'declined' ? 'Declined' : item.state === 'expired' ? 'Expired' : item.state === 'failed' ? 'Failed' : 'Closed'}</span></div>
  {#if taskArgs}
    <h3 class="task-title">{taskTitle}</h3>
    <p class="destination">{item.summary}</p>
    {#if taskDescription}<div class="preview-detail"><span>Details</span><p>{taskDescription}</p></div>{/if}
  {:else}
    <p class="sum">{item.summary}</p>
  {/if}

  {#if deletionArgs && (item.state === 'waiting' || item.state === 'working')}<DeletionPreview args={deletionArgs} onready={ready => deletionReady = ready} />{/if}
  {#if personalArgs && (item.state === 'waiting' || item.state === 'working')}<WorkspaceProposalPreview args={personalArgs} onready={(ready) => workspaceReady = ready} />{/if}

  {#if item.state === 'waiting' || item.state === 'working'}
    <div class="acts">
      <button class="btn primary" type="button" disabled={item.state === 'working' || assistant.batch !== null || (personalArgs !== undefined && !workspaceReady) || (deletionArgs !== undefined && !deletionReady)} onclick={() => assistant.resolve(item, 'approve')}>
        {item.state === 'working' ? (item.operation === 'create_ticket' ? 'Adding…' : 'Saving…') : 'Confirm'}
      </button>
      <button class="btn" type="button" disabled={item.state === 'working' || assistant.batch !== null} onclick={() => assistant.resolve(item, 'decline')}>Not now</button>
    </div>
    {#if item.state === 'waiting'}<p class="fine">Nothing changes until you confirm.{Number.isFinite(until) ? ` Open until ${clock(until)}.` : ''}</p>{:else}<p class="fine">{item.operation === 'create_ticket' ? 'Adding the approved task to Backlog…' : 'Saving the approved change…'}</p>{/if}
  {:else if item.state === 'confirmed'}
    <p class="result ok"><Icon name="check" size={14} /> {item.operation === 'create_ticket' ? 'Added to Backlog.' : 'Done.'}{item.ticket ? ` ${item.ticket.title}` : item.outcome ? ` ${item.outcome}` : ''}</p>
    {#if savedWorkspaceID}<a class="task-link" href={savedWorkspacePlace} onclick={openSavedWorkspace}>Open saved workspace</a>{/if}
    {#if item.ticket}
      {#if connected}<button class="task-link" type="button" onclick={openConnectedTask}>Open task</button>
      {:else}<a class="task-link" href={taskHref(item.ticket.projectId, item.ticket.id)}>Open task</a>{/if}
    {/if}
  {:else if item.state === 'declined'}
    <p class="result">Not done. You said no.</p>
  {:else if item.state === 'expired'}
    <p class="result">Not confirmed in time, so nothing changed. Ask again if you still want it.</p>
  {:else if item.state === 'failed'}
    <p class="result bad" role="alert">This could not be done.{item.outcome ? ` ${item.outcome}` : ''} Nothing was changed by the attempt unless it says otherwise.</p>
  {:else if item.state === 'uncertain'}
    <p class="result bad" role="alert">{item.outcome ?? 'The result is uncertain. Check the board before trying again.'}</p>
  {:else}
    <p class="result">No longer waiting. It was answered, or it timed out, while you were away.</p>
  {/if}
</section>

<style>
  .lab { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 16px; }
  .status { display: inline-flex; align-items: center; gap: 6px; color: var(--status-word, var(--text-2)); }
  .status-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--status-dot, var(--text-2)); }
  .confirm {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 14px 16px;
  }
  .confirm[data-state='waiting'],
  .confirm[data-state='working'] {
    --status-dot: var(--amber);
    --status-word: var(--warn-text);
  }
  .confirm[data-state='confirmed'] {
    --status-dot: var(--ok);
    --status-word: var(--ok-text);
  }
  .confirm[data-state='failed'] {
    --status-dot: var(--danger);
    --status-word: var(--danger-text);
  }
  .sum {
    font-size: 14px;
    overflow-wrap: anywhere;
  }
  .task-title { font-size: 15px; font-weight: 650; overflow-wrap: anywhere; }
  .destination { color: var(--text-2); font-size: 12.5px; }
  .preview-detail { display: grid; gap: 3px; }
  .preview-detail > span { color: var(--text-2); font-size: 11px; font-weight: 600; letter-spacing: .02em; }
  .preview-detail > p { font-size: 13px; overflow-wrap: anywhere; white-space: pre-wrap; }
  .acts {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    padding-top: 2px;
  }
  .fine,
  .result {
    font-size: 12.5px;
    color: var(--text-2);
  }
  .result {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .result.ok {
    color: var(--ok-text);
  }
  .result.bad {
    color: var(--danger-text);
    align-items: flex-start;
  }
  .task-link { width: fit-content; padding: 0; border: 0; background: none; font: inherit; font-size: 12.5px; color: var(--accent); text-decoration: underline; text-underline-offset: 3px; cursor: pointer; }
  .task-link:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; border-radius: 2px; }
</style>
