<script lang="ts">
  import { tick } from 'svelte';
  import { ApiError, api } from '../lib/api';
  import { RunFeed } from '../lib/feed.svelte';
  import FeedView from '../lib/FeedView.svelte';
  import { agentName, runElapsed, runStatus, timeAgo } from '../lib/format';
  import QuestionCard from '../lib/QuestionCard.svelte';
  import RunBadge from '../lib/RunBadge.svelte';
  import { router } from '../lib/router.svelte';
  import { app } from '../lib/state.svelte';
  import { TASK_STATES, TASK_STATE_LABELS, type Run, type TaskState, type Worktree } from '../lib/types';

  const task = $derived(app.allTasks.find((t) => t.id === router.taskId));
  const latest = $derived(app.latestRun[router.taskId]);

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
    api.listTaskRuns(id).then((rs) => (history = rs), (err) => app.handleError(err));
  });

  // ---- the activity stream of the shown run ----
  let feed = $state<RunFeed | null>(null);
  $effect(() => {
    const id = viewId;
    if (!id) {
      feed = null;
      return;
    }
    const f = new RunFeed(id);
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
    if (id) api.getWorktree(id).then((w) => (worktree = w), () => (worktree = null));
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
    if (t) app.upsertTask(t);
    else await app.refresh();
  }

  // Start
  let agentId = $state('');
  let instructions = $state('');
  let resume = $state(false);
  const available = $derived(app.agents.filter((a) => a.available));
  $effect(() => {
    if (!available.some((a) => a.id === agentId)) agentId = available[0]?.id ?? '';
  });
  const canResume = $derived(!!latest && !!latest.sessionRef && latest.agentId === agentId);
  $effect(() => {
    if (!canResume) resume = false;
  });

  async function start() {
    if (!task || !agentId) return;
    const r = await act(() => api.startRun(task.id, agentId, instructions.trim(), resume));
    if (r) {
      app.upsertRun(r);
      instructions = '';
      resume = false;
      nearBottom = true;
    }
  }

  // Message
  let message = $state('');
  async function send() {
    const text = message.trim();
    if (!run || !text) return;
    const r = await act(() => api.sendInput(run.id, text));
    if (r) {
      message = '';
      app.upsertRun(r);
      nearBottom = true;
    }
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      void send();
    }
  }

  async function finish() {
    if (!run) return;
    const r = await act(() => api.finishRun(run.id));
    if (r) app.upsertRun(r);
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
    const r = await act(() => api.stopRun(run.id));
    if (r) app.upsertRun(r);
  }

  const pending = $derived(app.questions.filter((q) => q.runId === run?.id));
  const canMessage = $derived(viewingLatest && !!run && (run.state === 'running' || (run.state === 'waiting_for_user' && run.waiting === 'idle')));
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
  <a class="back" href="#/board">← Board</a>

  {#if !task}
    <p class="card empty">{app.projects.length || app.connection !== 'live' ? 'This task was not found.' : 'Loading…'}</p>
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
    </header>

    {#if run && status}
      <section class="card runbar" data-tone={status.tone} aria-label="Agent session">
        <div class="runline">
          <RunBadge {run} />
          <span class="muted">{agentName(app.agents, run.agentId)} · {runElapsed(run, app.now)}</span>
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
        <p class="hint">{status.hint}</p>
        {#if worktree}
          <p class="where">
            <span class="muted">Working in</span> <code>{worktree.branch}</code>
            <button class="btn small quiet" onclick={copyPath} title={worktree.path}>{copied ? 'Copied' : 'Copy path'}</button>
          </p>
        {/if}
        {#if viewingLatest && status.active}
          <div class="actions">
            {#if run.state === 'waiting_for_user' && run.waiting === 'idle'}
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
        {:else}
          <label class="field">
            <span>Agent</span>
            <select class="select" bind:value={agentId}>
              {#each app.agents as a (a.id)}
                <option value={a.id} disabled={!a.available}>
                  {a.name}{a.version ? ` ${a.version}` : ''}{a.available ? '' : ` — ${a.detail ?? 'unavailable'}`}
                </option>
              {/each}
            </select>
          </label>
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
          <button class="btn primary" disabled={busy || !agentId} onclick={start}>{busy ? 'Starting…' : 'Start agent'}</button>
        {/if}
      </section>
    {:else if canStart && isDone}
      <p class="muted center">This task is in Done. Move it back to run an agent on it.</p>
    {/if}
  {/if}
</div>

{#if run && viewingLatest && (pending.length > 0 || canMessage)}
  <div class="dock">
    <div class="dock-inner">
      {#if pending.length > 0}
        <div class="questions">
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
            placeholder={run.state === 'running' ? 'Add a message — it is queued for the agent' : 'Reply to the agent'}
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
