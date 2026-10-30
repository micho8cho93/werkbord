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

  const groups = $derived(WORK_GROUPS.map(group => ({ ...group, tasks: scope.activeTasks.filter(t => shown(t) && workGroup(t, scope.tasks, scope.latestRun[t.id], effectiveOf(t).priority, app.now, scope.latestRun) === group.id).sort((a,b) => a.position-b.position) })));
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
  {#if !detail}
    <header class="work-tools">
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
        {#each groups as group (group.id)}
          <section class="queue-group" aria-labelledby="work-{group.id}">
            <header><h2 id="work-{group.id}">{group.title}</h2><span class="queue-count">{group.tasks.length}</span></header>
            <ul class="task-list">{#each group.tasks as task (task.id)}{@render taskRow(task)}{:else}<li class="empty-row">{!scope.loaded ? 'Loading tasks…' : filtering || search ? 'No tasks match your filters.' : group.id === 'now' ? 'Nothing needs your attention right now.' : group.id === 'next' ? 'No tasks ready to pick up.' : 'Nothing planned for later.'}</li>{/each}</ul>
          </section>
        {/each}
        {#if done.length}<details class="completed"><summary>Completed <span>{done.length}</span></summary><ul class="task-list">{#each done as task (task.id)}{@render taskRow(task)}{/each}</ul></details>{/if}
        <footer class="queue-footer"><button class="btn quiet small" onclick={openSalie}>Plan your next step with Salie <Icon name="right" size={14} /></button></footer>
      {/if}
    </section>
  {/if}
</div>
{#snippet taskRow(task: Task)}
  {@const run = scope.latestRun[task.id]}
  <TaskCard {task} {run} {scope} effective={effectiveOf(task)} selected={router.taskId === task.id} pending={!!pendingTasks[task.id]} mergeable={mergeable(task)} agentWork={agentWorkable(task,project)} branch={branchOf(task)} onstart={start} onmerge={merge} onmove={move} />

{/snippet}
<GitSheets projectId={project.id} store={git} />
<style>
 .work-view{height:100%;overflow:auto;padding:28px clamp(18px,3vw,40px);background:var(--surface);}.work-tools{display:flex;align-items:center;justify-content:space-between;gap:20px;flex-wrap:wrap;margin-bottom:22px;}h1{margin:0;font-size:24px;letter-spacing:-.025em;}.work-tools p{margin:6px 0 0;font-size:13px;}.work-actions{display:flex;gap:10px;align-items:center;}.work-new{display:none;}.work-search{display:flex;align-items:center;gap:8px;border:1px solid var(--border);border-radius:6px;background:var(--bg);padding:0 10px;color:var(--text-2);}.work-search input{width:190px;border:0;outline:none;background:transparent;color:var(--text);padding:9px 0;font:inherit;font-size:12px;}.work-search:focus-within{outline:2px solid var(--accent);outline-offset:2px;}
 .work-tabs{display:flex;gap:24px;align-items:center;border-bottom:1px solid var(--border);margin-bottom:24px;}.work-tabs a{display:flex;gap:7px;padding:10px 0 13px;border-bottom:2px solid transparent;color:var(--text-2);text-decoration:none;font-size:13px;}.work-tabs a[aria-current=page]{color:var(--text);border-color:var(--accent);font-weight:500;}.work-tabs a>span{font-size:12px;color:var(--text-2);}.archive{margin-left:auto;font-size:12px;color:var(--text-2);background:transparent;border:0;cursor:pointer;}.queue-filters{font-size:12px;color:var(--text-2);margin-bottom:20px;}.queue-filters summary{cursor:pointer;}.filters{display:flex;gap:14px;flex-wrap:wrap;padding:14px 0 0;}.filters label{display:flex;align-items:center;gap:8px;}.filters select{min-height:32px;border:1px solid var(--border);border-radius:6px;padding:4px 8px;color:var(--text);background:var(--surface);font:inherit;max-width:180px;}
 .queue-group{margin-bottom:26px;}.queue-group>header{display:flex;gap:8px;align-items:center;margin-bottom:6px;}h2{margin:0;font-size:14px;font-weight:600;}.queue-count{font-size:12px;color:var(--text-2);font-variant-numeric:tabular-nums;}.task-list{list-style:none;padding:0;margin:0;}.task-list :global(.work-card){border:0;border-bottom:1px solid var(--border);border-radius:0;box-shadow:none;background:transparent;}.empty-row{color:var(--text-2);padding:16px 28px;font-size:13px;}.completed summary{cursor:pointer;font-size:13px;color:var(--text-2);padding:12px 0;}.completed summary span{margin-left:6px;}.queue-footer{border-top:1px solid var(--border);margin-top:24px;padding-top:14px;}
 @media(max-width:899px){.work-new{display:inline-flex;}.work-tools{gap:16px;}.work-actions{width:100%;}.work-search{flex:1;min-width:0;}.work-search input{width:100%;min-width:0;}.work-search input,.archive{min-height:44px;}.work-view{padding:20px 16px;}.work-tabs{margin-bottom:20px;}.work-tabs a{min-height:44px;}.filters select{min-height:44px;}.queue-group{margin-bottom:22px;}}
</style>
