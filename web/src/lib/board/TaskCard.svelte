<script lang="ts">
  import { api } from '../api';
  import { cardStatus, quickChoices, type CardStatus } from '../board';
  import { agentLabel, priorityLabel, type Resolved } from '../execution';
  import Icon from '../Icon.svelte';
  import { choicesOf, describeAnswerFailure, isAllow, isDeny } from '../questions';
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
  const question = $derived(questions[0]);
  const choices = $derived(question ? quickChoices(choicesOf(question)) : []);
  const blockerChoices = $derived(run?.state === 'blocked' ? quickChoices(run.blocker?.options ?? []) : []);
  const agent = $derived(run ? agentLabel(app.agents, run.agentId) : effective.agent ? agentLabel(app.agents, effective.agent) : 'Any agent');
  const canStart = $derived(task.state !== 'done' && (!run || ['completed', 'failed', 'stopped'].includes(run.state)) && task.state !== 'review');
  const href = $derived(taskHref(task.projectId, task.id));

  let busy = $state('');

  async function answer(value: string) {
    if (!question || busy) return;
    busy = value;
    try {
      const settled = await api.answerQuestion(question, value);
      app.resolveQuestion(settled);
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
  class="card task"
  data-tone={status.tone}
  class:live={status.live}
  class:settled={task.state === 'done'}
  class:selected
  draggable={!pending}
  aria-busy={pending}
  ondragstart={(e) => ondragstart(e, task)}
  {ondragend}
>
  <a class="title" {href} draggable="false">{task.title}</a>
  <p class="st"><span class="dot" data-tone={status.tone} class:hollow={status.tone === 'neutral' || status.tone === 'idle'}></span>{pending ? 'Updating task…' : status.text}</p>
  {#if status.detail}<p class="well detail">{status.detail}</p>{/if}

  {#if question}
    <div class="acts">
      {#each choices as c (c)}
        <button class="btn small" class:primary={isAllow(c)} class:danger={isDeny(c)} disabled={!!busy} onclick={() => answer(c)}>{busy === c ? 'Sending…' : c}</button>
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
  {:else if task.state === 'review' && mergeable}
    <div class="acts">
      <button class="btn small primary" onclick={() => onmerge(task)}><Icon name="merge" size={14} />Merge…</button>
      <a class="btn small" {href}>Review</a>
    </div>
  {:else if task.state === 'doing' && run?.state === 'completed'}
    <div class="acts">
      <button class="btn small primary" disabled={pending} onclick={() => onmove(task, 'review')}><Icon name="right" size={14} />Move to Review</button>
    </div>
  {:else if canStart && task.state === 'backlog' && waitingFor.length === 0}
    <div class="acts">
      <button class="btn small" disabled={pending} onclick={() => onstart(task)} aria-label="{run ? 'Run an agent again on' : 'Start an agent on'} “{task.title}”"><Icon name="play" size={12} />{run ? 'Run again' : 'Start agent'}</button>
    </div>
  {:else if canStart && run?.state === 'failed'}
    <div class="acts"><button class="btn small" disabled={pending} onclick={() => onstart(task)} aria-label="Run an agent again on “{task.title}”"><Icon name="play" size={12} />Run again</button></div>
  {/if}

  <div class="mt">
    <span class="chip">{agent}</span>
    {#if effective.priority !== 'normal'}<span class="prio" data-p={effective.priority}>{priorityLabel(effective.priority)}</span>{/if}
  </div>
</li>

<style>
  .task {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 9px;
    padding: 12px;
    cursor: grab;
    transition:
      border-color 0.15s,
      box-shadow 0.15s,
      opacity 0.15s;
  }

  .task:active {
    cursor: grabbing;
  }

  .task:hover {
    border-color: color-mix(in srgb, var(--text-2) 45%, var(--border));
  }

  .live {
    border-color: var(--accent);
    box-shadow:
      var(--card-sh),
      var(--live-ring);
  }

  .live:hover {
    border-color: var(--accent);
  }

  .task[data-tone='ask'] {
    border-color: color-mix(in srgb, var(--warn) 55%, var(--border));
  }

  .selected,
  .selected:hover {
    border-color: var(--text);
    box-shadow:
      var(--card-sh),
      0 0 0 1px var(--text);
  }

  .settled {
    opacity: 0.72;
  }

  .title {
    font-weight: 500;
    font-size: 14px;
    line-height: 1.3;
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

  .st {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--text-2);
    min-width: 0;
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

  .detail {
    display: -webkit-box;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .acts {
    position: relative;
    z-index: 1;
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .mt {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .prio {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
  }

  .prio[data-p='high'] {
    color: var(--danger-text);
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
      font-size: 13px;
    }
  }
</style>
