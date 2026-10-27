<script lang="ts">
  import '../../../../internal/uipresentation/static/board.css';
  import '../../../../internal/uipresentation/static/board-drag.js';
  import { runReportStale } from '../format';
  import { assistant } from '../assistant/sessions.svelte';
  import { liveRun } from '../assistant/run-session.svelte';
  import { api } from '../api';
  import { cardStatus, quickChoices, type CardStatus } from '../board';
  import { agentLabel, priorityLabel, type Resolved } from '../execution';
  import Icon from '../Icon.svelte';
  import { spanOf, spanTitle } from '../timeline';
  import { answeredNotice, choicesOf, describeAnswerFailure, isAllow, isDeny } from '../questions';
  import { branchHref } from '../gitroute';
  import { taskHref } from '../router.svelte';
  import type { ProjectScope } from '../scope.svelte';
  import { app } from '../state.svelte';
  import type { GitBranch, Run, Task, TaskState } from '../types';

  // One task on the board: what it is, what its agent is doing or wants, and the one or two
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
    ondragstart,
    ondragend,
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
    ondragstart: (e: DragEvent, task: Task) => void;
    ondragend: () => void;
  } = $props();

  const questions = $derived(run ? scope.pendingFor(run.id) : []);
  const runner = $derived(run?.runnerId ? app.runners.find((r) => r.id === run.runnerId) : undefined);
  const decision = $derived(scope.decisions.find((d) => d.taskId === task.id));
  const waitingFor = $derived(
    (task.orchestration?.dependencies ?? [])
      .map((id) => scope.tasks.find((t) => t.id === id))
      .filter((t): t is Task => !!t && t.state !== 'review' && t.state !== 'done')
      .map((t) => t.title),
  );
  const status: CardStatus = $derived(
    cardStatus(task, run, {
      now: app.now,
      runnerName: app.runners.length > 1 && runner ? (runner.kind === 'local' ? 'this computer' : runner.name) : undefined,
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
  data-board-item={task.id}
  class:live={status.live && !stale}
  class:settled={task.state === 'done'}
  class:selected
  draggable={!pending}
  aria-busy={pending}
  ondragstart={(e) => ondragstart(e, task)}
  {ondragend}
>
  <button type="button" data-drag-handle draggable="false" disabled={pending} aria-label="Move task: {task.title}" title="Drag to a column, or use Left and Right arrows"><svg width="12" height="16" viewBox="0 0 12 16" fill="currentColor" aria-hidden="true"><circle cx="4" cy="3" r="1"/><circle cx="8" cy="3" r="1"/><circle cx="4" cy="8" r="1"/><circle cx="8" cy="8" r="1"/><circle cx="4" cy="13" r="1"/><circle cx="8" cy="13" r="1"/></svg></button>
  <a class="title" {href} draggable={!pending}>{task.title}</a>
  {#if pending || stale || task.state !== 'backlog' || status.text !== 'Not started'}
    <p class="st"><span class="dot" data-tone={status.tone} class:hollow={status.tone === 'neutral' || status.tone === 'idle'}></span>{pending ? 'Updating task…' : `${stale ? 'Last reported: ' : ''}${status.text}${run?.pendingRequests?.includes('stop') ? ' · stop requested' : run?.pendingRequests?.includes('finish') ? ' · finish requested' : ''}`}</p>
  {/if}
  <!-- A work project has no run to describe, so what the task is about is the one other line it has. -->
  {#if !agentWork && task.description}<p class="work-detail one">{task.description}</p>{/if}
  {#if status.detail}<p class="detail">{status.detail}</p>{/if}

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

  <!-- The agent chip says something only when an agent was chosen or has run: "any agent" is every card's default. -->
  {#if hasMeta || demand}
    <div class="foot">
      <p class="meta">
        {#if showAgent}<span>{agent}</span>{/if}
        {#if plan}<span>Planned {spanTitle(plan, new Date(app.now).getFullYear())}</span>{/if}
        {#if effective.priority !== 'normal'}<span class="prio" data-p={effective.priority}>{priorityLabel(effective.priority)}</span>{/if}
      </p>
      {#if demand === 'start'}
        <button class="btn small quiet demand" disabled={pending} onclick={() => onstart(task)} aria-label="{run ? 'Run an agent again on' : 'Start an agent on'} “{task.title}”"><Icon name="play" size={12} />{run ? 'Run again' : 'Start agent'}</button>
      {:else if demand === 'again'}
        <button class="btn small quiet demand" disabled={pending} onclick={() => onstart(task)} aria-label="Run an agent again on “{task.title}”"><Icon name="play" size={12} />Run again</button>
      {:else if demand === 'archive'}
        <button class="btn small quiet demand" disabled={!!busy || pending} onclick={archive}>{busy === 'archive' ? 'Archiving…' : 'Archive'}</button>
      {/if}
    </div>
  {/if}
{#if run && liveRun(run)}
  <div class="acts"><button class="btn small session-action" onclick={e => { e.stopPropagation(); assistant.openRun(task.projectId, run.id); }}>Open Session</button></div>
{/if}
</li>

<style>
  .task {
    --work-border: var(--border);
    --work-surface: var(--surface);
    --work-muted: var(--text-2);
    position: relative;
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 10px 12px;
    border-radius: var(--radius);
    cursor: grab;
    transition:
      border-color 0.15s,
      opacity 0.15s;
  }

  .task:active {
    cursor: grabbing;
  }

  .task:hover {
    border-color: color-mix(in srgb, var(--text-2) 45%, var(--border));
  }

  /* A state is a thin edge, not a ring or a coloured border: running is cobalt, waiting on you is amber. */
  .live::before,
  .task[data-tone='ask']::before {
    content: '';
    position: absolute;
    left: -1px;
    top: 10px;
    bottom: 10px;
    width: 3px;
    border-radius: 0 3px 3px 0;
    background: var(--accent);
  }

  .task[data-tone='ask']::before {
    background: var(--amber);
  }

  .selected,
  .selected:hover {
    border-color: var(--text);
  }

  .settled {
    opacity: 0.7;
  }

  .title {
    padding-right: 18px;
    font-weight: 500;
    font-size: 14px;
    line-height: 1.35;
    color: var(--text);
    text-decoration: none;
    overflow-wrap: anywhere;
  }

  /* The whole card opens the task; its buttons sit above that. */
  .title::after {
    content: '';
    position: absolute;
    inset: 0;
    border-radius: var(--radius);
  }

  .title:focus-visible {
    outline: none;
  }

  .title:focus-visible::after {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  /* The grip is there when the card is pointed at, focused or selected, and always on a touch screen. */
  [data-drag-handle] {
    opacity: 0;
    transition: opacity 0.12s;
  }

  .task:hover [data-drag-handle],
  .task:focus-within [data-drag-handle],
  .selected [data-drag-handle] {
    opacity: 1;
  }

  .st {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    color: var(--text-2);
    min-width: 0;
  }

  .st .dot {
    width: 7px;
    height: 7px;
  }

  .st .dot.hollow[data-tone='neutral'] {
    opacity: 0.45;
  }

  .st .dot[data-tone='work'] {
    animation: pulse 2.4s ease-in-out infinite;
  }

  .st .dot[data-tone='idle'] {
    background: transparent;
    box-shadow: inset 0 0 0 2px var(--accent);
  }

  .detail,
  .work-detail {
    font-size: 13px;
    line-height: 1.45;
    color: var(--text-2);
    overflow-wrap: anywhere;
  }

  .detail {
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .work-detail.one {
    display: -webkit-box;
    -webkit-line-clamp: 1;
    line-clamp: 1;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .acts {
    position: relative;
    z-index: 1;
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 2px;
  }

  .foot {
    position: relative;
    z-index: 1;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    min-height: 0;
  }

  .meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    min-width: 0;
    font-size: 12px;
    color: var(--text-2);
  }

  .meta span + span::before {
    content: '·';
    margin: 0 6px;
  }

  .prio[data-p='high'] {
    color: var(--danger-text);
  }

  /* What can be done but need not be: a quiet text control at the end of the footer. */
  .demand {
    margin-left: auto;
    margin-right: -6px;
    padding: 0 8px;
  }

  /* The answer is the point of the card: a thumb must not miss it. */
  @media (max-width: 899px), (pointer: coarse) {
    .acts :global(.btn),
    .demand {
      min-height: 44px;
    }
  }

  @media (pointer: coarse) {
    .task {
      padding: 14px;
      cursor: default;
    }

    .title {
      font-size: 16px;
    }

    .st {
      font-size: 14px;
    }

    [data-drag-handle] {
      opacity: 1;
    }
  }
</style>
