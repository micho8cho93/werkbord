<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../api';
  import { app } from '../state.svelte';
  import { assistant } from '../assistant/sessions.svelte';
  import { projectHref, taskHref } from '../location';
  import { router } from '../router.svelte';
  import Icon from '../Icon.svelte';
  import { blockingTasks, type WorkPlan } from '../work-queue';
  import { TASK_STATE_LABELS, type Project } from '../types';
  import type { ProjectScope } from '../scope.svelte';

  let { scope, project, search = '' }: { scope: ProjectScope; project: Project; search?: string } = $props();
  let plans = $state<WorkPlan[]>([]);
  let error = $state('');
  let busy = $state(false);
  let loaded = $state(false);
  let draft = $state<WorkPlan | null>(null);
  let tab = $state('tasks');
  const selected = $derived(new URLSearchParams(router.query).get('plan') ?? '');
  const current = $derived(plans.find(p => p.id === selected));
  const visible = $derived(plans.filter(p => `${p.title} ${p.description}`.toLowerCase().includes(search.toLowerCase())));
  const revision = $derived(scope.tasks.map(t => `${t.id}:${t.version}`).join('|'));
  const completed = $derived(current?.taskIds.filter(id => scope.tasks.find(t => t.id === id)?.state === 'done').length ?? 0);
  const blocked = $derived(current?.taskIds.filter(id => { const task = scope.tasks.find(t => t.id === id); return task && task.state !== 'done' && blockingTasks(task, scope.tasks, scope.latestRun).length; }).length ?? 0);
  const sessions = $derived(scope.history.filter(r => current?.taskIds.includes(r.taskId)));
  const listHref = $derived(projectHref(project.id, 'work') + '?section=plans');
  const tabs = [{ id: 'tasks', title: 'Tasks' }, { id: 'sessions', title: 'Sessions' }, { id: 'decisions', title: 'Decisions' }, { id: 'questions', title: 'Questions' }];

  $effect(() => {
    void revision;
    let live = true;
    api.workPlans(project.id).then(p => { if (live) { plans = p; loaded = true; error = ''; } }, e => { if (live) error = e.message; });
    return () => { live = false; };
  });
  onMount(() => {
    let live = true;
    const timer = setInterval(() => { api.workPlans(project.id).then(p => { if (live) plans = p; }, () => {}); }, 5000);
    return () => { live = false; clearInterval(timer); };
  });
  $effect(() => { if (new URLSearchParams(router.query).get('new') === 'plan') edit(); });
  $effect(() => { void selected; tab = 'tasks'; });

  function edit(plan?: WorkPlan) {
    draft = plan ? structuredClone($state.snapshot(plan)) : { id: '', projectId: project.id, title: '', description: '', taskIds: [], decisions: '', questions: '', version: 0 };
    error = '';
  }
  function reorder(index: number, delta: number) {
    if (!draft) return;
    const ids = [...draft.taskIds];
    [ids[index], ids[index + delta]] = [ids[index + delta], ids[index]];
    draft.taskIds = ids;
  }
  async function save(e: SubmitEvent) {
    e.preventDefault();
    if (!draft || busy) return;
    busy = true; error = '';
    try {
      const saved = await api.saveWorkPlan(project.id, draft);
      plans = [...plans.filter(p => p.id !== saved.id), saved];
      draft = null;
      location.hash = projectHref(project.id, 'work') + '?plan=' + encodeURIComponent(saved.id);
    } catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = false; }
  }
  function askSalie() {
    if (!current) return;
    if (window.parent !== window) { window.parent.postMessage({ type: 'werkbord.frame', event: 'work_salie', projectId: project.id, planId: current.id }, '*'); return; }
    assistant.draft = `Help me edit Work plan ${current.id} in project ${project.id}: ${current.title}. Read the plan and its tasks first.`;
    assistant.show();
  }
  const date = (at?: string) => at ? new Date(at).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' }) : 'Not recorded';
</script>

<section class="plans" aria-label="Structured plans">
  {#if error}<p class="error" role="alert">{error} Your draft is kept; check current plans before retrying.</p>{/if}
  {#if draft}
    <a class="back" href={listHref} onclick={() => draft = null}><Icon name="left" size={14} />Back to plans</a>
    <form onsubmit={save} aria-label="Plan editor">
      <header><h1>{draft.id ? 'Edit plan' : 'New plan'}</h1><p class="muted">Organise existing tasks into a clear sequence.</p></header>
      <label>Title<input class="input" required maxlength="200" bind:value={draft.title} /></label>
      <label>Description<textarea class="input" maxlength="20000" bind:value={draft.description}></textarea></label>
      <fieldset><legend>Tasks in order</legend>
        {#each draft.taskIds as id, i (id)}
          <div class="ordered"><span><span class="number">{i + 1}</span>{scope.tasks.find(t => t.id === id)?.title ?? 'Unavailable task'}</span><div class="actions"><button class="btn quiet small" type="button" disabled={i === 0} onclick={() => reorder(i, -1)} aria-label="Move task up">Up</button><button class="btn quiet small" type="button" disabled={i === draft!.taskIds.length - 1} onclick={() => reorder(i, 1)} aria-label="Move task down">Down</button><button class="btn quiet small" type="button" onclick={() => { if (draft) draft.taskIds = draft.taskIds.filter(t => t !== id); }}>Remove</button></div></div>
        {/each}
        <label>Add an existing task<select class="input" onchange={e => { if (draft && e.currentTarget.value) draft.taskIds = [...draft.taskIds, e.currentTarget.value]; e.currentTarget.value = ''; }}><option value="">Choose a task…</option>{#each scope.activeTasks.filter(t => !draft!.taskIds.includes(t.id)) as t (t.id)}<option value={t.id}>{t.title}</option>{/each}</select></label>
        <button class="btn quiet small" type="button" onclick={() => app.newTaskOpen = true}>Create a task…</button>
      </fieldset>
      <div class="context-fields"><label>Decisions<textarea class="input" maxlength="20000" bind:value={draft.decisions}></textarea></label><label>Open questions<textarea class="input" maxlength="20000" bind:value={draft.questions}></textarea></label></div>
      <div class="actions"><button class="btn primary" disabled={busy || !draft.title.trim()}>{busy ? 'Saving…' : 'Save plan'}</button><button class="btn" type="button" disabled={busy} onclick={() => { draft = null; if (!current) location.hash = listHref; }}>Cancel</button></div>
    </form>
  {:else if current}
    <a class="back" href={projectHref(project.id, 'work')}><Icon name="left" size={14} />Back to Work</a>
    <div class="plan-layout">
      <div class="plan-main">
        <header class="plan-heading"><div class="title-line"><h1>{current.title}</h1><span class="standing" class:blocked={blocked > 0}>{completed === current.taskIds.length && completed > 0 ? 'Complete' : blocked ? `${blocked} blocked` : completed ? 'In progress' : 'Ready'}</span></div><p class="description muted">{current.description || 'Add a description to give this plan context.'}</p></header>
        <div class="progress"><progress max={current.taskIds.length || 1} value={completed} aria-label="Plan progress"></progress><span>{completed} of {current.taskIds.length} tasks complete</span></div>
        <nav class="plan-tabs" aria-label="Plan sections">{#each tabs as item (item.id)}<button type="button" aria-current={tab === item.id ? 'page' : undefined} onclick={() => tab = item.id}>{item.title}{#if item.id === 'tasks'}<span>{current.taskIds.length}</span>{/if}</button>{/each}</nav>
        {#if tab === 'tasks'}
          <ol class="plan-tasks">{#each current.taskIds as id (id)}{@const task = scope.tasks.find(t => t.id === id)}
            <li class:complete={task?.state === 'done'}><span class="task-mark" aria-label={task?.state === 'done' ? 'Complete' : 'Incomplete'}>{#if task?.state === 'done'}<Icon name="check" size={13} />{/if}</span><div>{#if task}<a class="task-title" href={taskHref(project.id, id) + '?plan=' + encodeURIComponent(current.id)}>{task.title}</a><div class="task-meta">{TASK_STATE_LABELS[task.state]}{task.archivedAt ? ' · Archived' : ''}{#if task.orchestration?.dependencies?.length}<span class="dependency">Depends on {task.orchestration.dependencies.map(dep => scope.tasks.find(t => t.id === dep)?.title ?? 'Unavailable task').join(', ')}</span>{/if}</div>{:else}<span class="muted">Unavailable task</span>{/if}</div>{#if scope.latestRun[id]}<button class="btn quiet small" onclick={() => assistant.openRun(project.id, scope.latestRun[id].id)}>Open CLI session</button>{/if}</li>
          {:else}<li class="empty">No tasks in this plan yet. Edit the plan to add existing tasks.</li>{/each}</ol>
        {:else if tab === 'sessions'}
          <p class="section-note muted">Recent sessions associated with this plan’s tasks.</p><ul class="session-list">{#each sessions as run (run.id)}<li><div><strong>{scope.tasks.find(t => t.id === run.taskId)?.title ?? 'Unavailable task'}</strong><span class="muted">{run.agentId} · {run.state.replaceAll('_', ' ')} · {date(run.createdAt)}</span></div><button class="btn small" onclick={() => assistant.openRun(project.id, run.id)}>Open CLI session</button></li>{:else}<li class="empty">No associated sessions reported.</li>{/each}</ul>
        {:else if tab === 'decisions'}<h2>Decisions</h2><p class="description">{current.decisions || 'No decisions recorded yet.'}</p>
        {:else}<h2>Open questions</h2><p class="description">{current.questions || 'No open questions recorded.'}</p>{/if}
      </div>
      <aside class="plan-info" aria-label="Plan information"><h2>Plan info</h2><dl><dt>Project</dt><dd>{project.name}</dd><dt>Created</dt><dd>{date(current.createdAt)}</dd><dt>Updated</dt><dd>{date(current.updatedAt)}</dd><dt>Tasks</dt><dd>{current.taskIds.length} total · {completed} complete</dd></dl><h2>Actions</h2><button class="btn" onclick={() => edit(current)}><Icon name="edit" size={14} />Edit plan</button><button class="btn" onclick={askSalie}>Edit with Salie</button><a class="btn" href={projectHref(project.id, 'calendar')}>View in Calendar</a></aside>
    </div>
  {:else if selected && loaded}<p role="status">This plan is unavailable. <a href={listHref}>Back to plans</a></p>
  {:else}
    <header class="list-heading"><div><h2>Plans</h2><p class="muted">Related tasks, one shared objective.</p></div><button class="btn primary small" onclick={() => edit()}><Icon name="plus" size={14} />New plan</button></header>
    <ul class="plan-list">{#each visible as plan (plan.id)}{@const count = plan.taskIds.filter(id => scope.tasks.find(t => t.id === id)?.state === 'done').length}<li><a class="plan-link" href={projectHref(project.id, 'work') + '?plan=' + encodeURIComponent(plan.id)}><span class="plan-symbol"><Icon name="dependencies" size={17} /></span><span class="plan-text"><strong>{plan.title}</strong><span class="muted">{plan.description || `${plan.taskIds.length} related tasks`}</span></span><span class="plan-count">{count} / {plan.taskIds.length}<progress max={plan.taskIds.length || 1} value={count} aria-label="{plan.title} progress"></progress></span><Icon name="right" size={14} /></a></li>{:else}<li class="empty">{!loaded ? 'Loading plans…' : search ? 'No plans match your search.' : 'Create a plan to group related tasks. Individual tasks can stand on their own.'}</li>{/each}</ul>
  {/if}
</section>
<style>
  .plans{font-size:13px;min-width:0;} h1{font-size:24px;letter-spacing:-.025em;margin:0;} h2{font-size:14px;margin:0 0 12px;} p{margin:5px 0 0;line-height:1.6;} .muted{color:var(--text-2);} .back{display:inline-flex;align-items:center;gap:6px;color:var(--text-2);text-decoration:none;font-size:12px;margin:0 0 24px;}.back:hover{color:var(--text);}
  .plan-layout{display:grid;grid-template-columns:minmax(0,1fr) 230px;gap:32px;}.plan-main{min-width:0;} .plan-info{border-left:1px solid var(--border);padding-left:24px;}.plan-info .btn{display:flex;width:100%;justify-content:flex-start;margin:8px 0;} .plan-info h2:not(:first-child){margin-top:26px;} dl{margin:0;}dt{font-size:12px;color:var(--text-2);margin-top:18px;}dd{margin:4px 0 0;overflow-wrap:anywhere;}.title-line{display:flex;gap:12px;align-items:center;flex-wrap:wrap;}.standing{background:color-mix(in srgb,var(--ok) 10%,var(--surface));color:var(--ok-text);padding:4px 9px;border-radius:4px;font-size:12px;}.standing.blocked{color:var(--block-text);background:color-mix(in srgb,var(--block) 9%,var(--surface));}
  .description{white-space:pre-wrap;overflow-wrap:anywhere;max-width:72ch;}.plan-heading .description{margin-top:10px;}.progress{display:flex;align-items:center;gap:14px;margin:24px 0;}.progress>progress{flex:1;min-width:0;}.progress>span{font-size:12px;color:var(--text-2);font-variant-numeric:tabular-nums;}progress{height:5px;accent-color:var(--ok);border:0;width:100%;appearance:none;background:var(--surface-2);border-radius:5px;}progress::-webkit-progress-bar{background:var(--surface-2);border-radius:5px;}progress::-webkit-progress-value{background:var(--ok);border-radius:5px;}progress::-moz-progress-bar{background:var(--ok);border-radius:5px;}
  .plan-tabs{margin-bottom:8px;}
  .plan-tasks,.plan-list,.session-list{list-style:none;margin:0;padding:0;}.plan-tasks>li{display:grid;grid-template-columns:16px minmax(0,1fr) auto;gap:12px;align-items:start;padding:16px 4px;border-bottom:1px solid var(--border);}.task-mark{width:15px;height:15px;border:1px solid var(--text-2);border-radius:50%;margin-top:2px;display:grid;place-items:center;}.complete .task-mark{background:var(--ok);border-color:var(--ok);color:var(--surface);}.task-title{color:var(--text);text-decoration:none;font-weight:500;line-height:1.5;}.task-title:hover{text-decoration:underline;}.task-meta{font-size:12px;color:var(--text-2);margin-top:4px;}.dependency{display:block;color:var(--block-text);margin-top:4px;}.section-note{padding:12px 0;}.plan-main>h2{margin-top:24px;}
  .list-heading{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:20px;}.list-heading h2{font-size:16px;margin:0;}.plan-list>li+li{border-top:1px solid var(--border);}.plan-link{display:flex;align-items:center;gap:14px;padding:18px 12px;color:var(--text);text-decoration:none;border-radius:6px;}.plan-link:hover{background:var(--surface-2);}.plan-symbol{color:var(--text);width:32px;height:32px;display:grid;place-items:center;background:color-mix(in srgb,var(--accent) 7%,var(--surface));border-radius:6px;}.plan-text{display:grid;gap:5px;flex:1;min-width:0;}.plan-text strong{font-size:14px;font-weight:500;}.plan-text>span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}.plan-count{display:grid;gap:8px;min-width:80px;font-size:12px;color:var(--text-2);text-align:right;}.empty{color:var(--text-2);padding:24px 4px;line-height:1.6;}
  form{max-width:820px;display:grid;gap:20px;}form header{margin-bottom:8px;}label{display:grid;gap:8px;}textarea{min-height:92px;resize:vertical;}.context-fields{display:grid;grid-template-columns:1fr 1fr;gap:20px;}fieldset{border:1px solid var(--border);padding:16px;min-width:0;border-radius:6px;}legend{font-weight:500;}fieldset label{margin-top:14px;}.ordered{display:flex;justify-content:space-between;gap:12px;align-items:center;padding:10px 0;border-bottom:1px solid var(--border);}.ordered>span{display:flex;gap:12px;align-items:center;}.number{color:var(--text-2);font-size:12px;}.actions{display:flex;gap:8px;flex-wrap:wrap;}.session-list>li{display:flex;justify-content:space-between;gap:12px;padding:16px 0;border-bottom:1px solid var(--border);}.session-list>li>div{display:grid;gap:5px;}.session-list strong{font-weight:500;}.session-list .muted{font-size:12px;}
  @media(max-width:800px){.plan-layout{grid-template-columns:1fr;gap:24px;}.plan-info{border-left:0;border-top:1px solid var(--border);padding:20px 0 0;}.plan-info dl{display:grid;grid-template-columns:100px 1fr;gap:10px;}dt,dd{margin:0;}.plan-info .btn{width:auto;}.progress{flex-wrap:wrap;}.progress>span{width:100%;}.context-fields{grid-template-columns:1fr;}.plan-tasks>li{grid-template-columns:16px minmax(0,1fr);}.plan-tasks>li>.btn{grid-column:2;justify-self:start;}.plan-count{min-width:50px;}.plan-link{padding-inline:0;}.plan-tabs{gap:20px;}.plan-tabs button,.back{min-height:44px;}.ordered{flex-wrap:wrap;}.ordered .actions{margin-left:24px;}h1{font-size:22px;}}
</style>
