<script lang="ts">
  import { app } from './state.svelte';
  import { taskHref } from './router.svelte';
  let {project = $bindable(''), runner = $bindable(''), agent = $bindable(''), filtersOnly = false}: {project?: string; runner?: string; agent?: string; filtersOnly?: boolean} = $props();
  const machines = $derived((app.overview?.runners ?? []).filter(r=>(!runner || r.id===runner) && (!project || r.kind==='local' || r.projects.includes(project)) && (!agent || r.capabilities.agents.some(a=>a.id===agent))));
  const usage = $derived((app.overview?.usage ?? []).filter(u=>(!project || u.projectId===project) && (!runner || u.runnerId===runner) && (!agent || u.agentId===agent)));
  const agents = $derived([...new Set([...(app.overview?.usage ?? []).map(u=>u.agentId), ...app.agents.map(a=>a.id)])].sort());
  const usd = (n:number) => new Intl.NumberFormat(undefined,{style:'currency',currency:'USD',maximumFractionDigits:2}).format(n);
</script>
{#if filtersOnly}
<div class="filters" aria-label="Control Center filters">
  <label>Project<select class="select" bind:value={project}><option value="">All projects</option>{#each app.projects as p (p.id)}<option value={p.id}>{p.name}</option>{/each}</select></label>
  <label>Runner<select class="select" bind:value={runner}><option value="">All runners</option>{#each app.overview?.runners ?? [] as r (r.id)}<option value={r.id}>{r.name}</option>{/each}</select></label>
  <label>Agent<select class="select" bind:value={agent}><option value="">All agents</option>{#each agents as id (id)}<option value={id}>{app.agents.find(a=>a.id===id)?.name ?? id}</option>{/each}</select></label>
</div>
{:else}
<section aria-labelledby="infrastructure-title">
  <div class="heading"><h2 id="infrastructure-title">Infrastructure</h2><a href="#/settings">Manage runners</a></div>
  {#if !machines.length}<p class="muted">No runners match this view.</p>{/if}
  <ul class="machines">{#each machines as r (r.id)}
    <li>
      <div class="heading"><strong>{r.name}</strong><span class="muted">{r.disabled?'Disabled':r.online?'Online':'Offline'} · {r.currentRuns}/{r.capacity} capacity</span></div>
      <p class="muted">{r.os}/{r.arch} · {(r.capabilities?.agents??[]).filter(a=>a.available).map(a=>a.name).join(', ') || 'Agents unavailable'}</p>
      {#each (app.overview?.runs ?? []).filter(x=>x.run.runnerId===r.id && (!project||x.run.projectId===project) && (!agent||x.run.agentId===agent)) as item (item.run.id)}
        <a class="workload" href={taskHref(item.run.projectId,item.run.taskId)}>{item.taskTitle}<span class="muted">{item.projectName} · {item.run.state.replaceAll('_',' ')}</span></a>
      {/each}
      {#if !r.online && r.currentRuns>0}<p class="warning">Execution ownership retained. Reconnect this runner to reconcile its work.</p>{/if}
    </li>
  {/each}</ul>
</section>
<section aria-labelledby="usage-title">
  <h2 id="usage-title">Recent usage</h2>
  <p class="muted">Past 30 days · latest 500 runs per project. Tokens and costs cover only reported measurements; runtime measures elapsed ownership time.</p>
  {#if !usage.length}<p class="muted">No usage in this view yet.</p>{:else}
    <div class="table-wrap"><table>
      <thead><tr><th scope="col">Work</th><th scope="col">Runs / retries</th><th scope="col">Runtime</th><th scope="col">Reported tokens</th><th scope="col">Costs</th></tr></thead>
      <tbody>{#each usage as u (`${u.projectId}/${u.runnerId}/${u.agentId}`)}<tr>
        <td data-label="Work"><strong>{app.project(u.projectId)?.name ?? u.projectId}</strong><span>{app.runners.find(r=>r.id===u.runnerId)?.name ?? 'Earlier local runner'} · {u.agentId}</span></td>
        <td data-label="Runs / retries">{u.runs} / {u.retries}<span>{u.accepted} accepted · {u.rejected} rejected</span></td>
        <td data-label="Runtime">{Math.round(u.runtimeSeconds/60)} min</td>
        <td data-label="Reported tokens">{u.tokenRuns ? (u.inputTokens+u.outputTokens).toLocaleString() : 'Unknown'}<span>{u.tokenRuns}/{u.runs} runs reported tokens</span></td>
        <td data-label="Costs">{#if u.costRuns}<span>Known API: {u.actualCostRuns ? usd(u.actualCostUsd) : 'Unknown'}</span><span>API equivalent estimate: {u.estimatedCostRuns ? usd(u.estimatedCostUsd) : 'Unknown'}</span>{:else}Usage only · cost unknown{/if}<span>{u.costRuns}/{u.runs} runs reported a cost</span></td>
      </tr>{/each}</tbody>
    </table></div>
  {/if}
</section>
{/if}
<style>
  .filters { display: grid; grid-template-columns: repeat(3,minmax(0,1fr)); gap: 12px; } label { display: grid; gap: 6px; font-size: .86rem; }
  section { display: grid; gap: 10px; } h2 { font-size: 1rem; font-weight: 650; margin: 0; }
  .heading { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; } a { color: var(--text); text-underline-offset: 3px; } .heading>a { font-size: .85rem; }
  p { margin: 0; font-size: .85rem; } .machines { list-style: none; padding: 0; margin: 0; }
  li { border-top: 1px solid var(--border); padding-block: 12px; display: grid; gap: 6px; }
  .workload { display: flex; justify-content: space-between; gap: 8px; flex-wrap: wrap; font-size: .9rem; } .warning { color: var(--warn); }
  .table-wrap { overflow-x: auto; } table { border-collapse: collapse; width: 100%; font-size: .86rem; font-variant-numeric: tabular-nums; }
  th,td { text-align: left; padding: 12px 10px; border-bottom: 1px solid var(--border); vertical-align: top; } th { color: var(--text-2); font-weight: 600; }
  td>span { display: block; color: var(--text-2); font-size: .8rem; margin-top: 4px; } td:first-child { min-width: 150px; }
  @media(max-width:560px) {
    .filters { grid-template-columns: 1fr; }
    table,tbody { display: block; } thead { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
    tr { display: grid; grid-template-columns: minmax(0,1fr) minmax(0,1fr); gap: 8px 16px; padding-block: 12px; border-block-end: 1px solid var(--border); }
    td { display: block; padding: 0; border: 0; min-width: 0; }
    td:first-child, td:last-child { grid-column: 1/-1; }
    td::before { content: attr(data-label); display: block; margin-block-end: 4px; color: var(--text-2); font-size: .78rem; font-weight: 600; }
  }
</style>
