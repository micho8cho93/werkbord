<script lang="ts">
 import { onMount } from 'svelte';
 import { api } from '../api';
 import { app } from '../state.svelte';
 import { assistant } from '../assistant/sessions.svelte';
 import { taskHref } from '../location';
 import { router } from '../router.svelte';
 import type { ProjectScope } from '../scope.svelte';
 import type { Project } from '../types';
 import type { WorkPlan } from '../work-queue';
 let { scope, project }: {scope: ProjectScope; project: Project} = $props();
 let plans = $state<WorkPlan[]>([]), error = $state(''), busy = $state(false), loaded = $state(false);
 let selected = $state(new URLSearchParams(router.query).get('plan') ?? '');
 let draft = $state<WorkPlan | null>(null);
 const current = $derived(plans.find(p => p.id === selected));
 const revision = $derived(scope.tasks.map(t=>`${t.id}:${t.version}`).join('|'));
 $effect(() => { void revision; let live = true; api.workPlans(project.id).then(p => {if(live) {plans=p;loaded=true;}}, e => {if(live) error=e.message;}); return () => {live=false;}; });
 onMount(() => {const timer=setInterval(()=>{api.workPlans(project.id).then(p=>{plans=p;},()=>{});},5000);return ()=>clearInterval(timer);});
 function edit(plan?: WorkPlan) { draft = plan ? structuredClone($state.snapshot(plan)) : {id:'',projectId:project.id,title:'',description:'',taskIds:[],decisions:'',questions:'',version:0}; error=''; }
 $effect(() => { if(new URLSearchParams(router.query).get('new') === 'plan') edit(); });
 function reorder(index:number, delta:number) { if(!draft) return; const ids=[...draft.taskIds]; [ids[index],ids[index+delta]]=[ids[index+delta],ids[index]];draft.taskIds=ids; }
 async function save(e:SubmitEvent) {e.preventDefault();if(!draft || busy)return;busy=true;error='';try {const saved=await api.saveWorkPlan(project.id,draft);plans=[...plans.filter(p=>p.id!==saved.id),saved];selected=saved.id;draft=null;} catch(e) {error=e instanceof Error?e.message:String(e);} finally {busy=false;} }
 function askSalie() {if(!current)return;if(window.parent!==window){window.parent.postMessage({type:'werkbord.frame',event:'work_salie',projectId:project.id,planId:current.id},'*');return;}assistant.draft=`Help me edit Work plan ${current.id} in project ${project.id}: ${current.title}. Read the plan and its tasks first.`;assistant.show();}
</script>
<div class="plans">
 <header><h2>Plans</h2><button class="btn small" onclick={() => edit()}>New plan</button></header>
 {#if error}<p class="error" role="alert">{error} Your draft is kept; check current plans before retrying.</p>{/if}
 {#if draft}
  <form onsubmit={save}>
   <h3>{draft.id ? 'Edit plan' : 'New plan'}</h3>
   <label>Title<input class="input" required maxlength="200" bind:value={draft.title} /></label>
   <label>Description<textarea class="input" maxlength="20000" bind:value={draft.description}></textarea></label>
   <fieldset><legend>Tasks in order</legend>
    {#each draft.taskIds as id,i (id)}<div class="ordered"><span>{i+1}. {scope.tasks.find(t=>t.id===id)?.title ?? 'Unavailable task'}</span><button class="btn quiet small" type="button" disabled={i===0} onclick={()=>reorder(i,-1)} aria-label="Move task up">Up</button><button class="btn quiet small" type="button" disabled={i===draft!.taskIds.length-1} onclick={()=>reorder(i,1)} aria-label="Move task down">Down</button><button class="btn quiet small" type="button" onclick={()=>{if(draft)draft.taskIds=draft.taskIds.filter(t=>t!==id);}}>Remove</button></div>{/each}
    <label>Add an existing task<select onchange={e=>{if(draft && e.currentTarget.value) draft.taskIds=[...draft.taskIds,e.currentTarget.value];e.currentTarget.value='';}}><option value="">Choose a task…</option>{#each scope.activeTasks.filter(t=>!draft!.taskIds.includes(t.id)) as t (t.id)}<option value={t.id}>{t.title}</option>{/each}</select></label>
    <button class="btn quiet small" type="button" onclick={()=>app.newTaskOpen=true}>Create a task…</button>
   </fieldset>
   <label>Decisions<textarea class="input" maxlength="20000" bind:value={draft.decisions}></textarea></label>
   <label>Open questions<textarea class="input" maxlength="20000" bind:value={draft.questions}></textarea></label>
   <div class="actions"><button class="btn primary" disabled={busy || !draft.title.trim()}>{busy?'Saving…':'Save plan'}</button><button class="btn" type="button" disabled={busy} onclick={()=>draft=null}>Cancel</button></div>
  </form>
 {:else if current}
  <button class="btn quiet small" onclick={()=>selected=''}>All plans</button>
  <h3>{current.title}</h3><p class="description">{current.description}</p>
  {@const complete=current.taskIds.filter(id=>scope.tasks.find(t=>t.id===id)?.state==='done').length}
  <p class="muted">{complete} of {current.taskIds.length} tasks complete</p><progress max={current.taskIds.length || 1} value={complete} aria-label="Plan progress"></progress>
  <ol>{#each current.taskIds as id (id)}{@const task=scope.tasks.find(t=>t.id===id)}<li>{#if task}<a href={taskHref(project.id,id)}>{task.title}</a><span class="muted">{task.archivedAt ? 'Archived · ' : ''}{task.state}</span>{#if scope.latestRun[id]}<button class="btn quiet small" onclick={()=>assistant.openRun(project.id,scope.latestRun[id].id)}>Open CLI session</button>{/if}{#if task.orchestration?.dependencies?.length}<p class="muted">Depends on {task.orchestration.dependencies.map(dep=>scope.tasks.find(t=>t.id===dep)?.title ?? 'Unavailable task').join(', ')}</p>{/if}{:else}<span class="muted">Unavailable task</span>{/if}</li>{/each}</ol>
  {#if current.decisions}<h3>Decisions</h3><p class="description">{current.decisions}</p>{/if}
  {#if current.questions}<h3>Open questions</h3><p class="description">{current.questions}</p>{/if}
  <div class="actions"><button class="btn small" onclick={()=>edit(current)}>Edit plan</button><button class="btn small" onclick={askSalie}>Edit with Salie</button></div>
 {:else}
  {#each plans as plan (plan.id)}<button class="plan" onclick={()=>selected=plan.id}><strong>{plan.title}</strong><span>{plan.taskIds.filter(id=>scope.tasks.find(t=>t.id===id)?.state==='done').length} / {plan.taskIds.length} complete</span></button>{:else}<p class="muted">{loaded?'Group related tasks into a plan. Tasks can also stand on their own.':'Loading plans…'}</p>{/each}
 {/if}
</div>
<style>
 header,.actions { display:flex; align-items:center; justify-content:space-between; gap:8px; flex-wrap:wrap; } h2 { margin:0; font-size:16px; } h3 { font-size:14px; margin:18px 0 8px; }
 .plans { font-size:13px; } .plan { display:flex; flex-direction:column; align-items:flex-start; gap:4px; width:100%; text-align:left; padding:14px 0; border:0; border-bottom:1px solid var(--border); background:transparent; color:var(--text); cursor:pointer; } .plan:hover {color:var(--accent-text);} .plan span { color:var(--text-2); font-size:12px; }
 form,label { display:grid; gap:8px; } form { gap:14px; margin-top:14px; } textarea { min-height:80px; resize:vertical; } fieldset { border:1px solid var(--border); padding:12px; min-width:0; } .ordered { display:flex; flex-wrap:wrap; gap:4px; padding:8px 0; border-bottom:1px solid var(--border); } .ordered span { width:100%; } select { width:100%; } .description { white-space:pre-wrap; overflow-wrap:anywhere; } progress { width:100%; accent-color:var(--accent); height:6px; }
 ol { padding-left:22px; } li { padding:10px 0; border-bottom:1px solid var(--border); } li > span { display:block; margin-top:4px; } li a {color:var(--text);} .actions {justify-content:flex-start;} button,input,textarea {max-width:100%;}
</style>
