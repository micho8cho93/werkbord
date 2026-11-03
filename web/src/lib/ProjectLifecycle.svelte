<script lang="ts">
  import { globalHref } from './router.svelte';
  import { moveProject, type ProjectMove } from './project-lifecycle';
  import { erasure, TRASH_DAYS } from './trash';
  import { app } from './state.svelte';
  import type { Project } from './types';
  let { project, onchange }: { project: Project; onchange?: () => void } = $props();
  let busy = $state(false);
  let error = $state('');
  async function change(move: ProjectMove) {
    busy = true; error = '';
    try {
      if (!(await moveProject(project, move))) return;
      onchange?.();
      if (move !== 'restore') location.hash = `${globalHref('settings')}?history`;
    } catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = false; }
  }
</script>
<section class="pn">
  <h3>Project history</h3>
  <p class="muted">A project is archived first, then it can be deleted. Archived keeps everything and puts it aside. Deleted waits {TRASH_DAYS} days in Project history, where it can be restored, and is then erased. The repository files stay on disk either way.</p>
  {#if project.deletedAt}
    <p>Deleted {new Date(project.deletedAt).toLocaleDateString()} · {erasure(project.deletedAt, app.now)}</p>
    <div class="actions"><button class="btn primary" disabled={busy} onclick={() => change('restore')}>{busy ? 'Working…' : 'Restore project'}</button><button class="btn danger" disabled={busy} onclick={() => change('erase')}>Erase now</button></div>
  {:else if project.archivedAt}
    <p>Archived {new Date(project.archivedAt).toLocaleDateString()}</p>
    <div class="actions"><button class="btn primary" disabled={busy} onclick={() => change('restore')}>{busy ? 'Working…' : 'Restore project'}</button><button class="btn danger" disabled={busy} onclick={() => change('delete')}>Delete project</button></div>
  {:else}
    <div class="actions"><button class="btn" disabled={busy} onclick={() => change('archive')}>Archive project</button></div>
  {/if}
  {#if error}<p role="alert" class="error">{error}</p>{/if}
</section>
<style>
  h3 { font-size: 16px; }
  p { max-width: 70ch; }
  .actions { display: flex; gap: 8px; flex-wrap: wrap; }
</style>
