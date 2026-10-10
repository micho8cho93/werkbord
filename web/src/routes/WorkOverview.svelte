<script lang="ts">
  import { api } from '../lib/api';
  import LabelChip from '../lib/LabelChip.svelte';
  import { labelsOf } from '../lib/labels';
  import { projectHref, taskHref } from '../lib/router.svelte';
  import type { ProjectScope } from '../lib/scope.svelte';
  import { app } from '../lib/state.svelte';
  import { spanOf, spanTitle, daysBetween, localToday } from '../lib/timeline';
  import { TASK_STATES, TASK_STATE_LABELS, type Project, type TimelineWarning } from '../lib/types';

  // A work project at a glance: where its tasks stand, what is coming up, and whether the plan
  // contradicts itself. There is no repository, so there are no runs or Git here.

  let { scope, project }: { scope: ProjectScope; project: Project } = $props();

  const today = $derived(localToday(app.now));
  const counts = $derived(TASK_STATES.map((s) => ({ state: s, n: scope.activeTasks.filter((t) => t.state === s).length })));
  const upcoming = $derived(
    scope.activeTasks
      .filter((t) => t.state !== 'done')
      .flatMap((t) => {
        const s = spanOf(t);
        return s && daysBetween(today, s.end) >= 0 ? [{ t, s }] : [];
      })
      .sort((a, b) => daysBetween(b.s.start, a.s.start) || a.t.position - b.t.position)
      .slice(0, 6),
  );
  const underway = $derived(scope.activeTasks.filter((t) => t.state === 'doing' || t.state === 'review').slice(0, 6));

  let warnings = $state<TimelineWarning[]>([]);
  const signature = $derived(scope.tasks.map((t) => `${t.id}:${t.version}`).join(','));
  $effect(() => {
    void signature;
    let live = true;
    api.timeline(project.id).then(
      (w) => live && (warnings = w),
      () => {},
    );
    return () => (live = false);
  });
</script>

<div class="page">
  <section class="pn">
    <div class="ph">Board<a class="end small" href={projectHref(project.id, 'board')}>Open →</a></div>
    <ul class="counts">
      {#each counts as c (c.state)}<li><strong>{c.n}</strong><span>{TASK_STATE_LABELS[c.state]}</span></li>{/each}
    </ul>
  </section>

  <section class="pn">
    <div class="ph">Coming up<a class="end small" href={projectHref(project.id, 'timeline')}>Timeline →</a></div>
    {#if upcoming.length}
      <ul class="list">
        {#each upcoming as { t, s } (t.id)}
          <li>
            <a href={taskHref(project.id, t.id)}>{t.title}</a>
            {#each labelsOf(t, app.labels) as l (l.id)}<LabelChip label={l} small />{/each}
            <span class="mm end">{s.milestone ? 'Milestone · ' : ''}{spanTitle(s, Number(today.slice(0, 4)))}</span>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="muted small">Nothing is planned. Give a task dates to see it here and on the timeline.</p>
    {/if}
  </section>

  <section class="pn">
    <div class="ph">Under way<span class="chip">{underway.length}</span></div>
    {#if underway.length}
      <ul class="list">
        {#each underway as t (t.id)}
          <li><a href={taskHref(project.id, t.id)}>{t.title}</a><span class="mm end">{TASK_STATE_LABELS[t.state]}</span></li>
        {/each}
      </ul>
    {:else}
      <p class="muted small">Nothing is under way.</p>
    {/if}
  </section>

  <section class="pn">
    <div class="ph">Plan check<span class="chip">{warnings.length}</span></div>
    {#if warnings.length}
      <p class="small">{warnings.length} {warnings.length === 1 ? 'thing' : 'things'} in the plan {warnings.length === 1 ? 'needs' : 'need'} a look. <a href={projectHref(project.id, 'timeline')}>See the timeline →</a></p>
    {:else}
      <p class="muted small">Dependencies and dates agree with each other.</p>
    {/if}
  </section>
</div>

<style>
  .page {
    display: grid;
    gap: 16px;
    padding: 20px 24px 32px;
    max-width: 960px;
  }

  .counts {
    display: flex;
    flex-wrap: wrap;
    gap: 24px;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .counts li {
    display: grid;
  }

  .counts strong {
    font-size: 24px;
    font-weight: 600;
  }

  .counts span {
    font-size: 12px;
    color: var(--text-2);
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .list li {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 38px;
    border-top: 1px solid var(--border);
    font-size: 13px;
  }

  .list li:first-child {
    border-top: 0;
  }

  .list a {
    color: var(--text);
    text-decoration: none;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .list a:hover {
    text-decoration: underline;
  }

  .small {
    font-size: 13px;
  }

  @media (max-width: 899px) {
    .page {
      padding: 16px;
    }
  }
</style>
