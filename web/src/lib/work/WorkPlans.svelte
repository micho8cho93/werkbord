<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../api';
  import { app } from '../state.svelte';
  import { assistant } from '../assistant/sessions.svelte';
  import { projectHref, taskHref } from '../location';
  import { router } from '../router.svelte';
  import Icon from '../Icon.svelte';
  import type { WorkPlan } from '../work-queue';
  import { progressOf, stepOf, type StepKind } from '../work-steps';
  import type { Project } from '../types';
  import type { ProjectScope } from '../scope.svelte';
  import Crumbs from '../Crumbs.svelte';
  import { chainOf, pushed, withChain, type Origin } from '../origin';

  let { scope, project, search = '' }: { scope: ProjectScope; project: Project; search?: string } = $props();
  let plans = $state<WorkPlan[]>([]);
  let error = $state('');
  let busy = $state(false);
  let loaded = $state(false);
  let draft = $state<WorkPlan | null>(null);
  const selected = $derived(new URLSearchParams(router.query).get('plan') ?? '');
  const current = $derived(plans.find(p => p.id === selected));
  const visible = $derived(plans.filter(p => `${p.title} ${p.description}`.toLowerCase().includes(search.toLowerCase())));
  const revision = $derived(scope.tasks.map(t => `${t.id}:${t.version}`).join('|'));
  const progress = $derived(current ? progressOf(current, scope.tasks, scope.latestRun) : undefined);
  const sessions = $derived(scope.history.filter(r => current?.taskIds.includes(r.taskId)));
  // Back from a plan goes where the plan was opened from (Work in the window, a board…); a plan reached by a plain link goes to the board.
  const chain = $derived.by<Origin[]>(() => { const given = chainOf(router.query); return given.length ? given : [{ kind: 'board', projectId: project.id }]; });
  /** What this plan is to a page it opens: a way back to the plan, then to wherever the plan was reached from. */
  const here = $derived<Origin[]>(pushed({ kind: 'plan', projectId: project.id, planId: selected }, chain));
  const names = { plan: (_p: string, id: string) => plans.find(p => p.id === id)?.title };
  const listHref = $derived(projectHref(project.id, 'work') + '?section=plans');
  const taskHrefFor = (id: string) => withChain(taskHref(project.id, id) + '?plan=' + encodeURIComponent(selected), here);
  const tone: Record<StepKind, string> = { done: 'ok', running: 'work', attention: 'ask', review: 'ok', blocked: 'block', waiting: 'neutral', ready: 'neutral', missing: 'neutral' };
  const scheduleHref = $derived(withChain(projectHref(project.id, 'calendar'), here));

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
  /** Salie picks the plan up where it stands: the first step that is not complete. */
  function continueWithSalie() {
    if (!current) return;
    if (window.parent !== window) { window.parent.postMessage({ type: 'werkbord.frame', event: 'work_salie', projectId: project.id, planId: current.id }, '*'); return; }
    const at = progress?.upNext ? scope.tasks.find(t => t.id === progress!.upNext!.id)?.title : '';
    assistant.draft = `Continue Work plan ${current.id} in project ${project.id}: ${current.title}. Read the plan and its tasks first${at ? `, then pick up at “${at}”` : ''}.`;
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
  {:else if current && progress}
    <Crumbs {chain} {names} />
    <header class="plan-head" data-testid="plan-head">
      <h1>{current.title}</h1>
      <p class="description muted">{current.description || 'Add a description to give this plan context.'}</p>
      <div class="progress" data-standing={progress.standing}>
        <span class="standing">{progress.label}</span>
        <progress max={progress.total || 1} value={progress.done} aria-label="Plan progress"></progress>
        <span class="count">{progress.done} of {progress.total} tasks complete</span>
      </div>
      {#if progress.upNext}
        {@const next = progress.upNext}
        <p class="up-next"><span class="k">Up next</span><a href={taskHrefFor(next.id)}>{scope.tasks.find(t => t.id === next.id)?.title}</a><span class="muted">{next.step.label}{next.step.reason ? ` · ${next.step.reason}` : ''}</span></p>
      {/if}
      <div class="actions plan-actions">
        <button class="btn primary" onclick={continueWithSalie}>Continue with Salie</button>
        <button class="btn" onclick={() => edit(current)}><Icon name="edit" size={14} />Edit plan</button>
        <a class="btn" href={scheduleHref}>View schedule</a>
      </div>
    </header>
    <section aria-labelledby="plan-tasks-h">
      <h2 id="plan-tasks-h">Tasks in order</h2>
      <ol class="plan-tasks">{#each current.taskIds as id, i (id)}{@const task = scope.tasks.find(t => t.id === id)}{@const step = stepOf(task, scope.tasks, scope.latestRun)}
        <li data-step={step.kind} data-task-id={id}>
          <span class="task-mark" data-tone={tone[step.kind]} aria-hidden="true">{#if step.kind === 'done'}<Icon name="check" size={13} />{:else}{i + 1}{/if}</span>
          <div class="task-body">
            {#if task}<a class="task-title" href={taskHrefFor(id)}>{task.title}</a>{:else}<span class="task-title muted">Unavailable task</span>{/if}
            <p class="task-status"><span class="chip" data-tone={tone[step.kind]}>{step.label}</span>{#if task?.archivedAt}<span class="muted">Archived</span>{/if}{#if step.reason}<span class="reason">{step.reason}</span>{/if}</p>
            {#if step.blockedBy.length}<p class="dependency">Depends on {#each step.blockedBy as b, j (b.id)}{#if j}, {/if}{#if scope.tasks.some(t => t.id === b.id)}<a href={taskHrefFor(b.id)}>{b.title}</a>{:else}{b.title}{/if}{/each}</p>{/if}
          </div>
          <div class="task-acts">
            {#if task && step.next}<a class="btn small" class:primary={step.kind === 'attention' || step.kind === 'review'} href={taskHrefFor(id)}>{step.next}</a>{/if}
            {#if scope.latestRun[id]}<button class="btn quiet small" onclick={() => assistant.openRun(project.id, scope.latestRun[id].id, here)}>Open CLI session</button>{/if}
          </div>
        </li>
      {:else}<li class="empty">No tasks in this plan yet. Edit the plan to add existing tasks.</li>{/each}</ol>
    </section>
    <section class="context" aria-label="Plan context">
      <details open={!!current.decisions}><summary>Decisions</summary><p class="description">{current.decisions || 'No decisions recorded yet.'}</p></details>
      <details open={!!current.questions}><summary>Open questions</summary><p class="description">{current.questions || 'No open questions recorded.'}</p></details>
      <details><summary>Sessions<span class="count">{sessions.length}</span></summary>
        <ul class="session-list">{#each sessions as run (run.id)}<li><div><strong>{scope.tasks.find(t => t.id === run.taskId)?.title ?? 'Unavailable task'}</strong><span class="muted">{run.agentId} · {run.state.replaceAll('_', ' ')} · {date(run.createdAt)}</span></div><button class="btn small" onclick={() => assistant.openRun(project.id, run.id, here)}>Open CLI session</button></li>{:else}<li class="empty">No associated sessions reported.</li>{/each}</ul>
      </details>
      <p class="muted meta">Created {date(current.createdAt)} · Updated {date(current.updatedAt)}</p>
    </section>
  {:else if selected && loaded}<p role="status">This plan is unavailable. <a href={listHref}>Back to plans</a></p>
  {:else}
    <header class="list-heading"><div><h2>Plans</h2><p class="muted">Related tasks, one shared objective.</p></div><button class="btn primary small" onclick={() => edit()}><Icon name="plus" size={14} />New plan</button></header>
    <ul class="plan-list">{#each visible as plan (plan.id)}{@const count = plan.taskIds.filter(id => scope.tasks.find(t => t.id === id)?.state === 'done').length}<li><a class="plan-link" href={projectHref(project.id, 'work') + '?plan=' + encodeURIComponent(plan.id)}><span class="plan-symbol"><Icon name="dependencies" size={17} /></span><span class="plan-text"><strong>{plan.title}</strong><span class="muted">{plan.description || `${plan.taskIds.length} related tasks`}</span></span><span class="plan-count">{count} / {plan.taskIds.length}<progress max={plan.taskIds.length || 1} value={count} aria-label="{plan.title} progress"></progress></span><Icon name="right" size={14} /></a></li>{:else}<li class="empty">{!loaded ? 'Loading plans…' : search ? 'No plans match your search.' : 'Create a plan to group related tasks. Individual tasks can stand on their own.'}</li>{/each}</ul>
  {/if}
</section>
<style>
  .plans{font-size:13px;min-width:0;} h1{font-size:24px;letter-spacing:-.025em;margin:0;} h2{font-size:14px;margin:0 0 12px;} p{margin:5px 0 0;line-height:1.6;} .muted{color:var(--text-2);} .back{display:inline-flex;align-items:center;gap:6px;color:var(--text-2);text-decoration:none;font-size:12px;margin:0 0 24px;}.back:hover{color:var(--text);}
  .plan-head{display:grid;gap:12px;margin:8px 0 28px;max-width:78ch;}.plan-head h1{line-height:1.2;}.plan-head .description{margin:0;}.k{font-family:var(--mono);font-size:11px;text-transform:uppercase;letter-spacing:.14em;color:var(--text-2);}
  .description{white-space:pre-wrap;overflow-wrap:anywhere;max-width:72ch;}.progress{display:grid;grid-template-columns:auto minmax(80px,1fr) auto;align-items:center;gap:14px;}.progress>.count{font-size:12px;color:var(--text-2);font-variant-numeric:tabular-nums;}.standing{padding:4px 9px;border-radius:4px;font-size:12px;background:color-mix(in srgb,var(--ok) 10%,var(--surface));color:var(--ok-text);}.progress[data-standing=blocked] .standing,.progress[data-standing=attention] .standing{color:var(--block-text);background:color-mix(in srgb,var(--block) 9%,var(--surface));}.progress[data-standing=ready] .standing,.progress[data-standing=empty] .standing{color:var(--text-2);background:var(--surface-2);}
  progress{height:5px;accent-color:var(--ok);border:0;width:100%;appearance:none;background:var(--surface-2);border-radius:5px;}progress::-webkit-progress-bar{background:var(--surface-2);border-radius:5px;}progress::-webkit-progress-value{background:var(--ok);border-radius:5px;}progress::-moz-progress-bar{background:var(--ok);border-radius:5px;}
  .up-next{display:flex;flex-wrap:wrap;align-items:baseline;gap:4px 12px;margin:0;}.up-next a{color:var(--text);font-weight:500;}.plan-actions{margin-top:4px;}
  .plan-tasks,.plan-list,.session-list{list-style:none;margin:0;padding:0;}h2#plan-tasks-h{margin-bottom:4px;}
  .plan-tasks>li{display:grid;grid-template-columns:22px minmax(0,1fr) auto;gap:14px;align-items:start;padding:16px 4px;border-bottom:1px solid var(--border);}.task-mark{width:22px;height:22px;border:1px solid var(--text-2);border-radius:50%;display:grid;place-items:center;font-size:11px;color:var(--text-2);font-variant-numeric:tabular-nums;}.task-mark[data-tone=ok]{background:var(--ok);border-color:var(--ok);color:var(--surface);}.task-mark[data-tone=work]{border-color:var(--accent);color:var(--accent);}.task-mark[data-tone=ask],.task-mark[data-tone=block]{border-color:var(--block);color:var(--block-text);}
  .task-body{display:grid;gap:5px;min-width:0;}.task-title{color:var(--text);text-decoration:none;font-weight:500;line-height:1.5;overflow-wrap:anywhere;}a.task-title:hover{text-decoration:underline;}.task-status{display:flex;flex-wrap:wrap;align-items:baseline;gap:4px 10px;margin:0;font-size:12px;}.chip{padding:1px 8px;border-radius:999px;background:var(--surface-2);color:var(--text-2);}.chip[data-tone=ok]{color:var(--ok-text);background:color-mix(in srgb,var(--ok) 10%,var(--surface));}.chip[data-tone=work]{color:var(--accent);background:color-mix(in srgb,var(--accent) 9%,var(--surface));}.chip[data-tone=ask],.chip[data-tone=block]{color:var(--block-text);background:color-mix(in srgb,var(--block) 9%,var(--surface));}.reason{color:var(--text-2);overflow-wrap:anywhere;}.dependency{margin:0;font-size:12px;color:var(--text-2);}.dependency a{color:var(--text-2);}
  .task-acts{display:flex;flex-wrap:wrap;gap:6px;justify-content:flex-end;}li[data-step=done] .task-title{color:var(--text-2);}
  .context{margin-top:28px;display:grid;gap:2px;max-width:78ch;}.context details{border-top:1px solid var(--border);padding:4px 0;}.context summary{cursor:pointer;min-height:36px;display:flex;align-items:center;gap:8px;font-weight:500;}.context .count{color:var(--text-2);font-weight:400;font-size:12px;}.context details>p{margin-top:2px;}.meta{font-size:12px;margin-top:12px;}
  .list-heading{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:20px;}.list-heading h2{font-size:16px;margin:0;}.plan-list>li+li{border-top:1px solid var(--border);}.plan-link{display:flex;align-items:center;gap:14px;padding:18px 12px;color:var(--text);text-decoration:none;border-radius:6px;}.plan-link:hover{background:var(--surface-2);}.plan-symbol{color:var(--text);width:32px;height:32px;display:grid;place-items:center;background:color-mix(in srgb,var(--accent) 7%,var(--surface));border-radius:6px;}.plan-text{display:grid;gap:5px;flex:1;min-width:0;}.plan-text strong{font-size:14px;font-weight:500;}.plan-text>span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}.plan-count{display:grid;gap:8px;min-width:80px;font-size:12px;color:var(--text-2);text-align:right;}.empty{color:var(--text-2);padding:24px 4px;line-height:1.6;}
  form{max-width:820px;display:grid;gap:20px;}form header{margin-bottom:8px;}label{display:grid;gap:8px;}textarea{min-height:92px;resize:vertical;}.context-fields{display:grid;grid-template-columns:1fr 1fr;gap:20px;}fieldset{border:1px solid var(--border);padding:16px;min-width:0;border-radius:6px;}legend{font-weight:500;}fieldset label{margin-top:14px;}.ordered{display:flex;justify-content:space-between;gap:12px;align-items:center;padding:10px 0;border-bottom:1px solid var(--border);}.ordered>span{display:flex;gap:12px;align-items:center;}.number{color:var(--text-2);font-size:12px;}.actions{display:flex;gap:8px;flex-wrap:wrap;}.session-list>li{display:flex;justify-content:space-between;gap:12px;padding:16px 0;border-bottom:1px solid var(--border);}.session-list>li>div{display:grid;gap:5px;}.session-list strong{font-weight:500;}.session-list .muted{font-size:12px;}
  @media(max-width:800px){.progress{grid-template-columns:auto 1fr;}.progress>.count{grid-column:1/-1;}.context-fields{grid-template-columns:1fr;}.plan-tasks>li{grid-template-columns:22px minmax(0,1fr);}.task-acts{grid-column:2;justify-content:flex-start;}.task-acts .btn,.plan-actions .btn,.context summary{min-height:44px;}.plan-count{min-width:50px;}.plan-link{padding-inline:0;}.back{min-height:44px;}.ordered{flex-wrap:wrap;}.ordered .actions{margin-left:24px;}h1{font-size:22px;}}
</style>
