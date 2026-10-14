<script lang="ts">
  import Sheet from '../lib/Sheet.svelte';
  import { planningCall, type PlanningItem } from './planning';
  import { instantFromWall, wallTime } from '../lib/scheduling';
  import type { DateDrop } from '../lib/planningGestures';
  import { formatTime } from '../lib/clock.svelte';
  import PlanningCalendar from '../lib/PlanningCalendar.svelte';
  import { standing } from './aggregate';
  import { model } from './model.svelte';
  import WorkspaceTag from './WorkspaceTag.svelte';
  import type { Overview } from './types';
  let { overview }: { overview: Overview | null } = $props();
  let timeline = $state(false);
  const rows = $derived((overview?.entries ?? []).flatMap(e => [
    ...(e.summary?.planned ?? []).map(p => ({ ...p, id: e.workspace.id + '/' + p.id + '/plan', project: e.workspace.name + ' · ' + p.project, workspace: e.workspace.id, kind: 'planned' as const, status: e.summary?.work.find(w => w.id === p.id)?.status })),
    ...(e.summary?.schedule ?? []).map(p => { const date = new Date(p.at); const day = `${date.getFullYear()}-${String(date.getMonth()+1).padStart(2,'0')}-${String(date.getDate()).padStart(2,'0')}`; return { ...p, id: e.workspace.id + '/' + p.id + '/schedule', start: day, end: day, project: e.workspace.name + ' · ' + p.project, detail: formatTime(p.at) + ' · ' + p.state.replaceAll('_', ' '), workspace: e.workspace.id, kind: 'scheduled' as const }; })
  ]));
  const silent = $derived(overview ? overview.entries.filter(e => !e.summary && (e.error || e.workspace.state !== 'ready')) : []);
  let editing = $state<string>('');
  let draft = $state<PlanningItem | null>(null);
  let original = $state<PlanningItem | null>(null);
  let error = $state('');
  let busy = $state(false);
  let at = $state('');
  const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const row = $derived(rows.find(r => r.id === editing));
  async function open(id: string) {
    const r = rows.find(r => r.id === id); if (!r) return;
    editing = id; draft = null; error = ''; busy = true;
    try { const item = await planningCall(r.workspace, r.href, 'read'); if (editing === id) { draft = item; original = { ...item }; at = item.scheduledAt ? wallTime(item.scheduledAt, zone) : ''; } }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = false; }
  }
  async function move(drop: DateDrop) {
    const r = rows.find(r => r.id === drop.id); if (!r) return;
    const item = await planningCall(r.workspace, r.href, 'read');
    if (r.kind === 'planned' && (item.start !== r.start || item.end !== r.end)) { await model.refreshOverview(); throw new Error('This plan changed elsewhere. The calendar has been refreshed.'); }
    if (r.kind === 'scheduled' && Date.parse(item.scheduledAt) !== Date.parse(r.at)) { await model.refreshOverview(); throw new Error('This schedule changed elsewhere. The calendar has been refreshed.'); }
    await planningCall(r.workspace, r.href, r.kind === 'scheduled' ? 'reschedule' : 'move', { ...drop, version: item.version, scheduleVersion: item.scheduleVersion });
    await model.refreshOverview();
  }
  async function save() {
    if (!row || !draft || busy) return;
    busy = true; error = '';
    let detailsSaved = false;
    try {
      // Save words and planned dates together. Scheduled events only reschedule their start.
      if (row.kind === 'scheduled') {
        if (draft.title !== original?.title || draft.description !== original?.description) {
          const item = await planningCall(row.workspace, row.href, 'save', { ...draft });
          draft = item; original = { ...item }; detailsSaved = true;
        }
        if (at !== wallTime(draft.scheduledAt, zone)) await planningCall(row.workspace, row.href, 'reschedule', { ...draft, at: instantFromWall(at, zone) });
      } else await planningCall(row.workspace, row.href, 'save', { ...draft });
      await model.refreshOverview(); editing = ''; draft = null;
    } catch (e) { error = (detailsSaved ? 'Task details were saved; the schedule could not be confirmed. ' : '') + (e instanceof Error ? e.message : String(e)); }
    finally { busy = false; }
  }
</script>
<section class="page" aria-labelledby="cal-h">
  <header><h1 id="cal-h">Calendar</h1><p class="lede">Scheduled and planned work across all projects</p>
  <div class="seg" role="group" aria-label="Calendar presentation"><button aria-pressed={!timeline} onclick={() => timeline = false}>Calendar</button><button aria-pressed={timeline} onclick={() => timeline = true}>Timeline</button></div></header>
  {#if !overview}<p class="empty">Loading schedules…</p>{:else}<PlanningCalendar {rows} {timeline} onopen={id => void open(id)} onmove={move} />{/if}
  {#each silent as e (e.workspace.id)}<p class="note"><WorkspaceTag workspace={e.workspace} /> {standing(e)}: its schedule cannot be shown now.</p>{/each}
</section>
{#if editing}
  <Sheet side title="Edit task" onclose={() => { editing = ''; draft = null; }} width="38rem">
    {#if busy && !draft}<p role="status">Loading task…</p>{/if}
    {#if draft}<form id="universal-task-edit" class="edit" onsubmit={e => { e.preventDefault(); void save(); }}>
      <p class="context">{row?.project}</p>
      <label class="field">Title<input class="input" bind:value={draft.title} required maxlength="200" disabled={!draft.editable} /></label>
      <label class="field">Details<textarea class="input" rows="5" bind:value={draft.description} disabled={!draft.editable}></textarea></label>
      {#if row?.kind === 'scheduled'}
        <label class="field">Agent start time ({zone})<input class="input" type="datetime-local" bind:value={at} required disabled={!draft.canReschedule} /></label>
        <p class="context">This reschedules the agent start. Planned work dates stay separate.</p>
      {:else}
        <label class="field">Start date<input class="input" type="date" bind:value={draft.start} onchange={() => { if (draft?.milestone) draft.end = draft.start; }} required disabled={!draft.editable} /></label>
        <label class="field">End date<input class="input" type="date" bind:value={draft.end} min={draft.start} required disabled={!draft.editable || draft.milestone} /></label>
        <label><input type="checkbox" bind:checked={draft.milestone} onchange={() => { if (draft?.milestone) draft.end = draft.start; }} disabled={!draft.editable} /> Milestone</label>
        <p class="context">Planned dates do not start an agent.</p>
      {/if}
      {#if !draft.editable}<p class="context">You do not have permission to edit this task.</p>{/if}
    </form>{/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    {#snippet footer()}<button class="btn" onclick={() => editing = ''}>Close</button>{#if draft}<button class="btn primary" type="submit" form="universal-task-edit" disabled={busy || !(draft.editable || row?.kind === 'scheduled' && draft.canReschedule)}>{busy ? 'Saving…' : 'Save changes'}</button>{/if}{/snippet}
  </Sheet>
{/if}
<style>
  .edit { display: grid; gap: 16px; } .context { color: var(--text-2); font-size: 13px; }
  .page { padding: 24px; height: 100%; min-height: 0; overflow: auto; display: flex; flex-direction: column; }
  h1 { margin: 0; } .lede, .note, .empty { color: var(--text-2); } .lede { margin: 4px 0 16px; }
  header { margin-bottom: 12px; }
  @media (max-width: 720px) { .page { padding: 16px; } }
</style>
