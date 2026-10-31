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
  import { controlsFor, hasTaskContext, OUTCOME_LABELS, outcomeOf, overviewOf, providersOf, sessionState, summaryOf } from '../lib/agent-overview';
  import { stepOf } from '../lib/work-steps';
  import { timeAgo } from '../lib/format';
  import { projectHref } from '../lib/router.svelte';

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
  const names: Names = { task: (p, t) => app.scopes.get(p)?.tasks.find(x => x.id === t)?.title, plan: (_p, id) => plans.find(x => x.id === id)?.title };

  const key = $derived(isSessionKey(router.sub) ? router.sub : '');
  const selected = $derived(key ? list.find(s => s.key === key) : undefined);
  /** What the open session is to a page it opens: a way back to it, then to wherever it was reached from. */
  const here = $derived<Origin[]>(key ? pushed({ kind: 'session', key }, trailChain) : []);
  let provider = $state('');
  const sessions = $derived(unique(list));
  const ov = $derived(overviewOf(sessions, { project: projectFilter, provider, show: filter }));
  const summary = $derived(summaryOf(ov));
  const shownCount = $derived(ov.needsYou.length + ov.running.length + ov.recent.length);
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

{#snippet card(x: AgentSession, prominent: boolean)}
  <li class="card" class:prominent data-session={x.key} data-outcome={x.live ? undefined : outcomeOf(x)}>
    <button type="button" class="open-card" onclick={() => openAgents(x.key)} aria-label="Open {taskTitle(x) || x.title}">
      <span class="title">{taskTitle(x) || x.title}</span>
      <span class="meta">{agentName(app.agents, x.agentId)} · {repoOf(x)}{x.taskId ? '' : ' · Standalone'}{x.kind === 'terminal' ? ' · Terminal' : ''}{x.launchedBy ? ' · Started by Salie' : ''}</span>
      <span class="doing" data-live={x.live}>{#if x.live}<span class="state" data-waiting={!!x.waiting}>{x.waiting ? 'Needs you' : 'Running'}</span>{doing(x) === sessionState(x) ? '' : doing(x)}{:else}<span class="state" data-outcome={outcomeOf(x)}>{OUTCOME_LABELS[outcomeOf(x)]}</span>{timeAgo(x.updatedAt, app.now)}{/if}</span>
    </button>
  </li>
{/snippet}

<div class="agents" class:has-session={!!key}>
  <!-- The page's heading is the app header's ("Agents", above): this page adds no second one, only what it can do. -->
  {#if !key}
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
        {#if launchers.length}<button class="btn primary" type="button" aria-expanded={launchOpen} onclick={toggleLaunch}>New session</button>{/if}
      </div>
    </div>
  {/if}

  {#if launchOpen && !key}
    <form class="launch" onsubmit={e => { e.preventDefault(); void launch(); }}>
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

  {#if !key}
    <div class="overview" bind:this={sideEl} onscroll={() => { scrolled = sideEl?.scrollTop ?? 0; }}>
      {#if !loaded}<p role="status" class="muted">Loading sessions…</p>
      {:else}
        {#if summary.length}<ul class="summary" aria-label="Session summary">{#each summary as c (c.id)}<li data-kind={c.id}>{c.text}</li>{/each}</ul>{/if}
        {#if !shownCount}
          <p class="muted empty">{list.length ? 'No session matches this filter.' : 'No agent sessions yet. Start an agent on a task, ask Salie to, or open a new session.'}</p>
        {/if}
        {#if ov.needsYou.length}
          <section aria-labelledby="ag-needs"><h2 id="ag-needs">Needs you</h2><ul class="cards">{#each ov.needsYou as x (x.key)}{@render card(x, true)}{/each}</ul></section>
        {/if}
        {#if ov.running.length}
          <section aria-labelledby="ag-running"><h2 id="ag-running">Running</h2><ul class="cards">{#each ov.running as x (x.key)}{@render card(x, false)}{/each}</ul></section>
        {/if}
        {#if ov.recent.length}
          <section aria-labelledby="ag-recent"><h2 id="ag-recent">Recently finished</h2><ul class="cards">{#each ov.recent.slice(0, 12) as x (x.key)}{@render card(x, false)}{/each}</ul></section>
        {/if}
        {#if !terminalsListed}<p class="muted fine">Terminals are listed only on the computer they run on.</p>{/if}
        <section class="runners" aria-labelledby="ag-runners">
          <h2 id="ag-runners">Runners</h2>
          {#if app.runners.length}
            <ul class="runner-row">{#each app.runners as r (r.id)}<li data-online={r.online && !r.disabled}><span class="dot" data-tone={r.online && !r.disabled ? 'ok' : 'neutral'}></span><span class="title">{app.runnerName(r)}</span><span class="meta">{r.online && !r.disabled ? 'Available' : r.disabled ? 'Disabled' : 'Offline'} · {r.currentRuns}/{r.capacity}</span></li>{/each}</ul>
          {:else}<p class="muted fine">No runner is reporting.</p>{/if}
        </section>
      {/if}
    </div>
  {:else if !selected}
    <section class="open" aria-label="Open session">
      {#if !loaded}<p role="status" class="muted empty">Opening the session…</p>
      {:else}
        <div class="gone" role="status">
          <p>This session is no longer on this computer.</p>
          <p class="muted">A task run stays in its task’s history. A terminal that was not running when Werkbord restarted is kept only for a short while.</p>
          <button class="btn" type="button" onclick={() => openAgents()}>All sessions</button>
        </div>
      {/if}
    </section>
  {:else}
    {#key selected.key}
      <section class="open" aria-label="Open session" data-standalone={!taskful}>
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
            {#if !selected.live && selected.kind === 'run' && selected.taskId}<a class="btn small" href={withChain(taskHref(selected.projectId, selected.taskId), here)} data-testid="session-resume">Resume from task</a>{/if}
            {#if chain[0]?.kind !== 'salie'}<button class="btn small" type="button" onclick={() => backToSalie(selected.launchedBy)} data-testid="session-salie">{selected.launchedBy ? 'Return to Salie' : 'Ask Salie'}</button>{/if}
            {#if others.length}<label class="sr-only" for="session-switch">Switch session</label>
            <select id="session-switch" class="select switch" data-testid="session-switch" value="" onchange={e => { const k = e.currentTarget.value; e.currentTarget.value = ''; if (k) openAgents(k, chain); }}>
              <option value="">Switch session…</option>{#each others as x (x.key)}<option value={x.key}>{x.live ? (x.waiting ? '● ' : '') : '○ '}{taskTitle(x) || x.title} · {agentName(app.agents, x.agentId)}</option>{/each}
            </select>{/if}
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
            {#if selected.kind === 'run'}<RunSessionView session={runView(selected)} taskLink={false} header={false} {controls} />
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
              <a class="btn small" href={withChain(taskHref(selected.projectId, selected.taskId!), here)}>Open task</a>
            </aside>
          {/if}
        </div>
      </section>
    {/key}
  {/if}
</div>

<style>
  .agents { display: flex; flex-direction: column; height: 100%; min-height: 0; padding: 16px 24px 0; gap: 12px; box-sizing: border-box; }
  .head { display: flex; align-items: center; justify-content: space-between; gap: 12px 24px; flex-wrap: wrap; }
  .lede { margin: 0; font-size: 13px; max-width: 60ch; }
  .tools .select { width: auto; max-width: 200px; }
  .tools { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  h2 { font-size: 12px; font-weight: 600; color: var(--text-2); margin: 0 0 8px; }
  .launch { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px; padding: 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); }
  .launch label { display: grid; gap: 4px; font-size: 12px; }
  .launch p, .launch .row { grid-column: 1 / -1; margin: 0; }
  .row { display: flex; gap: 8px; }
  .overview { flex: 1; min-height: 0; overflow-y: auto; padding-bottom: 24px; display: grid; gap: 20px; align-content: start; max-width: 960px; }
  .summary { list-style: none; margin: 0; padding: 0; display: flex; flex-wrap: wrap; gap: 8px; }
  .summary li { padding: 3px 10px; border-radius: 999px; font-size: 12px; background: var(--surface-2); color: var(--text-2); }
  .summary li[data-kind='needsYou'] { background: color-mix(in srgb, var(--amber) 16%, var(--surface)); color: var(--text); }
  .summary li[data-kind='failed'] { color: var(--danger-text); }
  .cards { list-style: none; margin: 0; padding: 0; display: grid; gap: 6px; }
  .open-card { display: grid; gap: 3px; width: 100%; padding: 10px 12px; text-align: left; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; cursor: pointer; }
  .open-card:hover { background: var(--surface-2); }
  .prominent .open-card { padding: 14px 16px; border-left: 3px solid var(--warn); background: color-mix(in srgb, var(--amber) 8%, var(--surface)); }
  .open-card:focus-visible, .btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .title { font-size: 13px; font-weight: 600; overflow-wrap: anywhere; }
  .prominent .title { font-size: 14px; }
  .meta { font-size: 12px; color: var(--text-2); overflow-wrap: anywhere; }
  .doing { font-size: 12px; color: var(--text-2); display: flex; gap: 8px; align-items: baseline; flex-wrap: wrap; overflow-wrap: anywhere; }
  .state { padding: 1px 8px; border-radius: 999px; background: var(--surface-2); color: var(--text-2); font-size: 12px; }
  .state[data-waiting='true'] { background: color-mix(in srgb, var(--amber) 18%, var(--surface)); color: var(--text); }
  .state[data-outcome='failed'], .state[data-outcome='interrupted'] { color: var(--danger-text); }
  .state[data-outcome='completed'] { color: var(--ok-text); }
  .runners h2 { margin-top: 4px; }
  .runner-row { list-style: none; margin: 0; padding: 0; display: flex; flex-wrap: wrap; gap: 6px 16px; font-size: 12px; }
  .runner-row li { display: inline-flex; align-items: center; gap: 6px; }
  .runner-row .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--text-2); opacity: .5; }
  .runner-row .dot[data-tone='ok'] { background: var(--ok); opacity: 1; }
  .open { flex: 1; min-width: 0; min-height: 0; display: flex; flex-direction: column; border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; background: var(--bg); margin-bottom: 12px; }
  .trail { padding: 6px 12px 0; background: var(--surface); }
  .bar { display: flex; align-items: center; gap: 12px; padding: 8px 12px; border-bottom: 1px solid var(--border); background: var(--surface); flex-wrap: wrap; }
  .who { display: grid; gap: 2px; min-width: 0; flex: 1; }
  .open .bar h2.session-title { font-size: 16px; font-weight: 600; color: var(--text); margin: 0; overflow-wrap: anywhere; text-transform: none; letter-spacing: normal; font-family: inherit; }
  .metaline { margin: 0; display: flex; flex-wrap: wrap; align-items: center; gap: 2px 12px; font-size: 12px; color: var(--text-2); }
  .acts { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
  .switch { max-width: 220px; }
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
  @media (max-width: 899px) {
    .agents { padding: 12px 12px 0; }
    .body.with-context { grid-template-columns: minmax(0, 1fr); grid-template-rows: minmax(0, 1fr) auto; }
    .context { border-left: 0; border-top: 1px solid var(--border); max-height: 30%; }
    .open-card, .btn, .switch { min-height: 44px; }
  }
</style>
