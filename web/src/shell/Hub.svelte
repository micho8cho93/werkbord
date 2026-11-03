<script lang="ts">
  import Icon from '../lib/Icon.svelte';
  import Inbox from './Inbox.svelte';
  import CatchUp from './CatchUp.svelte';
  import BriefCard from './BriefCard.svelte';
  import { model } from './model.svelte';
  import { contextKey, parseContext } from './project-context';
  import { viewState } from './view-state.svelte';
  import { standing } from './aggregate';
  import { current, pending, type Decision } from './decisions';
  import { decisions } from './decisions.svelte';
  import { flagCounts, needAction, readyCount, rowDetail, rowsOf, urgentFirst, type Flag, type Row } from './workqueue';
  import WorkOverview from '../lib/work/WorkOverview.svelte';
  import WorkRow from '../lib/work/WorkRow.svelte';
  import * as answering from './answering';
  import type { Question } from '../lib/types';
  import { runKey } from '../lib/agent-sessions';
  import { timeAgo } from '../lib/format';
  import { participants } from './context-header';
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
  // Overview: what is running and what is next. The counts sit in the headings of what they count; the chips on Queue & Plans take you to the first of each kind.
  const running = $derived(queueRows.filter(r => r.state === 'running'));
  const upNext = $derived((sections.find(s => s.id === 'next')?.rows ?? []).slice(0, 5));
  const people = $derived(participants(overview));
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
  const decide = {
    askable,
    fresh: (d: Decision) => current(d, model.overview),
    openable: (d: Decision) => d.workspace.kind === 'personal' ? !!d.runId && model.personalReady : !!d.href,
    answerWith: (q: Question, answer: string) => answering.answerWith(q, answer, model.overview, tick),
    onSettled: (q: Question) => answering.settled(q),
    notify: (message: string) => { model.notice = message; },
    open: openDecision,
  };

  // Pushes to the team repositories the person's own Werkbord is watching, for the project in view.
  const repoNews = $derived(model.repoChanges.filter(c => !model.selectedProject || c.context === contextKey(model.selectedProject)).slice(0, 5));
  const repoTeams = $derived([...new Set(repoNews.map(c => c.workspace).filter(Boolean))].map(id => ({ id, name: model.view?.items.find(i => i.id === id)?.name ?? 'team' })));
  function openRepo(context: string) {
    const p = parseContext(context);
    if (p) { model.selectProject(p); void model.openGit(); }
  }
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
  const detail = (workspace: string, item: QueueItem) => rowDetail(overview, workspace, item);
</script>

<section class="work work-view" class:compact aria-labelledby="work-h">
  <header class="work-heading"><div><h1 id="work-h">Work</h1><p class="muted">Your tasks, plans and agent activity.</p></div><div class="actions work-actions">
    <label class="work-search"><Icon name="search" size={15} /><span class="visually-hidden">Search work</span><input type="search" bind:value={search} placeholder="Search work…" /></label>
    <button class="btn" type="button" onclick={() => model.newProject()} data-testid="work-new-project"><Icon name="plus" size={15} />New project</button><button class="btn" disabled={!choices.length} onclick={() => openCreate('plan')}>New plan</button><button class="btn primary" disabled={!choices.length} onclick={() => openCreate('task')}><Icon name="plus" size={15} />New task</button>
    {#if creating}<div class="project-choices" role="group" aria-label="Choose a project for new work"><div><strong>New {creating}</strong><button class="btn quiet small" onclick={() => creating = ''} aria-label="Close project choices"><Icon name="close" size={14} /></button></div><p class="muted">Choose the project for this {creating}.</p>{#each choices as choice (choice.workspace + choice.project.id)}<button onclick={() => openCreate(creating as 'task' | 'plan', choice)}>{choice.title}<Icon name="right" size={13} /></button>{/each}</div>{/if}
  </div></header>
  <nav class="work-tabs" aria-label="Work sections"><button aria-current={section === 'overview' ? 'page' : undefined} onclick={() => section = 'overview'}>Overview</button><button aria-current={section === 'queue' ? 'page' : undefined} onclick={() => model.selectedProject ? model.openProjectWork() : section = 'queue'}>Queue &amp; Plans<span>{queueCount}</span></button></nav>
  {#if !overview}<p role="status" class="empty">Loading work…</p>{/if}
  {#if overview && !choices.length && !overview.entries.some(e => e.error || !e.summary)}
    <div class="first-project" data-testid="first-project"><div><strong>Start with a project</strong><p class="muted">A project holds your tasks, plans, agent runs, calendar and Git. Make one for yourself, or for a team.</p></div><button class="btn primary" type="button" onclick={() => model.newProject()}><Icon name="plus" size={15} />New project</button></div>
  {/if}
  {#each overview?.entries.filter(e => e.error || e.workspace.state !== 'ready' || !e.summary) ?? [] as e (e.workspace.id)}<p role="status" class="source-notice">{e.workspace.name}: {standing(e)}</p>{/each}
  {#if overview?.entries.some(e => e.summary?.queueIncomplete)}<p role="status" class="source-notice">Some work is outside this summary’s limit. Open a project to see its complete queue and plans.</p>{/if}
  {#if section === 'overview'}
    <WorkOverview waiting={waiting} {decide} running={running.slice(0, 5)} {upNext} ready={readyCount(queueRows)} plans={plans} plansEmpty={search ? 'No plans match your search.' : 'Create a plan to group related tasks. Individual tasks can stand on their own.'}
      {people} {showWhere} {now} whereOf={entryOf} {detail} isOpen={openHere} onopen={open} seeAll={() => (section = 'queue')} {compact}>
      {#snippet brief()}<BriefCard>{#if !model.selectedProject}<CatchUp {overview} waiting={overview?.entries.reduce((n, e) => n + (e.summary?.attention.length ?? 0), 0) ?? 0} />{/if}</BriefCard>{/snippet}
      {#snippet attention()}<details id="work-attention" class="attention"><summary>Questions and workspace notices</summary><Inbox {overview} embedded /></details>{/snippet}
      {#snippet extra()}
        {#if repoNews.length}
          <section class="tray" aria-labelledby="repo-news-h" data-testid="repo-news">
            <header><h2 id="repo-news-h" class="section-title">Git activity</h2><span class="tray-note">changed on GitHub</span>{#each repoTeams as t (t.id)}<button class="btn quiet small" type="button" aria-pressed={model.mutedTeams.includes(t.id)} onclick={() => model.setTeamMuted(t.id, !model.mutedTeams.includes(t.id))}>{model.mutedTeams.includes(t.id) ? 'Unmute' : 'Mute'} {t.name}</button>{/each}</header>
            <ul class="tray-card rows">{#each repoNews as c (c.repo + c.branch + c.sha)}<li><button class="task" onclick={() => openRepo(c.context)}><span class="task-content"><strong>{c.project}: {c.created ? 'new branch ' + c.branch : c.isDefault && c.count ? c.count + (c.count === 1 ? ' new commit on ' : ' new commits on ') + c.branch : c.branch + ' updated'}</strong><span class="context">{c.authors?.length ? c.authors.join(', ') + ' · ' : ''}{timeAgo(new Date(c.at).toISOString(), now)}</span></span><span class="open-icon"><Icon name="right" size={14} /></span></button></li>{/each}</ul>
          </section>
        {/if}
      {/snippet}
    </WorkOverview>
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
{#snippet plansTray(limit: number, id: string)}
  <section class="tray" aria-labelledby={id}>
    <header><h2 {id} class="section-title">Plans</h2><span class="tray-note">{plans.length}</span></header>
    <ul class="tray-card rows plans">{#each (limit ? plans.slice(0, limit) : plans) as row (row.workspace.id + row.item.href)}<li><button class="task" onclick={() => open(row.workspace.id, row.item.href)}><span class="task-content"><strong>{row.item.title}</strong><span class="context">{row.item.project}{#if showWhere}{' · ' + row.workspace.name}{/if}</span></span><span class="plan-progress">{row.item.detail}</span></button></li>{:else}<li class="empty">{search ? 'No plans match your search.' : 'Create a plan to group related tasks. Individual tasks can stand on their own.'}</li>{/each}</ul>
  </section>
{/snippet}
{#snippet task(row: Row, index: number = 0)}
  <WorkRow {row} {index} live={row.item.group === 'done' ? undefined : detail(row.workspace, row.item)} current={openHere(row.workspace, row.item.href)} where={showWhere ? entryOf(row.workspace) : undefined} {now} onopen={open} />
{/snippet}
<style>
 p{margin:6px 0 0;}
 .actions{position:relative;}
 .scope{display:inline-flex;gap:2px;padding:3px;margin-bottom:16px;border:1px solid var(--border);border-radius:var(--radius-sm);background:var(--surface-2);}
 .scope button{min-height:32px;padding:4px 12px;border:0;border-radius:calc(var(--radius-sm) - 2px);background:transparent;color:var(--text-2);font:inherit;font-size:13px;cursor:pointer;}
 .scope button[aria-pressed=true]{background:var(--surface);color:var(--text);font-weight:500;}
 .scope button:focus-visible{outline:2px solid var(--accent);outline-offset:1px;}
 h2{font-size:14px;margin:0;font-weight:600;}
 .count{font-size:12px;color:var(--text-2);font-variant-numeric:tabular-nums;}
 ul{list-style:none;padding:0;margin:0;}
 li+li{border-top:1px solid var(--border);}
 .task{display:flex;align-items:center;gap:12px;width:100%;text-align:left;padding:14px 8px;background:transparent;border:0;color:var(--text);cursor:pointer;border-radius:5px;}
 .task:hover{background:var(--hover);}
 .task-content{display:grid;gap:4px;min-width:0;flex:1;}
 strong{font-size:14px;font-weight:500;line-height:1.5;}
 .context{font-size:12px;color:var(--text-2);overflow-wrap:anywhere;}
 .open-icon{color:var(--text-2);opacity:0;}
 .task:hover .open-icon,.task:focus-visible .open-icon{opacity:1;}
 .empty{padding:16px 28px;color:var(--text-2);font-size:13px;line-height:1.6;}
 .plans{margin-top:12px;}
 .plan-progress{font-size:12px;color:var(--text-2);}
 .attention{border-top:1px solid var(--border);padding-top:14px;margin-top:24px;}
 .attention summary,.completed summary{cursor:pointer;padding:10px 0;color:var(--text-2);font-size:13px;}
 .source-notice{padding:12px 0;color:var(--text-2);font-size:13px;}
 .project-choices{position:absolute;z-index:5;right:0;top:calc(100% + 10px);width:320px;max-width:calc(100vw - 40px);background:var(--surface);border:1px solid var(--border);box-shadow:var(--win-sh);border-radius:8px;padding:14px;}
 .project-choices>div{display:flex;justify-content:space-between;align-items:center;}
 .project-choices p{font-size:12px;margin:0 0 10px;}
 .project-choices>button{display:flex;width:100%;justify-content:space-between;align-items:center;text-align:left;border:0;background:transparent;padding:12px 4px;color:var(--text);cursor:pointer;font:inherit;}
 .project-choices>button:hover{background:var(--surface-2);}
 @media(max-width:900px){.actions{flex-wrap:wrap;width:100%;}.plan-progress{max-width:110px;}.task{padding:14px 0;}.open-icon{opacity:1;}}
 .work-grid{display:grid;grid-template-columns:minmax(0,1fr) minmax(18rem,26rem);gap:24px;align-items:start;}
 .work-side{position:sticky;top:16px;display:grid;gap:16px;min-width:0;align-content:start;}
 .work-main{min-width:0;display:grid;gap:16px;align-content:start;}
 .queue-note{font-size:12px;color:var(--text-2);font-variant-numeric:tabular-nums;}
 .context{display:flex;flex-wrap:wrap;align-items:center;gap:2px 8px;}
 .rows{overflow:clip;}
 .rows .empty{padding:14px 16px;}
 .rows .task{padding:12px 14px;}
 @media(max-width:1100px){.work-grid{grid-template-columns:minmax(0,1fr);}.work-side{position:static;}}
 .compact .work-grid{grid-template-columns:minmax(0,1fr);}
 .compact .work-side{position:static;}
 /* Attention: the edge of a row says why it is worth a look; the chips above the queue count them and take you to the first. */ .flags{display:flex;flex-wrap:wrap;gap:8px;list-style:none;padding:0;margin:0 0 18px;}
 .flag-chip{display:inline-flex;align-items:center;gap:8px;min-height:32px;padding:4px 12px;border:1px solid var(--border);border-radius:99px;background:var(--surface);color:var(--text);font:inherit;font-size:13px;cursor:pointer;transition:transform 160ms var(--ease),border-color 140ms,background-color 140ms;}
 .flag-chip:hover{transform:translateY(-1px);border-color:color-mix(in srgb,var(--chip,var(--text-2)) 55%,var(--border));background:color-mix(in srgb,var(--chip,var(--text-2)) 7%,var(--surface));}
 .flag-chip:focus-visible,.task:focus-visible{outline:2px solid var(--accent);outline-offset:-2px;}
 .flag-dot{width:8px;height:8px;border-radius:50%;background:var(--chip,var(--text-2));}
 [data-flag=ask]{--chip:var(--amber);}
 [data-flag=failed]{--chip:var(--danger);}
 [data-flag=review]{--chip:var(--ok);}
 [data-flag=high]{--chip:var(--block,var(--accent));}
 li{position:relative;}
 .flags li{border-top:0;}
 .task{transition:background-color 140ms,box-shadow 200ms var(--ease);}
 .task:hover{background:color-mix(in srgb,var(--chip,var(--accent)) 6%,var(--surface));}
 .open-icon{transition:opacity 140ms,transform 200ms var(--ease);}
 .task:hover .open-icon,.task:focus-visible .open-icon{transform:translateX(3px);}
 .compact .work-grid{grid-template-columns:minmax(0,1fr);gap:16px;}
 .compact .work-side{position:static;}
 .compact .work-heading{flex-direction:column;align-items:stretch;gap:12px;}
 .compact .actions{flex-wrap:wrap;width:100%;}
 .compact .work-search{flex:1 1 160px;min-width:0;}
 .compact .task{padding:12px 8px;}
 .first-project { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin: 0 0 16px; padding: 14px 16px; border: 1px dashed var(--edge-strong); border-radius: var(--radius-sm); background: var(--tray); }
 .first-project p { margin: 2px 0 0; font-size: 13px; }
</style>
