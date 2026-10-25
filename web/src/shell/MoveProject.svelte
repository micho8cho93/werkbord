<script lang="ts">
  import Sheet from '../lib/Sheet.svelte';
  import { ask } from './native';
  import { model } from './model.svelte';
  import type { ProjectMovePreview, ProjectMoveResult } from './types';

  let { source, project, name, onclose }: { source: string; project: string; name: string; onclose: () => void } = $props();
  const sourceKind = $derived(model.view?.items.find(i => i.id === source)?.kind);
  const destinations = $derived((model.view?.items ?? []).filter(i => i.state === 'ready' && i.kind !== sourceKind && (i.kind === 'personal' || sourceKind === 'personal')));
  let destination = $state('');
  let preview = $state<ProjectMovePreview | null>(null);
  let result = $state<ProjectMoveResult | null>(null);
  let busy = $state(false);
  let problem = $state('');
  let reviewed = $state(false);
  const close = () => { if (!busy) onclose(); };

  async function review(e: SubmitEvent) {
    e.preventDefault(); busy = true; problem = '';
    try { preview = await ask(a => a.PreviewProjectMove(source, destination, project)); }
    catch (e) { problem = (e as Error).message; }
    finally { busy = false; }
  }
  async function move() {
    if (!preview || !reviewed) return;
    busy = true; problem = '';
    try {
      const r = await ask(a => a.ConfirmProjectMove(preview!.token));
      if (!r.cancelled) { result = r; await model.refreshOverview(); }
    } catch (e) { problem = (e as Error).message; }
    finally { busy = false; }
  }
</script>

<Sheet title="Move project" onclose={close} width="40rem" layer={70}>
  <div class="move" data-testid="move-project-dialog" aria-busy={busy}>
    {#if result}
      <h3>{result.archived ? 'Project moved' : 'Destination saved'}</h3>
      <p>{name} is saved in {preview?.destination}.</p>
      {#if result.warning}<p role="alert">{result.warning}</p>{:else}<p>The original is archived with its history. You can restore it there.</p>{/if}
      <button class="btn primary" onclick={() => { model.goTo(destination, destination === 'personal' ? `#/p/${result!.id}/board` : `?tab=board&project=${result!.id}`); close(); }}>Open destination project</button>
    {:else if preview}
      <p><strong>{preview.source} → {preview.destination}</strong></p>
      <h3>Review the text that will be copied</h3>
      <dl><dt>Project name</dt><dd>{preview.snapshot.name}</dd><dt>Repository reference</dt><dd>{preview.snapshot.repository || 'None'}</dd></dl>
      <p>{preview.snapshot.tickets.length} task or ticket texts. Each starts in Backlog / Available as human work.</p>
      {#each preview.snapshot.tickets as ticket, index (index)}
        <section class="ticket" aria-label="Ticket {index + 1}"><h4>{ticket.title}</h4><p class="text">{ticket.description || 'No description'}</p></section>
      {/each}
      <p class="boundary">Only this text is copied. Runs, history, files, agent output, comments, labels, dates, assignments and schedules stay at the source. The destination has no local checkout. The original is archived once the destination is saved.</p>
      <label class="check"><input type="checkbox" bind:checked={reviewed} disabled={busy} />I reviewed this text and want to share it with {preview.destination}.</label>
      <div class="actions"><button class="btn" disabled={busy} onclick={() => { preview = null; reviewed = false; problem = ''; }}>Back</button><button class="btn primary" disabled={!reviewed || busy} onclick={move}>{busy ? 'Moving…' : 'Move project…'}</button></div>
    {:else}
      <form onsubmit={review}>
        <p>Move <strong>{name}</strong> from {sourceKind === 'personal' ? 'Personal' : model.view?.items.find(i => i.id === source)?.name}.</p>
        <label for="move-destination">Destination</label>
        <select id="move-destination" bind:value={destination} disabled={busy} required data-autofocus><option value="" disabled>Choose a destination</option>{#each destinations as d (d.id)}<option value={d.id}>{d.kind === 'personal' ? 'Personal' : d.name}</option>{/each}</select>
        {#if !destinations.length}<p>Connect {sourceKind === 'personal' ? 'a team' : 'Personal'} before moving this project.</p>{/if}
        <p>You will review exactly which text is copied before anything moves. The original stays recoverable with its history.</p>
        <button class="btn primary" disabled={!destination || busy}>{busy ? 'Reading project…' : 'Review project text'}</button>
      </form>
    {/if}
    {#if problem}<p role="alert" class="error">{problem}</p>{/if}
  </div>
</Sheet>

<style>
  .move, form { display: flex; flex-direction: column; gap: 16px; }
  p, h3, h4, dl { margin: 0; }
  h3 { font-size: 16px; }
  h4 { font-size: 14px; }
  p { line-height: 1.55; }
  select { width: 100%; padding: 8px; background: var(--surface); color: var(--text); border: 1px solid var(--border); border-radius: var(--radius); font: inherit; }
  dt { color: var(--text-2); font-size: 12px; margin-bottom: 4px; }
  dd { margin: 0 0 12px; overflow-wrap: anywhere; }
  .ticket { border-top: 1px solid var(--border); padding-top: 12px; }
  .text { white-space: pre-wrap; overflow-wrap: anywhere; margin-top: 8px; }
  .boundary { color: var(--text-2); border-top: 1px solid var(--border); padding-top: 16px; }
  .check { display: flex; gap: 10px; align-items: flex-start; line-height: 1.5; }
  .check input { flex: none; margin-top: 4px; accent-color: var(--accent); }
  .actions { display: flex; justify-content: flex-end; gap: 8px; }
  .error { color: var(--bad); }
  select:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
</style>
