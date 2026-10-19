<script module lang="ts">
  export interface RelationshipResult {
    id: string;
    version: number;
    status: string;
    recommendation: string;
    relationship: {
      source: {projectId:string;repository:string};
      target: {projectId:string;repository:string};
      type:string;evidence:string;lastVerified?:string;sourceRevision?:string;targetRevision?:string;
    };
  }
</script>
<script lang="ts">
  import { untrack } from 'svelte';
  import { api } from './api';
  import { app } from './state.svelte';
  let { projectId }: {projectId:string} = $props();
  let records = $state<RelationshipResult[]>([]);
  let source = $state('');
  let target = $state('');
  let targetProject = $state('');
  let type = $state('manual_association');
  let busy = $state(false);
  let loading = $state(true);
  let error = $state('');
  let generation = 0;
  $effect(() => { const id=projectId; untrack(() => { source=''; target=''; targetProject=''; void load(id); }); return () => { generation++; }; });
  async function load(id=projectId) {
    const ticket=++generation; loading=true; error='';
    try { const result=await api.relationships(id); if(ticket===generation) records=result; }
    catch(e) { if(ticket===generation) error=e instanceof Error?e.message:String(e); }
    finally { if(ticket===generation) loading=false; }
  }
  async function change(action:()=>Promise<unknown>) {
    if(busy) return; const id=projectId; busy=true; error='';
    try { await action(); if(id===projectId) await load(id); }
    catch(e) { if(id===projectId) error=e instanceof Error?e.message:String(e); }
    finally { busy=false; }
  }
</script>
<section class="pn" aria-label="Repository relationships">
  <h3>Repository relationships</h3>
  <p class="muted">Link projects to suggest compatibility checks when a repository changes. A link does not prove that a change will break another repository.</p>
  {#if error}<p role="alert">{error}</p>{/if}
  {#if loading}<p role="status">Loading relationships…</p>
  {:else if records.length===0}<p class="muted">No visible relationships. Add a link using the canonical remote identities of two registered projects.</p>
  {:else}
    <ul>
      {#each records as item (item.id)}
        <li>
          <p class="identity">{item.relationship.source.repository} → {item.relationship.target.repository}</p>
          <p>{item.relationship.type.replaceAll('_',' ')} · {item.status} · Evidence: {item.relationship.evidence}</p>
          {#if item.relationship.lastVerified}<p class="muted">Last verified: {new Date(item.relationship.lastVerified).toLocaleString()}</p>{/if}
          {#if item.relationship.source.projectId===projectId}
            <button class="btn" disabled={busy} onclick={() => change(() => api.acceptRelationship(projectId,item.id,item.version))}>Verify relationship</button>
            <button class="btn" disabled={busy} onclick={() => change(() => api.deleteRelationship(projectId,item.id,item.version))}>Remove link</button>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
  <form onsubmit={(event) => { event.preventDefault(); void change(() => api.saveRelationship(projectId,{source:{projectId,repository:source},target:{projectId:targetProject,repository:target},type,evidence:'manual',inferred:false})); }}>
    <h4>Add a relationship</h4>
    <label>Source repository identity<input class="input" required bind:value={source} placeholder="github.com/organisation/backend" /></label>
    <label>Target project<select class="select" required bind:value={targetProject}>
      <option value="" disabled>Choose a registered project</option>
      {#each app.projects.filter(project => project.id!==projectId && project.kind!=='work') as project (project.id)}
        <option value={project.id}>{project.name}</option>
      {/each}
    </select></label>
    <label>Target repository identity<input class="input" required bind:value={target} placeholder="github.com/organisation/frontend" /></label>
    <label>Relationship type<select class="select" bind:value={type}>
      <option value="manual_association">Manual association</option><option value="package_dependency">Package dependency</option><option value="api_consumer">API consumer → provider</option><option value="shared_authentication">Shared authentication</option><option value="deployment_dependency">Deployment dependency</option>
    </select></label>
    <p class="muted">The source consumes or depends on the target. Saving confirms the relationship for the current review.</p>
    <button class="btn primary" disabled={busy || !source || !target || !targetProject}>Save relationship</button>
  </form>
</section>
<style>
  form { display:grid; gap:12px; margin-top:24px; max-width:560px; }
  label { display:grid; gap:6px; }
  li { padding-block:12px; border-bottom:1px solid var(--border); }
  .identity { overflow-wrap:anywhere; }
  p { max-width:72ch; }
</style>
