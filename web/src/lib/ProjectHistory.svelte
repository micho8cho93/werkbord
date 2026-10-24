<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from './api';
  import { app } from './state.svelte';
  import { projectHref } from './router.svelte';
  import type { Project } from './types';
  let projects = $state<Project[]>([]);
  let loading = $state(true);
  let busy = $state('');
  let error = $state('');
  let message = $state('');
  let filter = $state<'all' | 'archived' | 'deleted'>('all');
  const shown = $derived(projects.filter(p => filter === 'archived' ? p.archivedAt : filter === 'deleted' ? p.deletedAt : true));
  async function load() {
    error = '';
    try { projects = await api.projectHistory(); }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { loading = false; }
  }
  async function restore(p: Project) {
    busy = p.id; message = ''; error = '';
    try {
      await api.setProjectLifecycle(p.id, 'active');
      app.projects = await api.listProjects(); app.refreshOverview();
      await load(); message = `${p.name} restored. Its tasks and memory are ready to continue.`;
    } catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = ''; }
  }
  onMount(load);
</script>
<section class="pn">
  <h3 class="band">Project history</h3>
  <p class="muted">Archived and deleted projects keep their tasks, completed work and memory. Restore one to carry on.</p>
  <div class="seg" role="group" aria-label="Project history filter">
    <button aria-pressed={filter === 'all'} onclick={() => filter = 'all'}>All projects</button>
    <button aria-pressed={filter === 'archived'} onclick={() => filter = 'archived'}>Archived</button>
    <button aria-pressed={filter === 'deleted'} onclick={() => filter = 'deleted'}>Deleted</button>
  </div>
  {#if error}<p class="error" role="alert">{error}</p><button class="btn quiet" onclick={load}>Retry</button>{/if}
  {#if message}<p role="status">{message}</p>{/if}
  {#if loading}<p class="muted" role="status">Loading project history…</p>
  {:else}
    <ul class="card-list">{#each shown as p (p.id)}
      <li class="card-row">
        <header><h4 class="title">{p.name}</h4><span class="tag">{p.deletedAt ? 'Deleted' : p.archivedAt ? 'Archived' : 'Active'} · {new Date(p.deletedAt || p.archivedAt || p.createdAt).toLocaleDateString()}</span></header>
        <span class="muted path">{p.repoPath || 'Work project'}</span>
        {#if p.lifecycle?.length}<details><summary>Activity ({p.lifecycle.length})</summary>{#each p.lifecycle as entry, i (i)}<p class="muted">{entry.state === 'active' ? 'Restored' : entry.state === 'deleted' ? 'Deleted' : 'Archived'} · {new Date(entry.at).toLocaleString()}</p>{/each}</details>{/if}
        <div class="actions">
          {#if p.archivedAt || p.deletedAt}<button class="btn" disabled={!!busy} onclick={() => restore(p)}>{busy === p.id ? 'Restoring…' : 'Restore project'}</button>
          {:else}<a class="btn open" href={projectHref(p.id, 'overview')}>Open project</a>{/if}
        </div>
      </li>
    {:else}<li class="muted">No {filter === 'all' ? '' : filter + ' '}projects yet.</li>{/each}</ul>
  {/if}
</section>
<style>
  p { max-width: 70ch; margin: 0; }
  .path { overflow-wrap: anywhere; font-size: 12px; }
</style>
