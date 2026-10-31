<script lang="ts">
  import { onMount, tick, untrack } from 'svelte';
  import { acknowledge, surface } from '../lib/motion';
  import { ApiError, api } from '../lib/api';
  import { BUSY_RUNNERS, agentLabel, compact, executionAgents, onlyBusy, optionLabel, priorityLabel, resolveFor, sourceLabel, summaryLine } from '../lib/execution';
  import ExecutionFields from '../lib/ExecutionFields.svelte';
  import { RunFeed } from '../lib/feed.svelte';
  import FeedView from '../lib/FeedView.svelte';
  import { agentName, runElapsed, runStatus, timeAgo } from '../lib/format';
  import Icon from '../lib/Icon.svelte';
  import { blockerLine, interactionLabel, isNotable } from '../lib/policy';
  import { assistant } from '../lib/assistant/sessions.svelte';
  import QuestionCard from '../lib/QuestionCard.svelte';
  import ConnectionBanner from '../lib/ConnectionBanner.svelte';
  import Settling from '../lib/Settling.svelte';
  import RunBadge from '../lib/RunBadge.svelte';
  import RunHandoff from '../lib/RunHandoff.svelte';
  import RunReviewSummary from '../lib/RunReviewSummary.svelte';
  import RunUsage from '../lib/RunUsage.svelte';
import { globalHref, projectHref, router, taskHref } from '../lib/router.svelte';
  import Crumbs from '../lib/Crumbs.svelte';
  import { chainOf, pushed, withChain, type Names, type Origin } from '../lib/origin';
  import { goOrigin, originHref } from '../lib/origin-nav';
  import type { WorkPlan } from '../lib/work-queue';
  import { nextAction, planOfTask, stepOf, type NextAction } from '../lib/work-steps';
  import { branchHref } from '../lib/gitroute';
  import { isPhone } from '../lib/phones/capability';
  import { schedulingLabels, schedulingReason } from '../lib/scheduling';
  import type { ProjectScope } from '../lib/scope.svelte';
  import { app } from '../lib/state.svelte';
  import EditTaskSheet from '../lib/task/EditTaskSheet.svelte';
  import { agentWorkable } from '../lib/task-presentation';
  import { spanOf, spanTitle } from '../lib/planning-dates';
  import ScheduleSheet from '../lib/task/ScheduleSheet.svelte';
  import { TASK_STATES, TASK_STATE_LABELS, type ExecutionConfig, type Project, type Run, type Task, type TaskState, type Worktree } from '../lib/types';

  // A task, opened over its board. The head says what the task is, once. Under it, one row of views: Overview (where the run stands,
  // what to review, how to start one), Chat (the agent's conversation, its questions and your replies) and the task's Execution,
  // Schedule and Runs. What needs you (a question, a blocker) opens on Chat and is pointed at from Overview. Esc closes.

  let { scope, project }: { scope: ProjectScope; project: Project } = $props();

  const task = $derived(scope.tasks.find((t) => t.id === router.taskId));
  const latest = $derived(scope.latestRun[router.taskId]);
  /** What this task's runs get: its own overrides, then the project's defaults, then yours. */
  const effective = $derived(resolveFor(task?.execution, project.execution, app.globalExecution));
  /** What it would get if the task itself set nothing. */
  const below = $derived(resolveFor({}, project.execution, app.globalExecution));
  const boardHref = $derived(projectHref(project.id, 'work') + (new URLSearchParams(router.query).get('plan') ? '?plan=' + encodeURIComponent(new URLSearchParams(router.query).get('plan')!) : ''));

  // Back goes where the person came from: the page the address names (Work, the Calendar, Agents, Salie, a session, a plan…), and
  // otherwise the plan or the board this task is under. The address carries it, so a reload or a pasted link goes back the same way.
  const planParam = $derived(new URLSearchParams(router.query).get('plan') ?? '');
  const chain = $derived.by<Origin[]>(() => {
    const given = chainOf(router.query);
    if (given.length) return given;
    return [planParam ? { kind: 'plan', projectId: project.id, planId: planParam } : { kind: 'board', projectId: project.id }];
  });
  const backHref = $derived(originHref(chain[0], chain.slice(1), router.taskId));
  let plans = $state<WorkPlan[]>([]);
  // The plans are read to know the task's parent plan, if it has one; a task needs none. A paired phone may not read plans.
  $effect(() => {
    void router.taskId;
    if (isPhone()) return;
    let live = true;
    void api.workPlans(project.id).then(p => { if (live) plans = p; }, () => {});
    return () => { live = false; };
  });
  const parentPlan = $derived(planOfTask(router.taskId, plans));
  const names: Names = {
    task: (p, t) => app.scopes.get(p)?.tasks.find(x => x.id === t)?.title,
    plan: (_p, id) => plans.find(x => x.id === id)?.title,
  };
  /** What this task is to a page it opens: a way back to it, then to wherever it was reached from. */
  const here = $derived<Origin[]>(pushed({ kind: 'task', projectId: project.id, taskId: router.taskId }, chain));

  function close() {
    goOrigin(chain[0], chain.slice(1), router.taskId);
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
    const requestedRunId = router.runId;
    void latest?.id; // a new run makes a new entry in the history
    picked = requestedRunId;
    history = [];
    if (!id) return;
    api.listTaskRuns(project.id, id).then((rs) => {
      history = rs;
      if (requestedRunId && !rs.some((r) => r.id === requestedRunId)) picked = '';
    }, (err) => app.handleError(err));
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
  /** The skip link is a button: the address bar is the router, so an `href="#…"` would navigate away from the task. */
  function skipToContent() {
    scroller?.focus();
    scroller?.scrollIntoView({ block: 'nearest' });
  }

  // ---- the description, said once: a long one is clamped, and the same button opens the rest ----
  let briefOpen = $state(false);
  const longBrief = $derived((task?.description?.length ?? 0) > 280 || (task?.description?.split('\n').length ?? 0) > 5);

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
    if (!task || busy || state === task.state) return;
    const t = await act(() => api.moveTask(task, state));
    if (t) {
      scope.upsertTask(t);
    }
    else await scope.load().catch((err) => app.handleError(err));
  }

  let editing = $state(false);
  let scheduling = $state(false);
  const decision = $derived(scope.decisions.find((d) => d.taskId === task?.id));
  const dependencies = $derived((task?.orchestration?.dependencies ?? []).map((id) => scope.tasks.find((t) => t.id === id)));

  // ---- start ----
  let instructions = $state('');
  let nextMode = $state<'' | 'fresh' | 'handoff' | 'resume'>('');
  let nextPurpose = $state('continue');
  let selectedContext = $state('');
  let nextRunFor = '';
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
        nextMode = '';
        nextPurpose = 'continue';
        selectedContext = '';
        message = '';
        nearBottom = true;
        briefOpen = false;
        setupOpen = null;
      });
    }
  });
  /** The agent the run will use: chosen here, else the task's effective one, else the first that works (or, while every
   *  runner is full, the first that will once a slot frees). */
  const startAgent = $derived(runChoice.agent || effective.agent || available[0]?.id || executionAgents(app.agents, app.runners, project.id, runChoice.runner || effective.runner, true)[0]?.id || '');
  const startingAgentReady = $derived(available.some((a) => a.id === startAgent));
  /** Nothing is wrong with the agent: every runner that has it is full. Say so rather than send the person to Settings. */
  const startBusy = $derived(!startingAgentReady && onlyBusy(app.agents, app.runners, project.id, runChoice.runner || effective.runner, runChoice.agent || effective.agent || ''));
  const canResume = $derived(!!latest && !latest.remote && !!latest.runnerId && !!latest.sessionRef && latest.agentId === startAgent && (!(runChoice.runner || effective.runner) || (runChoice.runner || effective.runner) === 'automatic' || (runChoice.runner || effective.runner) === latest.runnerId));
  $effect(() => {
    if (!canResume && nextMode === 'resume') nextMode = '';
  });
  $effect(() => {
    const id = latest?.id ?? '';
    if (id !== nextRunFor) { nextRunFor = id; nextMode = ''; selectedContext = ''; nextPurpose = 'continue'; }
  });

  async function start() {
    if (!task || !startingAgentReady || (latest && !nextMode) || (nextMode === 'resume' && !canResume)) return;
    const c = compact(runChoice);
    const choice = { mode:c.mode, runnerId: nextMode === 'resume' ? latest?.runnerId : c.runner, agentId: c.agent ?? '', model: c.model, reasoning: c.reasoning, interaction: c.interaction };
    const r = await act(() => nextMode === 'handoff' && latest
      ? api.continueRun(latest, choice, nextPurpose, instructions.trim(), selectedContext.trim())
      : api.startRun(
        project.id,
        task.id,
        choice,
        instructions.trim(),
        nextMode === 'resume',
      ),
    );
    if (r) {
      scope.upsertRun(r);
      instructions = '';
      nextMode = '';
      selectedContext = '';
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
  const step = $derived(stepOf(task, scope.tasks, scope.latestRun));
  const next = $derived<NextAction>(task ? nextAction(task, step, { agentWork, canStart: canStart && !(task.state === 'done'), hasRun: !!latest }) : { kind: 'none', label: '' });
  const stepTone = $derived(({ done: 'ok', running: 'work', attention: 'ask', review: 'ok', blocked: 'block', waiting: 'neutral', ready: 'neutral', missing: 'neutral' } as const)[step.kind]);
  /** The words for where the task is now: the step's label, with the time on a run that is going. */
  const nowLine = $derived(step.kind === 'running' && latest ? `Running · ${runElapsed(latest, app.now)}` : step.kind === 'ready' && !latest ? (agentWork ? 'Ready to start' : 'Ready') : step.label);
  // ---- where else this task is: its plan, session, Calendar entry and branch. Each link carries the way back to this task. ----
  const planHref = $derived.by(() => {
    if (!parentPlan) return '';
    // When the plan is the nearest step back, the link is Back itself; otherwise the plan returns here.
    const near = chain[0];
    if (near?.kind === 'plan' && near.planId === parentPlan.id) return originHref(near, chain.slice(1)) ?? '';
    return withChain(`${projectHref(project.id, 'work')}?plan=${encodeURIComponent(parentPlan.id)}`, here);
  });
  const scheduledAt = $derived(task?.orchestration?.enabled ? task.orchestration.scheduledAt : undefined);
  const calendarHref = $derived(task && scheduledAt ? withChain(`${projectHref(project.id, 'calendar')}?task=${encodeURIComponent(task.id)}`, here) : '');
  const scheduledText = $derived(scheduledAt ? new Date(scheduledAt).toLocaleString([], { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : '');
  const gitHref = $derived(run?.branch && !run.remote ? branchHref(project.id, 'local', run.branch) : '');
  /** The run set-up is open for a task with no run yet, and when the next action asks for it; the person's own choice wins. */
  let setupOpen = $state<boolean | null>(null);
  async function doNext() {
    if (!task) return;
    if (next.kind === 'answer') tab = 'chat';
    else if (next.kind === 'review') await move('done');
    else if (next.kind === 'reopen') await move('backlog');
    else if (next.kind === 'move') await move(nextState);
    else if (next.kind === 'start' || next.kind === 'again') {
      if (next.kind === 'start' && startingAgentReady) { await start(); return; }
      tab = 'overview';
      setupOpen = true;
      await tick();
      document.getElementById('task-setup')?.scrollIntoView({ block: 'nearest' });
    }
  }
  /** The run is idle or stopped for a decision, so the person can end it as done. It sits with Stop in the head, on every view. */
  const canFinishRun = $derived(viewingLatest && !!run && !!status?.active && ((run.state === 'waiting_for_user' && run.waiting === 'idle') || run.state === 'blocked'));
  /** Something only the person can do: an answer, or a run that stopped for a decision. */
  const needsYou = $derived(viewingLatest && (pending.length > 0 || isBlocked));

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

  const planSpan = $derived(task ? spanOf(task) : null);
  const hasPlanning = $derived(!!planSpan || dependencies.length > 0);
  const nextState = $derived(task?.state === 'backlog' ? 'doing' : task?.state === 'doing' ? 'review' : task?.state === 'review' ? 'done' : 'backlog');

  // ---- the views ----
  type Tab = 'overview' | 'chat' | 'run' | 'schedule' | 'history';
  let tab = $state<Tab>('overview');
  // A task that is not agent work has no conversation, execution or schedule; a work project has no runs.
  const tabs = $derived<{ id: Tab; label: string; count?: number; countLabel?: string }[]>([
    { id: 'overview', label: 'Overview' },
    ...(agentWork || !!run ? [{ id: 'chat' as const, label: 'Chat', count: pending.length, countLabel: `${pending.length} waiting for you` }] : []),
    ...(agentWork ? [{ id: 'run' as const, label: 'Execution' }, { id: 'schedule' as const, label: 'Schedule' }] : []),
    ...(agentWork || project.kind !== 'work' ? [{ id: 'history' as const, label: `Runs (${history.length})` }] : []),
  ]);
  let tabChosenFor = '';
  $effect(() => {
    const id = task?.id ?? '';
    if (id === tabChosenFor) return;
    tabChosenFor = id;
    tab = untrack(() => (needsYou ? 'chat' : 'overview'));
  });
  $effect(() => { if (!tabs.some((t) => t.id === tab)) tab = 'overview'; });
  // A view starts at its top, except the conversation, which starts at its end.
  $effect(() => {
    void tab;
    void tick().then(() => (tab === 'chat' ? toBottom(false) : scroller?.scrollTo({ top: 0, behavior: 'auto' })));
  });
  function onTabKey(event: KeyboardEvent) {
    const step = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0;
    if (!step && event.key !== 'Home' && event.key !== 'End') return;
    event.preventDefault();
    const at = tabs.findIndex((t) => t.id === tab);
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : (at + step + tabs.length) % tabs.length;
    tab = tabs[next].id;
    void tick().then(() => document.getElementById(`task-tab-${tab}`)?.focus());
  }
</script>

<svelte:window onkeydown={onWindowKey} />

{#snippet closer(titled = false)}
  {#if backHref}<a class="btn quiet small icon" class:close={titled} href={backHref} aria-label="Close panel" title={titled ? 'Close (Esc)' : undefined}><Icon name="close" /></a>
  {:else}<button class="btn quiet small icon" class:close={titled} type="button" aria-label="Close panel" title={titled ? 'Close (Esc)' : undefined} onclick={close}><Icon name="close" /></button>{/if}
{/snippet}
<a class="scrim" href={backHref ?? boardHref} onclick={(e) => { if (!backHref) { e.preventDefault(); close(); } }} aria-hidden="true" tabindex="-1"></a>
<section class="panel" transition:surface={{ x: 20, y: 0 }} aria-labelledby={task ? 'task-title' : undefined} aria-label={task ? undefined : 'Task'}>
  {#if !task}
    <header class="head">
      <Crumbs {chain} {names} fromTask={router.taskId} />
      {@render closer()}
    </header>
    {#if scope.loaded}<p class="empty">This task was not found in this project.</p>{:else}<Settling compact label="Opening the task…" />{/if}
  {:else}
    <header class="head" use:acknowledge={{ value: task.state, success: task.state === 'done' }}>
      <button class="skip" onclick={skipToContent}>Skip to task content</button>
      <Crumbs {chain} {names} fromTask={router.taskId} />
      <div class="top">
        <label class="state">
          <span class="visually-hidden">Task column</span>
          <span class="dot" data-tone={task.state === 'doing' ? 'work' : task.state === 'review' ? 'ok' : task.state === 'done' ? 'ok' : 'neutral'}></span>
          <select value={task.state} disabled={busy || !!task.archivedAt} onchange={(e) => move(e.currentTarget.value as TaskState)}>
            {#each TASK_STATES as s (s)}<option value={s}>{TASK_STATE_LABELS[s]}</option>{/each}
          </select>
        </label>
        <span class="spacer"></span>
        {#if task.archivedAt}
          <span class="chip">Archived</span><button class="btn small ok" disabled={busy} onclick={() => archive(false)}>Restore task</button>
        {:else if latest && runStatus(latest).active}
          {#if canFinishRun}<button class="btn small ok" disabled={busy} onclick={finish}><Icon name="check" size={14} />Finish</button>{/if}
          <button class="btn small danger" disabled={busy} onclick={() => { picked = ''; void stopLatest(); }}><Icon name="stop" size={12} />{confirmingStop ? 'Confirm stop' : 'Stop agent'}</button>
        {/if}
        {@render closer(true)}
      </div>
      <h2 class="title" id="task-title" tabindex="-1" bind:this={titleEl}>{task.title}</h2>
      {#if task.description}
        <div class="brief">
          <p id="task-brief" class:clamped={longBrief && !briefOpen}>{task.description}</p>
          {#if longBrief}<button class="link" type="button" aria-expanded={briefOpen} aria-controls="task-brief" onclick={() => (briefOpen = !briefOpen)}>{briefOpen ? 'Show less' : 'Show more'}</button>{/if}
        </div>
      {/if}
      {#if reviewFirst && run}
        <RunReviewSummary {run} onmerged={() => void move('done')} />
      {/if}
      <section class="focus" aria-label="Where this task stands" data-testid="task-focus" data-step={step.kind}>
        <div class="now">
          <span class="k">Now</span>
          <p class="line"><span class="dot" data-tone={stepTone} class:hollow={stepTone === 'neutral'}></span><strong>{nowLine}</strong></p>
          {#if step.reason}<p class="why">{step.reason}</p>{/if}
        </div>
        {#if !task.archivedAt}
          <div class="next">
            <span class="k">Next</span>
            {#if next.kind !== 'none'}
              <button class="btn primary" data-testid="task-next" disabled={busy} onclick={doNext}>{next.label}</button>
            {:else if step.blockedBy.length}
              <p class="why">Finish {step.blockedBy.length > 1 ? 'the blocking tasks' : 'the blocking task'} first.</p>
            {:else if step.kind === 'running'}
              <p class="why">Nothing needed. The agent is working.</p>
            {:else}
              <p class="why">Nothing to do right now.</p>
            {/if}
            {#if step.blockedBy.length}
              <ul class="blockers" aria-label="Blocked by">{#each step.blockedBy as b (b.id)}<li><a href={withChain(taskHref(project.id, b.id), here)}>{b.title}</a></li>{/each}</ul>
            {/if}
          </div>
        {/if}
      </section>
      <nav class="related" aria-label="Related">
        {#if parentPlan}<a class="rel" data-rel="plan" href={planHref}><span class="k">Plan</span>{parentPlan.title}</a>{/if}
        {#if run}<span class="rel"><span class="k">Session</span><button class="btn small" type="button" onclick={() => assistant.openRun(project.id, run.id, here)}>Open Session</button><span class="mm">{status?.label}</span></span>{/if}
        {#if calendarHref}<a class="rel" data-rel="calendar" href={calendarHref}><span class="k">Calendar</span>{scheduledText}</a>{/if}
        {#if gitHref}<a class="rel" data-rel="git" href={gitHref}><span class="k">{task.state === 'review' ? 'Review' : 'Git'}</span><code>{run?.branch}</code></a>{/if}
      </nav>
      {#if !task.archivedAt}
        <div class="task-actions" aria-label="Task actions">
          <button class="btn quiet small" aria-label="Edit task" onclick={() => (editing = true)}>Edit</button>
          <button class="btn quiet small" disabled={busy || !!latest && runStatus(latest).active} onclick={() => archive(true)} title="Move this task to the Archive. Its history is kept, and you can undo it.">Archive task</button>
        </div>
      {/if}
    </header>

    <!-- One row of views, in the shared tab style. Each has its own panel below, and the arrow keys move between them. -->
    <div class="tabs section-tabs" role="tablist" aria-label="Task detail views" tabindex="-1" onfocusin={(event) => (event.target as HTMLElement).scrollIntoView({ block: 'nearest', inline: 'nearest' })}>
      {#each tabs as t (t.id)}
        <button type="button" role="tab" id="task-tab-{t.id}" aria-selected={tab === t.id} aria-controls="task-view" tabindex={tab === t.id ? 0 : -1} onkeydown={onTabKey} onclick={() => (tab = t.id)}>
          {t.label}
          {#if t.count}<span class="num pend" aria-label={t.countLabel}>{t.count}</span>{/if}
        </button>
      {/each}
    </div>

    {#snippet planning()}
      <section class="block" aria-labelledby="plan-h">
        <h3 id="plan-h">Planning</h3>
        <dl class="kv">
          <dt>When</dt><dd>{planSpan ? `${planSpan.milestone ? 'Milestone · ' : ''}${spanTitle(planSpan, new Date().getFullYear())}` : 'Not planned'}</dd>
          {#if dependencies.length}<dt>After</dt><dd>{dependencies.map((d) => d?.title ?? 'a removed task').join(', ')}</dd>{/if}
        </dl>
      </section>
    {/snippet}

    {#snippet viewing()}
      {#if run && !viewingLatest}
        <p class="viewing" role="status">Viewing {run.attempt ? `run #${run.attempt}` : 'an earlier run'}. <button class="link" type="button" onclick={() => (picked = '')}>Show the latest</button></p>
      {/if}
    {/snippet}

    <div class="body">
      <div class="main">
        <ConnectionBanner />
        <div class="scroll" id="task-view" role="tabpanel" aria-labelledby="task-tab-{tab}" bind:this={scroller} onscroll={trackScroll} tabindex="-1">
          {#if actionError}<p class="error banner" role="alert">{actionError}</p>{/if}
          {#if (tab === 'overview' || tab === 'chat') && run?.pendingRequests?.length && status?.active}
            <p class="note delivery" role="status" data-testid="pending-requests">Waiting for runner acknowledgment: {run.pendingRequests.map(k => ({ send: 'message delivery', respond: 'answer delivery', stop: 'stop requested', finish: 'finish requested' })[k]).join(' · ')}. The last reported run state still applies.</p>
          {/if}

          {#if tab === 'overview'}
            {@render viewing()}

            {#if run && status}
              <section class="block" aria-labelledby="run-h" data-tone={status.tone}>
                <h3 id="run-h">This run</h3>
                <div class="runbar">
                  <RunBadge {run} />
                  <span class="mm">
                    {agentName(app.agents, run.agentId)}{run.model ? ` · ${optionLabel(app.agentOptions.get(run.agentId)?.models, run.model)}` : ''}{run.reasoning
                      ? ` · ${optionLabel(app.agentOptions.get(run.agentId)?.reasoning, run.reasoning)}`
                      : ''} · {runElapsed(run, app.now)}
                  </span>
                </div>
                {#if status.active}
                  {@const runner = app.runners.find(r => r.id === run.runnerId)}
                  <p class="note">{run.remote ? runner?.name ?? 'Remote runner' : 'This computer'} · {app.connection !== 'live' ? 'availability unknown' : runner ? runner.disabled ? 'disabled' : runner.online ? 'online' : 'offline' : 'availability unknown'}{runner?.lastSeenAt ? ` · last seen ${timeAgo(runner.lastSeenAt, app.now)}` : ''}{run.activityAt ? ` · last activity ${timeAgo(run.activityAt, app.now)}` : ' · no activity reported yet'}</p>
                {/if}
                {#if isNotable(run.policy) || run.policy.interaction !== effective.interaction}
                  <p class="note">This run: {interactionLabel(run.policy)}</p>
                {/if}
                {#if !needsYou && (!status.active || status.tone !== 'work')}
                  <p class="note">{status.hint}</p>
                {/if}
              </section>
            {/if}

            {#if run && status && !status.active && !reviewFirst}
              <RunHandoff {run} {scope} onmerged={() => void move('done')} />
            {:else if reviewFirst && run}
              <details class="review-evidence">
                <summary>Full run and Git evidence</summary>
                <RunHandoff {run} {scope} onmerged={() => void move('done')} hideReviewActions />
              </details>
            {/if}

            {#if hasPlanning}{@render planning()}{/if}

            {#if canStart && !isDone}
              <details class="start block" id="task-setup" open={setupOpen ?? !latest} ontoggle={(e) => (setupOpen = e.currentTarget.open)}>
                <summary>{latest ? 'Set up another run' : 'Set up a run'}</summary>
                {#if available.length === 0 && !startBusy}
                  <p class="error">No coding agent is ready.</p>
                  <p class="note"><a href="{globalHref('settings')}?agents">Open Settings → Agents</a> to set up an agent.</p>
                {:else}
                  {#if latest}
                    <label class="field" for="next-run-mode"><span>How should the next run start?</span><select id="next-run-mode" class="select" bind:value={nextMode}>
                      <option value="">Choose an approach</option>
                      <option value="handoff">Continue this work with a handoff</option>
                      {#if canResume}<option value="resume">Resume the previous conversation</option>{/if}
                      <option value="fresh">Start a fresh conversation</option>
                    </select></label>
                    <p class="note">Previous branch: <strong>{latest.branch || 'not recorded'}</strong>. The task’s worktree is reused when available; moving work to another runner may require reconciliation.</p>
                    {#if nextMode === 'handoff'}<p class="note">Starts a new conversation with the task, handoff, Git evidence and selected context.</p>
                    {:else if nextMode === 'resume'}<p class="note">Resumes the previous conversation with the same agent on its local runner.</p>
                    {:else if nextMode === 'fresh'}<p class="note">Starts a new conversation with the task and your instructions, without a handoff.</p>{/if}
                  {/if}
                  <p class="note">
                    {latest ? 'The run uses' : 'The agent works in its own Git worktree, separate from your checkout. This run uses'}
                    <strong>{summaryLine(resolveFor(task.execution, project.execution, app.globalExecution, compact(runChoice)), app.agents, app.agentOptions) ||
                      'Lowest cost, selected when the run starts'}</strong>{#if !runChoice.agent && effective.agent}&nbsp;<span>(chosen by {sourceLabel(effective.sources.agent)})</span>{/if}.
                  </p>
                  <ExecutionFields bind:value={runChoice} inherited={effective} idPrefix="run" showPriority={false} dense />
                  {#if latest && nextMode === 'handoff'}
                    <label class="field" for="next-run-purpose"><span>Purpose</span><select id="next-run-purpose" class="select" bind:value={nextPurpose}><option value="continue">Continue implementation</option><option value="review">Review this implementation</option><option value="fix">Fix review findings</option></select></label>
                    <label class="field"><span>Selected context <span class="muted">optional</span></span><textarea class="input" rows="3" maxlength="16000" bind:value={selectedContext} placeholder="Review criteria, relevant decisions, or questions to carry forward"></textarea></label>
                  {/if}
                  <label class="field">
                    <span>Extra instructions <span class="muted">optional</span></span>
                    <textarea class="input" rows="2" placeholder="Anything to add to the task…" bind:value={instructions}></textarea>
                  </label>
                  <div class="start-row">
                    <button class="btn primary" disabled={busy || !startingAgentReady || (!!latest && !nextMode)} onclick={start}><Icon name="play" size={12} />{busy ? 'Starting…' : nextMode === 'handoff' ? 'Start continuation' : nextMode === 'resume' ? 'Resume conversation' : 'Start agent'}</button>
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

          {:else if tab === 'chat'}
            {@render viewing()}
            {#if run && run.state === 'blocked' && run.blocker}
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
            {/if}
            {#if feed}
              {#if feed.loading}
                <Settling compact label="Loading the conversation…" />
              {:else if feed.items.length === 0}
                <p class="center">Nothing has happened in this run yet.</p>
              {/if}
              <FeedView answeredBelow={dockIds} items={feed.items} hasMore={feed.hasMore} loadingOlder={feed.loadingOlder} onOlder={() => feed?.loadOlder()} />
              {#if feed.error}<p class="error" role="alert">{feed.error}</p>{/if}
            {:else}
              <div class="quiet-state">
                <p>No agent has worked on this task yet.</p>
                {#if canStart && !isDone}<button class="btn small" type="button" onclick={() => (tab = 'overview')}>Set up a run</button>{/if}
              </div>
            {/if}
            <div class="feed-end" bind:this={feedEnd} aria-hidden="true"></div>

          {:else if tab === 'run'}
            <section class="block" aria-labelledby="how-h">
              <h3 id="how-h">How it runs</h3>
              <dl class="kv">
                <dt>Agent</dt><dd>{effective.agent ? agentLabel(app.agents, effective.agent) : 'First that works'}</dd>
                {#if effective.model}<dt>Model</dt><dd>{optionLabel(app.agentOptions.get(effective.agent)?.models, effective.model)}</dd>{/if}
                {#if effective.reasoning}<dt>Reasoning level</dt><dd>{optionLabel(app.agentOptions.get(effective.agent)?.reasoning, effective.reasoning)}</dd>{/if}
                <dt>Runner</dt><dd>{effective.runner && effective.runner !== 'automatic' ? (app.runners.find((r) => r.id === effective.runner)?.name ?? effective.runner) : 'Automatic'}</dd>
                <dt>Agent decisions</dt><dd>{interactionLabel({ interaction: effective.interaction })}</dd>
                <dt>Priority</dt><dd>{priorityLabel(effective.priority)}</dd>
              </dl>
            </section>
            {#if run}
              <section class="block" aria-labelledby="where-h">
                <h3 id="where-h">Where</h3>
                {#if worktree}
                  <p class="fact"><code>{worktree.branch}</code></p>
                  <button class="btn small" onclick={copyPath} title={worktree.path}><Icon name="copy" size={14} />{copied ? 'Copied' : 'Copy worktree path'}</button>
                {/if}
                <RunUsage {run} showAssessment={!reviewFirst} />
              </section>
            {/if}

          {:else if tab === 'schedule'}
            <section class="block" aria-labelledby="sched-h">
              <div class="sh"><h3 id="sched-h">Schedule</h3><button class="btn quiet small" aria-label={task.orchestration?.enabled ? 'Change schedule' : 'Schedule this task'} disabled={!!task.archivedAt} onclick={() => (scheduling = true)}>{task.orchestration?.enabled ? 'Change' : 'Schedule'}</button></div>
              {#if decision && task.orchestration?.enabled}
                <p class="fact"><strong>{schedulingLabels[decision.state]}</strong> · {schedulingReason(decision)}</p>
              {:else}
                <p class="fact muted">Runs when you start it.</p>
              {/if}
              {#if task.orchestration?.runId}<p class="note">The scheduled run was dispatched. Choose “Schedule another run” in the schedule to authorise another attempt.</p>{/if}
              {#if dependencies.length}
                <p class="fact">Waits for {dependencies.map((t) => t?.title ?? 'a removed task').join(', ')}</p>
              {/if}
            </section>

          {:else if tab === 'history'}
            <section class="block" aria-labelledby="runs-h">
              <h3 id="runs-h">Runs</h3>
              <ul class="runs">
                {#each [...history].reverse() as r, i (r.id)}
                  {@const s = runStatus(r)}
                  <li>
                    <button class="run-pick" aria-pressed={r.id === viewId} onclick={() => { picked = r.id; tab = 'overview'; }}>
                      <span class="dot" data-tone={s.tone}></span>
                      <span class="rl">{i === 0 ? 'Latest' : `#${r.attempt ?? history.length - i}`} · {agentName(app.agents, r.agentId)}</span>
                      <span class="mm">{timeAgo(r.createdAt, app.now)}</span>
                    </button>
                  </li>
                {:else}<li class="muted">No runs yet.</li>{/each}
              </ul>
            </section>
          {/if}
        </div>

        {#if tab === 'chat'}
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
        {/if}
      </div>
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
    min-width: 0;
    max-width: 100vw;
    position: fixed;
    z-index: 21;
    top: 0;
    right: 0;
    bottom: 0;
    width: min(760px, calc(100vw - var(--rail-w) - 120px));
    display: flex;
    flex-direction: column;
    background: var(--bg);
    border-left: 1px solid var(--border);
    box-shadow: var(--win-sh);
  }

  .head {
    position: relative;
    min-width: 0;
    overflow-wrap: anywhere;
    flex: none;
    padding: 12px 20px 14px 24px;
    display: grid;
    gap: 10px;
  }

  .top { display: flex; align-items: center; gap: 8px; }
  .spacer { flex: 1; }

  /* The head and the views are separated by the tab row's own hairline (shared .section-tabs). */
  .tabs { padding: 0 12px; border-top: 1px solid var(--border); }

  .task-actions { display: flex; flex-wrap: wrap; gap: 8px; }

  /* Where the task stands: one block, in the order the questions come. The objective is the title above; this is Now and Next. */
  .focus { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 12px 24px; align-items: start; padding: 12px 14px; border: 1px solid var(--border); border-left-width: 3px; border-radius: var(--radius); background: var(--surface); }
  .focus[data-step='attention'], .focus[data-step='blocked'] { border-left-color: var(--warn); background: color-mix(in srgb, var(--amber) 7%, var(--surface)); }
  .focus[data-step='running'] { border-left-color: var(--accent); }
  .focus[data-step='review'], .focus[data-step='done'] { border-left-color: var(--ok); }
  .focus .now, .focus .next { display: grid; gap: 4px; justify-items: start; min-width: 0; }
  .focus .next { justify-items: end; text-align: right; }
  .focus .k, .related .k { font-family: var(--mono); font-size: 11px; text-transform: uppercase; letter-spacing: 0.14em; color: var(--text-2); }
  .focus .line { margin: 0; display: flex; align-items: center; gap: 8px; font-size: 15px; }
  .focus .why { margin: 0; font-size: 13px; color: var(--text-2); line-height: 1.5; overflow-wrap: anywhere; }
  .blockers { list-style: none; margin: 0; padding: 0; display: grid; gap: 2px; font-size: 13px; }
  .related { display: flex; flex-wrap: wrap; gap: 6px 20px; font-size: 13px; }
  .related:empty { display: none; }
  .rel { display: inline-flex; align-items: center; gap: 8px; color: var(--text); text-decoration: none; min-width: 0; max-width: 100%; }
  a.rel:hover { text-decoration: underline; }
  a.rel { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .title:focus,
  .scroll:focus {
    outline: none;
  }

  /* Out of sight until it takes focus, then the first thing in the panel. */
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
    font-size: 13px;
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

  /* The column, as a pill that is also a picker: moving a task is one choice. */
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
  .state .dot[data-tone='neutral'] { opacity: 0.5; }
  .state select {
    appearance: none;
    border: 0;
    background: transparent;
    font: inherit;
    color: var(--text);
    cursor: pointer;
    padding-right: 2px;
  }
  .state select:focus-visible { outline: none; }
  .state:focus-within { outline: 2px solid var(--accent); outline-offset: 2px; }

  /* Under the head, so that it outranks the global label style every h2 gets in Paper and Graphite (app.css). */
  .head .title {
    font-family: inherit;
    text-transform: none;
    color: var(--text);
    font-size: 20px;
    font-weight: 600;
    letter-spacing: -0.015em;
    line-height: 1.25;
    overflow-wrap: anywhere;
  }

  /* The description, once. A long one shows its first lines and the button below shows the rest. */
  .brief { display: grid; justify-items: start; gap: 2px; }
  .brief p { margin: 0; color: var(--text-2); font-size: 13px; line-height: 1.5; white-space: pre-wrap; overflow-wrap: anywhere; }
  .brief p.clamped { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 4; line-clamp: 4; overflow: hidden; }
  .brief p:not(.clamped) { max-height: min(45vh, 24rem); overflow: auto; }
  .brief .link { min-height: 32px; font-size: 12px; }

  .body { flex: 1; min-height: 0; display: flex; flex-direction: column; }

  .main {
    position: relative;
    min-width: 0;
    min-height: 0;
    flex: 1;
    display: flex;
    flex-direction: column;
  }

  .scroll {
    position: relative;
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
    scrollbar-gutter: stable;
    padding: 4px 24px 24px;
    display: flex;
    flex-direction: column;
  }

  /* Sections of a view are separated by a hairline, not boxed one inside another. */
  .block { display: grid; gap: 8px; padding: 16px 0; border-top: 1px solid var(--border); }
  .scroll > .block:first-child, .viewing + .block { border-top: 0; }
  .block h3, .sh h3 {
    font-family: var(--mono);
    font-size: 12px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.14em;
    color: var(--text-2);
    margin: 0;
  }
  .sh { display: flex; align-items: center; justify-content: space-between; min-height: 24px; }

  .viewing { margin: 12px 0 0; padding: 8px 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface-2); font-size: 13px; color: var(--text-2); }


  .runbar { display: flex; align-items: center; flex-wrap: wrap; gap: 6px 12px; }
  .block[data-tone='work'] { border-left: 2px solid var(--accent); padding-left: 12px; }

  .note { margin: 0; font-size: 13px; color: var(--text-2); overflow-wrap: anywhere; }
  .delivery { margin-top: 12px; }
  .fact { margin: 0; font-size: 13px; overflow-wrap: anywhere; }
  .fact code { font-size: 12px; }


  .blocker {
    display: grid;
    gap: 8px;
    margin: 8px 0;
    padding: 12px 14px;
    border-radius: var(--radius);
    background: color-mix(in srgb, var(--block) 9%, var(--surface));
    border: 1px solid color-mix(in srgb, var(--block) 35%, var(--border));
  }
  .blocker .what { margin: 0; font-weight: 600; overflow-wrap: anywhere; }
  .blocker .detail { margin: 0; font-size: 13px; white-space: pre-wrap; overflow-wrap: anywhere; }
  .options { display: flex; flex-wrap: wrap; gap: 8px; }

  .review-evidence { padding: 16px 0; border-top: 1px solid var(--border); }
  .review-evidence > summary { min-height: 44px; display: flex; align-items: center; font-size: 13px; cursor: pointer; }
  .review-evidence[open] > :not(summary) { margin-top: 10px; }

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

  .feed-end { height: 1px; flex: none; }
  .banner { margin: 12px 0 0; padding: 8px 10px; border: 1px solid color-mix(in srgb, var(--danger) 40%, var(--border)); border-radius: var(--radius-sm); }
  .center { text-align: center; padding: 12px 0; color: var(--text-2); font-size: 13px; }
  .quiet-state { display: grid; justify-items: center; gap: 10px; padding: 48px 0; color: var(--text-2); font-size: 13px; }
  .quiet-state p { margin: 0; }

  .start { padding-bottom: 8px; }
  .start > :global(*) { margin-top: 12px; }
  .start > summary { margin-top: 0; cursor: pointer; min-height: 32px; display: flex; align-items: center; font-weight: 600; }
  @media (pointer: coarse) { .start > summary { min-height: 44px; } }

  .field { display: grid; gap: 6px; font-size: 13px; font-weight: 500; }
  .field .muted { font-family: var(--mono); font-size: 12px; font-weight: 400; }
  .start-row { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 10px 16px; }

  .jump { position: absolute; right: 24px; bottom: 84px; z-index: 2; border-radius: 999px; }

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
  .message-help { flex-basis: 100%; font-size: 12px; }
  .composer.blocked { border-top-color: color-mix(in srgb, var(--block) 50%, var(--border)); }
  .composer .input { flex: 1; min-height: 38px; max-height: 40dvh; resize: none; field-sizing: content; }

  .field .muted, .kv dt { color: var(--text-2); }
  .kv { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 6px 12px; margin: 0; font-size: 13px; }
  .kv dd { margin: 0; overflow-wrap: anywhere; }

  .runs { list-style: none; margin: 0; padding: 0; display: grid; gap: 2px; }
  .run-pick {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    min-height: 36px;
    padding: 0 8px;
    border: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    text-align: left;
    font-size: 13px;
  }
  .run-pick:hover { background: var(--surface-2); }
  .run-pick[aria-pressed='true'] { background: var(--surface-2); box-shadow: var(--press-sh); }
  .rl { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  :global(.scroll .usage) { font-size: 13px; }

  /* Small windows: a narrower panel. */
  @media (max-width: 1280px) {
    .panel { width: min(760px, calc(100vw - var(--rail-w) - 40px)); }
  }

  /* Phone: the task is its own page over everything, with a way back. */
  @media (max-width: 899px) {
    .scrim { display: none; }

    .panel {
      width: auto;
      left: 0;
      z-index: 30;
      border-left: 0;
      box-shadow: none;
      animation: none;
      padding-top: env(safe-area-inset-top);
    }

    .head { padding: 8px 16px 12px; max-height: 50dvh; overflow-y: auto; }
    /* The head already scrolls on a phone; an expanded description does not scroll inside it as well. */
    .brief p:not(.clamped) { max-height: none; overflow: visible; }

    /* Back, the column and Stop do not fit one row at 375px: the top controls wrap rather than clip. */
    .top { flex-wrap: wrap; }
    .top .btn { min-height: 44px; }
    .top .btn:not(.icon) { flex: 1 1 auto; }

    .task-actions .btn { flex: 1 1 auto; min-height: 44px; white-space: normal; }
    .focus { grid-template-columns: 1fr; }
    .focus .next { justify-items: start; text-align: left; }
    .focus .next .btn, .rel { min-height: 44px; }
    a.rel { display: inline-flex; }
    .state { height: 44px; }
    .close { display: inline-flex; width: 44px; height: 44px; }

    .tabs { padding-inline: 4px; }
    .scroll { padding: 4px 16px 20px; }
    .composer, .asks { padding-inline: 16px; }
  }
</style>
