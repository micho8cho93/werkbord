<script lang="ts">
  import Icon from '../lib/Icon.svelte';
  import Inbox from './Inbox.svelte';
  import CatchUp from './CatchUp.svelte';
  import { model } from './model.svelte';
  import { executionLabel, standing } from './aggregate';
  import type { Overview, QueueItem } from './types';

  let { overview }: { overview: Overview | null } = $props();
  let section = $state('queue');
  let search = $state('');
  let creating = $state<'task' | 'plan' | ''>('');
  const all = $derived(overview?.entries.flatMap(e => (e.summary?.queue ?? []).map(item => ({ workspace: e.workspace, item }))) ?? []);
  const rows = $derived(all.filter(r => `${r.item.title} ${r.item.project} ${r.item.detail ?? ''}`.toLowerCase().includes(search.toLowerCase())));
  const groups = [{ id: 'now', title: 'Now' }, { id: 'next', title: 'Next' }, { id: 'later', title: 'Later' }];
  const plans = $derived(rows.filter(r => r.item.group === 'plan'));
  const completed = $derived(rows.filter(r => r.item.group === 'done'));
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
  <nav class="work-tabs" aria-label="Work sections"><button aria-current={section === 'queue' ? 'page' : undefined} onclick={() => section = 'queue'}>Queue<span>{all.filter(r => !['done', 'plan'].includes(r.item.group)).length}</span></button><button aria-current={section === 'plans' ? 'page' : undefined} onclick={() => section = 'plans'}>Plans<span>{all.filter(r => r.item.group === 'plan').length}</span></button></nav>
  {#if !overview}<p role="status" class="empty">Loading work…</p>{/if}
  {#each overview?.entries.filter(e => e.error || e.workspace.state !== 'ready' || !e.summary) ?? [] as e (e.workspace.id)}<p role="status" class="source-notice">{e.workspace.name}: {standing(e)}</p>{/each}
  {#if overview?.entries.some(e => e.summary?.queueIncomplete)}<p role="status" class="source-notice">Some work is outside this summary’s limit. Open a project to see its complete queue and plans.</p>{/if}
  {#if section === 'queue'}
    <div class="queue">{#each groups as group (group.id)}{@const items = rows.filter(r => r.item.group === group.id)}<section aria-labelledby="queue-{group.id}"><header><h2 id="queue-{group.id}">{group.title}</h2><span class="count">{items.length}</span></header><ul>{#each items as row (row.workspace.id + row.item.href)}<li><button class="task" onclick={() => model.goTo(row.workspace.id, row.item.href)}><span class="task-marker" class:active={row.item.group === 'now'} aria-hidden="true"></span><span class="task-content"><strong>{row.item.title}</strong><span class="context">{row.item.project} · {row.workspace.name}{#if detail(row.workspace.id, row.item)}<span class="detail" class:blocked={row.item.detail?.startsWith('Blocked')}> · {detail(row.workspace.id, row.item)}</span>{/if}</span></span><span class="open-icon"><Icon name="right" size={14} /></span></button></li>{:else}<li class="empty">{search ? 'No tasks match your search.' : group.id === 'now' ? 'Nothing needs your attention right now.' : group.id === 'next' ? 'No tasks ready to pick up.' : 'Nothing planned for later.'}</li>{/each}</ul></section>{/each}
      {#if completed.length}<details class="completed"><summary>Completed · {completed.length}</summary><ul>{#each completed as row (row.workspace.id + row.item.href)}<li><button class="task" onclick={() => model.goTo(row.workspace.id, row.item.href)}><span class="task-marker complete"><Icon name="check" size={12} /></span><span class="task-content"><strong>{row.item.title}</strong><span class="context">{row.item.project} · {row.workspace.name}</span></span></button></li>{/each}</ul></details>{/if}
    </div>
    {#if !model.selectedProject}<CatchUp {overview} waiting={overview?.entries.reduce((n, e) => n + (e.summary?.attention.length ?? 0), 0) ?? 0} />{/if}
    <details id="work-attention" class="attention"><summary>Questions and workspace notices</summary><Inbox {overview} embedded /></details>
  {:else}
    <section aria-labelledby="plans-h"><h2 id="plans-h">Plans</h2><p class="section-note muted">Related tasks, one shared objective.</p><ul class="plans">{#each plans as row (row.workspace.id + row.item.href)}<li><button class="task" onclick={() => model.goTo(row.workspace.id, row.item.href)}><span class="plan-symbol"><Icon name="dependencies" size={18} /></span><span class="task-content"><strong>{row.item.title}</strong><span class="context">{row.item.project} · {row.workspace.name}</span></span><span class="plan-progress">{row.item.detail}</span><Icon name="right" size={14} /></button></li>{:else}<li class="empty">{search ? 'No plans match your search.' : 'Create a plan to group related tasks. Individual tasks can stand on their own.'}</li>{/each}</ul></section>
  {/if}
</section>
<style>
 p{margin:6px 0 0;}.actions{position:relative;}.queue>section{margin-bottom:26px;}.queue>section>header{display:flex;gap:8px;align-items:center;margin-bottom:6px;}h2{font-size:14px;margin:0;font-weight:600;}.count{font-size:12px;color:var(--text-2);font-variant-numeric:tabular-nums;}ul{list-style:none;padding:0;margin:0;}li+li{border-top:1px solid var(--border);}.task{display:flex;align-items:center;gap:12px;width:100%;text-align:left;padding:14px 8px;background:transparent;border:0;color:var(--text);cursor:pointer;border-radius:5px;}.task:hover{background:color-mix(in srgb,var(--accent) 3%,var(--surface));}.task-content{display:grid;gap:4px;min-width:0;flex:1;}strong{font-size:14px;font-weight:500;line-height:1.5;}.context,.detail{font-size:12px;color:var(--text-2);overflow-wrap:anywhere;}.blocked{color:var(--block-text);}.task-marker{height:14px;width:14px;border:1px solid var(--text-2);border-radius:50%;flex:none;}.task-marker.active{background:var(--accent);border-color:var(--accent);box-shadow:inset 0 0 0 3px var(--surface);}.task-marker.complete{display:grid;place-items:center;background:var(--ok);border-color:var(--ok);color:var(--surface);}.open-icon{color:var(--text-2);opacity:0;}.task:hover .open-icon,.task:focus-visible .open-icon{opacity:1;}.empty{padding:16px 28px;color:var(--text-2);font-size:13px;line-height:1.6;}.section-note{margin-bottom:16px;font-size:13px;}.plans{margin-top:12px;}.plan-symbol{color:var(--text);background:color-mix(in srgb,var(--accent) 7%,var(--surface));width:32px;height:32px;display:grid;place-items:center;border-radius:6px;}.plan-progress{font-size:12px;color:var(--text-2);}.attention{border-top:1px solid var(--border);padding-top:14px;margin-top:24px;}.attention summary,.completed summary{cursor:pointer;padding:10px 0;color:var(--text-2);font-size:13px;}.source-notice{padding:12px 0;color:var(--text-2);font-size:13px;}.project-choices{position:absolute;z-index:5;right:0;top:calc(100% + 10px);width:320px;max-width:calc(100vw - 40px);background:var(--surface);border:1px solid var(--border);box-shadow:var(--win-sh);border-radius:8px;padding:14px;}.project-choices>div{display:flex;justify-content:space-between;align-items:center;}.project-choices p{font-size:12px;margin:0 0 10px;}.project-choices>button{display:flex;width:100%;justify-content:space-between;align-items:center;text-align:left;border:0;background:transparent;padding:12px 4px;color:var(--text);cursor:pointer;font:inherit;}.project-choices>button:hover{background:var(--surface-2);}
 @media(max-width:900px){.actions{flex-wrap:wrap;width:100%;}.plan-progress{max-width:110px;}.task{padding:14px 0;}.open-icon{opacity:1;}}
</style>
