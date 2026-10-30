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
  import { WORK_GROUPS, workGroup } from '../lib/work-queue';
  import { assistant } from '../lib/assistant/sessions.svelte';
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
  let viewingArchive = $state(false);
  let agentFilter = $state('');
  let runnerFilter = $state('');
  let priorityFilter = $state<Priority | ''>('');
  const labelFilter = noFilter();
  const filtering = $derived(!!(agentFilter || runnerFilter || priorityFilter) || filterActive(labelFilter));

  const effectiveOf = (t: Task): Resolved => resolveFor(t.execution, project.execution, app.globalExecution);

  function shown(t: Task): boolean {
    if (!filtering) return true;
    if (!matches(t, labelFilter)) return false;
    const run = scope.latestRun[t.id];
    const eff = effectiveOf(t);
    if (agentFilter && (run?.agentId ?? eff.agent) !== agentFilter) return false;
    if (runnerFilter && (run?.runnerId ?? eff.runner) !== runnerFilter) return false;
    if (priorityFilter && eff.priority !== priorityFilter) return false;
    return true;
  }

  const groups = $derived(WORK_GROUPS.map(group => ({ ...group, tasks: scope.activeTasks.filter(t => shown(t) && workGroup(t, scope.tasks, scope.latestRun[t.id], effectiveOf(t).priority, app.now) === group.id).sort((a,b) => a.position-b.position) })));
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
  <header class="work-tools">
    <div><h1>Work</h1><p class="muted">A queue for your tasks, with plans for related work.</p></div>
    <div class="actions">
      <button class="btn" onclick={openSalie}>Open Salie</button>
      <button class="btn" aria-pressed={viewingArchive} onclick={() => viewingArchive = !viewingArchive}>{viewingArchive ? 'Back to Work' : `Archive (${scope.archivedTasks.length})`}</button>
      <button class="btn primary work-new" onclick={() => app.newTaskOpen = true}><Icon name="plus" size={16} />New task</button>
    </div>
  </header>
  <div class="work-layout">
    <section class="queue" aria-label="Work queue">
      <div class="filters">
        <label>Priority <select bind:value={priorityFilter}><option value="">All priorities</option><option value="high">High</option><option value="normal">Normal</option><option value="low">Low</option></select></label>
        {#if app.agents.length > 1}<label>Agent <select bind:value={agentFilter}><option value="">All agents</option>{#each app.agents as a (a.id)}<option value={a.id}>{a.name}</option>{/each}</select></label>{/if}
        {#if app.runners.length > 1}<label>Runner <select bind:value={runnerFilter}><option value="">All runners</option>{#each app.runners as r (r.id)}<option value={r.id}>{app.runnerName(r)}</option>{/each}</select></label>{/if}
        {#if filtering}<button class="btn quiet small" onclick={() => { agentFilter = ''; runnerFilter = ''; priorityFilter = ''; setFilter(project.id, noFilter()); }}>Reset filters</button>{/if}
      </div>
      {#if viewingArchive}<Archive {scope} />{:else}
        {#each groups as group (group.id)}
          <section class="queue-group" aria-labelledby="work-{group.id}">
            <header><h2 id="work-{group.id}">{group.title}</h2><span class="chip">{group.tasks.length}</span><span class="muted">{group.detail}</span></header>
            <ul class="task-list">
              {#each group.tasks as task (task.id)}
                {@render taskRow(task)}
              {:else}<li class="empty-row">{!scope.loaded ? 'Loading tasks…' : filtering ? 'No tasks match these filters.' : `Nothing ${group.id === 'now' ? 'needs you now' : group.id === 'next' ? 'ready to pick up' : 'planned for later'}.`}</li>{/each}
            </ul>
          </section>
        {/each}
        {#if done.length}<details class="completed"><summary>Completed · {done.length}</summary><ul class="task-list">{#each done as task (task.id)}{@render taskRow(task)}{/each}</ul></details>{/if}
      {/if}
    </section>
    <aside aria-label="Structured plans"><WorkPlans {scope} {project} /></aside>
  </div>
</div>
{#snippet taskRow(task: Task)}
  {@const run = scope.latestRun[task.id]}
  <TaskCard {task} {run} {scope} effective={effectiveOf(task)} selected={router.taskId === task.id} pending={!!pendingTasks[task.id]} mergeable={mergeable(task)} agentWork={agentWorkable(task,project)} branch={branchOf(task)} onstart={start} onmerge={merge} onmove={move} />

{/snippet}
<GitSheets projectId={project.id} store={git} />
<style>
 .work-new { display:none; }
 .filters select { min-height:32px; border:1px solid var(--border); padding:4px 8px; border-radius:var(--radius-sm); background:var(--surface); color:var(--text); font:inherit; }
 .work-view { height:100%; overflow:auto; padding:20px 24px; }
 .work-tools { display:flex; align-items:center; justify-content:space-between; gap:16px; flex-wrap:wrap; margin-bottom:20px; }
 h1 { margin:0; } p { margin:4px 0 0; } .actions,.filters { display:flex; gap:8px; flex-wrap:wrap; align-items:center; }
 .work-layout { display:grid; grid-template-columns:minmax(0, 1fr) minmax(280px,340px); gap:28px; }
 .queue,aside { min-width:0; } .filters { margin-bottom:20px; font-size:12px; } label { display:flex; align-items:center; gap:6px; }
 select { max-width:160px; } .queue-group { margin-bottom:24px; }
 .queue-group > header { display:flex; gap:8px; align-items:baseline; margin-bottom:8px; flex-wrap:wrap; }
 h2 { margin:0; font-size:16px; } header .muted { font-size:12px; }
 .task-list { list-style:none; padding:0; margin:0; border-top:1px solid var(--border); }
 .task-list :global(.work-card) { margin:0; border:0; border-bottom:1px solid var(--border); border-radius:0; box-shadow:none; padding:12px 0; background:transparent; }
 .empty-row { color:var(--text-2); padding:14px 0; font-size:13px; }
 .completed { margin-top:16px; } summary { cursor:pointer; padding:12px 0; }
 @media(max-width:1000px) { .work-layout { grid-template-columns:1fr; } aside { border-top:1px solid var(--border); padding-top:20px; } }
 @media(max-width:899px) { .work-new { display:inline-flex; } .filters select { min-height:44px; } }
 @media(max-width:600px) { .work-new { display:none; }
 .filters select { min-height:32px; border:1px solid var(--border); padding:4px 8px; border-radius:var(--radius-sm); background:var(--surface); color:var(--text); font:inherit; }
 .work-view { padding:16px; } .actions .btn { min-height:44px; } }
</style>
