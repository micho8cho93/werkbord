<script lang="ts">
  import { api } from './api';
  import { app } from './state.svelte';
  import { globalHref } from './router.svelte';
  import type { Project } from './types';
  let { project, onchange }: { project: Project; onchange?: () => void } = $props();
  let busy = $state(false);
  let error = $state('');
  async function change(state: 'active' | 'archived' | 'deleted') {
    if (state !== 'active' && !confirm(`${state === 'archived' ? 'Archive' : 'Delete'} ${project.name}? You can restore it from Settings → Project history. Tasks, memory and files will be kept.`)) return;
    busy = true; error = '';
    try {
      await api.setProjectLifecycle(project.id, state);
      app.projects = await api.listProjects();
      app.refreshOverview();
      onchange?.();
      if (state !== 'active') location.hash = `${globalHref('settings')}?history`;
    } catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = false; }
  }
</script>
<section class="pn">
  <h3>Project history</h3>
  <p class="muted">Archive a project to put it aside, or delete it to remove it from your active projects. Both can be restored here later, with tasks, completed work and memory intact. Repository files stay on disk.</p>
  {#if project.archivedAt || project.deletedAt}
    <p>{project.deletedAt ? 'Deleted' : 'Archived'} {new Date(project.deletedAt || project.archivedAt!).toLocaleDateString()}</p>
    <button class="btn primary" disabled={busy} onclick={() => change('active')}>{busy ? 'Restoring…' : 'Restore project'}</button>
  {:else}
    <div class="actions"><button class="btn" disabled={busy} onclick={() => change('archived')}>Archive project</button><button class="btn danger" disabled={busy} onclick={() => change('deleted')}>Delete project</button></div>
  {/if}
  {#if error}<p role="alert" class="error">{error}</p>{/if}
</section>
<style>
  h3 { font-size: 16px; }
  p { max-width: 70ch; }
  .actions { display: flex; gap: 8px; flex-wrap: wrap; }
</style>
