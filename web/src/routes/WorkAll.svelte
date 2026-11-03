<script lang="ts">
  import Icon from '../lib/Icon.svelte';
  import WebOverview from '../lib/work/WebOverview.svelte';
  import WorkRow from '../lib/work/WorkRow.svelte';
  import ControlCenter from './ControlCenter.svelte';
  import { workSummary } from '../lib/work/summary.svelte';
  import { ApiError, api } from '../lib/api';
  import { blockingTasks, workGroup } from '../lib/work-queue';
  import { orderDrops, type OrderDrop } from '../lib/orderGestures';
  import { globalHref, projectHref } from '../lib/location';
  import { router } from '../lib/router.svelte';
  import { app } from '../lib/state.svelte';
  import { readyCount, rowDetail, rowsOf, urgentFirst } from '../shell/workqueue';
  import type { Row } from '../shell/workqueue';

  // Work for every project: the same page a project has (Overview, and the queue with its plans), over all of them. The Overview is the
  // one the Mac window shows; the queue is the summary's rows in Now, Next and Later, each of which can be dragged to another.
  const section = $derived(new URLSearchParams(router.query).get('section'));
  const queueTab = $derived(section === 'queue');
  // The page this one replaced (its filters, browser alerts and the Team tickets waiting for a repository) stays one tab away until each has a place.
  const feedTab = $derived(section === 'feed');
  let search = $state('');
  let now = $state(Date.now());
  $effect(() => { const timer = setInterval(() => (now = Date.now()), 30_000); return () => clearInterval(timer); });
  $effect(() => { void app.overview; void workSummary.refresh(); });

  const overview = $derived(workSummary.overview);
  const all = $derived((overview?.entries ?? []).flatMap((e) => (e.summary?.queue ?? []).map((item) => ({ workspace: e.workspace, item }))));
  const rows = $derived(all.filter((r) => `${r.item.title} ${r.item.project} ${r.item.detail ?? ''}`.toLowerCase().includes(search.toLowerCase())));
  const queueRows = $derived(rowsOf(overview, rows.filter((r) => !['done', 'plan'].includes(r.item.group)).map((r) => ({ workspace: r.workspace.id, item: r.item }))));
  const GROUPS = [{ id: 'now', title: 'Now' }, { id: 'next', title: 'Next' }, { id: 'later', title: 'Later' }] as const;
  const sections = $derived(GROUPS.map((g) => ({ ...g, rows: g.id === 'later' ? queueRows.filter((r) => r.item.group === g.id) : urgentFirst(queueRows.filter((r) => r.item.group === g.id)) })));
  const plans = $derived(rows.filter((r) => r.item.group === 'plan'));
  const queueCount = $derived(all.filter((r) => !['done', 'plan'].includes(r.item.group)).length);
  const note = (id: string, list: Row[]) => id === 'now' ? `${list.filter((r) => r.asking || r.flag).length} need action` : id === 'next' ? `${readyCount(list)} ready` : '';
  const open = (_workspace: string, href: string) => { location.hash = href; };

  // ---- a row dragged to another of Now, Next and Later: the task's own priority, saved in the task's own project ----
  // Order inside a section is worked out from urgency across projects, so it is not stored here; a project's own page keeps it.
  const SECTION_PRIORITY = { now: 'high', next: 'normal', later: 'low' } as const;
  const SECTION_NAME = { now: 'Now', next: 'Next', later: 'Later' } as const;
  async function reorder(drop: OrderDrop) {
    const to = drop.toScope?.split(':')[1] as keyof typeof SECTION_PRIORITY | undefined;
    if (!to || !(to in SECTION_PRIORITY) || drop.toScope === drop.scope) {
      app.notify('Across projects the order follows urgency. Open a project to put its tasks in your own order, or drag a task to another of Now, Next and Later.');
      return;
    }
    const row = queueRows.find((r) => r.item.id === drop.id);
    const match = row && /^#\/p\/([^/?#]+)\/task\/([^/?#]+)/.exec(row.item.href);
    if (!row || !match) return;
    const projectId = decodeURIComponent(match[1]), taskId = decodeURIComponent(match[2]);
    try {
      const scope = app.enter(projectId);
      if (!scope) return;
      if (!scope.loaded) await scope.load();
      const task = scope.activeTasks.find((t) => t.id === taskId);
      if (!task) { app.notify('That task is not in the queue any more.'); return; }
      const priority = SECTION_PRIORITY[to];
      const lands = workGroup(task, scope.tasks, scope.latestRun[task.id], priority, app.now, scope.latestRun);
      if (lands !== to) {
        const waiting = blockingTasks(task, scope.tasks, scope.latestRun);
        app.notify(lands === 'now' ? `“${task.title}” stays in Now while an agent is working on it, it needs you, or it is in review.`
          : waiting.length ? `“${task.title}” waits for ${waiting.map((t) => `“${t.title}”`).join(', ')}, so it stays in Later until that is done.`
          : `“${task.title}” cannot move to ${SECTION_NAME[to]} right now.`);
        return;
      }
      scope.upsertTask(await api.editTask(task, { execution: { ...task.execution, priority } }));
      app.notify(`“${task.title}” moved to ${SECTION_NAME[to]}${priority === 'normal' ? '' : ` (${priority} priority)`}.`, 5000);
      void workSummary.refresh();
    } catch (err) {
      app.notify(err instanceof ApiError && err.code === 'conflict' ? 'This changed on another device. The queue has been refreshed.' : err instanceof Error ? err.message : String(err));
      void workSummary.refresh();
    }
  }
  const choose = () => { app.newTaskOpen = true; };
  const newPlan = () => { const id = app.lastProjectId || app.projects[0]?.id; if (id) location.hash = `${projectHref(id, 'work')}?new=plan`; };
</script>

<div class="work-view">
  <header class="work-heading">
    <div><h1>Work</h1><p class="muted">Your tasks, plans and agent activity.</p></div>
    <div class="work-actions">
      <label class="work-search"><Icon name="search" size={15} /><span class="visually-hidden">Search work</span><input type="search" bind:value={search} placeholder="Search work…" /></label>
      <a class="btn" href="{globalHref('projects')}?new"><Icon name="plus" size={15} />New project</a>
      <button class="btn" type="button" disabled={!app.projects.length} onclick={newPlan}>New plan</button>
      <button class="btn primary" type="button" disabled={!app.projects.length} onclick={choose}><Icon name="plus" size={15} />New task</button>
    </div>
  </header>
  <nav class="work-tabs" aria-label="Work sections">
    <a href={globalHref('control')} aria-current={!queueTab && !feedTab ? 'page' : undefined}>Overview</a>
    <a href="{globalHref('control')}?section=queue" aria-current={queueTab ? 'page' : undefined}>Queue &amp; Plans<span>{queueCount}</span></a>
    <a href="{globalHref('control')}?section=feed" aria-current={feedTab ? 'page' : undefined}>Feed{#if app.needsYou}<span>{app.needsYou}</span>{/if}</a>
  </nav>
  {#if feedTab}
    <ControlCenter />
  {:else if !queueTab}
    <WebOverview {search} toQueue={() => (location.hash = `${globalHref('control')}?section=queue`)} />
  {:else if !overview}
    <p class="empty" role="status">Loading work…</p>
  {:else}
    <div class="work-grid">
      <div class="work-main">
        <div class="queue-stack" use:orderDrops={reorder}>
          {#each sections as group (group.id)}
            <section class="queue-card" aria-labelledby="queue-{group.id}" data-section={group.id} data-order-group="queue" data-order-drop="tasks:{group.id}" data-order-name={group.title}>
              <header><h2 id="queue-{group.id}">{group.title}</h2>{#if group.id !== 'later'}<span class="queue-note">{note(group.id, group.rows)}</span>{/if}</header>
              {#if group.rows.length}<ul data-order-list data-order-group="queue" data-order-scope="tasks:{group.id}" aria-label="{group.title} tasks">{#each group.rows as row, i (row.workspace + row.item.href)}<WorkRow {row} index={i} sortable live={rowDetail(overview, row.workspace, row.item)} {now} onopen={open} />{/each}</ul>
              {:else}<p class="queue-empty">{search ? 'No tasks match your search.' : group.id === 'now' ? 'Nothing needs your attention right now.' : group.id === 'next' ? 'No tasks ready to pick up.' : 'Nothing planned for later.'}</p>{/if}
            </section>
          {/each}
        </div>
      </div>
      <div class="work-side">
        <section class="tray" aria-labelledby="plans-h">
          <header><h2 id="plans-h" class="section-title">Plans</h2><span class="tray-note">{plans.length}</span></header>
          <ul class="tray-card rows plans">{#each plans as p (p.item.href)}<li><a class="task" href={p.item.href}><span class="task-content"><strong>{p.item.title}</strong><span class="context">{p.item.project}</span></span><span class="plan-progress">{p.item.detail}</span></a></li>{:else}<li class="empty">Create a plan to group related tasks. Individual tasks can stand on their own.</li>{/each}</ul>
        </section>
      </div>
    </div>
  {/if}
</div>

<style>
  .work-grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(16rem, 22rem); gap: 24px; align-items: start; }
  .work-main { min-width: 0; }
  .work-side { position: sticky; top: 16px; }
  .queue-note { font-size: 12px; color: var(--text-2); font-variant-numeric: tabular-nums; }
  ul { list-style: none; padding: 0; margin: 0; }
  :global(.work-view) .plans { margin-top: 12px; }
  .plan-progress { font-size: 12px; color: var(--text-2); }
  @media (max-width: 1100px) { .work-grid { grid-template-columns: minmax(0, 1fr); } .work-side { position: static; } }
</style>
