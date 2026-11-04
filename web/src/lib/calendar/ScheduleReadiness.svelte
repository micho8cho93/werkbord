<script lang="ts">
  import { schedulingLabels } from '../scheduling';
  import { formatDateTime } from '../clock.svelte';
  import type { Overview } from '../../shell/types';
  let { overview, open }: { overview: Overview | null; open: (workspace: string, href: string) => void } = $props();
  const order: Record<string, number> = { runnable: 0, queued: 1, waiting_capacity: 2, waiting_usage: 2, waiting_runner: 2, waiting_retry: 2, waiting_dependency: 3, waiting_schedule: 4, potentially_conflicting: 5, blocked: 6 };
  const rows = $derived((overview?.entries ?? []).filter(e => !e.error && e.workspace.state === 'ready').flatMap(e => (e.summary?.readiness ?? e.summary?.schedule ?? []).map(item => ({ workspace: e.workspace.id, name: e.workspace.name, item }))).sort((a, b) => (order[a.item.state] ?? 7) - (order[b.item.state] ?? 7) || (a.item.at ?? '').localeCompare(b.item.at ?? '')));
</script>
{#if rows.length}
<section class="readiness" aria-labelledby="readiness-h">
  <h2 id="readiness-h" class="section-title">Agent schedules</h2>
  <p class="muted">Why work can start or is waiting. Planned work dates above do not start agents.</p>
  <ul>{#each rows as r (r.workspace + r.item.id)}<li>
    <button class="task" onclick={() => open(r.workspace, r.item.href)}>{r.item.title}</button>
    <span class="muted">{r.item.project}{r.item.at ? ' · ' + formatDateTime(r.item.at) : ''}</span>
    <strong>{(schedulingLabels as Record<string, string>)[r.item.state] || r.item.state.replaceAll('_', ' ')}</strong>
    {#if r.item.reason}<p>{r.item.reason}</p>{/if}
  </li>{/each}</ul>
</section>
{/if}
<style>
  .readiness { flex: none; margin: 16px 24px; } h2 { margin: 0; } p { margin: 6px 0; font-size: 13px; overflow-wrap: anywhere; }
  .muted { color: var(--text-2); font-size: 12px; } ul { list-style: none; padding: 0; margin: 12px 0 0; }
  li { display: grid; gap: 4px; padding: 12px 0; border-top: 1px solid var(--border); }
  .task { justify-self: start; border: 0; padding: 0; background: none; color: var(--text); text-align: left; font: inherit; font-weight: 500; cursor: pointer; }
  .task:hover { color: var(--accent); } .task:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
  strong { font-size: 12px; font-weight: 500; }
</style>
