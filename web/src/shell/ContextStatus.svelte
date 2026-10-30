<script lang="ts">
  import { model } from './model.svelte';
  const unavailable = $derived((model.contextOverview?.entries ?? []).filter(e => e.error || !e.summary));
</script>
{#if !['settings', 'help', 'workspaces'].includes(model.page)}
  <div class="context" aria-label="Current project">
    <span>{model.contextLabel}</span>
    {#if model.selectedProject && !model.projects.some(p => p.workspace === model.selectedProject?.workspace && p.project === model.selectedProject?.project)}
      <span role="status">This project is unavailable. Reconnect its workspace or choose another project.</span>
    {/if}
    {#each unavailable as e (e.workspace.id)}<span role="status">{e.workspace.name}: {e.error} Its work is excluded.</span>{/each}
  </div>
{/if}
<style>
  .context { display: flex; flex-wrap: wrap; gap: 6px 20px; flex: none; padding: 12px 24px; border-bottom: 1px solid var(--border); color: var(--text-2); font-size: 13px; }
</style>
