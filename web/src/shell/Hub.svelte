<script lang="ts">
  import Icon from '../lib/Icon.svelte';
  import Inbox from './Inbox.svelte';
  import CatchUp from './CatchUp.svelte';
  import { model } from './model.svelte';
  import { viewState } from './view-state.svelte';
  import { executionLabel, standing } from './aggregate';
  import { current, pending, type Decision } from './decisions';
  import { decisions } from './decisions.svelte';
  import { needAction, readyCount, rowsOf, STATE_LABEL, urgentFirst, type Row } from './workqueue';
  import { runKey } from '../lib/agent-sessions';
  import { timeAgo } from '../lib/format';
  import WorkspaceTag from './WorkspaceTag.svelte';
  import type { Overview, QueueItem } from './types';

  let { overview }: { overview: Overview | null } = $props();
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
  $effect(() => { viewState.work = { section: section === 'plans' ? 'plans' : 'queue', search, everyone }; });
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
  const decision = $derived<Decision | undefined>(waiting[0]);
  const reachable = $derived(!!decision && current(decision, model.overview));
  // Only where this person can act: their own Werkbord with the session to open, or a Team workspace that answered and named the place.
  const canOpen = $derived(!!decision && reachable && (decision.workspace.kind === 'personal' ? !!decision.runId && model.personalReady : !!decision.href));
  // Back from the session lands here: ask once for the queue as it is now rather than wait for the next refresh.
  $effect(() => { void model.refreshOverview(); });
  function openDecision(d: Decision) {
    // The session lists its open questions itself, and Back returns to Work with its project context, search and section as left.
    if (d.workspace.kind === 'personal' && d.runId) void model.openAgents(runKey(d.runId));
    else if (d.href) void model.open(d.workspace.id, d.href);
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

<section class="work work-view" aria-labelledby="work-h">
  <header class="work-heading"><div><h1 id="work-h">Work</h1><p class="muted">Your tasks, plans and agent activity.</p></div><div class="actions work-actions">
    <label class="work-search"><Icon name="search" size={15} /><span class="visually-hidden">Search work</span><input type="search" bind:value={search} placeholder="Search work…" /></label>
    <button class="btn" disabled={!choices.length} onclick={() => openCreate('plan')}>New plan</button><button class="btn primary" disabled={!choices.length} onclick={() => openCreate('task')}><Icon name="plus" size={15} />New task</button>
    {#if creating}<div class="project-choices" role="group" aria-label="Choose a project for new work"><div><strong>New {creating}</strong><button class="btn quiet small" onclick={() => creating = ''} aria-label="Close project choices"><Icon name="close" size={14} /></button></div><p class="muted">Choose the project for this {creating}.</p>{#each choices as choice (choice.workspace + choice.project.id)}<button onclick={() => openCreate(creating as 'task' | 'plan', choice)}>{choice.title}<Icon name="right" size={13} /></button>{/each}</div>{/if}
  </div></header>
  <nav class="work-tabs" aria-label="Work sections"><button aria-current={section === 'queue' ? 'page' : undefined} onclick={() => section = 'queue'}>Queue<span>{queueCount}</span></button><button aria-current={section === 'plans' ? 'page' : undefined} onclick={() => section = 'plans'}>Plans<span>{all.filter(r => r.item.group === 'plan').length}</span></button></nav>
  {#if !overview}<p role="status" class="empty">Loading work…</p>{/if}
  {#each overview?.entries.filter(e => e.error || e.workspace.state !== 'ready' || !e.summary) ?? [] as e (e.workspace.id)}<p role="status" class="source-notice">{e.workspace.name}: {standing(e)}</p>{/each}
  {#if overview?.entries.some(e => e.summary?.queueIncomplete)}<p role="status" class="source-notice">Some work is outside this summary’s limit. Open a project to see its complete queue and plans.</p>{/if}
  {#if section === 'queue'}
    <div class="work-grid">
    <div class="work-main">
    {#if othersHold}<div class="scope" role="group" aria-label="Whose work to show"><button type="button" aria-pressed={!everyone} onclick={() => everyone = false}>Mine and unclaimed</button><button type="button" aria-pressed={everyone} onclick={() => everyone = true}>Everyone</button></div>{/if}
    <div class="queue-stack">
      {#each sections as group (group.id)}
        <section class="queue-card" aria-labelledby="queue-{group.id}" data-section={group.id}>
          <header><h2 id="queue-{group.id}">{group.title}</h2>{#if group.id !== 'later'}<span class="queue-note">{note(group.id, group.rows)}</span>{/if}</header>
          {#if group.rows.length}<ul>{#each group.rows as row (row.workspace + row.item.href)}{@render task(row)}{/each}</ul>
          {:else}<p class="queue-empty">{search ? 'No tasks match your search.' : group.id === 'now' ? 'Nothing needs your attention right now.' : group.id === 'next' ? 'No tasks ready to pick up.' : 'Nothing planned for later.'}</p>{/if}
        </section>
      {/each}
      {#if completed.length}<section class="queue-card" aria-label="Completed work"><details class="queue-section completed"><summary>Completed<span class="count">{completed.length}</span></summary><ul>{#each completed as row (row.workspace + row.item.href)}{@render task(row)}{/each}</ul></details></section>{/if}
    </div>
    {#if !model.selectedProject}<CatchUp {overview} waiting={overview?.entries.reduce((n, e) => n + (e.summary?.attention.length ?? 0), 0) ?? 0} />{/if}
    <details id="work-attention" class="attention"><summary>Questions and workspace notices</summary><Inbox {overview} embedded /></details>
    </div>
    <aside class="decision" aria-labelledby="decision-h" data-testid="next-decision">
      <h2 id="decision-h">Your next decision</h2>
      <div aria-live="polite">
      {#if decision}
        <p class="decision-count">{waiting.length === 1 ? '1 waiting' : `${waiting.length} waiting`}, oldest first</p>
        <p class="decision-prompt">{decision.prompt}</p>
        <dl class="decision-facts">
          <div><dt>Task</dt><dd>{decision.task || 'Task'}</dd></div>
          <div><dt>Project</dt><dd>{decision.project || 'Project'}{#if showWhere} <WorkspaceTag workspace={decision.workspace} />{/if}</dd></div>
          {#if decision.agent}<div><dt>Agent</dt><dd>{decision.agent}</dd></div>{/if}
          {#if decision.askedAt}<div><dt>Asked</dt><dd>{timeAgo(decision.askedAt, now)}</dd></div>{/if}
        </dl>
        {#if !reachable}<p class="decision-note" role="status">{decision.workspace.name} is not answering. This is as last reported; it stays here until the workspace confirms it was answered.</p>{/if}
        <button class="btn primary" type="button" disabled={!canOpen} onclick={() => decision && openDecision(decision)}>{decision.workspace.kind === 'personal' ? 'Open session' : 'Open in ' + decision.workspace.name}</button>
        {#if reachable && !canOpen}<p class="decision-note">{decision.workspace.kind === 'personal' ? 'Your own Werkbord is not ready to open this session.' : 'This workspace did not say where to answer. Open it to find the question.'}</p>{/if}
      {:else}
        <p class="decision-empty">No agent is waiting on you.</p>
      {/if}
      </div>
    </aside>
    </div>
  {:else}
    <section aria-labelledby="plans-h"><h2 id="plans-h" class="visually-hidden">Plans</h2><p class="section-note muted">Related tasks, one shared objective.</p><ul class="plans">{#each plans as row (row.workspace.id + row.item.href)}<li><button class="task" onclick={() => model.goTo(row.workspace.id, row.item.href)}><span class="plan-symbol"><Icon name="dependencies" size={18} /></span><span class="task-content"><strong>{row.item.title}</strong><span class="context">{row.item.project} · {row.workspace.name}</span></span><span class="plan-progress">{row.item.detail}</span><Icon name="right" size={14} /></button></li>{:else}<li class="empty">{search ? 'No plans match your search.' : 'Create a plan to group related tasks. Individual tasks can stand on their own.'}</li>{/each}</ul></section>
  {/if}
</section>
{#snippet task(row: Row)}
  {@const live = row.item.group === 'done' ? undefined : detail(row.workspace, row.item)}
  <li><button class="task" data-state={row.state} onclick={() => model.goTo(row.workspace, row.item.href)}>
    <span class="task-marker" data-state={row.state} aria-hidden="true">{#if row.state === 'done'}<Icon name="check" size={10} />{/if}</span>
    <span class="task-content"><strong>{row.item.title}</strong><span class="context"><span class="proj">{row.item.project}</span>{#if row.item.plan}<span class="sep" aria-hidden="true">›</span><span class="plan">{row.item.plan}</span>{/if}{#if showWhere && entryOf(row.workspace)}<WorkspaceTag workspace={entryOf(row.workspace)!} />{/if}{#if live}<span class="detail" class:blocked={row.state === 'blocked'}>{live}</span>{/if}</span></span>
    {#if row.asking}<span class="ask-chip">Decision</span>{/if}
    <span class="state" data-state={row.state}>{STATE_LABEL[row.state]}</span>
    {#if row.item.owner && row.item.group !== 'done'}<span class="owner-chip" data-me={row.item.mine ? '' : undefined} role="img" aria-label={row.item.mine ? 'Held by you' : 'Held by ' + row.item.owner} title={row.item.mine ? 'Held by you' : 'Held by ' + row.item.owner}>{initials(row.item.owner)}</span>{/if}
    <span class="open-icon"><Icon name="right" size={14} /></span>
  </button></li>
{/snippet}
<style>
 p{margin:6px 0 0;}.actions{position:relative;}.scope{display:inline-flex;gap:2px;padding:3px;margin-bottom:16px;border:1px solid var(--border);border-radius:var(--radius-sm);background:var(--surface-2);}.scope button{min-height:32px;padding:4px 12px;border:0;border-radius:calc(var(--radius-sm) - 2px);background:transparent;color:var(--text-2);font:inherit;font-size:13px;cursor:pointer;}.scope button[aria-pressed=true]{background:var(--surface);color:var(--text);font-weight:500;}.scope button:focus-visible{outline:2px solid var(--accent);outline-offset:1px;}h2{font-size:14px;margin:0;font-weight:600;}.count{font-size:12px;color:var(--text-2);font-variant-numeric:tabular-nums;}ul{list-style:none;padding:0;margin:0;}li+li{border-top:1px solid var(--border);}.task{display:flex;align-items:center;gap:12px;width:100%;text-align:left;padding:14px 8px;background:transparent;border:0;color:var(--text);cursor:pointer;border-radius:5px;}.task:hover{background:color-mix(in srgb,var(--accent) 3%,var(--surface));}.task-content{display:grid;gap:4px;min-width:0;flex:1;}strong{font-size:14px;font-weight:500;line-height:1.5;}.context,.detail{font-size:12px;color:var(--text-2);overflow-wrap:anywhere;}.blocked{color:var(--block-text);}.task-marker{height:14px;width:14px;border:1px solid var(--text-2);border-radius:50%;flex:none;}.open-icon{color:var(--text-2);opacity:0;}.task:hover .open-icon,.task:focus-visible .open-icon{opacity:1;}.empty{padding:16px 28px;color:var(--text-2);font-size:13px;line-height:1.6;}.section-note{margin-bottom:16px;font-size:13px;}.plans{margin-top:12px;}.plan-symbol{color:var(--text);background:color-mix(in srgb,var(--accent) 7%,var(--surface));width:32px;height:32px;display:grid;place-items:center;border-radius:6px;}.plan-progress{font-size:12px;color:var(--text-2);}.attention{border-top:1px solid var(--border);padding-top:14px;margin-top:24px;}.attention summary,.completed summary{cursor:pointer;padding:10px 0;color:var(--text-2);font-size:13px;}.source-notice{padding:12px 0;color:var(--text-2);font-size:13px;}.project-choices{position:absolute;z-index:5;right:0;top:calc(100% + 10px);width:320px;max-width:calc(100vw - 40px);background:var(--surface);border:1px solid var(--border);box-shadow:var(--win-sh);border-radius:8px;padding:14px;}.project-choices>div{display:flex;justify-content:space-between;align-items:center;}.project-choices p{font-size:12px;margin:0 0 10px;}.project-choices>button{display:flex;width:100%;justify-content:space-between;align-items:center;text-align:left;border:0;background:transparent;padding:12px 4px;color:var(--text);cursor:pointer;font:inherit;}.project-choices>button:hover{background:var(--surface-2);}
 @media(max-width:900px){.actions{flex-wrap:wrap;width:100%;}.plan-progress{max-width:110px;}.task{padding:14px 0;}.open-icon{opacity:1;}}

 .work-grid{display:grid;grid-template-columns:minmax(0,1fr) 300px;gap:28px;align-items:start;}.work-main{min-width:0;}
 .queue-note{font-size:12px;color:var(--text-2);font-variant-numeric:tabular-nums;}
 .context{display:flex;flex-wrap:wrap;align-items:center;gap:2px 8px;}.context .sep{opacity:.6;}.context .plan{color:var(--text-2);}.context .detail::before{content:'·';margin-right:8px;}
 .task-marker{display:grid;place-items:center;}.task-marker[data-state=action]{background:var(--amber);border-color:var(--amber);box-shadow:inset 0 0 0 3px var(--surface);}.task-marker[data-state=running]{background:var(--accent);border-color:var(--accent);box-shadow:inset 0 0 0 3px var(--surface);}.task-marker[data-state=blocked]{border-style:dashed;border-color:var(--block);}.task-marker[data-state=waiting]{border-style:dotted;}.task-marker[data-state=done]{background:var(--ok);border-color:var(--ok);color:var(--surface);}
 .state{font-size:12px;color:var(--text-2);white-space:nowrap;}.state[data-state=action]{color:var(--warn-text);font-weight:500;}.state[data-state=blocked]{color:var(--block-text,var(--text-2));}.state[data-state=running]{color:var(--accent);}.state[data-state=ready],.state[data-state=done]{opacity:.8;}
 .ask-chip{font-size:11px;padding:1px 8px;border:1px solid var(--border);border-radius:99px;color:var(--text-2);}
 .decision{position:sticky;top:16px;border:1px solid var(--border);border-radius:var(--radius);background:var(--surface);padding:16px;display:grid;gap:10px;}.decision h2{font-size:14px;}
 .decision-count{margin:0;font-size:12px;color:var(--text-2);}.decision-prompt{margin:0;font-size:14px;line-height:1.5;overflow-wrap:anywhere;display:-webkit-box;-webkit-line-clamp:6;line-clamp:6;-webkit-box-orient:vertical;overflow:hidden;}
 .decision-facts{margin:0;display:grid;gap:6px;font-size:12px;}.decision-facts div{display:grid;grid-template-columns:64px 1fr;gap:8px;}.decision-facts dt{color:var(--text-2);}.decision-facts dd{margin:0;overflow-wrap:anywhere;}
 .decision-note{margin:0;font-size:12px;color:var(--text-2);line-height:1.5;}.decision-empty{margin:0;font-size:13px;color:var(--text-2);}.decision .btn{justify-self:start;}
 @media(max-width:1100px){.work-grid{grid-template-columns:minmax(0,1fr);}.decision{position:static;order:-1;}}
</style>
