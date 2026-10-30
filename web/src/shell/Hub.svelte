<script lang="ts">
 import Inbox from './Inbox.svelte';
 import { model } from './model.svelte';
 import type { Overview } from './types';
 import { standing } from './aggregate';
 let {overview}: {overview: Overview|null}=$props();
 const rows=$derived(overview?.entries.flatMap(e=>(e.summary?.queue??[]).map(item=>({workspace:e.workspace,item})))??[]);
 const groups=[{id:'now',title:'Now',detail:'Active work and immediate priorities'},{id:'next',title:'Next',detail:'Ready to pick up'},{id:'later',title:'Later',detail:'Planned or waiting on dependencies'}];
 const plans=$derived(rows.filter(r=>r.item.group==='plan'));
 const completed=$derived(rows.filter(r=>r.item.group==='done'));
 let destination=$state('');
 const choices=$derived(overview?.entries.flatMap(e=>(e.summary?.projects??[]).map(p=>({workspace:e.workspace.id,project:p,title:p.name+' · '+e.workspace.name})))??[]);
 const target=$derived(choices.find(c=>c.workspace+'/'+c.project.id===destination)??(model.selectedProject?choices.find(c=>c.workspace===model.selectedProject?.workspace&&c.project.id===model.selectedProject?.project):choices.length===1?choices[0]:undefined));
 function create(kind:'task'|'plan'){if(!target)return;const href=target.project.href;model.goTo(target.workspace,href+(href.includes('?')?'&':'?')+'new='+kind);}
</script>
<section class="work" aria-labelledby="work-h">
 <header><div><h1 id="work-h">Work</h1><p class="muted">Tasks and plans across {model.contextLabel}.</p></div><div class="actions">
 {#if !model.selectedProject && choices.length>1}<label class="visually-hidden" for="work-project">Project for new work</label><select id="work-project" bind:value={destination}><option value="">Choose project…</option>{#each choices as c (c.workspace+c.project.id)}<option value={c.workspace+'/'+c.project.id}>{c.title}</option>{/each}</select>{/if}
 <button class="btn" disabled={!target} onclick={()=>create('plan')}>New plan</button><button class="btn primary" disabled={!target} onclick={()=>create('task')}>New task</button>
 </div></header>
 {#if !overview}<p role="status">Loading work…</p>{/if}
 {#each overview?.entries.filter(e=>e.error||e.workspace.state!=='ready'||!e.summary)??[] as e (e.workspace.id)}<p role="status">{e.workspace.name}: {standing(e)}</p>{/each}
 {#if overview?.entries.some(e=>e.summary?.queueIncomplete)}<p role="status">Some work is outside this summary’s limit. Open a project in Work to see its complete queue and plans.</p>{/if}
 <div class="layout"><div class="queue">
 {#each groups as group (group.id)}{@const items=rows.filter(r=>r.item.group===group.id)}<section aria-labelledby="queue-{group.id}"><header><h2 id="queue-{group.id}">{group.title}</h2><span class="count">{items.length}</span><span class="muted small">{group.detail}</span></header><ul>{#each items as r (r.workspace.id+r.item.href)}<li><button class="task" onclick={()=>model.goTo(r.workspace.id,r.item.href)}><strong>{r.item.title}</strong><span class="context">{r.item.project} · {r.workspace.name}</span>{#if r.item.detail}<span class="detail" class:blocked={r.item.detail.startsWith('Blocked')}>{r.item.detail}</span>{/if}</button></li>{:else}<li class="empty">No tasks here.</li>{/each}</ul></section>{/each}
 {#if completed.length}<details><summary>Completed · {completed.length}</summary><ul>{#each completed as r (r.workspace.id+r.item.href)}<li><button class="task" onclick={()=>model.goTo(r.workspace.id,r.item.href)}>{r.item.title}<span class="context">{r.item.project} · {r.workspace.name}</span></button></li>{/each}</ul></details>{/if}
 <details class="attention"><summary>Questions and workspace notices</summary><Inbox {overview} embedded /></details>
 </div><aside aria-labelledby="plans-h"><h2 id="plans-h">Plans</h2><ul>{#each plans as r (r.workspace.id+r.item.href)}<li><button class="task" onclick={()=>model.goTo(r.workspace.id,r.item.href)}><strong>{r.item.title}</strong><span class="context">{r.item.project} · {r.workspace.name}</span><span class="detail">{r.item.detail}</span></button></li>{:else}<li class="empty">Group related tasks into a plan. Tasks can also stand on their own.</li>{/each}</ul></aside></div>
</section>
<style>
 .work{height:100%;overflow:auto;padding:20px clamp(16px,2vw,32px);} h1{margin:0;} p{margin:4px 0 0;} header{display:flex;align-items:center;justify-content:space-between;gap:16px;flex-wrap:wrap;margin-bottom:20px;} .actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap;} select{max-width:260px;}
 .layout{display:grid;grid-template-columns:minmax(0,1fr) minmax(280px,340px);gap:28px;} .queue,aside{min-width:0;} .queue>section{margin-bottom:24px;} section>header{justify-content:flex-start;gap:8px;margin-bottom:8px;align-items:baseline;} h2{font-size:16px;margin:0;} .count{font-size:12px;color:var(--text-2);font-variant-numeric:tabular-nums;} .small{font-size:12px;} ul{list-style:none;margin:8px 0 0;padding:0;border-top:1px solid var(--border);} li{border-bottom:1px solid var(--border);} .task{display:flex;flex-direction:column;gap:5px;width:100%;text-align:left;padding:12px 0;color:var(--text);background:transparent;border:0;cursor:pointer;} .task:hover strong{color:var(--accent-text);text-decoration:underline;} strong{font-weight:500;font-size:14px;} .context,.detail{font-size:12px;color:var(--text-2);overflow-wrap:anywhere;} .blocked{color:var(--block-text);} .empty{padding:14px 0;color:var(--text-2);font-size:13px;} .attention{margin-top:24px;} summary{cursor:pointer;padding:12px 0;font-size:13px;}
 @media(max-width:1000px){.layout{grid-template-columns:1fr;} aside{border-top:1px solid var(--border);padding-top:20px;}}
</style>
