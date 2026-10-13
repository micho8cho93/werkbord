<script lang="ts">
  import { formatTime } from '../lib/clock.svelte';
  import PlanningCalendar from '../lib/PlanningCalendar.svelte';
  import { standing } from './aggregate';
  import { model } from './model.svelte';
  import WorkspaceTag from './WorkspaceTag.svelte';
  import type { Overview } from './types';
  let { overview }: { overview: Overview | null } = $props();
  let timeline = $state(false);
  const rows = $derived((overview?.entries ?? []).flatMap(e => [
    ...(e.summary?.planned ?? []).map(p => ({ ...p, id: e.workspace.id + '/' + p.id + '/plan', project: e.workspace.name + ' · ' + p.project, workspace: e.workspace.id })),
    ...(e.summary?.schedule ?? []).map(p => { const date = new Date(p.at); const day = `${date.getFullYear()}-${String(date.getMonth()+1).padStart(2,'0')}-${String(date.getDate()).padStart(2,'0')}`; return { ...p, id: e.workspace.id + '/' + p.id + '/schedule', start: day, end: day, project: e.workspace.name + ' · ' + p.project, detail: formatTime(p.at) + ' · ' + p.state.replaceAll('_', ' '), workspace: e.workspace.id }; })
  ]));
  const silent = $derived(overview ? overview.entries.filter(e => !e.summary && (e.error || e.workspace.state !== 'ready')) : []);
  function open(id: string) { const row = rows.find(r => r.id === id); if (row) void model.open(row.workspace, row.href); }
</script>
<section class="page" aria-labelledby="cal-h">
  <header><h1 id="cal-h">Calendar</h1><p class="lede">Scheduled and planned work across all projects</p>
  <div class="seg" role="group" aria-label="Calendar presentation"><button aria-pressed={!timeline} onclick={() => timeline = false}>Calendar</button><button aria-pressed={timeline} onclick={() => timeline = true}>Timeline</button></div></header>
  {#if !overview}<p class="empty">Loading schedules…</p>{:else}<PlanningCalendar {rows} {timeline} onopen={open} />{/if}
  {#each silent as e (e.workspace.id)}<p class="note"><WorkspaceTag workspace={e.workspace} /> {standing(e)}: its schedule cannot be shown now.</p>{/each}
</section>
<style>
  .page { padding: 24px; height: 100%; overflow: auto; }
  h1 { margin: 0; } .lede, .note, .empty { color: var(--text-2); } .lede { margin: 4px 0 16px; }
  header { margin-bottom: 12px; }
  @media (max-width: 720px) { .page { padding: 16px; } }
</style>
