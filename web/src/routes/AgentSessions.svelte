<script lang="ts">
  import { onMount } from 'svelte';
  import { app } from '../lib/state.svelte';
  import { api } from '../lib/api';
  import type { WorkPlan } from '../lib/work-queue';
  import { router, taskHref } from '../lib/router.svelte';
  import { agentSessionsApi, asTerminal, isSessionKey, ordered, stateLabel, unique, type AgentSession, type AgentSessionList } from '../lib/agent-sessions';
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
  const implied = $derived(chain.length === 0 || chain.length === 1 && chain[0].kind === 'agents');
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
  const visible = $derived(ordered(unique(list)).filter(s =>
    (!projectFilter || s.projectId === projectFilter)
    && (filter === 'all' || (filter === 'live' ? s.live : filter === 'task' ? !!s.taskId : !s.taskId))));
  const needsYou = $derived(visible.filter(s => s.live && s.waiting));
  const running = $derived(visible.filter(s => s.live && !s.waiting));
  const recent = $derived(visible.filter(s => !s.live));
  const groups = $derived([{ label: 'Needs you', items: needsYou }, { label: 'Running', items: running }, { label: 'Recent', items: recent }]);
  const projectName = (id: string) => app.project(id)?.name ?? id;

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

<div class="agents" class:has-session={!!key}>
  <!-- The page's heading is the app header's ("Agents", above): this page adds no second one, only what it can do. -->
  {#if launchers.length}
    <div class="head"><button class="btn" type="button" aria-expanded={launchOpen} onclick={toggleLaunch}>New terminal</button></div>
  {/if}

  {#if launchOpen}
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
        <button class="btn primary" disabled={launchBusy || !launchAgent || !launchProject} type="submit">{launchBusy ? 'Opening…' : 'Open terminal'}</button>
        <button class="btn quiet" type="button" disabled={launchBusy} onclick={() => (launchOpen = false)}>Cancel</button>
      </div>
      {#if !repositories.length}<p role="status">Add a local repository in Projects to open a terminal.</p>{/if}
      {#if launchError}<p role="alert" class="bad">{launchError}</p>{/if}
    </form>
  {/if}

  <div class="layout">
    <aside class="side" aria-label="Sessions" bind:this={sideEl} onscroll={() => { scrolled = sideEl?.scrollTop ?? 0; }}>
      <div class="filters">
        <label class="sr-only" for="sessions-filter">Show</label>
        <select id="sessions-filter" class="select" bind:value={filter}>
          <option value="all">All sessions</option><option value="live">Running now</option><option value="task">For a task</option><option value="standalone">Standalone</option>
        </select>
        {#if !windowScoped}<label class="sr-only" for="sessions-project">Project</label>{/if}
        {#if !windowScoped}<select id="sessions-project" class="select" bind:value={ownProject}>
          <option value="">All projects</option>{#each app.projects as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
        </select>{/if}
      </div>
      {#if error}<p role="alert" class="bad">{error} <button class="btn small" onclick={() => refresh()}>Refresh</button></p>{/if}
      {#if !loaded}<p role="status" class="muted">Loading sessions…</p>
      {:else if !visible.length}
        <p class="muted empty">{list.length ? 'No session matches this filter.' : 'No agent sessions yet. Start an agent on a task, ask Salie to, or open a terminal.'}</p>
      {/if}
      {#each groups as group (group.label)}
        {#if group.items.length}
          <section aria-label={group.label}>
            <h2>{group.label}</h2>
            <ul>
              {#each group.items as s (s.key)}
                <li>
                  <button type="button" class="item" aria-current={s.key === key ? 'true' : undefined} onclick={() => openAgents(s.key)} data-session={s.key}>
                    <span class="title">{taskTitle(s) || s.title}</span>
                    <span class="meta">{agentName(app.agents, s.agentId)} · {s.kind === 'terminal' ? 'Terminal' : 'Task run'} · {stateLabel(s)}</span>
                    <span class="meta">{projectName(s.projectId)}{s.taskId ? '' : ' · Standalone'}{s.launchedBy ? ' · Started by Salie' : ''}</span>
                  </button>
                </li>
              {/each}
            </ul>
          </section>
        {/if}
      {/each}
      {#if !terminalsListed && loaded}<p class="muted fine">Terminals are listed only on the computer they run on.</p>{/if}
      <section aria-label="Runners" class="runners">
        <h2>Runners</h2>
        {#if app.runners.length}
          <ul>{#each app.runners as r (r.id)}<li><span class="title">{r.name}</span><span class="meta">{r.online && !r.disabled ? 'Online' : r.disabled ? 'Disabled' : 'Offline'} · {r.currentRuns}/{r.capacity} running</span></li>{/each}</ul>
        {:else}<p class="muted fine">No runner is reporting.</p>{/if}
      </section>
    </aside>

    <section class="open" aria-label="Open session">
      {#if !key}
        <p class="muted empty">Choose a session to open it.</p>
      {:else if !selected}
        {#if !loaded}<p role="status" class="muted">Opening the session…</p>
        {:else}
          <div class="gone" role="status">
            <p>This session is no longer on this computer.</p>
            <p class="muted">A task run stays in its task’s history. A terminal that was not running when Werkbord restarted is kept only for a short while.</p>
            <button class="btn" type="button" onclick={() => openAgents()}>All sessions</button>
          </div>
        {/if}
      {:else}
        {#key selected.key}
          <div class="trail" class:implied><Crumbs chain={trailChain} {names} /></div>
          <div class="bar">
            <div class="who">
              <strong>{taskTitle(selected) || selected.title}</strong>
              <span class="muted">{agentName(app.agents, selected.agentId)} · {projectName(selected.projectId)} · {stateLabel(selected)}{selected.live ? '' : ' · Ended'}</span>
            </div>
            <div class="acts">
              {#if selected.taskId}<a class="btn small" href={withChain(taskHref(selected.projectId, selected.taskId), here)} data-testid="session-task">Open task</a>{/if}
              {#if chain[0]?.kind !== 'salie'}<button class="btn small" type="button" onclick={() => backToSalie(selected.launchedBy)} data-testid="session-salie">{selected.launchedBy ? 'Return to Salie' : 'Ask Salie'}</button>{/if}
            </div>
          </div>
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
          <div class="session">
            {#if selected.kind === 'run'}<RunSessionView session={runView(selected)} taskLink={false} />
            {:else}<TerminalSessionView session={terminalView(selected)} />{/if}
          </div>
        {/key}
      {/if}
    </section>
  </div>
</div>

<style>
  .agents { display: flex; flex-direction: column; height: 100%; min-height: 0; padding: 20px 24px 0; gap: 12px; box-sizing: border-box; }
  .head { display: flex; align-items: flex-start; justify-content: flex-end; gap: 16px; }
  h2 { font-size: 12px; font-weight: 600; color: var(--text-2); margin: 16px 0 6px; }
  .launch { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px; padding: 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); }
  .launch label { display: grid; gap: 4px; font-size: 12px; }
  .launch p, .launch .row { grid-column: 1 / -1; margin: 0; }
  .row { display: flex; gap: 8px; }
  .layout { flex: 1; min-height: 0; display: grid; grid-template-columns: minmax(240px, 320px) minmax(0, 1fr); gap: 16px; }
  .side { min-height: 0; overflow-y: auto; padding-bottom: 16px; }
  .filters { display: grid; gap: 6px; }
  ul { list-style: none; margin: 0; padding: 0; display: grid; gap: 4px; }
  .item { display: grid; gap: 2px; width: 100%; padding: 8px 10px; text-align: left; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; cursor: pointer; }
  .item:hover { background: var(--surface-2); }
  .item[aria-current='true'] { border-color: var(--accent); background: var(--surface-2); }
  .item:focus-visible, .btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .title { font-size: 13px; font-weight: 600; overflow-wrap: anywhere; }
  .meta { font-size: 12px; color: var(--text-2); overflow-wrap: anywhere; }
  .runners li { display: grid; gap: 2px; padding: 4px 2px; }
  .open { min-width: 0; min-height: 0; display: flex; flex-direction: column; border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; background: var(--bg); }
  .bar { display: flex; align-items: center; gap: 12px; padding: 8px 12px; border-bottom: 1px solid var(--border); background: var(--surface); flex-wrap: wrap; }
  .who { display: grid; gap: 2px; min-width: 0; flex: 1; }
  .who span { font-size: 12px; overflow-wrap: anywhere; }
  .acts { display: flex; gap: 8px; }
  .trail { padding: 6px 12px 0; background: var(--surface); }
  .link { display: flex; align-items: center; gap: 12px; padding: 6px 12px; border-bottom: 1px solid var(--border); flex-wrap: wrap; }
  .link label { display: flex; align-items: center; gap: 8px; font-size: 12px; }
  .session { flex: 1; min-height: 0; display: flex; flex-direction: column; }
  .empty, .gone { padding: 24px; }
  .fine { font-size: 12px; }
  .bad { color: var(--danger-text); }
  .sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
  /* Beside the list, "Agents" is already on screen: the way back to it is only drawn where the list is out of sight (a phone, a narrow window). */
  @media (min-width: 900px) { .trail.implied { display: none; } }
  @media (max-width: 899px) {
    .agents { padding: 12px 12px 0; }
    .layout { grid-template-columns: minmax(0, 1fr); }
    .agents.has-session .side { display: none; }
    .agents:not(.has-session) .open { display: none; }
    .item, .btn { min-height: 44px; }
  }
</style>
