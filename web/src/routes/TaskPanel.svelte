<script lang="ts">
  import { onMount, tick, untrack } from 'svelte';
  import { MediaQuery } from 'svelte/reactivity';
  import { ApiError, api } from '../lib/api';
  import { BUSY_RUNNERS, agentLabel, compact, executionAgents, onlyBusy, optionLabel, priorityLabel, resolveFor, sourceLabel, summaryLine } from '../lib/execution';
  import ExecutionFields from '../lib/ExecutionFields.svelte';
  import { RunFeed } from '../lib/feed.svelte';
  import FeedView from '../lib/FeedView.svelte';
  import { agentName, runElapsed, runStatus, timeAgo } from '../lib/format';
  import Icon from '../lib/Icon.svelte';
  import { blockerLine, interactionLabel, isNotable } from '../lib/policy';
  import QuestionCard from '../lib/QuestionCard.svelte';
  import ConnectionBanner from '../lib/ConnectionBanner.svelte';
  import RunBadge from '../lib/RunBadge.svelte';
  import RunHandoff from '../lib/RunHandoff.svelte';
  import RunReviewSummary from '../lib/RunReviewSummary.svelte';
  import RunUsage from '../lib/RunUsage.svelte';
import { globalHref, projectHref, router, taskHref } from '../lib/router.svelte';
  import { schedulingLabels } from '../lib/scheduling';
  import type { ProjectScope } from '../lib/scope.svelte';
  import { app } from '../lib/state.svelte';
  import EditTaskSheet from '../lib/task/EditTaskSheet.svelte';
  import { agentWorkable } from '../lib/board';
  import LabelChip from '../lib/LabelChip.svelte';
  import { labelsOf, modeLabel, modeOf } from '../lib/labels';
  import { spanOf, spanTitle } from '../lib/timeline';
  import ScheduleSheet from '../lib/task/ScheduleSheet.svelte';
  import { TASK_STATES, TASK_STATE_LABELS, type ExecutionConfig, type Project, type Run, type Task, type TaskState, type Worktree } from '../lib/types';

  // A task, opened over its board: the run and the conversation on the left, the facts about the
  // task on the right. What needs you (a question, a blocker) comes first. Esc closes.

  let { scope, project }: { scope: ProjectScope; project: Project } = $props();

  const task = $derived(scope.tasks.find((t) => t.id === router.taskId));
  const latest = $derived(scope.latestRun[router.taskId]);
  /** What this task's runs get: its own overrides, then the project's defaults, then yours. */
  const effective = $derived(resolveFor(task?.execution, project.execution, app.globalExecution));
  /** What it would get if the task itself set nothing. */
  const below = $derived(resolveFor({}, project.execution, app.globalExecution));
  const boardHref = $derived(projectHref(project.id, 'board'));

  function close() {
    location.hash = boardHref;
  }

  // ---- which run is shown: the latest, unless the user picked an earlier one ----
  let history = $state<Run[]>([]);
  let picked = $state('');
  const viewId = $derived(picked && history.some((r) => r.id === picked) ? picked : (latest?.id ?? ''));
  const run = $derived(viewId === latest?.id ? latest : history.find((r) => r.id === viewId));
  const status = $derived(run ? runStatus(run) : undefined);
  const viewingLatest = $derived(!!run && run.id === latest?.id);
  const reviewFirst = $derived(task?.state === 'review' && !!run && !!status && !status.active && viewingLatest);

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
  // Keep the end of the conversation visible when new activity arrives.
  let scroller = $state<HTMLElement>();
  let feedEnd = $state<HTMLElement>();
  let nearBottom = $state(true);
  function trackScroll() {
    if (!scroller || !feedEnd) return;
    nearBottom = scroller.scrollTop + scroller.clientHeight >= feedEnd.offsetTop - 140;
  }
  function toBottom(smooth = true) {
    if (!scroller || !feedEnd) return;
    scroller.scrollTo({ top: Math.max(0, feedEnd.offsetTop - scroller.clientHeight + 24), behavior: smooth ? 'smooth' : 'auto' });
  }
  $effect(() => {
    void feed?.items;
    if (status && !status.active) return;
    if (untrack(() => nearBottom)) void tick().then(() => toBottom(false));
  });

  let viewedStatus = '';
  $effect(() => {
    const id = run?.id;
    const active = status?.active;
    const key = `${id}:${active}`;
    if (key === viewedStatus) return;
    viewedStatus = key;
    if (id && active === false) void tick().then(() => scroller?.scrollTo({ top: 0, behavior: 'auto' }));
  });

  // ---- focus: into the panel when it opens or shows another task, and back to where it came from when it closes ----
  let titleEl = $state<HTMLElement>();
  let detailsEl = $state<HTMLElement>();
  let focusedFor = '';
  $effect(() => {
    const id = titleEl ? task?.id : undefined;
    if (!id || id === focusedFor) return;
    focusedFor = id;
    titleEl?.focus({ preventScroll: true });
  });
  onMount(() => {
    const active = document.activeElement;
    const opener = active instanceof HTMLElement && active !== document.body && !active.closest('.panel') ? active : undefined;
    const openerHref = taskHref(project.id, router.taskId);
    return () => {
      // Only when focus was left in the panel (or lost with it); someone who moved on elsewhere keeps their place.
      const focused = document.activeElement;
      if (!focused || focused === document.body || focused.closest('.panel')) {
        const boardOpener = opener?.isConnected ? opener : [...document.querySelectorAll<HTMLAnchorElement>('a[href]')]
          .find((anchor) => anchor.getAttribute('href') === openerHref);
        if (boardOpener?.isConnected && !boardOpener.closest('.panel')) boardOpener.focus({ preventScroll: true });
      }
    };
  });
  /** Skip links are buttons: the address bar is the router, so an `href="#…"` would navigate away from the task. */
  function skipTo(el: HTMLElement | undefined) {
    el?.focus();
    el?.scrollIntoView({ block: 'nearest' });
  }

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

  let editing = $state(false);
  let scheduling = $state(false);
  const decision = $derived(scope.decisions.find((d) => d.taskId === task?.id));
  const dependencies = $derived((task?.orchestration?.dependencies ?? []).map((id) => scope.tasks.find((t) => t.id === id)));

  // ---- start ----
  let instructions = $state('');
  let resume = $state(false);
  /** What differs for this run only; empty means it gets what the task gets. */
  let runChoice = $state<ExecutionConfig>({});
  let runChoiceFor = '';
  const available = $derived(executionAgents(app.agents, app.runners, project.id, runChoice.runner || effective.runner));
  // A new task starts from a clean slate.
  $effect(() => {
    const id = task?.id ?? '';
    if (id !== runChoiceFor) {
      runChoiceFor = id;
      runChoice = {};
      untrack(() => {
        editing = false;
        scheduling = false;
        instructions = '';
        message = '';
        nearBottom = true;
      });
    }
  });
  /** The agent the run will use: chosen here, else the task's effective one, else the first that works (or, while every
   *  runner is full, the first that will once a slot frees). */
  const startAgent = $derived(runChoice.agent || effective.agent || available[0]?.id || executionAgents(app.agents, app.runners, project.id, runChoice.runner || effective.runner, true)[0]?.id || '');
  const startingAgentReady = $derived(available.some((a) => a.id === startAgent));
  /** Nothing is wrong with the agent: every runner that has it is full. Say so rather than send the person to Settings. */
  const startBusy = $derived(!startingAgentReady && onlyBusy(app.agents, app.runners, project.id, runChoice.runner || effective.runner, runChoice.agent || effective.agent || ''));
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
        { runnerId: c.runner, agentId: c.agent ?? '', model: c.model, reasoning: c.reasoning, interaction: c.interaction },
        instructions.trim(),
        resume,
      ),
    );
    if (r) {
      scope.upsertRun(r);
      instructions = '';
      resume = false;
      runChoice = {};
      nearBottom = true;
    }
  }

  // ---- message ----
  let message = $state('');
  async function sendText(text: string) {
    text = text.trim();
    if (!run || !text || busy) return;
    const r = await act(() => api.sendInput(project.id, run.id, text));
    if (r) {
      if (r.remote) app.notify('Message queued. Waiting for the runner to acknowledge delivery.');
      message = '';
      scope.upsertRun(r);
      nearBottom = true;
    }
  }
  const send = () => sendText(message);
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      void send();
    }
  }

  async function finish() {
    if (!run) return;
    const r = await act(() => api.finishRun(project.id, run.id));
    if (r) {
      scope.upsertRun(r);
      if (r.remote && !r.endedAt) app.notify('Finish requested. Ownership stays with the runner until it reports the result.');
    }
  }

  // Confirm a stop inline while keeping the control in view.
  let confirmingStop = $state(false);
  let stopTimer: ReturnType<typeof setTimeout> | undefined;
  const stopIdentity = $derived(`${router.taskId}:${latest?.id ?? ''}`);
  $effect(() => {
    void stopIdentity;
    confirmingStop = false;
    return () => clearTimeout(stopTimer);
  });
  async function stopLatest() {
    const stopping = latest;
    if (!stopping || busy) return;
    if (!confirmingStop) {
      confirmingStop = true;
      stopTimer = setTimeout(() => (confirmingStop = false), 4000);
      return;
    }
    clearTimeout(stopTimer);
    confirmingStop = false;
    const r = await act(() => api.stopRun(project.id, stopping.id));
    if (r) {
      scope.upsertRun(r);
      if (r.remote && !r.endedAt) app.notify('Stop requested. Ownership stays with the runner until it reports the result.');
    }
  }

  const pending = $derived(run ? scope.pendingFor(run.id) : []);
  /** The questions the dock below the feed asks, with their answers. The feed and the status note do not say them again. */
  const dockIds = $derived(new Set(run && viewingLatest ? pending.map((q) => q.id) : []));
  const isBlocked = $derived(viewingLatest && run?.state === 'blocked');
  const canMessage = $derived(
    viewingLatest && !!run && (run.state === 'running' || run.state === 'blocked' || (run.state === 'waiting_for_user' && run.waiting === 'idle')),
  );
  /** Human work, and work in a project with no repository, is never handed to an agent. */
  const agentWork = $derived(!!task && agentWorkable(task, project));
  const canStart = $derived(agentWork && !task?.archivedAt && (!latest || !runStatus(latest).active));
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

  // Esc closes the panel, unless a dialog inside it or a field is in use.
  function onWindowKey(e: KeyboardEvent) {
    if (e.key !== 'Escape' || editing || scheduling || app.switcherOpen || app.newTaskOpen) return;
    const el = document.activeElement;
    if (el instanceof HTMLTextAreaElement || (el instanceof HTMLInputElement && el.value)) {
      el.blur();
      return;
    }
    close();
  }

  async function archive(archived: boolean) {
    if (!task || busy) return;
    const saved = await act(() => api.archiveTask(task, archived));
    if (!saved) {
      await scope.load().catch((e) => app.handleError(e));
      return;
    }
    scope.upsertTask(saved);
    if (archived) {
      close();
      app.notify(`Archived “${saved.title}”. Its history stays in the board's Archive.`, 12000, { label: 'Undo', run: () => void undoArchive(saved) });
    }
  }

  /** Puts an archived task back where it was. The panel may be gone by then, so this uses the saved task, not the open one. */
  async function undoArchive(archivedTask: Task) {
    try {
      const restored = await api.archiveTask(archivedTask, false);
      scope.upsertTask(restored);
      app.notify(`Restored “${restored.title}” to ${TASK_STATE_LABELS[restored.state]}.`);
    } catch (e) {
      app.handleError(e);
      await scope.load().catch((err) => app.handleError(err));
    }
  }

  let factsSection = $state<'details' | 'run' | 'schedule' | 'history'>('details');
  // A task that is not agent work has no execution or schedule to show.
  $effect(() => {
    if (!agentWork && (factsSection === 'run' || factsSection === 'schedule')) factsSection = 'details';
    // A work project has no runs: its task has only its details.
    if (project.kind === 'work' && factsSection === 'history') factsSection = 'details';
  });
  const taskLabels = $derived(task ? labelsOf(task, app.labels) : []);
  const planSpan = $derived(task ? spanOf(task) : null);
  let factsOpen = $state(false);
  const narrow = new MediaQuery('(max-width: 1280px)');
  const runWith = $derived(summaryLine(effective, app.agents, app.agentOptions) || 'the first agent that works');
</script>

<svelte:window onkeydown={onWindowKey} />

<a class="scrim" href={boardHref} aria-hidden="true" tabindex="-1"></a>
<section class="panel" aria-labelledby={task ? 'task-title' : undefined} aria-label={task ? undefined : 'Task'}>
  {#if !task}
    <header class="head">
      <a class="btn quiet small icon" href={boardHref} aria-label="Close panel"><Icon name="close" /></a>
    </header>
    <p class="empty">{scope.loaded ? 'This task was not found in this project.' : 'Loading…'}</p>
  {:else}
    <header class="head">
      <div class="skips">
        <button class="skip" onclick={() => skipTo(scroller)}>Skip to activity</button>
        <button class="skip" onclick={() => skipTo(detailsEl)}>Skip to task details</button>
      </div>
      <div class="top">
        <a class="btn quiet small icon back" href={boardHref} aria-label="Back to the board"><Icon name="left" /></a>
        <label class="state">
          <span class="visually-hidden">Task column</span>
          <span class="dot" data-tone={task.state === 'doing' ? 'work' : task.state === 'review' ? 'ok' : task.state === 'done' ? 'ok' : 'neutral'}></span>
          <select value={task.state} disabled={busy || !!task.archivedAt} onchange={(e) => move(e.currentTarget.value as TaskState)}>
            {#each TASK_STATES as s (s)}<option value={s}>{TASK_STATE_LABELS[s]}</option>{/each}
          </select>
        </label>
        <span class="spacer"></span>
        {#if task.archivedAt}
          <span class="chip">Archived</span><button class="btn small" disabled={busy} onclick={() => archive(false)}>Restore task</button>
        {:else}
          {#if latest && runStatus(latest).active}<button class="btn small danger" disabled={busy} onclick={() => { picked = ''; void stopLatest(); }}><Icon name="stop" size={12} />{confirmingStop ? 'Confirm stop' : 'Stop agent'}</button>{/if}
          <button class="btn small" disabled={busy || !!latest && runStatus(latest).active} onclick={() => archive(true)} title="Move this task to the Archive. Its history is kept, and you can undo it.">Archive task</button>
          <button class="btn small" aria-label="Edit task" onclick={() => (editing = true)}>Edit</button>
        {/if}
        <a class="btn quiet small icon close" href={boardHref} aria-label="Close panel" title="Close (Esc)"><Icon name="close" /></a>
      </div>
      <h2 class="title" id="task-title" tabindex="-1" bind:this={titleEl}>{task.title}</h2>
      {#if reviewFirst && run}
        <RunReviewSummary {run} onmerged={() => void move('done')} />
      {/if}
      {#if agentWork}
        <p class="runs-with">
          Runs with <strong>{runWith}</strong>
          <span class="muted">· {interactionLabel({ interaction: effective.interaction })}{effective.priority !== 'normal' ? ` · ${priorityLabel(effective.priority)} priority` : ''}</span>
        </p>
      {:else}
        <p class="runs-with">{project.kind === 'work' ? 'Done by a person. Move it across the board as the work goes.' : 'Done by a person: no agent is started on it.'}</p>
      {/if}
      {#if taskLabels.length}
        <ul class="task-labels" aria-label="Labels">{#each taskLabels as l (l.id)}<li><LabelChip label={l} /></li>{/each}</ul>
      {/if}
    </header>

    {#snippet facts()}
        {#if factsSection === 'details'}
          <section>
            <h3>Details</h3>
            <p class="desc">{task.description || 'No description added.'}</p>
          </section>
          <section>
            <div class="sh"><h3>Plan</h3><button class="btn quiet small" aria-label="Change plan" disabled={!!task.archivedAt} onclick={() => (editing = true)}>Change</button></div>
            <dl class="kv">
              <dt>Who</dt><dd>{modeLabel(modeOf(task))}</dd>
              <dt>When</dt><dd>{planSpan ? `${planSpan.milestone ? 'Milestone · ' : ''}${spanTitle(planSpan, new Date().getFullYear())}` : 'Not planned'}</dd>
              {#if dependencies.length}<dt>After</dt><dd>{dependencies.map((d) => d?.title ?? 'a removed task').join(', ')}</dd>{/if}
            </dl>
          </section>
        {/if}

        {#if factsSection === 'run'}
        <section>
          <div class="sh"><h3>How it runs</h3><button class="btn quiet small" aria-label="Change how it runs" disabled={!!task.archivedAt} onclick={() => (editing = true)}>Change</button></div>
          <dl class="kv">
            <dt>Agent</dt><dd>{effective.agent ? agentLabel(app.agents, effective.agent) : 'First that works'}</dd>
            {#if effective.model}<dt>Model</dt><dd>{optionLabel(app.agentOptions.get(effective.agent)?.models, effective.model)}</dd>{/if}
            {#if effective.reasoning}<dt>Reasoning</dt><dd>{optionLabel(app.agentOptions.get(effective.agent)?.reasoning, effective.reasoning)}</dd>{/if}
            <dt>Runner</dt><dd>{effective.runner && effective.runner !== 'automatic' ? (app.runners.find((r) => r.id === effective.runner)?.name ?? effective.runner) : 'Automatic'}</dd>
            <dt>Asks you</dt><dd>{interactionLabel({ interaction: effective.interaction })}</dd>
            <dt>Priority</dt><dd>{priorityLabel(effective.priority)}</dd>
          </dl>
        </section>

        {/if}
        {#if factsSection === 'schedule'}
        <section>
          <div class="sh"><h3>Schedule</h3><button class="btn quiet small" aria-label={task.orchestration?.enabled ? 'Change schedule' : 'Schedule this task'} disabled={!!task.archivedAt} onclick={() => (scheduling = true)}>{task.orchestration?.enabled ? 'Change' : 'Schedule'}</button></div>
          {#if decision && task.orchestration?.enabled}
            <p class="fact"><strong>{schedulingLabels[decision.state]}</strong> · {decision.reason}</p>
          {:else}
            <p class="fact muted">Runs when you start it.</p>
          {/if}
          {#if task.orchestration?.runId}<p class="note">One-shot schedule dispatched. Save the schedule again to arm another attempt.</p>{/if}
          {#if dependencies.length}
            <p class="fact">Waits for {dependencies.map((t) => t?.title ?? 'a removed task').join(', ')}</p>
          {/if}
        </section>

        {/if}
        {#if factsSection === 'run' && run}
          <section>
            <h3>Where</h3>
            {#if worktree}
              <p class="fact"><code>{worktree.branch}</code></p>
              <button class="btn small" onclick={copyPath} title={worktree.path}><Icon name="copy" size={14} />{copied ? 'Copied' : 'Copy worktree path'}</button>
            {/if}
            <RunUsage {run} />
          </section>
        {/if}

        {#if factsSection === 'history'}
          <section>
            <h3>Runs</h3>
            <ul class="runs">
              {#each [...history].reverse() as r, i (r.id)}
                {@const s = runStatus(r)}
                <li>
                  <button class="run-pick" aria-pressed={r.id === viewId} onclick={() => { picked = r.id; factsOpen = false; }}>
                    <span class="dot" data-tone={s.tone}></span>
                    <span class="rl">{i === 0 ? 'Latest' : `#${r.attempt ?? history.length - i}`} · {agentName(app.agents, r.agentId)}</span>
                    <span class="mm">{timeAgo(r.createdAt, app.now)}</span>
                  </button>
                </li>
              {:else}<li class="muted">No runs yet.</li>{/each}
            </ul>
          </section>
        {/if}
    {/snippet}

    <div class="facts-tabs" role="group" aria-label="Task detail views">
      {#if narrow.current}<button class="btn small quiet" aria-pressed={!factsOpen} onclick={() => factsOpen = false}>Activity</button>{/if}
      {#each agentWork ? ['details', 'run', 'schedule', 'history'] : project.kind === 'work' ? ['details'] : ['details', 'history'] as tab (tab)}<button class="btn small quiet" aria-pressed={(!narrow.current || factsOpen) && factsSection === tab} onclick={() => { factsSection = tab as typeof factsSection; factsOpen = true; }}>{tab === 'run' ? 'Execution' : tab === 'history' ? `Runs (${history.length})` : tab === 'details' ? 'Details' : 'Schedule'}</button>{/each}
    </div>
    <div class="body" class:facts-open={factsOpen}>
      <!-- The run: what needs you, what happened, and what to say next. -->
      <div class="main">
        <ConnectionBanner />
        <div class="scroll" bind:this={scroller} onscroll={trackScroll} tabindex="-1">
          <h3 class="visually-hidden">Activity</h3>
          {#if run && status}
            <div class="runbar" data-tone={status.tone}>
              <RunBadge {run} />
              <span class="mm">
                {agentName(app.agents, run.agentId)}{run.model ? ` · ${optionLabel(app.agentOptions.get(run.agentId)?.models, run.model)}` : ''}{run.reasoning
                  ? ` · ${optionLabel(app.agentOptions.get(run.agentId)?.reasoning, run.reasoning)}`
                  : ''} · {runElapsed(run, app.now)}
              </span>
              {#if viewingLatest && status.active}
                <span class="run-acts">
                  {#if (run.state === 'waiting_for_user' && run.waiting === 'idle') || run.state === 'blocked'}
                    <button class="btn small" disabled={busy} onclick={finish}><Icon name="check" size={14} />Finish</button>
                  {/if}

                </span>
              {/if}
            </div>
            {#if status.active}
              {@const runner = app.runners.find(r => r.id === run.runnerId)}
              <p class="note">{run.remote ? runner?.name ?? 'Remote runner' : 'This computer'} · {app.connection !== 'live' ? 'availability unknown' : runner ? runner.disabled ? 'disabled' : runner.online ? 'online' : 'offline' : 'availability unknown'}{runner?.lastSeenAt ? ` · last seen ${timeAgo(runner.lastSeenAt, app.now)}` : ''}{run.activityAt ? ` · last activity ${timeAgo(run.activityAt, app.now)}` : ' · no activity reported yet'}</p>
              {#if run.pendingRequests?.length}
                <p class="note" role="status" data-testid="pending-requests">Waiting for runner acknowledgment: {run.pendingRequests.map(k => ({ send: 'message delivery', respond: 'answer delivery', stop: 'stop requested', finish: 'finish requested' })[k]).join(' · ')}. The last reported run state still applies.</p>
              {/if}
            {/if}
            {#if isNotable(run.policy) || run.policy.interaction !== effective.interaction}
              <p class="note">This run: {interactionLabel(run.policy)}</p>
            {/if}
            {#if run.state === 'blocked' && run.blocker}
              <div class="blocker" role="group" aria-label="Why the run is blocked">
                <p class="what">{blockerLine(run)}</p>
                {#if run.blocker.detail}<p class="detail">{run.blocker.detail}</p>{/if}
                {#if run.blocker.options?.length && isBlocked}
                  <div class="options">
                    {#each run.blocker.options as o (o)}<button class="btn small" disabled={busy} onclick={() => sendText(o)}>{o}</button>{/each}
                  </div>
                {/if}
                <p class="note">
                  {run.blocker.source === 'question' ? 'The agent asked, and this run does not put questions to you.' : 'The agent reported that it could not go on.'}
                  Raised {timeAgo(run.blocker.raisedAt, app.now)}.
                </p>
              </div>
            {:else if (!status.active || status.tone !== 'work') && !dockIds.size}
              <p class="note">{status.hint}</p>
            {/if}
          {/if}

          {#if actionError}<p class="error banner" role="alert">{actionError}</p>{/if}

          {#if run && status && !status.active && !reviewFirst}
            <RunHandoff {run} {scope} inherited={effective} canContinue={canStart && !isDone && viewingLatest} onmerged={() => void move('done')} />
          {:else if reviewFirst && run}
            <details class="review-evidence">
              <summary>Full run and Git evidence</summary>
              <RunHandoff {run} {scope} inherited={effective} canContinue={canStart && !isDone && viewingLatest} onmerged={() => void move('done')} hideReviewActions />
            </details>
          {/if}

          {#if feed}
            {#if feed.loading}
              <p class="center">Loading activity…</p>
            {:else if feed.items.length === 0}
              <p class="center">Nothing has happened in this run yet.</p>
            {/if}
            <FeedView answeredBelow={dockIds} items={feed.items} hasMore={feed.hasMore} loadingOlder={feed.loadingOlder} onOlder={() => feed?.loadOlder()} />
            {#if feed.error}<p class="error" role="alert">{feed.error}</p>{/if}
          {/if}
          <div class="feed-end" bind:this={feedEnd} aria-hidden="true"></div>


          {#if canStart && !isDone}
            <details class="start" open={!latest}>
              <summary>{latest ? 'Run it again' : 'Run an agent on this task'}</summary>
              {#if available.length === 0 && !startBusy}
                <p class="error">No coding agent is ready. {app.agents.map((a) => `${a.name}: ${a.detail ?? 'unavailable'}`).join(' · ')}</p>
                {#each app.agents.filter((a) => a.guidance) as a (a.id)}<p class="note">{a.name}: {a.guidance}</p>{/each}
                <p class="note"><a href={globalHref('settings')}>Open Settings</a> to set up an agent.</p>
              {:else}
                <p class="note">
                  The agent works in its own Git worktree on a new branch; your checkout is not touched. This run uses
                  <strong>{summaryLine(resolveFor(task.execution, project.execution, app.globalExecution, compact(runChoice)), app.agents, app.agentOptions) ||
                    `${agentLabel(app.agents, startAgent)} with its own defaults`}</strong>{#if !runChoice.agent && effective.agent}<span> (chosen by {sourceLabel(effective.sources.agent)})</span>{/if}.
                </p>
                <ExecutionFields bind:value={runChoice} inherited={effective} idPrefix="run" showPriority={false} dense />
                <label class="field">
                  <span>Extra instructions <span class="muted">optional</span></span>
                  <textarea class="input" rows="2" placeholder="Anything to add to the task…" bind:value={instructions}></textarea>
                </label>
                <div class="start-row">
                  {#if canResume}
                    <label class="check"><input type="checkbox" bind:checked={resume} /><span>Continue the previous conversation</span></label>
                  {/if}
                  <button class="btn primary" disabled={busy || !startingAgentReady} onclick={start}><Icon name="play" size={12} />{busy ? 'Starting…' : 'Start agent'}</button>
                </div>
                {#if startBusy}
                  <p class="note" role="status">{BUSY_RUNNERS} <a href="{globalHref('settings')}?runners">See runners</a></p>
                {:else if !startingAgentReady && startAgent}
                  <p class="error">
                    {agentLabel(app.agents, startAgent)} cannot be used right now: {app.agents.find((a) => a.id === startAgent)?.detail ?? 'unavailable'}. Choose another
                    agent above, or fix it under Settings.
                  </p>
                {/if}
              {/if}
            </details>
          {:else if canStart && isDone}
            <p class="center">This task is in Done. Move it back to run an agent on it.</p>
          {/if}

        </div>

        {#if !nearBottom && feed && feed.items.length > 0}
          <button class="btn small jump" aria-label="Jump to latest activity" onclick={() => toBottom()}><Icon name="down" size={14} />Latest</button>
        {/if}

        {#if run && viewingLatest && pending.length > 0}
          <div class="asks" role="region" aria-label="The agent is waiting for your answer">
            {#each pending as q (q.id)}<div class="ask-card"><QuestionCard question={q} /></div>{/each}
          </div>
        {:else if run && canMessage}
          <form
            class="composer"
            class:blocked={isBlocked}
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
              placeholder={isBlocked ? 'Tell the agent how to proceed' : run.state === 'running' ? 'Add a message — it is queued for the agent' : 'Reply to the agent'}
              bind:value={message}
              onkeydown={onKey}
              aria-describedby="message-help"
              title="Enter to send · Shift+Enter for a new line"
            ></textarea>
            <button class="btn primary" type="submit" disabled={busy || !message.trim()}>Send</button>
            <span id="message-help" class="message-help muted">Enter to send · Shift+Enter for a new line</span>
          </form>
        {/if}
      </div>

      <!-- The facts about the task. -->
      <aside class="side" aria-label="About this task" tabindex="-1" bind:this={detailsEl}>
        {@render facts()}
      </aside>
    </div>
  {/if}
</section>

{#if task && editing}<EditTaskSheet {task} {scope} inherited={below} onclose={() => (editing = false)} />{/if}
{#if task && scheduling}<ScheduleSheet {task} {scope} onclose={() => (scheduling = false)} />{/if}

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 20;
    background: color-mix(in srgb, var(--bg) 35%, transparent);
    cursor: default;
  }

  .panel {
    position: fixed;
    z-index: 21;
    top: 0;
    right: 0;
    bottom: 0;
    width: min(1040px, calc(100vw - var(--rail-w) - 120px));
    display: flex;
    flex-direction: column;
    background: var(--bg);
    border-left: 1px solid var(--border);
    box-shadow: var(--win-sh);
    animation: slide 0.22s var(--ease);
  }

  @keyframes slide {
    from {
      transform: translateX(24px);
      opacity: 0;
    }
  }

  .facts-tabs { display: flex; flex-wrap: wrap; gap: 4px; padding: 8px 24px; border-bottom: 1px solid var(--border); flex: none; }
  .facts-tabs button[aria-pressed="true"] { color: var(--accent); background: var(--surface-2); }
  .scroll, .side { overflow-x: hidden; scrollbar-gutter: stable; }
  .head {
    flex: none;
    padding: 12px 20px 14px 24px;
    border-bottom: 1px solid var(--border);
    display: grid;
    gap: 8px;
  }

  .top {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .back {
    display: none;
  }

  .title:focus,
  .scroll:focus,
  .side:focus {
    outline: none;
  }

  /* Out of sight until it takes focus, then the first thing in the panel. */
  .skips {
    display: contents;
  }
  .skip {
    position: absolute;
    left: 24px;
    top: 8px;
    z-index: 2;
    padding: 6px 10px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--surface);
    color: var(--accent);
    font: inherit;
    font-size: 12.5px;
    cursor: pointer;
    transform: translateY(-200%);
    opacity: 0;
    pointer-events: none;
  }
  .skip:focus {
    transform: none;
    opacity: 1;
    pointer-events: auto;
  }

  .side :global(h3) {
    font-family: var(--mono);
    font-size: 11.5px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.14em;
    color: var(--text-2);
  }

  .spacer {
    flex: 1;
  }

  /* The column, as a raised pill that is also a picker: moving a task is one choice. */
  .state {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    height: 28px;
    padding: 0 10px;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: var(--btn-bg);
    box-shadow: var(--btn-sh);
    font-size: 12px;
    font-weight: 500;
  }

  .state .dot[data-tone='neutral'] {
    opacity: 0.5;
  }

  .state select {
    appearance: none;
    border: 0;
    background: transparent;
    font: inherit;
    color: var(--text);
    cursor: pointer;
    padding-right: 2px;
  }

  .state select:focus-visible {
    outline: none;
  }

  .state:focus-within {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .title {
    font-family: inherit;
    text-transform: none;
    color: var(--text);
    font-size: 20px;
    font-weight: 600;
    letter-spacing: -0.015em;
    line-height: 1.25;
    overflow-wrap: anywhere;
  }

  .task-labels {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin: 6px 0 0;
    padding: 0;
    list-style: none;
  }

  .runs-with {
    font-size: 12.5px;
  }

  .runs-with strong {
    font-weight: 500;
  }

  .review-evidence > summary {
    min-height: 44px;
    display: flex;
    align-items: center;
    font-size: 12.5px;
  }

  .review-evidence[open] {
    padding: 8px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface);
  }

  .review-evidence[open] > :not(summary) { margin-top: 10px; }

  .body {
    flex: 1;
    min-height: 0;
    display: grid;
    grid-template-columns: minmax(0, 1fr) 300px;
  }

  .main {
    position: relative;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .scroll {
    position: relative;
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 16px 24px 24px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .runbar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px 12px;
    padding: 10px 12px;
    border-radius: var(--radius);
    background: var(--surface);
    border: 1px solid var(--border);
    box-shadow: var(--card-sh);
  }

  .runbar[data-tone='work'] {
    border-color: var(--accent);
    box-shadow:
      var(--card-sh),
      var(--live-ring);
  }

  .run-acts {
    margin-left: auto;
    display: flex;
    gap: 8px;
  }

  .note {
    font-size: 12.5px;
    color: var(--text-2);
    overflow-wrap: anywhere;
  }

  .blocker {
    display: grid;
    gap: 8px;
    padding: 12px 14px;
    border-radius: var(--radius);
    background: color-mix(in srgb, var(--block) 9%, var(--surface));
    border: 1px solid color-mix(in srgb, var(--block) 35%, var(--border));
  }

  .blocker .what {
    font-weight: 600;
    overflow-wrap: anywhere;
  }

  .blocker .detail {
    font-size: 13px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .options {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  /* What the agent is asking sits where you answer, always in view, in the colour of "needs you". */
  .asks {
    flex: none;
    display: grid;
    gap: 10px;
    max-height: 55%;
    overflow-y: auto;
    padding: 12px 24px calc(12px + env(safe-area-inset-bottom));
    border-top: 1px solid color-mix(in srgb, var(--warn) 55%, var(--border));
    background: color-mix(in srgb, var(--amber) 9%, var(--bg));
  }

  .ask-card {
    padding: 14px;
    border-radius: var(--radius);
    background: var(--surface);
    border: 1px solid color-mix(in srgb, var(--warn) 55%, var(--border));
    box-shadow: var(--card-sh);
  }

  .feed-end {
    height: 1px;
    flex: none;
  }

  .banner {
    padding: 8px 10px;
    border: 1px solid color-mix(in srgb, var(--danger) 40%, var(--border));
    border-radius: var(--radius-sm);
  }

  .center {
    text-align: center;
    padding: 12px 0;
    color: var(--text-2);
    font-size: 13px;
  }

  .start {
    padding: 14px;
    border-radius: var(--radius);
    background: var(--surface);
    border: 1px solid var(--border);
    box-shadow: var(--card-sh);
  }

  .start > :global(*) { margin-top: 12px; }
  .start > summary { margin-top: 0; cursor: pointer; min-height: 44px; font-weight: 600; }

  .field {
    display: grid;
    gap: 6px;
    font-size: 13px;
    font-weight: 500;
  }

  .field .muted {
    font-family: var(--mono);
    font-size: 11.5px;
    font-weight: 400;
  }

  .start-row {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    flex-wrap: wrap;
    gap: 10px 16px;
  }

  .check {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    margin-right: auto;
  }

  .jump {
    position: absolute;
    right: 24px;
    bottom: 84px;
    z-index: 2;
    border-radius: 999px;
  }

  .composer {
    flex: none;
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: flex-end;
    padding: 12px 24px calc(12px + env(safe-area-inset-bottom));
    border-top: 1px solid var(--border);
    background: var(--bg);
  }

  .message-help { flex-basis: 100%; font-size: 11.5px; }

  .composer.blocked {
    border-top-color: color-mix(in srgb, var(--block) 50%, var(--border));
  }

  .composer .input {
    flex: 1;
    min-height: 38px;
    max-height: 40dvh;
    resize: none;
    field-sizing: content;
  }

  .side {
    min-height: 0;
    overflow-y: auto;
    padding: 16px 20px 24px;
    border-left: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: 20px;
    background: color-mix(in srgb, var(--surface-2) 40%, var(--bg));
  }

  .side :global(section) {
    display: grid;
    gap: 8px;
  }


  .sh {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: 24px;
  }

  .desc {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font-size: 13px;
  }

  .kv {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: 6px 12px;
    margin: 0;
    font-size: 13px;
  }

  .kv dt {
    color: var(--text-2);
  }

  .kv dd {
    margin: 0;
    overflow-wrap: anywhere;
  }

  .fact {
    font-size: 13px;
    overflow-wrap: anywhere;
  }

  .fact code {
    font-size: 12px;
  }

  .side :global(.usage) {
    font-size: 12.5px;
  }

  .runs {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 2px;
  }

  .run-pick {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    min-height: 32px;
    padding: 0 8px;
    border: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    text-align: left;
    font-size: 12.5px;
  }

  .run-pick:hover {
    background: var(--surface-2);
  }

  .run-pick[aria-pressed='true'] {
    background: var(--surface-2);
    box-shadow: var(--press-sh);
  }

  .rl {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* Small windows switch between activity and facts inside the same viewport. */
  @media (max-width: 1280px) {
    .panel {
      width: min(760px, calc(100vw - var(--rail-w) - 40px));
    }

    .body {
      grid-template-columns: minmax(0, 1fr);
    }

    .side {
      display: none;
    }

    .facts-open .main { display: none; }
    .facts-open .side {
      display: flex;
      border-left: 0;
    }
  }

  /* Phone: the task is its own page over everything, with a way back. */
  @media (max-width: 899px) {
    .scrim {
      display: none;
    }

    .panel {
      width: auto;
      left: 0;
      z-index: 30;
      border-left: 0;
      box-shadow: none;
      animation: none;
      padding-top: env(safe-area-inset-top);
    }

    .head {
      padding: 8px 16px 12px;
    }

    /* Back, the column, Stop, Archive task and Edit do not fit one row at 375px: the actions wrap rather than clip. */
    .top {
      flex-wrap: wrap;
    }

    .top .btn {
      min-height: 44px;
    }

    /* Back and the column on the first row; the task's actions share the second, each as wide as it needs. */
    .spacer {
      flex-basis: 100%;
      height: 0;
    }

    .top .btn:not(.icon) {
      flex: 1 1 auto;
    }

    .state {
      height: 44px;
    }

    .back {
      display: inline-flex;
    }

    .close {
      display: none;
    }

    .scroll {
      padding: 14px 16px 20px;
    }

    .composer,
    .asks {
      padding-inline: 16px;
    }

    .side {
      padding: 16px;
    }
  }
</style>
