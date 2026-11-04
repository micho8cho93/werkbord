<script lang="ts">
  import WorkOverview from './WorkOverview.svelte';
  import BriefCard from './BriefCard.svelte';
  import Attention from './Attention.svelte';
  import { PERSONAL, workSummary } from './summary.svelte';
  import { api } from '../api';
  import { app } from '../state.svelte';
  import { taskHref } from '../location';
  import { current, pending } from '../../shell/decisions';
  import type { Decision } from '../../shell/decisions';
  import { participants } from '../../shell/context-header';
  import { readyCount, rowDetail, rowsOf, urgentFirst } from '../../shell/workqueue';
  import { scopedOverview } from '../../shell/project-context';
  import { reachable } from '../../shell/answering-reach';
  import type { Question } from '../types';

  // The Work page's Overview in the web app: the shared one, fed from the workspace summary and told what pressing a row means here
  // (open the task on its own page). It shows the whole workspace, or one project when the page is a project's.
  let { projectId = '', search = '', toQueue }: { projectId?: string; search?: string; toQueue: () => void } = $props();

  let now = $state(Date.now());
  $effect(() => { const timer = setInterval(() => (now = Date.now()), 1000); return () => clearInterval(timer); });
  // Read again when something happens and now and then: the page is a summary of what agents are doing.
  $effect(() => { void app.overview; void workSummary.refresh(); });
  $effect(() => { const timer = setInterval(() => void workSummary.refresh(), 15_000); return () => clearInterval(timer); });

  const selected = $derived(projectId ? { workspace: PERSONAL.id, project: projectId } : null);
  const overview = $derived(scopedOverview(workSummary.overview, [PERSONAL], selected));
  const rows = $derived((overview?.entries.flatMap((e) => (e.summary?.queue ?? []).map((item) => ({ workspace: e.workspace, item }))) ?? []).filter((r) => `${r.item.title} ${r.item.project} ${r.item.detail ?? ''}`.toLowerCase().includes(search.toLowerCase())));
  const queueRows = $derived(rowsOf(overview, rows.filter((r) => r.item.group !== 'done' && r.item.group !== 'plan').map((r) => ({ workspace: r.workspace.id, item: r.item }))));
  const running = $derived(queueRows.filter((r) => r.state === 'running').slice(0, 5));
  const upNext = $derived(urgentFirst(queueRows.filter((r) => r.item.group === 'next')).slice(0, 5));
  const plans = $derived(rows.filter((r) => r.item.group === 'plan'));
  const waiting = $derived(pending(workSummary.decisions.list, selected, new Set(Object.keys(workSummary.decisions.answered))));

  // A task opens on its own page; the summary's address is the workspace's own (#/p/<project>/task/<task>).
  const open = (_workspace: string, href: string) => { location.hash = href; };
  const decide = {
    askable: (d: Decision) => !!d.question && reachable(workSummary.overview, now, workSummary.readAt),
    fresh: (d: Decision) => current(d, workSummary.overview),
    openable: (d: Decision) => !!d.runId || !!d.href,
    answerWith: (q: Question, answer: string) => api.answerQuestion(q, answer),
    onSettled: (q: Question) => { workSummary.decisions.settle('personal|question-' + q.id); void workSummary.refresh(); },
    notify: (message: string) => app.notify(message),
    open: (d: Decision) => { if (d.href) open('personal', d.href); else if (d.question) location.hash = taskHref(d.question.projectId, d.question.taskId, d.runId ?? ''); },
  };
</script>

{#if !overview}
  <p class="status" role="status">Loading work…</p>
{:else}
  <WorkOverview {waiting} {decide} {running} {upNext} ready={readyCount(queueRows)} {plans}
    plansEmpty={search ? 'No plans match your search.' : 'Create a plan to group related tasks. Individual tasks can stand on their own.'}
    people={participants(overview)} showWhere={false} {now} whereOf={() => undefined} detail={(w, item) => rowDetail(overview, w, item)}
    isOpen={() => false} onopen={open} seeAll={toQueue}>
    {#snippet attention()}<Attention {overview} answerWith={(q, answer) => decide.answerWith(q, answer)} onSettled={decide.onSettled} open={(workspace, href) => { if (href) open(workspace, href); }} notify={decide.notify} />{/snippet}
    {#snippet brief()}
      <BriefCard scope={projectId ? (app.project(projectId)?.name ?? 'this project') : 'all projects'}
        projectContext={projectId ? { workspace: 'personal', project: projectId, label: app.project(projectId)?.name ?? '' } : null}
        frame={null} inline hidden={app.remote} />
    {/snippet}
  </WorkOverview>
{/if}
