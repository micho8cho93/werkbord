<script lang="ts">
  import { tick as afterRender } from 'svelte';
  import Icon from '../lib/Icon.svelte';
  import Inbox from './Inbox.svelte';
  import CatchUp from './CatchUp.svelte';
  import BriefCard from './BriefCard.svelte';
  import { model } from './model.svelte';
  import { contextKey, parseContext } from './project-context';
  import { viewState } from './view-state.svelte';
  import { executionLabel, standing } from './aggregate';
  import { current, pending, type Decision } from './decisions';
  import { decisions } from './decisions.svelte';
  import { FLAG_LABEL, flagCounts, needAction, readyCount, rowsOf, STATE_LABEL, urgentFirst, type Flag, type Row } from './workqueue';
  import QuestionCard from '../lib/QuestionCard.svelte';
  import * as answering from './answering';
  import type { Question } from '../lib/types';
  import { runKey } from '../lib/agent-sessions';
  import { timeAgo } from '../lib/format';
  import WorkspaceTag from './WorkspaceTag.svelte';
  import type { Overview, QueueItem } from './types';

  // `compact`: the list sits beside an open task or session, so it is one column and the decisions come first.
  let { overview, compact = false }: { overview: Overview | null; compact?: boolean } = $props();
  // The section, the search and whose work is shown are kept by the window, so Work is as it was left after a task and Back.
  let section = $state<string>(viewState.work.section);
  let search = $state(viewState.work.search);
  let creating = $state<'task' | 'plan' | ''>('');
  let now = $state(Date.now());
  $effect(() => { const timer = setInterval(() => now = Date.now(), 30000); return () => clearInterval(timer); });
  const all = $derived(overview?.entries.flatMap(e => (e.summary?.queue ?? []).map(item => ({ workspace: e.workspace, item }))) ?? []);
  const rows = $derived(all.filter(r => `${r.item.title} ${r.item.project} ${r.item.detail ?? ''}`.toLowerCase().includes(search.toLowerCase())));
  const groups = [{ id: 'now', title: 'Now' }, { id: 'next', title: 'Next' }, { id: 'later', title: 'Later' }];
  // Team work held by someone else is out of the way by default: this is where a person looks for what is theirs to do. Personal work and
  // unclaimed Team work have no holder, so they always show.
  let everyone = $state(viewState.work.everyone);
  $effect(() => { viewState.work = { section: section === 'queue' ? 'queue' : 'overview', search, everyone }; });
  const othersHold = $derived(all.some(r => r.item.owner && !r.item.mine && r.item.group !== 'plan' && r.item.group !== 'done'));
  const queueRows = $derived(rowsOf(overview, rows.filter(r => everyone || !r.item.owner || r.item.mine).map(r => ({ workspace: r.workspace.id, item: r.item }))));
  const entryOf = (id: string) => overview?.entries.find(e => e.workspace.id === id)?.workspace;
  // Which workspace a task is in only matters when there is more than one to tell apart.
  const showWhere = $derived((overview?.entries.length ?? 0) > 1 && !model.selectedProject);
  const sections = $derived(groups.map(g => ({ ...g, rows: g.id === 'later' ? queueRows.filter(r => r.item.group === g.id) : urgentFirst(queueRows.filter(r => r.item.group === g.id)) })));
  const note = (id: string, rows: Row[]) => {
    if (id === 'now') { const n = needAction(rows); return n === 1 ? '1 needs action' : `${n} need action`; }
    return id === 'next' ? `${readyCount(rows)} ready` : '';
  };

  // Your next decision: the question that has waited longest, in the project context the window has. It is what an agent is blocked on,
  // so it is taken in the order it arrived; everything else on this page keeps its own order.
  const waiting = $derived(pending(decisions.list, model.selectedProject, new Set(Object.keys(decisions.answered))));
  // Back from the session lands here: ask once for the queue as it is now rather than wait for the next refresh.
  $effect(() => { void model.refreshOverview(); });
  function openDecision(d: Decision) {
    // The session lists its open questions itself, and Back returns to Work with its project context, search and section as left.
    if (d.workspace.kind === 'personal' && d.runId) void model.openAgents(runKey(d.runId), [], true);
    else if (d.href) open(d.workspace.id, d.href);
  }

  /** A task or session opens beside the list when the window is wide enough, so the list stays where it was; otherwise it takes the page. */
  function open(workspace: string, href: string) {
    if (model.paneable) void model.openPane(workspace, href);
    else void model.goTo(workspace, href);
  }
  const openHere = (workspace: string, href: string) => model.pane?.workspace === workspace && model.pane.place === href;

  // What most deserves a look, counted by why, above the queue: each says how many and takes you to the first.
  const flags = $derived(flagCounts(queueRows));
  // Overview: where things stand in five numbers (each takes you to the first of its kind in the queue), what is running, and what is next.
  const running = $derived(queueRows.filter(r => r.state === 'running'));
  const upNext = $derived((sections.find(s => s.id === 'next')?.rows ?? []).slice(0, 5));
  const flagN = (flag: Flag) => flags.find(f => f.flag === flag)?.count ?? 0;
  const stats = $derived<{ id: string; n: number; label: string; tone: string; flag?: Flag }[]>([
    { id: 'ask', n: waiting.length || flagN('ask'), label: 'Need your answer', tone: 'ask', flag: 'ask' },
    { id: 'running', n: running.length, label: 'Running', tone: 'work' },
    { id: 'review', n: flagN('review'), label: 'Ready for review', tone: 'ok', flag: 'review' },
    { id: 'failed', n: flagN('failed'), label: 'Failed or blocked', tone: 'block', flag: 'failed' },
    { id: 'ready', n: readyCount(queueRows), label: 'Ready to start', tone: '' },
  ]);
  async function showFlag(flag?: Flag) {
    if (!flag) return;
    section = 'queue';
    await afterRender();
    seek(flag);
  }
  function seek(flag: Flag) {
    const el = document.querySelector<HTMLElement>(`.work-view [data-flag="${flag}"]`);
    el?.scrollIntoView({ behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth', block: 'center' });
    el?.querySelector<HTMLElement>('button.task')?.focus({ preventScroll: true });
  }

  // Questions are answered where they are listed, with the agent's own context in view, and the answer goes the same way it does from Inbox.
  let tick = $state(Date.now());
  $effect(() => { const timer = setInterval(() => (tick = Date.now()), 1000); return () => clearInterval(timer); });
  const canAnswer = $derived(answering.reachable(model.overview, tick));
  const askable = (d: Decision) => d.workspace.kind === 'personal' && !!d.question && canAnswer;
  const SHOWN = 3;
  const shownDecisions = $derived(waiting.slice(0, SHOWN));
  const answerWith = (q: Question, answer: string) => answering.answerWith(q, answer, model.overview, tick);
  const onSettled = (q: Question) => answering.settled(q);

  // Pushes to the team repositories the person's own Werkbord is watching, for the project in view.
  const repoNews = $derived(model.repoChanges.filter(c => !model.selectedProject || c.context === contextKey(model.selectedProject)).slice(0, 5));
  const repoTeams = $derived([...new Set(repoNews.map(c => c.workspace).filter(Boolean))].map(id => ({ id, name: model.view?.items.find(i => i.id === id)?.name ?? 'team' })));
  function openRepo(context: string) {
    const p = parseContext(context);
    if (p) { model.selectProject(p); void model.openGit(); }
  }
  const initials = (name: string) => name.split(/\s+/).filter(Boolean).slice(0, 2).map(w => w[0].toUpperCase()).join('') || '?';
  const plans = $derived(rows.filter(r => r.item.group === 'plan'));
  const queueCount = $derived(all.filter(r => !['done', 'plan'].includes(r.item.group)).length);
  const completed = $derived(rowsOf(overview, rows.filter(r => r.item.group === 'done').map(r => ({ workspace: r.workspace.id, item: r.item }))));
  const choices = $derived(overview?.entries.flatMap(e => (e.summary?.projects ?? []).map(p => ({ workspace: e.workspace.id, project: p, title: p.name + ' · ' + e.workspace.name }))) ?? []);
  const target = $derived(model.selectedProject ? choices.find(c => c.workspace === model.selectedProject?.workspace && c.project.id === model.selectedProject?.project) : choices.length === 1 ? choices[0] : undefined);
  function openCreate(kind: 'task' | 'plan', choice = target) {
    if (!choice) { creating = creating === kind ? '' : kind; return; }
    creating = '';
    const href = choice.project.href;
    model.goTo(choice.workspace, href + (href.includes('?') ? '&' : '?') + 'new=' + kind);
  }
  function detail(workspace: string, item: QueueItem) {
    if (item.detail?.startsWith('Blocked')) return item.detail;
    const execution = overview?.entries.find(e => e.workspace.id === workspace)?.summary?.work.find(w => w.href === item.href);
    return execution?.execution ? (execution.report?.stale ? 'Last reported · ' : '') + executionLabel(execution.execution) : item.detail?.replace('backlog', 'Ready to start').replace('in_progress', 'In progress').replace('doing', 'In progress').replace('available', 'Available');
  }
</script>

<section class="work work-view" class:compact aria-labelledby="work-h">
  <header class="work-heading"><div><h1 id="work-h">Work</h1><p class="muted">Your tasks, plans and agent activity.</p></div><div class="actions work-actions">
    <label class="work-search"><Icon name="search" size={15} /><span class="visually-hidden">Search work</span><input type="search" bind:value={search} placeholder="Search work…" /></label>
    <button class="btn" disabled={!choices.length} onclick={() => openCreate('plan')}>New plan</button><button class="btn primary" disabled={!choices.length} onclick={() => openCreate('task')}><Icon name="plus" size={15} />New task</button>
    {#if creating}<div class="project-choices" role="group" aria-label="Choose a project for new work"><div><strong>New {creating}</strong><button class="btn quiet small" onclick={() => creating = ''} aria-label="Close project choices"><Icon name="close" size={14} /></button></div><p class="muted">Choose the project for this {creating}.</p>{#each choices as choice (choice.workspace + choice.project.id)}<button onclick={() => openCreate(creating as 'task' | 'plan', choice)}>{choice.title}<Icon name="right" size={13} /></button>{/each}</div>{/if}
  </div></header>
  <nav class="work-tabs" aria-label="Work sections"><button aria-current={section === 'overview' ? 'page' : undefined} onclick={() => section = 'overview'}>Overview</button><button aria-current={section === 'queue' ? 'page' : undefined} onclick={() => model.selectedProject ? model.openProjectWork() : section = 'queue'}>Queue &amp; Plans<span>{queueCount}</span></button></nav>
  {#if !overview}<p role="status" class="empty">Loading work…</p>{/if}
  {#each overview?.entries.filter(e => e.error || e.workspace.state !== 'ready' || !e.summary) ?? [] as e (e.workspace.id)}<p role="status" class="source-notice">{e.workspace.name}: {standing(e)}</p>{/each}
  {#if overview?.entries.some(e => e.summary?.queueIncomplete)}<p role="status" class="source-notice">Some work is outside this summary’s limit. Open a project to see its complete queue and plans.</p>{/if}
  {#if section === 'overview'}
    <div class="work-grid overview">
      <div class="work-main">
        {#if repoNews.length}
          <section class="tray" aria-labelledby="repo-news-h" data-testid="repo-news">
            <header><h2 id="repo-news-h" class="section-title">Repository</h2><span class="tray-note">changed on GitHub</span>{#each repoTeams as t (t.id)}<button class="btn quiet small" type="button" aria-pressed={model.mutedTeams.includes(t.id)} onclick={() => model.setTeamMuted(t.id, !model.mutedTeams.includes(t.id))}>{model.mutedTeams.includes(t.id) ? 'Unmute' : 'Mute'} {t.name}</button>{/each}</header>
            <ul class="tray-card rows">{#each repoNews as c (c.repo + c.branch + c.sha)}<li><button class="task" onclick={() => openRepo(c.context)}><span class="task-content"><strong>{c.project}: {c.created ? 'new branch ' + c.branch : c.isDefault && c.count ? c.count + (c.count === 1 ? ' new commit on ' : ' new commits on ') + c.branch : c.branch + ' updated'}</strong><span class="context">{c.authors?.length ? c.authors.join(', ') + ' · ' : ''}{timeAgo(new Date(c.at).toISOString(), now)}</span></span><span class="open-icon"><Icon name="right" size={14} /></span></button></li>{/each}</ul>
          </section>
        {/if}
        <section class="tray stats" aria-labelledby="stats-h" data-testid="work-stats">
          <header><h2 id="stats-h" class="section-title">Where things stand</h2><span class="tray-note">{model.selectedProject ? model.contextLabel : 'all projects'}</span></header>
          <ul class="stat-grid">
            {#each stats as s (s.id)}<li class="tray-card stat" data-tone={s.tone} data-zero={s.n === 0 ? '' : undefined}><button type="button" class="stat-button" disabled={!s.flag} onclick={() => showFlag(s.flag)}><span class="stat-n">{s.n}</span><span class="stat-label">{s.label}</span></button></li>{/each}
          </ul>
        </section>
        {@render decisionBlock()}
        <BriefCard>{#if !model.selectedProject}<CatchUp {overview} waiting={overview?.entries.reduce((n, e) => n + (e.summary?.attention.length ?? 0), 0) ?? 0} />{/if}</BriefCard>
    <details id="work-attention" class="attention"><summary>Questions and workspace notices</summary><Inbox {overview} embedded /></details>

      </div>
      <div class="work-side">
        <section class="tray" aria-labelledby="running-h">
          <header><h2 id="running-h" class="section-title">Running now</h2><span class="tray-note">{running.length}</span></header>
          <ul class="tray-card rows">{#each running.slice(0, 5) as row, i (row.workspace + row.item.href)}{@render task(row, i)}{:else}<li class="empty">No agent is working right now.</li>{/each}</ul>
        </section>
        <section class="tray" aria-labelledby="next-h">
          <header><h2 id="next-h" class="section-title">Up next</h2><button type="button" class="link" onclick={() => section = 'queue'}>See all</button></header>
          <ul class="tray-card rows">{#each upNext as row, i (row.workspace + row.item.href)}{@render task(row, i)}{:else}<li class="empty">No tasks ready to pick up.</li>{/each}</ul>
        </section>
        {@render plansTray(3, 'plans-o-h')}
      </div>
    </div>
  {:else}
    {#if flags.length}
      <ul class="flags" aria-label="What needs a look" data-testid="work-flags">
        {#each flags as f (f.flag)}<li><button type="button" class="flag-chip" data-flag={f.flag} onclick={() => seek(f.flag)}><span class="flag-dot" aria-hidden="true"></span><strong>{f.count}</strong> {f.label.toLowerCase()}</button></li>{/each}
      </ul>
    {/if}
    <div class="work-grid">
    <div class="work-main">
    {#if othersHold}<div class="scope" role="group" aria-label="Whose work to show"><button type="button" aria-pressed={!everyone} onclick={() => everyone = false}>Mine and unclaimed</button><button type="button" aria-pressed={everyone} onclick={() => everyone = true}>Everyone</button></div>{/if}
    <div class="queue-stack">
      {#each sections as group (group.id)}
        <section class="queue-card" aria-labelledby="queue-{group.id}" data-section={group.id}>
          <header><h2 id="queue-{group.id}">{group.title}</h2>{#if group.id !== 'later'}<span class="queue-note">{note(group.id, group.rows)}</span>{/if}</header>
          {#if group.rows.length}<ul>{#each group.rows as row, i (row.workspace + row.item.href)}{@render task(row, i)}{/each}</ul>
          {:else}<p class="queue-empty">{search ? 'No tasks match your search.' : group.id === 'now' ? 'Nothing needs your attention right now.' : group.id === 'next' ? 'No tasks ready to pick up.' : 'Nothing planned for later.'}</p>{/if}
        </section>
      {/each}
      {#if completed.length}<section class="queue-card" aria-label="Completed work"><details class="queue-section completed"><summary>Completed<span class="count">{completed.length}</span></summary><ul>{#each completed as row, i (row.workspace + row.item.href)}{@render task(row, i)}{/each}</ul></details></section>{/if}
    </div>
    </div>
      <div class="work-side">
        {@render plansTray(0, 'plans-h')}
      </div>
    </div>
  {/if}
</section>
{#snippet decisionBlock()}
    <aside class="decision" class:urgent={waiting.length > 0} aria-labelledby="decision-h" data-testid="next-decision">
      <h2 id="decision-h" class="section-title">{waiting.length ? 'Waiting for you' : 'Your next decision'}{#if waiting.length}<span class="pill wb-pop" aria-label="{waiting.length} waiting">{waiting.length}</span>{/if}</h2>
      <div aria-live="polite" class="decision-list">
      {#if shownDecisions.length}
        <p class="decision-count">{waiting.length === 1 ? '1 waiting' : `${waiting.length} waiting`}, oldest first</p>
        {#each shownDecisions as d, i (d.key)}
          {@const here = current(d, model.overview)}
          {@const openable = d.workspace.kind === 'personal' ? !!d.runId && model.personalReady : !!d.href}
          <article class="decision-card wb-rise" style:--i={i} data-testid="decision" data-kind={d.question?.kind}>
            {#if !(askable(d) && d.question)}<p class="decision-prompt">{d.prompt}</p>{/if}
            <dl class="decision-facts">
              <div><dt>Task</dt><dd>{d.task || 'Task'}</dd></div>
              <div><dt>Project</dt><dd>{d.project || 'Project'}{#if showWhere} <WorkspaceTag workspace={d.workspace} />{/if}</dd></div>
              {#if d.agent}<div><dt>Agent</dt><dd>{d.agent}</dd></div>{/if}
              {#if d.askedAt}<div><dt>Asked</dt><dd>{timeAgo(d.askedAt, now)}</dd></div>{/if}
            </dl>
            {#if !here}<p class="decision-note" role="status">{d.workspace.name} is not answering. This is as last reported; it stays here until the workspace confirms it was answered.</p>{/if}
            {#if askable(d) && d.question}
              <QuestionCard question={{ ...d.question, state: 'pending' }} showMeta={false} revealContext={d.question.kind === 'approval'} {answerWith} {onSettled} notify={(message) => model.notice = message} />
              {#if openable}<button class="btn quiet small" type="button" onclick={() => openDecision(d)}>{d.workspace.kind === 'personal' ? 'Open session' : 'Open in ' + d.workspace.name}</button>{/if}
            {:else}
              <button class="btn primary" type="button" disabled={!(here && openable)} onclick={() => openDecision(d)}>{d.workspace.kind === 'personal' ? 'Open session' : 'Open in ' + d.workspace.name}</button>
              {#if here && !openable}<p class="decision-note">{d.workspace.kind === 'personal' ? 'Your own Werkbord is not ready to open this session.' : 'This workspace did not say where to answer. Open it to find the question.'}</p>{/if}
            {/if}
          </article>
        {/each}
        {#if waiting.length > SHOWN}<p class="decision-more">{waiting.length - SHOWN} more waiting, oldest first. Answer these and the next appears.</p>{/if}
      {:else}
        <p class="decision-empty">No agent is waiting on you.</p>
      {/if}
      </div>
    </aside>
{/snippet}
{#snippet plansTray(limit: number, id: string)}
  <section class="tray" aria-labelledby={id}>
    <header><h2 {id} class="section-title">Plans</h2><span class="tray-note">{plans.length}</span></header>
    <ul class="tray-card rows plans">{#each (limit ? plans.slice(0, limit) : plans) as row (row.workspace.id + row.item.href)}<li><button class="task" onclick={() => open(row.workspace.id, row.item.href)}><span class="task-content"><strong>{row.item.title}</strong><span class="context">{row.item.project}{#if showWhere} · {row.workspace.name}{/if}</span></span><span class="plan-progress">{row.item.detail}</span></button></li>{:else}<li class="empty">{search ? 'No plans match your search.' : 'Create a plan to group related tasks. Individual tasks can stand on their own.'}</li>{/each}</ul>
  </section>
{/snippet}
{#snippet task(row: Row, index: number = 0)}
  {@const live = row.item.group === 'done' ? undefined : detail(row.workspace, row.item)}
  <li data-flag={row.flag} class="wb-rise" style:--i={index}><button class="task" data-state={row.state} data-flag={row.flag} aria-current={openHere(row.workspace, row.item.href) ? 'true' : undefined} onclick={() => open(row.workspace, row.item.href)}>
    <span class="task-content"><strong>{row.item.title}</strong><span class="context"><span class="proj">{row.item.project}</span>{#if row.item.plan}<span class="sep" aria-hidden="true">›</span><span class="plan">{row.item.plan}</span>{/if}{#if showWhere && entryOf(row.workspace)}<WorkspaceTag workspace={entryOf(row.workspace)!} />{/if}{#if live}<span class="detail" class:blocked={row.state === 'blocked'}>{live}</span>{/if}</span></span>
    {#if row.asking}<span class="ask-chip">Decision</span>{:else if row.flag}<span class="flag" data-flag={row.flag}>{FLAG_LABEL[row.flag]}</span>{/if}
    <span class="state" data-state={row.state}>{STATE_LABEL[row.state]}</span>
    {#if row.item.owner && row.item.group !== 'done'}<span class="owner-chip" data-me={row.item.mine ? '' : undefined} role="img" aria-label={row.item.mine ? 'Held by you' : 'Held by ' + row.item.owner} title={row.item.mine ? 'Held by you' : 'Held by ' + row.item.owner}>{initials(row.item.owner)}</span>{/if}
    <span class="task-state" data-state={row.state} aria-hidden="true">{#if row.state === 'done'}<Icon name="check" size={14} />{:else if row.state === 'running'}<span class="spin"></span>{/if}</span>
    <span class="open-icon"><Icon name="right" size={14} /></span>
  </button></li>
{/snippet}
<style>
 p{margin:6px 0 0;}.actions{position:relative;}.scope{display:inline-flex;gap:2px;padding:3px;margin-bottom:16px;border:1px solid var(--border);border-radius:var(--radius-sm);background:var(--surface-2);}.scope button{min-height:32px;padding:4px 12px;border:0;border-radius:calc(var(--radius-sm) - 2px);background:transparent;color:var(--text-2);font:inherit;font-size:13px;cursor:pointer;}.scope button[aria-pressed=true]{background:var(--surface);color:var(--text);font-weight:500;}.scope button:focus-visible{outline:2px solid var(--accent);outline-offset:1px;}h2{font-size:14px;margin:0;font-weight:600;}.count{font-size:12px;color:var(--text-2);font-variant-numeric:tabular-nums;}ul{list-style:none;padding:0;margin:0;}li+li{border-top:1px solid var(--border);}.task{display:flex;align-items:center;gap:12px;width:100%;text-align:left;padding:14px 8px;background:transparent;border:0;color:var(--text);cursor:pointer;border-radius:5px;}.task:hover{background:var(--hover);}.task-content{display:grid;gap:4px;min-width:0;flex:1;}strong{font-size:14px;font-weight:500;line-height:1.5;}.context,.detail{font-size:12px;color:var(--text-2);overflow-wrap:anywhere;}.blocked{color:var(--block-text);}.task-state{display:grid;place-items:center;flex:none;width:22px;height:22px;color:var(--text-2);}.open-icon{color:var(--text-2);opacity:0;}.task:hover .open-icon,.task:focus-visible .open-icon{opacity:1;}.empty{padding:16px 28px;color:var(--text-2);font-size:13px;line-height:1.6;}.plans{margin-top:12px;}.plan-progress{font-size:12px;color:var(--text-2);}.attention{border-top:1px solid var(--border);padding-top:14px;margin-top:24px;}.attention summary,.completed summary{cursor:pointer;padding:10px 0;color:var(--text-2);font-size:13px;}.source-notice{padding:12px 0;color:var(--text-2);font-size:13px;}.project-choices{position:absolute;z-index:5;right:0;top:calc(100% + 10px);width:320px;max-width:calc(100vw - 40px);background:var(--surface);border:1px solid var(--border);box-shadow:var(--win-sh);border-radius:8px;padding:14px;}.project-choices>div{display:flex;justify-content:space-between;align-items:center;}.project-choices p{font-size:12px;margin:0 0 10px;}.project-choices>button{display:flex;width:100%;justify-content:space-between;align-items:center;text-align:left;border:0;background:transparent;padding:12px 4px;color:var(--text);cursor:pointer;font:inherit;}.project-choices>button:hover{background:var(--surface-2);}
 @media(max-width:900px){.actions{flex-wrap:wrap;width:100%;}.plan-progress{max-width:110px;}.task{padding:14px 0;}.open-icon{opacity:1;}}

 .work-grid{display:grid;grid-template-columns:minmax(0,1fr) minmax(18rem,26rem);gap:24px;align-items:start;}.work-side{position:sticky;top:16px;display:grid;gap:16px;min-width:0;align-content:start;}.work-main{min-width:0;display:grid;gap:16px;align-content:start;}
 .queue-note{font-size:12px;color:var(--text-2);font-variant-numeric:tabular-nums;}
 .context{display:flex;flex-wrap:wrap;align-items:center;gap:2px 8px;}.context .sep{opacity:.6;}.context .plan{color:var(--text-2);}.context .detail::before{content:'·';margin-right:8px;}
 .task-state[data-state=action]::before{content:'';width:10px;height:10px;border-radius:50%;background:var(--amber);}.task-state[data-state=blocked]::before{content:'';width:10px;height:10px;border-radius:50%;border:2px dashed var(--block);}.task-state[data-state=done]{color:var(--ok);}
 /* Overview: five numbers on a tray, then trays for what is running, what is next and the plans. */
 .stat-grid{list-style:none;margin:0;padding:0;display:grid;grid-template-columns:repeat(auto-fit,minmax(6.5rem,1fr));gap:10px;}
 .stat-grid>li+li{border-top:0;}.stat{padding:0;overflow:clip;}
 .stat-button{display:grid;gap:2px;width:100%;padding:12px 14px;text-align:left;background:transparent;border:0;color:var(--text);font:inherit;cursor:pointer;}
 .stat-button:disabled{cursor:default;}.stat-button:not(:disabled):hover{background:var(--hover);}.stat-button:focus-visible{outline:2px solid var(--accent);outline-offset:-2px;}
 .stat-n{font-size:var(--fs-page);font-weight:600;letter-spacing:-.025em;line-height:1.1;font-variant-numeric:tabular-nums;}
 .stat-label{font-size:var(--fs-meta);color:var(--text-2);}
 .stat[data-tone=ask] .stat-n{color:var(--warn-text);}.stat[data-tone=work] .stat-n{color:var(--accent);}.stat[data-tone=ok] .stat-n{color:var(--ok-text);}.stat[data-tone=block] .stat-n{color:var(--block-text,var(--text));}
 .stat[data-zero] .stat-n{color:var(--text-2);}
 .rows{overflow:clip;}.rows .empty{padding:14px 16px;}.rows .task{padding:12px 14px;}
 .link{margin-left:auto;border:0;background:none;color:var(--accent);font:inherit;font-size:var(--fs-meta);cursor:pointer;padding:0;}.link:hover{text-decoration:underline;}
 .decision.urgent{background:var(--tray-ask);border-color:var(--tray-ask-edge);}
 .state{font-size:12px;color:var(--text-2);white-space:nowrap;}.state[data-state=action]{color:var(--warn-text);font-weight:500;}.state[data-state=blocked]{color:var(--block-text,var(--text-2));}.state[data-state=running]{color:var(--accent);}.state[data-state=ready],.state[data-state=done]{opacity:.8;}
 .ask-chip{font-size:11px;padding:1px 8px;border:1px solid var(--border);border-radius:99px;color:var(--text-2);}
 .decision{border:1px solid var(--border);border-radius:var(--radius);background:var(--surface);padding:16px;display:grid;gap:10px;}
 .decision-count{margin:0;font-size:12px;color:var(--text-2);}.decision-prompt{margin:0;font-size:14px;line-height:1.5;overflow-wrap:anywhere;display:-webkit-box;-webkit-line-clamp:6;line-clamp:6;-webkit-box-orient:vertical;overflow:hidden;}
 .decision-facts{margin:0;display:grid;gap:6px;font-size:12px;}.decision-facts div{display:grid;grid-template-columns:64px 1fr;gap:8px;}.decision-facts dt{color:var(--text-2);}.decision-facts dd{margin:0;overflow-wrap:anywhere;}
 .decision-note{margin:0;font-size:12px;color:var(--text-2);line-height:1.5;}.decision-empty{margin:0;font-size:13px;color:var(--text-2);}.decision .btn{justify-self:start;}
 @media(max-width:1100px){.work-grid{grid-template-columns:minmax(0,1fr);}.work-side{position:static;}}
 .compact .work-grid{grid-template-columns:minmax(0,1fr);}.compact .work-side{position:static;}

 /* Attention: the edge of a row says why it is worth a look; the chips above the queue count them and take you to the first. */
 .flags{display:flex;flex-wrap:wrap;gap:8px;list-style:none;padding:0;margin:0 0 18px;}
 .flag-chip{display:inline-flex;align-items:center;gap:8px;min-height:32px;padding:4px 12px;border:1px solid var(--border);border-radius:99px;background:var(--surface);color:var(--text);font:inherit;font-size:13px;cursor:pointer;transition:transform 160ms var(--ease),border-color 140ms,background-color 140ms;}
 .flag-chip:hover{transform:translateY(-1px);border-color:color-mix(in srgb,var(--chip,var(--text-2)) 55%,var(--border));background:color-mix(in srgb,var(--chip,var(--text-2)) 7%,var(--surface));}
 .flag-chip:focus-visible,.task:focus-visible{outline:2px solid var(--accent);outline-offset:-2px;}
 .flag-dot{width:8px;height:8px;border-radius:50%;background:var(--chip,var(--text-2));}
 [data-flag=ask]{--chip:var(--amber);}[data-flag=failed]{--chip:var(--danger);}[data-flag=review]{--chip:var(--ok);}[data-flag=high]{--chip:var(--block,var(--accent));}
 li{position:relative;}
 .flags li{border-top:0;}
 .task{transition:background-color 140ms,box-shadow 200ms var(--ease);}
 .task[data-flag]{box-shadow:inset 3px 0 0 var(--chip);}
 .task[data-flag=ask]{background:color-mix(in srgb,var(--amber) 8%,var(--surface));}
 .task:hover{background:color-mix(in srgb,var(--chip,var(--accent)) 6%,var(--surface));}
 .task[aria-current=true]{background:color-mix(in srgb,var(--accent) 9%,var(--surface));box-shadow:inset 3px 0 0 var(--accent);}
 .open-icon{transition:opacity 140ms,transform 200ms var(--ease);}
 .task:hover .open-icon,.task:focus-visible .open-icon{transform:translateX(3px);}
 .flag{font-size:11px;font-weight:500;padding:1px 8px;border-radius:99px;white-space:nowrap;color:var(--text);background:color-mix(in srgb,var(--chip) 16%,var(--surface));border:1px solid color-mix(in srgb,var(--chip) 40%,var(--border));}
 .ask-chip{color:var(--text);background:color-mix(in srgb,var(--amber) 20%,var(--surface));border-color:color-mix(in srgb,var(--amber) 55%,var(--border));font-weight:500;}
 .decision{transition:border-color 200ms,box-shadow 300ms;}
 .decision.urgent{border-color:color-mix(in srgb,var(--warn) 55%,var(--border));}
 .decision h2{display:flex;align-items:center;gap:8px;}
 .pill{min-width:22px;padding:1px 7px;border-radius:99px;background:var(--amber);color:#1d1b18;font-size:12px;font-weight:600;text-align:center;font-variant-numeric:tabular-nums;}
 .decision-list{display:grid;gap:14px;}
 .decision-card{display:grid;gap:10px;padding-top:14px;border-top:1px solid var(--border);}
 .decision-card:first-of-type{padding-top:0;border-top:0;}
 .decision-more{margin:0;font-size:12px;color:var(--text-2);}
 .decision-card :global(.ask){margin:0;}
 .decision-card .btn.quiet{justify-self:start;}
 .compact .work-grid{grid-template-columns:minmax(0,1fr);gap:16px;}.compact .work-side{position:static;}
 .compact .decision{position:static;order:-1;}
 .compact .work-heading{flex-direction:column;align-items:stretch;gap:12px;}
 .compact .actions{flex-wrap:wrap;width:100%;}
 .compact .work-search{flex:1 1 160px;min-width:0;}
 .compact .task{padding:12px 8px;}
 .compact .ask-chip,.compact .flag{display:none;}
 @media(prefers-reduced-motion:no-preference){
  .decision.urgent{animation:wb-breathe 2.6s ease-in-out infinite;}
 }
</style>
