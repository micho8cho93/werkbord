<script lang="ts">
  import '../../../../internal/uipresentation/static/work.css';
  import { blockingTasks } from '../work-queue';
  import { runReportStale } from '../format';
  import { assistant } from '../assistant/sessions.svelte';
  import { api } from '../api';
  import { cardStatus, quickChoices, type CardStatus } from '../task-presentation';
  import { agentLabel, priorityLabel, type Resolved } from '../execution';
  import Icon from '../Icon.svelte';
  import { spanOf, spanTitle } from '../planning-dates';
  import { answeredNotice, choicesOf, describeAnswerFailure, isAllow, isDeny } from '../questions';
  import { branchHref } from '../gitroute';
  import { taskHref } from '../router.svelte';
  import type { ProjectScope } from '../scope.svelte';
  import { app } from '../state.svelte';
  import type { GitBranch, Run, Task, TaskState } from '../types';

  // One task in Work: what it is, what its agent is doing or wants, and the one or two
  // things you would do about it, right here. The whole card opens the task.

  let {
    task,
    run,
    scope,
    effective,
    selected = false,
    pending = false,
    mergeable = false,
    agentWork = true,
    branch,
    onstart,
    onmerge,
    onmove,
  }: {
    task: Task;
    run: Run | undefined;
    scope: ProjectScope;
    effective: Resolved;
    selected?: boolean;
    pending?: boolean;
    mergeable?: boolean;
    /** An agent may be started on it: not human work, and not in a project with no repository. */
    agentWork?: boolean;
    /** The task's branch, when Git knows it: what review means, and what a merge takes. */
    branch?: GitBranch;
    onstart: (task: Task) => void;
    onmerge: (task: Task) => void;
    onmove: (task: Task, state: TaskState) => void;
  } = $props();

  const questions = $derived(run ? scope.pendingFor(run.id) : []);
  const runner = $derived(run?.runnerId ? app.runners.find((r) => r.id === run.runnerId) : undefined);
  const decision = $derived(scope.decisions.find((d) => d.taskId === task.id));
  const dependencies = $derived(blockingTasks(task, scope.tasks, scope.latestRun));
  const waitingFor = $derived(dependencies.map(t => t.title));
  const status: CardStatus = $derived(
    cardStatus(task, run, {
      now: app.now,
      runnerName: (app.remote || app.runners.length > 1) && runner ? app.runnerName(runner) : undefined,
      questions,
      decision,
      waitingFor,
      branch: branch ? `${branch.name}${branch.vsTarget.ahead ? ` · ${branch.vsTarget.ahead} ${branch.vsTarget.ahead === 1 ? 'commit' : 'commits'} ahead` : ''}` : undefined,
    }),
  );
  const stale = $derived(!!run && runReportStale(run, app.connection === 'live', app.runners));
  const question = $derived(questions[0]);
  const choices = $derived(question ? quickChoices(choicesOf(question)) : []);
  const blockerChoices = $derived(run?.state === 'blocked' ? quickChoices(run.blocker?.options ?? []) : []);
  const agent = $derived(run ? agentLabel(app.agents, run.agentId) : effective.agent ? agentLabel(app.agents, effective.agent) : '');
  const canStart = $derived(agentWork && task.state !== 'done' && (!run || ['completed', 'failed', 'stopped'].includes(run.state)) && task.state !== 'review');
  const plan = $derived(spanOf(task));
  const href = $derived(taskHref(task.projectId, task.id));
  // What the card asks of the person has full buttons. What they could do but need not (start, run again, archive) is one quiet
  // text control in the footer, so a column of cards reads as titles and states, not a row of buttons.
  const attention = $derived(!!question || run?.state === 'blocked' || status.tone === 'idle' || task.state === 'review' || (task.state === 'doing' && run?.state === 'completed'));
  const demand = $derived(
    task.state === 'done' ? 'archive'
    : attention ? ''
    : canStart && task.state === 'backlog' && waitingFor.length === 0 ? 'start'
    : canStart && run?.state === 'failed' ? 'again'
    : '',
  );
  const showAgent = $derived(agentWork && !!(run || effective.agent));
  const hasMeta = $derived(showAgent || effective.priority !== 'normal' || !!plan);

  let busy = $state('');

  async function archive() {
    if (busy || pending) return;
    busy = 'archive';
    try {
      scope.upsertTask(await api.archiveTask(task, true));
      app.notify('Task archived. Its details are kept in Archive.');
    } catch (err) {
      app.handleError(err);
      await scope.load().catch(e => app.handleError(e));
    } finally { busy = ''; }
  }

  async function answer(value: string) {
    if (!question || busy) return;
    busy = value;
    try {
      const settled = await api.answerQuestion(question, value);
      app.resolveQuestion(settled);
      if (settled.state === 'answered') app.notify(answeredNotice(settled, value));
    } catch (err) {
      const failure = describeAnswerFailure(err);
      if (failure.settled) app.resolveQuestion(question);
      app.notify(failure.message);
    } finally {
      busy = '';
    }
  }

  async function reply(value: string) {
    if (!run || busy) return;
    busy = value;
    try {
      scope.upsertRun(await api.sendInput(task.projectId, run.id, value));
    } catch (err) {
      app.notify(err instanceof Error ? err.message : String(err));
    } finally {
      busy = '';
    }
  }
</script>

<li
  class="card task work-card"
  data-tone={status.tone}
  data-task-id={task.id}
  class:live={status.live && !stale}
  class:settled={task.state === 'done'}
  class:selected
  aria-busy={pending}
>
  <span class="task-marker" class:finished={task.state === 'done'} aria-label={task.state === 'done' ? 'Complete' : status.text}>{#if task.state === 'done'}<Icon name="check" size={12} />{/if}</span>
  <div class="task-content">
  <a class="title" {href}>{task.title}</a>
  {#if pending || stale || task.state !== 'backlog' || (waitingFor.length === 0 && status.text !== 'Not started')}
    <p class="st"><span class="dot" data-tone={status.tone} class:hollow={status.tone === 'neutral' || status.tone === 'idle'}></span>{pending ? 'Updating task…' : `${stale ? 'Last reported: ' : ''}${status.text}${run?.pendingRequests?.includes('stop') ? ' · stop requested' : run?.pendingRequests?.includes('finish') ? ' · finish requested' : ''}`}</p>
  {/if}
  {#if dependencies.length && task.state !== 'done'}<p class="dependency">Blocked by {#each dependencies as dependency, i (dependency.id)}{#if i}, {/if}<a href={taskHref(task.projectId,dependency.id)}>{dependency.title}</a>{/each}</p>{/if}
  <!-- A work project has no run to describe, so what the task is about is the one other line it has. -->
  {#if !agentWork && task.description}<p class="work-detail one">{task.description}</p>{/if}
  {#if status.detail}<p class="detail">{status.detail}</p>{/if}

    {#if hasMeta}<p class="meta">{#if showAgent}<span>{agent}</span>{/if}{#if plan}<span>Planned {spanTitle(plan, new Date(app.now).getFullYear())}</span>{/if}{#if effective.priority !== 'normal'}<span class="prio" data-p={effective.priority}>{priorityLabel(effective.priority)}</span>{/if}</p>{/if}
  </div>
  <div class="row-actions">
  {#if question}
    <div class="acts">
      {#each choices as c (c)}
        <button class="btn small" class:primary={isAllow(c)} class:danger={isDeny(c, question?.kind)} disabled={!!busy} onclick={() => answer(c)}>{busy === c ? 'Sending…' : c}</button>
      {/each}
      <a class="btn small" class:primary={choices.length === 0} {href}><Icon name="reply" size={14} />{choices.length ? 'Reply' : 'Answer'}</a>
    </div>
  {:else if run?.state === 'blocked'}
    <div class="acts">
      {#each blockerChoices as c (c)}
        <button class="btn small" disabled={!!busy} onclick={() => reply(c)}>{busy === c ? 'Sending…' : c}</button>
      {/each}
      <a class="btn small" {href}><Icon name="reply" size={14} />Reply</a>
    </div>
  {:else if status.tone === 'idle'}
    <div class="acts"><a class="btn small primary" {href}><Icon name="reply" size={14} />Reply</a></div>
  {:else if task.state === 'review'}
    <div class="acts">
      <a class="btn small primary" href={branch && !run?.remote ? branchHref(task.projectId, branch.scope, branch.name) : href}>{branch && !run?.remote ? 'Review changes' : 'Review result'}</a>
      {#if mergeable}<button class="btn small ok" onclick={() => onmerge(task)}><Icon name="merge" size={14} />Merge…</button>{/if}
    </div>
  {:else if task.state === 'doing' && run?.state === 'completed'}
    <div class="acts">
      <button class="btn small primary" disabled={pending} onclick={() => onmove(task, 'review')}><Icon name="right" size={14} />Move to Review</button>
    </div>
  {/if}

  {#if demand}<div class="foot">
      {#if demand === 'start'}
        <button class="btn small quiet demand" disabled={pending} onclick={() => onstart(task)} aria-label="{run ? 'Run an agent again on' : 'Start an agent on'} “{task.title}”"><Icon name="play" size={12} />{run ? 'Run again' : 'Start agent'}</button>
      {:else if demand === 'again'}
        <button class="btn small quiet demand" disabled={pending} onclick={() => onstart(task)} aria-label="Run an agent again on “{task.title}”"><Icon name="play" size={12} />Run again</button>
      {:else if demand === 'archive'}
        <button class="btn small quiet demand" disabled={!!busy || pending} onclick={archive}>{busy === 'archive' ? 'Archiving…' : 'Archive'}</button>
      {/if}
    </div>
  {/if}
{#if run}
  <div class="acts"><button class="btn small session-action" onclick={e => { e.stopPropagation(); assistant.openRun(task.projectId, run.id); }}>Open Session</button></div>
{/if}
  </div>
</li>

<style>
 .task{--work-border:var(--border);--work-surface:var(--surface);--work-muted:var(--text-2);display:grid;grid-template-columns:15px minmax(0,1fr) auto;gap:12px;align-items:center;padding:13px 8px;position:relative;cursor:default;}.task:hover,.task.selected{background:color-mix(in srgb,var(--accent) 3%,var(--surface));}.task-marker{width:14px;height:14px;border:1px solid var(--text-2);border-radius:50%;display:grid;place-items:center;color:var(--surface);}.task-marker.finished{background:var(--ok);border-color:var(--ok);}.live .task-marker{border-color:var(--accent);box-shadow:inset 0 0 0 3px var(--surface);background:var(--accent);}.task[data-tone=ask] .task-marker{border-color:var(--warn);background:var(--warn);}.task-content{min-width:0;}.title{font-weight:500;font-size:14px;line-height:1.5;color:var(--text);text-decoration:none;overflow-wrap:anywhere;}.title:hover{text-decoration:underline;}.title::after{content:'';position:absolute;inset:0;}.title:focus-visible{outline:none;}.title:focus-visible::after{outline:2px solid var(--accent);outline-offset:2px;}.st,.detail,.work-detail,.meta,.dependency{font-size:12px;line-height:1.5;color:var(--text-2);margin:4px 0 0;overflow-wrap:anywhere;}.st{display:flex;align-items:center;gap:6px;}.st .dot{width:6px;height:6px;}.dependency{color:var(--block-text);position:relative;z-index:1;}.dependency a{color:inherit;}.detail,.work-detail{display:-webkit-box;-webkit-line-clamp:1;line-clamp:1;-webkit-box-orient:vertical;overflow:hidden;}.meta{display:flex;gap:0;flex-wrap:wrap;}.meta span+span::before{content:'·';margin:0 6px;}.prio[data-p=high]{color:var(--danger-text);}.row-actions{position:relative;z-index:1;display:flex;gap:8px;align-items:center;flex-wrap:wrap;justify-content:flex-end;max-width:350px;}.acts,.foot{display:flex;gap:6px;align-items:center;flex-wrap:wrap;}.row-actions :global(.btn){font-size:12px;}.settled .task-content{color:var(--text-2);}
 @media(max-width:800px){.task{grid-template-columns:15px minmax(0,1fr);padding:14px 0;align-items:start;}.task-marker{margin-top:4px;}.row-actions{grid-column:2;justify-content:flex-start;max-width:none;}.row-actions :global(.btn){min-height:44px;}}
</style>
