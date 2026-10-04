<script lang="ts">
  import { tick } from 'svelte';
  import { ApiError, api } from '../lib/api';
  import { RunFeed } from '../lib/feed.svelte';
  import FeedView from '../lib/FeedView.svelte';
  import { agentName, runElapsed, runStatus, timeAgo } from '../lib/format';
  import { agentLabel, compact, optionLabel, priorityLabel, resolveFor, sourceLabel, summaryLine } from '../lib/execution';
  import ExecutionFields from '../lib/ExecutionFields.svelte';
  import { blockerLine, interactionLabel, isNotable } from '../lib/policy';
  import QuestionCard from '../lib/QuestionCard.svelte';
  import RunBadge from '../lib/RunBadge.svelte';
  import { globalHref, projectHref, router } from '../lib/router.svelte';
  import type { ProjectScope } from '../lib/scope.svelte';
  import { app } from '../lib/state.svelte';
  import {
    TASK_STATES,
    TASK_STATE_LABELS,
    type ExecutionConfig,
    type Project,
    type Run,
    type TaskState,
    type Worktree,
  } from '../lib/types';

  let { scope, project }: { scope: ProjectScope; project: Project } = $props();

  // Everything on this page belongs to one project, and is read and changed through it.
  const task = $derived(scope.tasks.find((t) => t.id === router.taskId));
  const latest = $derived(scope.latestRun[router.taskId]);
  /** What this task's runs get: its own overrides, then the project's defaults, then yours. */
  const effective = $derived(resolveFor(task?.execution, project.execution, app.globalExecution));
  /** What it would get if the task itself set nothing: shown as "Same as …" while editing. */
  const below = $derived(resolveFor({}, project.execution, app.globalExecution));

  // ---- which run is shown: the latest, unless the user picked an earlier one ----
  let history = $state<Run[]>([]);
  let picked = $state('');
  const viewId = $derived(picked && history.some((r) => r.id === picked) ? picked : (latest?.id ?? ''));
  const run = $derived(viewId === latest?.id ? latest : history.find((r) => r.id === viewId));
  const status = $derived(run ? runStatus(run) : undefined);
  const viewingLatest = $derived(!!run && run.id === latest?.id);

  $effect(() => {
    const id = router.taskId;
    void latest?.id; // a new run makes a new entry in the history
    picked = '';
    if (!id) return;
    api.listTaskRuns(project.id, id).then((rs) => (history = rs), (err) => app.handleError(err));
  });

  // ---- the activity stream of the shown run ----
  let feed = $state<RunFeed | null>(null);
  $effect(() => {
    const id = viewId;
    if (!id) {
      feed = null;
      return;
    }
    const f = new RunFeed(project.id, id);
    feed = f;
    const stopWatching = app.watchRun(id, (ev) => f.push(ev));
    const stopReconnect = app.onReconnect(() => void f.load());
    void f.load();
    return () => {
      stopWatching();
      stopReconnect();
    };
  });

  // ---- where the work is ----
  let worktree = $state<Worktree | null>(null);
  $effect(() => {
    const id = run?.worktreeId;
    worktree = null;
    if (id) api.getWorktree(project.id, id).then((w) => (worktree = w), () => (worktree = null));
  });

  // ---- keeping the newest activity in view, unless the reader scrolled away ----
  let nearBottom = $state(true);
  function trackScroll() {
    nearBottom = window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 140;
  }
  function toBottom() {
    window.scrollTo({ top: document.documentElement.scrollHeight, behavior: 'smooth' });
  }
  $effect(() => {
    void feed?.items;
    if (nearBottom) void tick().then(() => window.scrollTo({ top: document.documentElement.scrollHeight }));
  });
  $effect(() => {
    window.addEventListener('scroll', trackScroll, { passive: true });
    return () => window.removeEventListener('scroll', trackScroll);
  });

  // ---- actions ----
  let actionError = $state('');
  let busy = $state(false);

  async function act<T>(fn: () => Promise<T>): Promise<T | undefined> {
    busy = true;
    actionError = '';
    try {
      return await fn();
    } catch (err) {
      actionError = err instanceof Error ? err.message : String(err);
      if (err instanceof ApiError && err.status === 401) app.handleError(err);
      return undefined;
    } finally {
      busy = false;
    }
  }

  async function move(state: TaskState) {
    if (!task || state === task.state) return;
    const t = await act(() => api.moveTask(task, state));
    if (t) scope.upsertTask(t);
    else await scope.load().catch((err) => app.handleError(err));
  }

  // Edit the task: its words, and how its runs are carried out.
  let editing = $state(false);
  let editTitle = $state('');
  let editDescription = $state('');
  let editExecution = $state<ExecutionConfig>({});
  function startEditing() {
    if (!task) return;
    editTitle = task.title;
    editDescription = task.description;
    editExecution = { ...task.execution };
    editing = true;
  }
  async function saveEdit() {
    if (!task || !editTitle.trim()) return;
    const t = await act(() =>
      api.editTask(task, { title: editTitle.trim(), description: editDescription, execution: compact(editExecution) }),
    );
    if (t) {
      scope.upsertTask(t);
      editing = false;
    } else if (actionError.includes('modified') || actionError.includes('version')) {
      await scope.load().catch((err) => app.handleError(err));
    }
  }

  // Start
  let instructions = $state('');
  let resume = $state(false);
  /** What differs for this run only; empty means it gets what the task gets. */
  let runChoice = $state<ExecutionConfig>({});
  let showRunOptions = $state(false);
  let runChoiceFor = '';
  const available = $derived(app.agents.filter((a) => a.available));
  // A new task starts from a clean choice.
  $effect(() => {
    const id = task?.id ?? '';
    if (id !== runChoiceFor) {
      runChoiceFor = id;
      runChoice = {};
    }
  });
  /** The agent the run will use: chosen here, else the task's effective one, else the first that works. */
  const startAgent = $derived(runChoice.agent || effective.agent || available[0]?.id || '');
  const startingAgentReady = $derived(available.some((a) => a.id === startAgent));
  const canResume = $derived(!!latest && !!latest.sessionRef && latest.agentId === startAgent);
  $effect(() => {
    if (!canResume) resume = false;
  });

  async function start() {
    if (!task || !startingAgentReady) return;
    const c = compact(runChoice);
    const r = await act(() =>
      api.startRun(
        project.id,
        task.id,
        { agentId: c.agent ?? '', model: c.model, reasoning: c.reasoning, interaction: c.interaction },
        instructions.trim(),
        resume,
      ),
    );
    if (r) {
      scope.upsertRun(r);
      instructions = '';
      resume = false;
      runChoice = {};
      showRunOptions = false;
      nearBottom = true;
    }
  }

  // Message
  let message = $state('');
  async function sendText(text: string) {
    text = text.trim();
    if (!run || !text) return;
    const r = await act(() => api.sendInput(project.id, run.id, text));
    if (r) {
      message = '';
      scope.upsertRun(r);
      nearBottom = true;
    }
  }
  const send = () => sendText(message);
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      void send();
    }
  }

  async function finish() {
    if (!run) return;
    const r = await act(() => api.finishRun(project.id, run.id));
    if (r) scope.upsertRun(r);
  }

  // Stopping loses the agent's current work in progress, so it asks twice.
  let confirmingStop = $state(false);
  let stopTimer: ReturnType<typeof setTimeout> | undefined;
  async function stop() {
    if (!run) return;
    if (!confirmingStop) {
      confirmingStop = true;
      stopTimer = setTimeout(() => (confirmingStop = false), 4000);
      return;
    }
    clearTimeout(stopTimer);
    confirmingStop = false;
    const r = await act(() => api.stopRun(project.id, run.id));
    if (r) scope.upsertRun(r);
  }

  const pending = $derived(run ? scope.pendingFor(run.id) : []);
  const isBlocked = $derived(viewingLatest && run?.state === 'blocked');
  const canMessage = $derived(
    viewingLatest &&
      !!run &&
      (run.state === 'running' || run.state === 'blocked' || (run.state === 'waiting_for_user' && run.waiting === 'idle')),
  );
  const canStart = $derived(!latest || !runStatus(latest).active);
  const isDone = $derived(task?.state === 'done');

  let copied = $state(false);
  async function copyPath() {
    if (!worktree) return;
    try {
      await navigator.clipboard.writeText(worktree.path);
      copied = true;
      setTimeout(() => (copied = false), 1500);
    } catch {
      // Clipboard not available; the path is visible to copy by hand.
    }
  }
</script>

<div class="page">
  <a class="back" href={projectHref(project.id, 'board')}>← Board</a>

  {#if !task}
    <p class="card empty">{scope.loaded ? 'This task was not found in this project.' : 'Loading…'}</p>
  {:else}
    <header class="head">
      <div class="title-row">
        <h2 class="title">{task.title}</h2>
        <label class="move">
          <span class="visually-hidden">Move this task to</span>
          <select class="select" value={task.state} disabled={busy} onchange={(e) => move(e.currentTarget.value as TaskState)}>
            {#each TASK_STATES as s (s)}
              <option value={s}>{TASK_STATE_LABELS[s]}</option>
            {/each}
          </select>
        </label>
      </div>
      {#if task.description}
        <details class="desc">
          <summary>Description</summary>
          <p>{task.description}</p>
        </details>
      {/if}
      <p class="interaction">
        <span class="muted">Runs with:</span>
        <strong>{summaryLine(effective, app.agents, app.agentOptions) || 'the first agent that works'}</strong>
        <span class="muted">· {interactionLabel({ interaction: effective.interaction })}{effective.priority !== 'normal' ? ` · ${priorityLabel(effective.priority)} priority` : ''}</span>
        <button class="btn small quiet" onclick={() => (editing ? (editing = false) : startEditing())}>{editing ? 'Cancel' : 'Edit task'}</button>
      </p>
      {#if editing}
        <form
          class="card edit"
          onsubmit={(e) => {
            e.preventDefault();
            void saveEdit();
          }}
        >
          <label class="field">
            <span>Title</span>
            <input class="input" maxlength="200" bind:value={editTitle} required />
          </label>
          <label class="field">
            <span>Description</span>
            <textarea class="input" rows="3" bind:value={editDescription}></textarea>
          </label>
          <ExecutionFields bind:value={editExecution} inherited={below} idPrefix="edit-task" />
          <p class="muted small">A change applies to runs started after it; a run that is already working keeps its own.</p>
          <div class="actions">
            <button class="btn primary" type="submit" disabled={busy || !editTitle.trim()}>Save</button>
            <button class="btn" type="button" onclick={() => (editing = false)}>Cancel</button>
          </div>
        </form>
      {/if}
    </header>

    {#if run && status}
      <section class="card runbar" data-tone={status.tone} aria-label="Agent session">
        <div class="runline">
          <RunBadge {run} />
          <span class="muted">{agentName(app.agents, run.agentId)}{run.model ? ` · ${optionLabel(app.agentOptions.get(run.agentId)?.models, run.model)}` : ''}{run.reasoning ? ` · ${optionLabel(app.agentOptions.get(run.agentId)?.reasoning, run.reasoning)}` : ''} · {runElapsed(run, app.now)}</span>
          {#if history.length > 1}
            <label class="which">
              <span class="visually-hidden">Show run</span>
              <select class="select" value={viewId} onchange={(e) => (picked = e.currentTarget.value)}>
                {#each [...history].reverse() as r, i (r.id)}
                  <option value={r.id}>{i === 0 ? 'Latest' : `Earlier`} · {runStatus(r).label} · {timeAgo(r.createdAt, app.now)}</option>
                {/each}
              </select>
            </label>
          {/if}
        </div>
        {#if isNotable(run.policy) || run.policy.interaction !== effective.interaction}
          <p class="small muted">This run: {interactionLabel(run.policy)}</p>
        {/if}
        {#if run.state === 'blocked' && run.blocker}
          <div class="blocker" role="group" aria-label="Why the run is blocked">
            <p class="what">{blockerLine(run)}</p>
            {#if run.blocker.detail}<p class="detail">{run.blocker.detail}</p>{/if}
            {#if run.blocker.options?.length && isBlocked}
              <div class="options">
                {#each run.blocker.options as o (o)}
                  <button class="btn" disabled={busy} onclick={() => sendText(o)}>{o}</button>
                {/each}
              </div>
            {/if}
            <p class="small muted">
              {run.blocker.source === 'question' ? 'The agent asked, and this run does not put questions to you.' : 'The agent reported that it could not go on.'}
              Raised {timeAgo(run.blocker.raisedAt, app.now)}.
            </p>
          </div>
        {:else}
          <p class="hint">{status.hint}</p>
        {/if}
        {#if worktree}
          <p class="where">
            <span class="muted">Working in</span> <code>{worktree.branch}</code>
            <button class="btn small quiet" onclick={copyPath} title={worktree.path}>{copied ? 'Copied' : 'Copy path'}</button>
          </p>
        {/if}
        {#if viewingLatest && status.active}
          <div class="actions">
            {#if (run.state === 'waiting_for_user' && run.waiting === 'idle') || run.state === 'blocked'}
              <button class="btn small" disabled={busy} onclick={finish}>Finish session</button>
            {/if}
            <button class="btn small danger" class:armed={confirmingStop} disabled={busy} onclick={stop}>
              {confirmingStop ? 'Tap again to stop' : 'Stop'}
            </button>
          </div>
        {/if}
      </section>
    {/if}

    {#if actionError}
      <p class="error banner" role="alert">{actionError}</p>
    {/if}

    {#if feed}
      {#if feed.loading}
        <p class="muted center">Loading activity…</p>
      {:else if feed.items.length === 0}
        <p class="muted center">Nothing has happened in this run yet.</p>
      {/if}
      <FeedView items={feed.items} hasMore={feed.hasMore} loadingOlder={feed.loadingOlder} onOlder={() => feed?.loadOlder()} />
      {#if feed.error}<p class="error" role="alert">{feed.error}</p>{/if}
    {/if}

    {#if canStart && !isDone}
      <section class="card start" aria-label="Run an agent">
        <h2>{latest ? 'Run it again' : 'Run an agent on this task'}</h2>
        <p class="muted">
          The agent works in its own Git worktree on a new branch, so your checkout is not touched.
        </p>
        {#if available.length === 0}
          <p class="error">
            No coding agent is ready. {app.agents.map((a) => `${a.name}: ${a.detail ?? 'unavailable'}`).join(' · ')}
          </p>
          {#each app.agents.filter((a) => a.guidance) as a (a.id)}<p class="muted small">{a.name}: {a.guidance}</p>{/each}
          <p class="small"><a href={globalHref('settings')}>Open Settings</a> to set up an agent.</p>
        {:else}
          <p class="muted small">
            This run uses <strong>{summaryLine(resolveFor(task.execution, project.execution, app.globalExecution, compact(runChoice)), app.agents, app.agentOptions) || `${agentLabel(app.agents, startAgent)} with its own defaults`}</strong>
            {#if !runChoice.agent && effective.agent}<span>(chosen by {sourceLabel(effective.sources.agent)})</span>{/if}.
          </p>
          <button type="button" class="options-toggle" aria-expanded={showRunOptions} onclick={() => (showRunOptions = !showRunOptions)}>
            {showRunOptions ? 'Hide options' : 'Change for this run'}
          </button>
          {#if showRunOptions}
            <ExecutionFields bind:value={runChoice} inherited={effective} idPrefix="run" showPriority={false} />
          {/if}
          <label class="field">
            <span>Extra instructions <span class="muted">(optional)</span></span>
            <textarea class="input" rows="2" placeholder="Anything to add to the task…" bind:value={instructions}></textarea>
          </label>
          {#if canResume}
            <label class="check">
              <input type="checkbox" bind:checked={resume} />
              <span>Continue the previous conversation</span>
            </label>
          {/if}
          <button class="btn primary" disabled={busy || !startingAgentReady} onclick={start}>{busy ? 'Starting…' : 'Start agent'}</button>
          {#if !startingAgentReady && startAgent}
            <p class="error">{agentLabel(app.agents, startAgent)} cannot be used right now: {app.agents.find((a) => a.id === startAgent)?.detail ?? 'unavailable'}. Change the agent above, or fix it under Settings.</p>
          {/if}
        {/if}
      </section>
    {:else if canStart && isDone}
      <p class="muted center">This task is in Done. Move it back to run an agent on it.</p>
    {/if}
  {/if}
</div>

{#if run && viewingLatest && (pending.length > 0 || canMessage)}
  <div class="dock" class:asking={pending.length > 0} class:blocked={isBlocked && pending.length === 0}>
    <div class="dock-inner">
      {#if pending.length > 0}
        <div class="questions" role="region" aria-label="The agent is waiting for your answer">
          {#each pending as q (q.id)}
            <QuestionCard question={q} />
          {/each}
        </div>
      {:else}
        <form
          class="composer"
          onsubmit={(e) => {
            e.preventDefault();
            void send();
          }}
        >
          <label class="visually-hidden" for="message">Message to the agent</label>
          <textarea
            id="message"
            class="input"
            rows="1"
            placeholder={isBlocked
              ? 'Tell the agent how to proceed'
              : run.state === 'running'
                ? 'Add a message — it is queued for the agent'
                : 'Reply to the agent'}
            bind:value={message}
            onkeydown={onKey}
          ></textarea>
          <button class="btn primary" type="submit" disabled={busy || !message.trim()}>Send</button>
        </form>
      {/if}
    </div>
  </div>
{/if}

{#if !nearBottom && feed && feed.items.length > 0}
  <button class="jump btn small" onclick={toBottom}>↓ Latest</button>
{/if}

<style>
  .options-toggle {
    justify-self: start;
    min-height: 36px;
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--accent);
    font-weight: 550;
  }

  .page {
    display: grid;
    gap: 14px;
    max-width: 52rem;
    padding-bottom: 12px;
  }

  .back {
    justify-self: start;
    font-weight: 550;
    text-decoration: none;
    min-height: 32px;
    display: inline-flex;
    align-items: center;
  }

  .head {
    display: grid;
    gap: 8px;
  }

  .title-row {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    justify-content: space-between;
  }

  .title {
    font-size: 1.25rem;
    font-weight: 650;
    text-transform: none;
    letter-spacing: normal;
    color: var(--text);
    overflow-wrap: anywhere;
  }

  .move .select {
    width: auto;
    min-height: 34px;
    font-size: 0.85rem;
  }

  .desc summary {
    cursor: pointer;
    color: var(--text-2);
    font-size: 0.85rem;
  }

  .desc p {
    margin-top: 6px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .runbar {
    display: grid;
    gap: 8px;
    padding: 12px;
    border-left: 3px solid var(--text-2);
  }

  .runbar[data-tone='work'],
  .runbar[data-tone='idle'] {
    border-left-color: var(--accent);
  }

  .runbar[data-tone='ask'] {
    border-left-color: var(--warn);
  }

  .runbar[data-tone='block'] {
    border-left-color: var(--block);
  }

  .interaction {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 2px 8px;
    font-size: 0.85rem;
  }

  .small {
    font-size: 0.8rem;
  }

  .edit {
    display: grid;
    gap: 12px;
    padding: 14px;
  }

  /* Why the run stopped rather than guess, and the choices the agent saw. */
  .blocker {
    display: grid;
    gap: 8px;
    padding: 10px 12px;
    border-radius: var(--radius-sm);
    background: color-mix(in srgb, var(--block) 9%, var(--surface));
  }

  .blocker .what {
    font-weight: 600;
    overflow-wrap: anywhere;
  }

  .blocker .detail {
    font-size: 0.88rem;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .options {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .runbar[data-tone='ok'] {
    border-left-color: var(--ok);
  }

  .runbar[data-tone='bad'] {
    border-left-color: var(--danger);
  }

  .runline {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 12px;
  }

  .which {
    margin-left: auto;
  }

  .which .select {
    width: auto;
    min-height: 32px;
    font-size: 0.8rem;
  }

  .hint {
    font-size: 0.88rem;
    color: var(--text-2);
    overflow-wrap: anywhere;
  }

  .where {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 8px;
    font-size: 0.85rem;
  }

  .where code {
    overflow-wrap: anywhere;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .btn.armed {
    background: var(--danger);
    border-color: var(--danger);
    color: #fff;
  }

  .banner {
    padding: 8px 10px;
    border: 1px solid color-mix(in srgb, var(--danger) 40%, var(--border));
    border-radius: var(--radius-sm);
  }

  .center {
    text-align: center;
    padding: 12px 0;
  }

  .start {
    display: grid;
    gap: 12px;
    padding: 14px;
  }

  .field {
    display: grid;
    gap: 4px;
    font-size: 0.9rem;
    font-weight: 550;
  }

  .field .muted {
    font-weight: 400;
  }

  .check {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 0.9rem;
  }

  .check input {
    width: 18px;
    height: 18px;
  }

  /* The reply box and any questions stay within thumb reach, above the tab bar. */
  .dock {
    position: sticky;
    bottom: calc(var(--tabbar-h) + env(safe-area-inset-bottom));
    z-index: 4;
    margin: 0 -16px;
    padding: 10px 16px;
    background: color-mix(in srgb, var(--bg) 92%, transparent);
    backdrop-filter: blur(10px);
    border-top: 1px solid var(--border);
  }

  .dock-inner {
    max-width: 52rem;
  }

  /* An agent is blocked: the dock is an alert, not a reply box. */
  .dock.asking {
    border-top: 3px solid var(--warn);
    background: color-mix(in srgb, var(--warn) 8%, var(--bg));
  }

  .dock.blocked {
    border-top: 3px solid var(--block);
    background: color-mix(in srgb, var(--block) 7%, var(--bg));
  }

  .questions {
    display: grid;
    gap: 14px;
    max-height: 55dvh;
    overflow-y: auto;
  }

  .composer {
    display: flex;
    gap: 8px;
    align-items: flex-end;
  }

  .composer .input {
    flex: 1;
    min-height: 42px;
    max-height: 40dvh;
    resize: none;
    field-sizing: content;
  }

  .jump {
    position: fixed;
    right: 16px;
    bottom: calc(var(--tabbar-h) + env(safe-area-inset-bottom) + 80px);
    z-index: 4;
    background: var(--surface);
    box-shadow: 0 2px 8px rgb(0 0 0 / 0.18);
    border-radius: 999px;
  }

  @media (min-width: 900px) {
    .dock {
      bottom: 0;
      margin: 0 -24px;
      padding: 12px 24px;
    }

    .jump {
      bottom: 90px;
    }
  }
</style>
