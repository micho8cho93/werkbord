<script lang="ts">
  import { ApiError, api } from '../lib/api';
  import { agentWorkable } from '../lib/task-presentation';
  import Archive from '../lib/work/Archive.svelte';
  import TaskCard from '../lib/work/TaskCard.svelte';
  import { agentLabel, resolveFor, type Resolved } from '../lib/execution';
  import { setFilter } from '../lib/filters.svelte';
  import { filterActive, matches, noFilter } from '../lib/labels';
  import { runStatus } from '../lib/format';
  import GitSheets from '../lib/git/GitSheets.svelte';
  import { sheets } from '../lib/git/sheets.svelte';
  import { gitStore } from '../lib/git/store.svelte';
  import Icon from '../lib/Icon.svelte';
  import WorkPlans from '../lib/work/WorkPlans.svelte';
  import { WORK_GROUPS, workGroup, type WorkPlan } from '../lib/work-queue';
  import { isPhone } from '../lib/phones/capability';
  import { orderDrops, positionBetween, reorderedIds, spreadPositions, type OrderDrop } from '../lib/orderGestures';
  import { assistant } from '../lib/assistant/sessions.svelte';
  import { projectHref } from '../lib/location';
  import { router } from '../lib/router.svelte';
  import type { ProjectScope } from '../lib/scope.svelte';
  import { app } from '../lib/state.svelte';
  import { type GitBranch, type Priority, type Project, type Task, type TaskState } from '../lib/types';

  let { scope, project }: { scope: ProjectScope; project: Project } = $props();

  // Branches, for the review column: what can be merged from the card. Loaded while the board is open.
  const git = $derived(gitStore(project.id));
  $effect(() => git.watch());

  $effect(() => { if (new URLSearchParams(router.query).get('new') === 'task') app.newTaskOpen = true; });
  function openSalie() {if(window.parent!==window) window.parent.postMessage({type:'werkbord.frame',event:'work_salie',projectId:project.id},'*');else assistant.show();}
  // ---- filters ----
  let search = $state('');
  const params = $derived(new URLSearchParams(router.query));
  const detail = $derived(!!params.get('plan') || params.get('new') === 'plan');
  const plansShown = $derived(detail || params.get('section') === 'plans');
  let viewingArchive = $state(false);
  let agentFilter = $state('');
  let runnerFilter = $state('');
  let priorityFilter = $state<Priority | ''>('');
  const labelFilter = noFilter();
  const filtering = $derived(!!(agentFilter || runnerFilter || priorityFilter) || filterActive(labelFilter));

  const effectiveOf = (t: Task): Resolved => resolveFor(t.execution, project.execution, app.globalExecution);

  function shown(t: Task): boolean {
    if (search && !`${t.title} ${t.description}`.toLowerCase().includes(search.toLowerCase())) return false;
    if (!filtering) return true;
    if (!matches(t, labelFilter)) return false;
    const run = scope.latestRun[t.id];
    const eff = effectiveOf(t);
    if (agentFilter && (run?.agentId ?? eff.agent) !== agentFilter) return false;
    if (runnerFilter && (run?.runnerId ?? eff.runner) !== runnerFilter) return false;
    if (priorityFilter && eff.priority !== priorityFilter) return false;
    return true;
  }

  // Plans the queue can show a task under: the first plan that lists it. A plan is a series of tasks, so its tasks sit together.
  let plans = $state<WorkPlan[]>([]);
  $effect(() => {
    // A paired phone may not read plans, so on one the queue is flat and never asks.
    if (isPhone()) return;
    let live = true;
    const load = () => api.workPlans(project.id).then(p => { if (live) plans = p; }, () => {});
    void load();
    const timer = setInterval(load, 5000);
    return () => { live = false; clearInterval(timer); };
  });
  const planOf = $derived.by(() => {
    const own: Record<string, WorkPlan> = {};
    for (const plan of plans) for (const id of plan.taskIds) own[id] ??= plan;
    return own;
  });
  const progressOf = (plan: WorkPlan) => `${plan.taskIds.filter(id => scope.tasks.find(t => t.id === id)?.state === 'done').length} of ${plan.taskIds.length} complete`;
  const groups = $derived(WORK_GROUPS.map(group => {
    const tasks = scope.activeTasks.filter(t => shown(t) && workGroup(t, scope.tasks, scope.latestRun[t.id], effectiveOf(t).priority, app.now, scope.latestRun) === group.id);
    const standalone = tasks.filter(t => !planOf[t.id]).sort((a, b) => a.position - b.position);
    const sections = plans
      .map(plan => ({ plan, tasks: plan.taskIds.map(id => tasks.find(t => t.id === id && planOf[id] === plan)).filter((t): t is Task => !!t) }))
      .filter(section => section.tasks.length);
    return { ...group, tasks, standalone, sections };
  }));
  const done = $derived(scope.activeTasks.filter(t => t.state === 'done' && shown(t)).sort((a,b) => b.updatedAt.localeCompare(a.updatedAt)));
  // ---- what can be merged from the board: the task's own branch, while it is not merged yet ----
  function branchOf(task: Task): GitBranch | undefined {
    const run = scope.latestRun[task.id];
    return git.overview?.branches.find(
      (b) => b.scope === 'local' && !b.target && (b.werkbord.taskId === task.id || (!!run?.branch && b.name === run.branch)),
    );
  }

  function mergeable(task: Task): boolean {
    const run = scope.latestRun[task.id];
    if (run?.remote) return false;
    const b = branchOf(task);
    return !!b && !b.merged && b.vsTarget.ahead > 0;
  }

  let pendingTasks = $state<Record<string, boolean>>({});

  // ---- ordering: a person's drag (or Alt+Arrow) becomes a stored position, or a new order of a plan's tasks ----
  async function reorder(drop: OrderDrop) {
    const [kind, key] = drop.scope.split(':');
    try {
      if (kind === 'plan') {
        const plan = plans.find(p => p.id === key);
        if (!plan) return;
        const taskIds = reorderedIds(plan.taskIds, drop);
        plans = plans.map(p => p.id === plan.id ? { ...p, taskIds } : p);
        const saved = await api.saveWorkPlan(project.id, { ...plan, taskIds });
        plans = plans.map(p => p.id === saved.id ? saved : p);
        return;
      }
      const list = groups.find(g => g.id === key)?.standalone ?? [];
      const task = list.find(t => t.id === drop.id);
      if (!task || pendingTasks[task.id]) return;
      const position = positionBetween(list.find(t => t.id === drop.before)?.position, list.find(t => t.id === drop.after)?.position);
      pendingTasks[task.id] = true;
      try {
        if (position !== null) {
          scope.upsertTask({ ...task, position });
          scope.upsertTask(await api.editTask(task, { position }));
          return;
        }
        // The neighbours have no room between them: number the whole list again, in the order the person chose.
        const order = list.filter(t => t.id !== task.id);
        order.splice(drop.to, 0, task);
        const spread = spreadPositions(order.length);
        for (const [i, item] of order.entries()) if (item.position !== spread[i]) scope.upsertTask(await api.editTask(scope.tasks.find(t => t.id === item.id) ?? item, { position: spread[i] }));
      } finally { pendingTasks[task.id] = false; }
    } catch (err) {
      app.notify(err instanceof ApiError && err.code === 'conflict' ? 'This changed on another device. The queue has been refreshed.' : err instanceof Error ? err.message : String(err));
      await scope.load().catch(e => app.handleError(e));
      plans = await api.workPlans(project.id).catch(() => plans);
    }
  }

  // ---- actions ----
  async function move(task: Task, state: TaskState) {
    if (pendingTasks[task.id]) return;
    pendingTasks[task.id] = true;
    try {
      const saved = await api.moveTask(task, state);
      scope.upsertTask(saved);
    } catch (err) {
      if (err instanceof ApiError && err.code === 'conflict') {
        app.notify('This task changed on another device. The board has been refreshed.');
        await scope.load().catch((e) => app.handleError(e));
      } else { app.notify(err instanceof Error ? err.message : String(err)); await scope.load().catch(e => app.handleError(e)); }
    } finally { pendingTasks[task.id] = false; }
  }

  async function start(task: Task) {
    if (pendingTasks[task.id]) return;
    pendingTasks[task.id] = true;
    try {
      const r = await api.startRun(project.id, task.id);
      scope.upsertRun(r);
      // Reconcile the persisted card before enabling another drop.
      await scope.load();
      const current = scope.activeTasks.find(t => t.id === task.id);
      if (current?.state === 'backlog' && runStatus(r).active) {
        try { scope.upsertTask(await api.moveTask(current, 'doing')); }
        catch (e) {
          if (!(e instanceof ApiError && e.code === 'conflict')) throw e;
          await scope.load();
          if (scope.activeTasks.find(t => t.id === task.id)?.state !== 'doing') throw e;
        }
      }
      app.notify(`${agentLabel(app.agents, r.agentId)} started on “${task.title}”.`, 4000);
    } catch (err) {
      app.notify(err instanceof Error ? err.message : String(err));
    } finally { pendingTasks[task.id] = false; }
  }

  async function merge(task: Task) {
    if (!git.overview) await git.load();
    const b = branchOf(task);
    if (!b) {
      app.notify('This task has no branch to merge.');
      return;
    }
    sheets.open({ kind: 'merge', branch: b.name, onmerged: () => void move(task, 'done') });
  }

</script>

<div class="work-view">
  {#if !detail}
    <header class="work-heading">
      <div><h1>Work</h1><p class="muted">Your tasks, plans and agent activity.</p></div>
      <div class="work-actions"><label class="work-search"><Icon name="search" size={15} /><span class="visually-hidden">{plansShown ? 'Search plans' : 'Search tasks'}</span><input type="search" bind:value={search} placeholder={plansShown ? 'Search plans…' : 'Search tasks…'} /></label><button class="btn primary work-new" onclick={() => app.newTaskOpen = true}><Icon name="plus" size={15} />New task</button></div>
    </header>
    <nav class="work-tabs" aria-label="Work sections"><a href={projectHref(project.id, 'work')} aria-current={!plansShown ? 'page' : undefined}>Queue<span>{scope.activeTasks.filter(t => t.state !== 'done').length}</span></a><a href={projectHref(project.id, 'work') + '?section=plans'} aria-current={plansShown ? 'page' : undefined}>Plans</a>{#if !plansShown}<button class="archive" aria-pressed={viewingArchive} onclick={() => viewingArchive = !viewingArchive}>{viewingArchive ? 'Back to Work' : `Archive (${scope.archivedTasks.length})`}</button>{/if}</nav>
  {/if}
  {#if plansShown}<WorkPlans {scope} {project} {search} />
  {:else}
    <section class="queue" aria-label="Work queue">
      <details class="queue-filters"><summary>Filter tasks{filtering ? ' · Active' : ''}</summary><div class="filters">
        <label>Priority <select bind:value={priorityFilter}><option value="">All priorities</option><option value="high">High</option><option value="normal">Normal</option><option value="low">Low</option></select></label>
        {#if app.agents.length > 1}<label>Agent <select bind:value={agentFilter}><option value="">All agents</option>{#each app.agents as a (a.id)}<option value={a.id}>{a.name}</option>{/each}</select></label>{/if}
        {#if app.runners.length > 1}<label>Runner <select bind:value={runnerFilter}><option value="">All runners</option>{#each app.runners as r (r.id)}<option value={r.id}>{app.runnerName(r)}</option>{/each}</select></label>{/if}
        {#if filtering}<button class="btn quiet small" onclick={() => { agentFilter = ''; runnerFilter = ''; priorityFilter = ''; setFilter(project.id, noFilter()); }}>Reset filters</button>{/if}
      </div></details>
      {#if viewingArchive}<Archive {scope} />{:else}
        <div class="queue-stack" use:orderDrops={reorder}>
        {#each groups as group (group.id)}
          <section class="queue-card" aria-labelledby="work-{group.id}">
            <header><h2 id="work-{group.id}">{group.title}</h2><span class="queue-count">{group.tasks.length}</span><span class="queue-detail">{group.detail}</span></header>
            {#if !group.tasks.length}<p class="queue-empty">{!scope.loaded ? 'Loading tasks…' : filtering || search ? 'No tasks match your filters.' : group.id === 'now' ? 'Nothing needs your attention right now.' : group.id === 'next' ? 'No tasks ready to pick up.' : 'Nothing planned for later.'}</p>{/if}
            {#if group.standalone.length}<ul data-order-list data-order-scope="tasks:{group.id}" aria-label="{group.title} tasks">{#each group.standalone as task (task.id)}{@render taskRow(task, true)}{/each}</ul>{/if}
            {#each group.sections as section (section.plan.id)}
              <details class="queue-section" open>
                <summary>{section.plan.title}<span class="progress">{progressOf(section.plan)}</span></summary>
                <ul data-order-list data-order-scope="plan:{section.plan.id}" aria-label="Tasks in {section.plan.title}">{#each section.tasks as task (task.id)}{@render taskRow(task, true)}{/each}</ul>
              </details>
            {/each}
          </section>
        {/each}
        {#if done.length}<section class="queue-card" aria-label="Completed work"><details class="queue-section completed"><summary>Completed<span class="count">{done.length}</span></summary><ul>{#each done as task (task.id)}{@render taskRow(task, false)}{/each}</ul></details></section>{/if}
        </div>
        <footer class="queue-footer"><button class="btn quiet small" onclick={openSalie}>Plan your next step with Salie <Icon name="right" size={14} /></button></footer>
      {/if}
    </section>
  {/if}
</div>
{#snippet taskRow(task: Task, sortable: boolean)}
  {@const run = scope.latestRun[task.id]}
  <TaskCard {task} {run} {scope} effective={effectiveOf(task)} selected={router.taskId === task.id} pending={!!pendingTasks[task.id]} mergeable={mergeable(task)} agentWork={agentWorkable(task,project)} branch={branchOf(task)} onstart={start} onmerge={merge} onmove={move} {sortable} />

{/snippet}
<GitSheets projectId={project.id} store={git} />
<style>
 .work-new{display:none;}.archive{margin-left:auto;font-size:12px;color:var(--text-2);background:transparent;border:0;cursor:pointer;}.queue-filters{font-size:12px;color:var(--text-2);margin-bottom:20px;}.queue-filters summary{cursor:pointer;}.filters{display:flex;gap:14px;flex-wrap:wrap;padding:14px 0 0;}.filters label{display:flex;align-items:center;gap:8px;}.filters select{min-height:32px;border:1px solid var(--border);border-radius:6px;padding:4px 8px;color:var(--text);background:var(--surface);font:inherit;max-width:180px;}
 .queue-card :global(.work-card){border-inline-width:0;border-bottom-width:0;border-radius:0;box-shadow:none;background:transparent;}.queue-footer{margin-top:20px;padding-top:4px;}
 @media(max-width:899px){.work-new{display:inline-flex;}.archive{min-height:44px;}.filters select{min-height:44px;}}
</style>
