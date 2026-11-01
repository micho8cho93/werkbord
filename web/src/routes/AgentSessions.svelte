<script lang="ts">
  import { onMount } from 'svelte';
  import { app } from '../lib/state.svelte';
  import { api } from '../lib/api';
  import type { WorkPlan } from '../lib/work-queue';
  import { router, taskHref } from '../lib/router.svelte';
  import { agentSessionsApi, asTerminal, isSessionKey, unique, type AgentSession, type AgentSessionList } from '../lib/agent-sessions';
  import { backToSalie, openAgents } from '../lib/agent-nav';
  import { RunSession } from '../lib/assistant/run-session.svelte';
  import RunSessionView from '../lib/assistant/RunSession.svelte';
  import TerminalSessionView from '../lib/assistant/TerminalSession.svelte';
  import { connectedAgents, nativeCLILaunchers } from '../lib/assistant/connected-agents';
  import { selectionApi, type CatalogAgent } from '../lib/selection';
  import { isPhone } from '../lib/phones/capability';
  import { agentName } from '../lib/format';
  import { terminalApi, type TerminalSession } from '../lib/assistant/terminal';
  import { embedded } from '../lib/embed';
  import Crumbs from '../lib/Crumbs.svelte';
  import { chainOf, pushed, withChain, type Names, type Origin } from '../lib/origin';
  import { readAgentView, writeAgentView } from '../lib/agent-view';
  import { tick } from 'svelte';
  import { controlsFor, hasTaskContext, isTrouble, OUTCOME_LABELS, outcomeOf, overviewOf, providersOf, sessionState, summaryOf } from '../lib/agent-overview';
  import SessionCard from '../lib/agents/SessionCard.svelte';
  import Icon from '../lib/Icon.svelte';
  import { stepOf } from '../lib/work-steps';
  import { timeAgo } from '../lib/format';
  import { projectHref, sessionsHref } from '../lib/router.svelte';

  // Agents: every coding-agent session in one list, and the one that is open. A session is listed from the record that owns it
  // (a task run from the runner, a terminal from the terminal service) and is named by its key; opening one here attaches to the
  // same process Salie started or the task page shows. The run's view and the terminal's renderer are the ones Salie and the task
  // page use, so there is one terminal implementation and one run view. Nothing here starts a run: that is the task, or Salie
  // with the person's confirmation. A terminal can be opened here, on its own or for a task.

  let list = $state<AgentSession[]>([]);
  let terminalsListed = $state(true);
  let loaded = $state(false);
  let error = $state('');
  // The filter and the scroll of the list are kept for the tab, so Back from a session's task finds the list as it was left. In the
  // window the project is the window's own project context (the one selector), told by the address; elsewhere the page has its own.
  const kept = readAgentView();
  let filter = $state(kept.filter);
  const windowScoped = embedded();
  let ownProject = $state(windowScoped ? '' : (new URLSearchParams(router.query).get('project') ?? kept.project));
  const projectFilter = $derived(windowScoped ? (new URLSearchParams(router.query).get('project') ?? '') : ownProject);
  let sideEl = $state<HTMLElement>();
  let scrolled = kept.scroll;
  $effect(() => { writeAgentView({ filter, project: windowScoped ? '' : ownProject, scroll: scrolled }); });
  let restored = false;
  $effect(() => {
    if (!loaded || restored || !sideEl) return;
    restored = true;
    void tick().then(() => sideEl?.scrollTo({ top: kept.scroll, behavior: 'auto' }));
  });

  // Back from a session goes where the person came from; a session opened from the list itself goes back to the list.
  const chain = $derived(chainOf(router.query));
  const trailChain = $derived<Origin[]>(chain.length ? chain : [{ kind: 'agents' }]);
  // A plan in the route is named by its title; its project's plans are read once, only when the route has a plan in it.
  let plans = $state<WorkPlan[]>([]);
  $effect(() => {
    const plan = chain.find(o => o.kind === 'plan');
    if (plan) void api.workPlans(plan.projectId).then(p => (plans = p), () => {});
  });
  const names: Names = { task: (p, t) => app.scopes.get(p)?.tasks.find(x => x.id === t)?.title, plan: (_p, id) => plans.find(x => x.id === id)?.title, session: k => { const found = list.find(s => s.key === k); return found ? `Session · ${agentName(app.agents, found.agentId)}` : undefined; } };

  const key = $derived(isSessionKey(router.sub) ? router.sub : '');
  const selected = $derived(key ? list.find(s => s.key === key) : undefined);
  /** What the open session is to a page it opens: a way back to it, then to wherever it was reached from. */
  const here = $derived<Origin[]>(key ? pushed({ kind: 'session', key }, trailChain) : []);
  let provider = $state('');
  const sessions = $derived(unique(list));
  const ov = $derived(overviewOf(sessions, { project: projectFilter, provider, show: filter }));
  const summary = $derived(summaryOf(ov));
  const shownCount = $derived(ov.needsYou.length + ov.running.length + ov.recent.length);
  // What went wrong sits above what is running: a failure is something to act on, a run in progress is not.
  const trouble = $derived(ov.recent.filter(isTrouble));
  const finished = $derived(ov.recent.filter(x => !isTrouble(x)));
  /** Shown inside Work, beside a task: the one session alone, with no list of the others. */
  const paneMode = $derived(new URLSearchParams(router.query).get('pane') === '1');
  /**
   * Opens a session beside the list by moving this page's own address, which the window follows. Nothing is asked of the window and no
   * page is rebuilt, so the list keeps its place, its filter and its scroll. Choosing from the list makes the list the way back.
   */
  function select(next: string): void {
    const q = new URLSearchParams(router.query);
    // A throwaway built to make an address, not state that is read while drawing.
    // eslint-disable-next-line svelte/prefer-svelte-reactivity
    const params = new URLSearchParams();
    for (const name of ['project', 'pane']) { const v = q.get(name); if (v) params.set(name, v); }
    location.hash = sessionsHref(next) + (params.size ? `?${params}` : '');
  }
  /** What this controller can do to the open session, from what the session offers and whether the controller is reachable. */
  const controls = $derived(controlsFor(selected, app.connection));
  /** Only a session that belongs to a task has task context; a standalone one gives its space to the conversation. */
  const taskful = $derived(hasTaskContext(selected));
  /** The session's task, its plans and where the task stands: a small panel, not a second page. */
  const taskScope = $derived(selected?.taskId ? app.scopes.get(selected.projectId) : undefined);
  const task = $derived(selected?.taskId ? taskScope?.tasks.find(t => t.id === selected.taskId) : undefined);
  const taskStep = $derived(task && taskScope ? stepOf(task, taskScope.tasks, taskScope.latestRun) : undefined);
  let sessionPlans = $state<WorkPlan[]>([]);
  $effect(() => {
    const id = selected?.taskId;
    const project = selected?.projectId;
    sessionPlans = [];
    if (!id || !project || isPhone()) return;
    let live = true;
    void api.workPlans(project).then(p => { if (live) sessionPlans = p.filter(x => x.taskIds.includes(id)); }, () => {});
    return () => { live = false; };
  });
  /** What a live session is doing, in a line: the run's latest activity when this page knows it, else its state. */
  const doing = (x: AgentSession): string => (x.kind === 'run' && x.taskId ? app.scopes.get(x.projectId)?.latestRun[x.taskId]?.activity : '') || sessionState(x);
  /** Other sessions to switch to from an open one. Switching only changes which one is shown: nothing is started, stopped or re-attached. */
  const others = $derived(sessions.filter(x => x.key !== key && (!projectFilter || x.projectId === projectFilter)).sort((a, b) => Number(b.live) - Number(a.live) || Number(!!b.waiting) - Number(!!a.waiting) || b.updatedAt.localeCompare(a.updatedAt)).slice(0, 12));
  const projectName = (id: string) => app.project(id)?.name ?? id;
  const repoOf = (x: AgentSession) => x.repository && x.repository !== x.title ? x.repository : projectName(x.projectId);

  let generation = 0;
  async function refresh(): Promise<void> {
    const mine = ++generation;
    try {
      const out: AgentSessionList = await agentSessionsApi.list();
      if (mine !== generation) return;
      list = out.sessions;
      terminalsListed = out.terminals;
      error = '';
    } catch (e) {
      if (mine === generation) error = e instanceof Error ? e.message : String(e);
    } finally {
      if (mine === generation) loaded = true;
    }
  }

  // One RunSession per run, kept while the page is open, so switching between sessions neither loses a feed nor attaches twice.
  // Plain caches, not state: they are read while drawing and must not make it draw again.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  const runViews = new Map<string, RunSession>();
  function runView(s: AgentSession): RunSession {
    let view = runViews.get(s.id);
    if (!view) { view = new RunSession(s.projectId, s.id); view.attach(); runViews.set(s.id, view); }
    return view;
  }
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  const terminals = new Map<string, TerminalSession>();
  function terminalView(s: AgentSession): TerminalSession {
    const next = asTerminal(s);
    const kept = terminals.get(s.id);
    if (!kept) { terminals.set(s.id, next); return next; }
    kept.state = next.state;
    return kept;
  }

  onMount(() => {
    void refresh();
    void loadLaunchers();
    const timer = setInterval(() => { if (!document.hidden) void refresh(); }, 4000);
    const stop = app.onReconnect(() => void refresh());
    return () => { clearInterval(timer); stop(); for (const v of runViews.values()) v.disconnect(); runViews.clear(); };
  });

  // ---- a terminal of the agent's own command line, on its own or for a task ----
  let launchers = $state<CatalogAgent[]>([]);
  let launchOpen = $state(false);
  let launchAgent = $state('');
  let launchProject = $state('');
  let launchTask = $state('');
  let launchBusy = $state(false);
  let launchError = $state('');
  const repositories = $derived(app.projects.filter(p => p.repoPath && !p.archivedAt && !p.deletedAt));
  const launchTasks = $derived(launchProject ? (app.scopes.get(launchProject)?.activeTasks ?? []).filter(t => t.state !== 'done') : []);
  async function loadLaunchers(): Promise<void> {
    if (app.remote || isPhone()) return;
    try {
      const [catalog, settings] = await Promise.all([selectionApi.catalog(), selectionApi.settings()]);
      launchers = connectedAgents(catalog, settings.agents).filter(a => nativeCLILaunchers.has(a.id));
    } catch { /* Fail closed: Settings lists what is connected and why not. */ }
  }
  $effect(() => { if (launchProject) app.enter(launchProject); });
  async function launch(): Promise<void> {
    const agent = launchers.find(a => a.id === launchAgent);
    if (!agent?.interactive || !launchProject || launchBusy) return;
    launchBusy = true; launchError = '';
    try {
      const made = await terminalApi.create(launchProject, agent.id, agent.interactive.device, launchTask || undefined);
      launchOpen = false; launchTask = '';
      await refresh();
      openAgents(`terminal:${made.id}`);
    } catch (e) { launchError = e instanceof Error ? e.message : String(e); }
    finally { launchBusy = false; }
  }
  function toggleLaunch(): void {
    launchOpen = !launchOpen;
    if (launchOpen) { launchAgent ||= launchers[0]?.id ?? ''; launchProject ||= repositories[0]?.id ?? ''; }
  }

  // ---- the task a terminal belongs to ----
  const taskTitle = (s: AgentSession) => s.taskId ? (app.scopes.get(s.projectId)?.tasks.find(t => t.id === s.taskId)?.title ?? s.title) : '';
  let linkBusy = $state(false);
  const linkable = $derived(selected?.kind === 'terminal' ? (app.scopes.get(selected.projectId)?.activeTasks ?? []).filter(t => t.state !== 'done') : []);
  $effect(() => { if (selected?.kind === 'terminal') app.enter(selected.projectId); });
  async function link(taskId: string): Promise<void> {
    if (!selected || selected.kind !== 'terminal' || linkBusy) return;
    linkBusy = true;
    try { await agentSessionsApi.associate(selected.projectId, selected.id, taskId); await refresh(); }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { linkBusy = false; }
  }
</script>

{#snippet card(x: AgentSession, tone: 'needs' | 'trouble' | 'running' | 'done', index: number)}
  <SessionCard session={x} {tone} {index} live={app.connection === 'live'} selected={x.key === key}
    title={taskTitle(x) || x.title}
    meta="{agentName(app.agents, x.agentId)} · {repoOf(x)}{x.taskId ? '' : ' · Standalone'}{x.kind === 'terminal' ? ' · Terminal' : ''}{x.launchedBy ? ' · Started by Salie' : ''}"
    doing={x.live
      ? { state: x.waiting ? 'Needs you' : 'Running', detail: doing(x) === sessionState(x) ? '' : doing(x), waiting: !!x.waiting }
      : { state: OUTCOME_LABELS[outcomeOf(x)], detail: timeAgo(x.updatedAt, app.now), outcome: outcomeOf(x) }}
    onopen={() => select(x.key)} onchange={() => void refresh()} />
{/snippet}

{#snippet groups()}
  {#if !shownCount}
    <p class="muted empty">{list.length ? 'No session matches this filter.' : 'No agent sessions yet. Start an agent on a task, ask Salie to, or open a new session.'}</p>
  {/if}
  {#if ov.needsYou.length}
    <section class="tray" aria-labelledby="ag-needs" data-tone="ask"><header><h2 id="ag-needs" class="section-title" data-tone="needs">Needs you</h2><span class="tray-note">{ov.needsYou.length}</span></header><ul class="cards">{#each ov.needsYou as x, i (x.key)}{@render card(x, 'needs', i)}{/each}</ul></section>
  {/if}
  {#if trouble.length}
    <section class="tray" aria-labelledby="ag-trouble"><header><h2 id="ag-trouble" class="section-title" data-tone="trouble">Needs a look</h2><span class="tray-note">{trouble.length}</span></header><ul class="cards">{#each trouble as x, i (x.key)}{@render card(x, 'trouble', i)}{/each}</ul></section>
  {/if}
  {#if ov.running.length}
    <section class="tray" aria-labelledby="ag-running"><header><h2 id="ag-running" class="section-title" data-tone="running">Running</h2><span class="tray-note">{ov.running.length}</span></header><ul class="cards">{#each ov.running as x, i (x.key)}{@render card(x, 'running', i)}{/each}</ul></section>
  {/if}
  {#if finished.length}
    <section class="tray" aria-labelledby="ag-recent"><header><h2 id="ag-recent" class="section-title">Recently finished</h2><span class="tray-note">{finished.length}</span></header><ul class="cards">{#each finished.slice(0, 12) as x, i (x.key)}{@render card(x, 'done', i)}{/each}</ul></section>
  {/if}
  {#if !terminalsListed}<p class="muted fine">Terminals are listed only on the computer they run on.</p>{/if}
{/snippet}

{#snippet runnersTray()}
  <section class="tray runners" aria-labelledby="ag-runners">
    <header><h2 id="ag-runners" class="section-title">Runners</h2></header>
    {#if app.runners.length}
      <ul class="tray-card runner-list">{#each app.runners as r (r.id)}<li data-online={r.online && !r.disabled}><span class="dot" data-tone={r.online && !r.disabled ? 'ok' : 'neutral'}></span><span class="title">{app.runnerName(r)}</span><span class="meta">{r.online && !r.disabled ? 'Available' : r.disabled ? 'Disabled' : 'Offline'} · {r.currentRuns}/{r.capacity}</span></li>{/each}</ul>
    {:else}<p class="muted fine">No runner is reporting.</p>{/if}
  </section>
{/snippet}

<div class="agents" class:has-session={!!key} class:pane={paneMode}>
  <!-- The page's heading is the app header's ("Agents", above): this page adds no second one, only what it can do. -->
  {#if !paneMode}
    <div class="head" data-testid="agents-overview-head">
      <p class="lede muted">Coding agents at work, and what needs you. Scheduled work is in the Calendar until it starts.</p>
      <div class="tools">
        <label class="sr-only" for="sessions-filter">Show</label>
        <select id="sessions-filter" class="select" bind:value={filter}>
          <option value="all">All sessions</option><option value="live">Running now</option><option value="task">For a task</option><option value="standalone">Standalone</option>
        </select>
        {#if providersOf(sessions).length > 1}<label class="sr-only" for="sessions-provider">Provider</label>
        <select id="sessions-provider" class="select" bind:value={provider}><option value="">All providers</option>{#each providersOf(sessions) as id (id)}<option value={id}>{agentName(app.agents, id)}</option>{/each}</select>{/if}
        {#if !windowScoped}<label class="sr-only" for="sessions-project">Project</label>
        <select id="sessions-project" class="select" bind:value={ownProject}><option value="">All projects</option>{#each app.projects as p (p.id)}<option value={p.id}>{p.name}</option>{/each}</select>{/if}
        {#if launchers.length}<button class="btn primary" type="button" aria-expanded={launchOpen} onclick={toggleLaunch}><Icon name="plus" size={14} />New session</button>{/if}
      </div>
    </div>
  {/if}

  {#if launchOpen && !paneMode}
    <form class="launch wb-rise" onsubmit={e => { e.preventDefault(); void launch(); }}>
      <label>Agent
        <select class="select" bind:value={launchAgent} disabled={launchBusy}>{#each launchers as a (a.id)}<option value={a.id}>{a.name}</option>{/each}</select>
      </label>
      <label>Repository
        <select class="select" bind:value={launchProject} onchange={() => (launchTask = '')} disabled={launchBusy}>{#each repositories as p (p.id)}<option value={p.id}>{p.name}</option>{/each}</select>
      </label>
      <label>For task <span class="muted">(optional)</span>
        <select class="select" bind:value={launchTask} disabled={launchBusy}><option value="">None: a standalone session</option>{#each launchTasks as t (t.id)}<option value={t.id}>{t.title}</option>{/each}</select>
      </label>
      <p class="muted">Opens the agent’s own command line on this computer with your CLI permissions. It is not a sandbox.</p>
      <div class="row">
        <button class="btn primary" disabled={launchBusy || !launchAgent || !launchProject} type="submit">{launchBusy ? 'Opening…' : 'Open session'}</button>
        <button class="btn quiet" type="button" disabled={launchBusy} onclick={() => (launchOpen = false)}>Cancel</button>
      </div>
      {#if !repositories.length}<p role="status">Add a local repository in Projects to open a session.</p>{/if}
      {#if launchError}<p role="alert" class="bad">{launchError}</p>{/if}
    </form>
  {/if}

  {#if error}<p role="alert" class="bad">{error} <button class="btn small" onclick={() => refresh()}>Refresh</button></p>{/if}

  <div class="split" class:two={!!key && !paneMode}>
    {#if !paneMode}
      <div class="overview" bind:this={sideEl} onscroll={() => { scrolled = sideEl?.scrollTop ?? 0; }}>
        {#if !loaded}<p role="status" class="muted">Loading sessions…</p>
        {:else if key}
          {#if summary.length}<ul class="summary" aria-label="Session summary">{#each summary as c (c.id)}<li data-kind={c.id} class="wb-pop">{c.text}</li>{/each}</ul>{/if}
          {@render groups()}
          {@render runnersTray()}
        {:else}
          <!-- With no session open the page uses its width: the sessions in a grid of small cards, and beside them what is happening now. -->
          <div class="wide">
            <div class="groups-col">{@render groups()}</div>
            <aside class="rail" aria-label="Where agents stand">
              <section class="tray stand" aria-labelledby="ag-stand" data-testid="agents-stand">
                <header><h2 id="ag-stand" class="section-title">Right now</h2></header>
                <ul class="stat-grid" aria-label="Session summary">
                  <li class="tray-card stat" data-kind="needsYou" data-zero={ov.counts.needsYou ? undefined : ''}><span class="stat-n">{ov.counts.needsYou}</span><span class="stat-label">{ov.counts.needsYou === 1 ? 'Needs you' : 'Need you'}</span></li>
                  <li class="tray-card stat" data-kind="running" data-zero={ov.counts.running ? undefined : ''}><span class="stat-n">{ov.counts.running}</span><span class="stat-label">Running</span></li>
                  <li class="tray-card stat" data-kind="failed" data-zero={ov.counts.failed ? undefined : ''}><span class="stat-n">{ov.counts.failed}</span><span class="stat-label">Failed</span></li>
                </ul>
              </section>
              {@render runnersTray()}
            </aside>
          </div>
        {/if}
      </div>
    {/if}

    {#if key && !selected}
      <section class="open" aria-label="Open session">
        {#if !loaded}<p role="status" class="muted empty">Opening the session…</p>
        {:else}
          <div class="gone" role="status">
            <p>This session is no longer on this computer.</p>
            <p class="muted">A task run stays in its task’s history. A terminal that was not running when Werkbord restarted is kept only for a short while.</p>
            <button class="btn" type="button" onclick={() => select('')}>All sessions</button>
          </div>
        {/if}
      </section>
    {:else if selected}
      {#key selected.key}
        <section class="open" aria-label="Open session" data-standalone={!taskful} data-kind={selected.kind}>
          <div class="trail"><Crumbs chain={trailChain} {names} /></div>
          <header class="bar">
            <div class="who">
              <h2 class="session-title">{taskTitle(selected) || selected.title}</h2>
              <p class="metaline" data-testid="session-meta">
                <span>{agentName(app.agents, selected.agentId)}</span><span>{repoOf(selected)}</span>
                <span class="state" data-waiting={!!selected.waiting} data-outcome={selected.live ? undefined : outcomeOf(selected)}>{sessionState(selected)}</span>
                {#if !selected.taskId}<span>Standalone</span>{/if}
                {#if app.connection !== 'live'}<span class="state" data-outcome="interrupted">Availability unknown</span>{/if}
              </p>
            </div>
            <div class="acts">
              
              {#if chain[0]?.kind !== 'salie'}<button class="btn small" type="button" onclick={() => backToSalie(selected.launchedBy)} data-testid="session-salie">{selected.launchedBy ? 'Return to Salie' : 'Ask Salie'}</button>{/if}
              {#if paneMode && others.length}<label class="sr-only" for="session-switch">Switch session</label>
              <select id="session-switch" class="select switch" data-testid="session-switch" value="" onchange={e => { const k = e.currentTarget.value; e.currentTarget.value = ''; if (k) location.hash = sessionsHref(k) + (router.query ? `?${router.query}` : ''); }}>
                <option value="">Switch session…</option>{#each others as x (x.key)}<option value={x.key}>{x.live ? (x.waiting ? '● ' : '') : '○ '}{taskTitle(x) || x.title} · {agentName(app.agents, x.agentId)}</option>{/each}
              </select>{/if}
              {#if !paneMode}<button class="btn small quiet close" type="button" onclick={() => select('')} aria-label="Close session" title="Back to all sessions" data-testid="session-close"><Icon name="close" size={14} /></button>{/if}
            </div>
          </header>
          {#if controls.reason && selected.live}<p class="viewonly" role="status" data-testid="session-viewonly">{controls.reason}</p>{/if}
          {#if selected.kind === 'terminal' && (linkable.length || selected.taskId)}
            <div class="link">
              <label>Task
                <select class="select" value={selected.taskId ?? ''} disabled={linkBusy} onchange={e => void link(e.currentTarget.value)}>
                  <option value="">None: a standalone session</option>
                  {#if selected.taskId && !linkable.some(t => t.id === selected.taskId)}<option value={selected.taskId}>{taskTitle(selected)}</option>{/if}
                  {#each linkable as t (t.id)}<option value={t.id}>{t.title}</option>{/each}
                </select>
              </label>
              <span class="muted fine">Linking only records which task this terminal is for. It sends nothing to the agent.</span>
            </div>
          {/if}
          <div class="body" class:with-context={taskful}>
            <div class="session">
              {#if selected.kind === 'run'}<RunSessionView session={runView(selected)} taskLink={false} header={false} variant="terminal" {controls} />
              {:else}<TerminalSessionView session={terminalView(selected)} identity={false} interactive={controls.terminal} />{/if}
            </div>
            {#if taskful}
              <aside class="context" aria-label="Task context" data-testid="session-context">
                <span class="k">Task</span>
                <a class="ctitle" href={withChain(taskHref(selected.projectId, selected.taskId!), here)} data-testid="session-task">{task?.title ?? taskTitle(selected)}</a>
                {#if taskStep}<p class="cstate"><strong>{taskStep.label}</strong>{taskStep.reason ? ` · ${taskStep.reason}` : ''}</p>{/if}
                {#each sessionPlans as plan (plan.id)}
                  <span class="k">Plan</span>
                  <a class="ctitle" href={withChain(`${projectHref(selected.projectId, 'work')}?plan=${encodeURIComponent(plan.id)}`, here)} data-testid="session-plan">{plan.title}</a>
                {/each}
                <a class="btn small" class:primary={!selected.live && selected.kind === 'run'} href={withChain(taskHref(selected.projectId, selected.taskId!), here)} data-testid={!selected.live && selected.kind === 'run' ? 'session-resume' : undefined}>{!selected.live && selected.kind === 'run' ? 'Resume from task' : 'Open task'}</a>
              </aside>
            {/if}
          </div>
        </section>
      {/key}
    {/if}
  </div>
</div>

<style>
  .agents { display: flex; flex-direction: column; height: 100%; min-height: 0; padding: 16px 24px 0; gap: 12px; box-sizing: border-box; }
  .agents.pane { padding: 0; gap: 0; }
  .head { display: flex; align-items: center; justify-content: space-between; gap: 12px 24px; flex-wrap: wrap; }
  .lede { margin: 0; font-size: 13px; max-width: 60ch; }
  .tools .select { width: auto; max-width: 200px; }
  .tools { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  h2 { font-size: 12px; font-weight: 600; color: var(--text-2); margin: 0; }
  /* A heading says how urgent its group is with a dot as well as a word. */
  h2[data-tone]::before { content: ''; display: inline-block; width: 7px; height: 7px; margin-right: 8px; border-radius: 50%; background: var(--text-2); vertical-align: 1px; }
  h2[data-tone='needs'] { color: var(--warn-text); }
  h2[data-tone='needs']::before { background: var(--amber); }
  h2[data-tone='trouble'] { color: var(--danger-text); }
  h2[data-tone='trouble']::before { background: var(--danger); }
  h2[data-tone='running']::before { background: var(--accent); }
  .launch { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px; padding: 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); }
  .launch label { display: grid; gap: 4px; font-size: 12px; }
  .launch p, .launch .row { grid-column: 1 / -1; margin: 0; }
  .row { display: flex; gap: 8px; }
  /* The list and the open session sit side by side: choosing a session never takes the list away. */
  .split { flex: 1; min-height: 0; display: grid; grid-template-columns: minmax(0, 1fr); gap: 16px; transition: grid-template-columns 280ms var(--ease); }
  .split.two { grid-template-columns: minmax(300px, 380px) minmax(0, 1fr); }
  .overview { min-height: 0; overflow-y: auto; padding: 2px 4px 24px 2px; display: grid; gap: 12px; align-content: start; scrollbar-gutter: stable; }
  .summary { list-style: none; margin: 0; padding: 0; display: flex; flex-wrap: wrap; gap: 8px; }
  .summary li { padding: 3px 10px; border-radius: 999px; font-size: 12px; background: var(--surface-2); color: var(--text-2); }
  .summary li[data-kind='needsYou'] { background: color-mix(in srgb, var(--amber) 22%, var(--surface)); color: var(--text); font-weight: 600; }
  .summary li[data-kind='failed'] { color: var(--danger-text); }
  .cards { list-style: none; margin: 0; padding: 0; display: grid; gap: 8px; }
  /* Open with no session: the cards sit in a grid, two or three across, and a rail beside them says what is happening. */
  .wide { display: grid; grid-template-columns: minmax(0, 1fr) minmax(15rem, 20rem); gap: 16px; align-items: start; }
  .groups-col { min-width: 0; display: grid; gap: 12px; align-content: start; }
  .groups-col .cards { grid-template-columns: repeat(auto-fill, minmax(17rem, 1fr)); }
  .rail { position: sticky; top: 0; display: grid; gap: 12px; min-width: 0; align-content: start; }
  .stat-grid { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
  .stat { display: grid; gap: 2px; padding: 10px 12px; }
  .stat-n { font-size: var(--fs-page); font-weight: 600; letter-spacing: -.025em; line-height: 1.1; font-variant-numeric: tabular-nums; }
  .stat-label { font-size: var(--fs-meta); color: var(--text-2); }
  .stat[data-kind='needsYou'] .stat-n { color: var(--warn-text); }
  .stat[data-kind='running'] .stat-n { color: var(--accent); }
  .stat[data-kind='failed'] .stat-n { color: var(--danger-text); }
  .stat[data-zero] .stat-n { color: var(--text-2); }
  .runner-list { list-style: none; margin: 0; padding: 0; overflow: clip; }
  .runner-list li { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 2px 8px; align-items: center; padding: 10px 12px; }
  .runner-list li + li { border-top: 1px solid var(--border); }
  .runner-list .meta { grid-column: 2; }
  .runner-list .title { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .title { font-size: 13px; font-weight: 600; overflow-wrap: anywhere; }
  .meta { font-size: 12px; color: var(--text-2); overflow-wrap: anywhere; }
  .state { padding: 1px 8px; border-radius: 999px; background: var(--surface-2); color: var(--text-2); font-size: 12px; }
  .state[data-waiting='true'] { background: color-mix(in srgb, var(--amber) 18%, var(--surface)); color: var(--text); }
  .state[data-outcome='failed'], .state[data-outcome='interrupted'] { color: var(--danger-text); }
  .state[data-outcome='completed'] { color: var(--ok-text); }
  .runner-list .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--text-2); opacity: .5; }
  .runner-list .dot[data-tone='ok'] { background: var(--ok); opacity: 1; }
  .open { min-width: 0; min-height: 0; display: flex; flex-direction: column; border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; background: var(--bg); margin-bottom: 12px; }
  .pane .open { border: 0; border-radius: 0; margin: 0; flex: 1; }
  .trail { padding: 6px 12px 0; background: var(--surface); }
  .bar { display: flex; align-items: center; gap: 12px; padding: 8px 12px; border-bottom: 1px solid var(--border); background: var(--surface); flex-wrap: wrap; }
  .who { display: grid; gap: 2px; min-width: 0; flex: 1; }
  .open .bar h2.session-title { font-size: 16px; font-weight: 600; color: var(--text); margin: 0; overflow-wrap: anywhere; text-transform: none; letter-spacing: normal; font-family: inherit; }
  .metaline { margin: 0; display: flex; flex-wrap: wrap; align-items: center; gap: 2px 12px; font-size: 12px; color: var(--text-2); }
  .acts { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
  .switch { max-width: 220px; }
  .close { min-width: 32px; padding-inline: 8px; }
  .viewonly { margin: 0; padding: 6px 12px; font-size: 13px; color: var(--text-2); background: var(--surface-2); border-bottom: 1px solid var(--border); }
  .link { display: flex; align-items: center; gap: 12px; padding: 6px 12px; border-bottom: 1px solid var(--border); flex-wrap: wrap; }
  .link label { display: flex; align-items: center; gap: 8px; font-size: 12px; }
  .body { flex: 1; min-height: 0; display: grid; grid-template-columns: minmax(0, 1fr); }
  .body.with-context { grid-template-columns: minmax(0, 1fr) 240px; }
  .session { min-width: 0; min-height: 0; display: flex; flex-direction: column; }
  .context { display: grid; align-content: start; gap: 6px; padding: 12px; border-left: 1px solid var(--border); background: var(--surface); font-size: 13px; overflow-y: auto; }
  .context .k { font-family: var(--mono); font-size: 11px; text-transform: uppercase; letter-spacing: .14em; color: var(--text-2); margin-top: 6px; }
  .context .k:first-child { margin-top: 0; }
  .ctitle { color: var(--text); font-weight: 500; overflow-wrap: anywhere; }
  .cstate { margin: 0; font-size: 12px; color: var(--text-2); overflow-wrap: anywhere; }
  .context .btn { justify-self: start; margin-top: 8px; }
  .empty, .gone { padding: 24px 0; }
  .gone { padding: 24px; }
  .fine { font-size: 12px; }
  .bad { color: var(--danger-text); }
  .sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
  @media (prefers-reduced-motion: no-preference) {
    /* The open session arrives from the side it lives on, so the list reads as the thing it came from. */
    .open { animation: pan 300ms var(--ease) both; }
    @keyframes pan { from { opacity: 0; transform: translateX(18px); } to { opacity: 1; transform: none; } }
  }
  @media (max-width: 1000px) { .wide { grid-template-columns: minmax(0, 1fr); } .rail { position: static; } }
  @media (max-width: 899px) {
    .agents { padding: 12px 12px 0; }
    /* Too narrow for both: the session takes the page, and closing it brings the list back as it was. */
    .split.two { grid-template-columns: minmax(0, 1fr); }
    .split.two .overview { display: none; }
    .body.with-context { grid-template-columns: minmax(0, 1fr); grid-template-rows: minmax(0, 1fr) auto; }
    .context { border-left: 0; border-top: 1px solid var(--border); max-height: 30%; }
    .btn, .switch { min-height: 44px; }
  }
</style>
