<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../api';
  import { confirmAction } from '../confirm.svelte';
  import { timeAgo } from '../format';
  import { taskHref } from '../router.svelte';
  import type { ProjectScope } from '../scope.svelte';
  import { app } from '../state.svelte';
  import { ARCHIVE_AFTER, erasure, TRASH_DAYS } from '../trash';
  import { TASK_STATE_LABELS, type Task } from '../types';
  let { scope }: { scope: ProjectScope } = $props();

  // Finished work moves on in three steps: Done, then Archived, then Deleted. Archiving keeps everything and takes the task off the board
  // (by hand, or after the days this project says); deleting moves it to Recently deleted, where it can be restored for 30 days; after that
  // it is erased, with its runs. A task is archived before it can be deleted.
  let view = $state<'archived' | 'deleted'>('archived');
  let search = $state('');
  let busy = $state('');
  let afterDays = $state(0);
  let saved = $state('');
  const match = (t: Task) => `${t.title} ${t.description}`.toLowerCase().includes(search.toLowerCase());
  const archived = $derived(scope.archivedTasks.filter(match).sort((a, b) => b.archivedAt!.localeCompare(a.archivedAt!)));
  const deleted = $derived(scope.deletedTasks.filter(match).sort((a, b) => b.deletedAt!.localeCompare(a.deletedAt!)));
  const doneNow = $derived(scope.activeTasks.filter(t => t.state === 'done').length);

  onMount(() => { void api.archiveSettings(scope.projectId).then(s => { afterDays = s.afterDays; }, () => {}); });

  async function run(task: Task, work: () => Promise<void>) {
    if (busy) return;
    busy = task.id;
    try { await work(); }
    catch (e) { app.handleError(e); await scope.load().catch(e => app.handleError(e)); }
    finally { busy = ''; }
  }
  const restore = (task: Task) => run(task, async () => { scope.upsertTask(await api.archiveTask(task, false)); app.notify('Task restored to ' + TASK_STATE_LABELS[task.state] + '.'); });
  const untrash = (task: Task) => run(task, async () => { scope.upsertTask(await api.untrashTask(task)); app.notify('Task back in the Archive.'); });
  async function remove(task: Task) {
    if (!(await confirmAction({ title: `Delete “${task.title}”?`, message: `It moves to Recently deleted and can be restored for ${TRASH_DAYS} days. After that it is erased, with its runs and conversation.`, confirmLabel: 'Delete task' }))) return;
    await run(task, async () => { scope.upsertTask(await api.trashTask(task)); app.notify('Task moved to Recently deleted.'); });
  }
  async function erase(task: Task) {
    if (!(await confirmAction({ title: `Erase “${task.title}” now?`, message: 'This cannot be undone. The task, its runs and its conversation are erased. Git branches and files on disk are not touched.', confirmLabel: 'Erase now', danger: true }))) return;
    await run(task, async () => { await api.eraseTask(task); scope.tasks = scope.tasks.filter(t => t.id !== task.id); app.notify('Task erased.'); });
  }
  async function archiveDone() {
    if (busy) return;
    busy = 'all';
    try { const out = await api.archiveDone(scope.projectId); for (const t of out) scope.upsertTask(t); app.notify(out.length ? `${out.length} done ${out.length === 1 ? 'task' : 'tasks'} archived.` : 'Nothing done to archive.'); }
    catch (e) { app.handleError(e); }
    finally { busy = ''; }
  }
  async function setAfter(days: number) {
    saved = '';
    try { const s = await api.setArchiveSettings(scope.projectId, days); afterDays = s.afterDays; saved = s.afterDays ? `Done tasks are archived ${s.afterDays} days after they last change.` : 'Done tasks stay on the board until you archive them.'; }
    catch (e) { app.handleError(e); }
  }
</script>
<section class="archive" aria-label="Task archive">
  <div class="heading">
    <div><h2 class="section-title">{view === 'archived' ? 'Archived work' : 'Recently deleted'}</h2>
      <p class="muted">{view === 'archived' ? 'Archived tasks and cleared Done cards. Their conversations, runs and Git evidence are kept.' : `Deleted tasks wait here for ${TRASH_DAYS} days, then are erased with their runs. Restore one before then to keep it.`}</p></div>
    <label class="field">Search<input class="input" type="search" bind:value={search} placeholder="Title or task details" /></label>
  </div>
  <div class="bar">
    <div class="seg" role="group" aria-label="Archive sections">
      <button aria-pressed={view === 'archived'} onclick={() => (view = 'archived')}>Archived <span class="n">{scope.archivedTasks.length}</span></button>
      <button aria-pressed={view === 'deleted'} onclick={() => (view = 'deleted')}>Recently deleted <span class="n">{scope.deletedTasks.length}</span></button>
    </div>
    {#if view === 'archived'}
      <div class="auto">
        <button class="btn small" disabled={!!busy || !doneNow} onclick={archiveDone}>{busy === 'all' ? 'Archiving…' : `Archive done tasks${doneNow ? ` (${doneNow})` : ''}`}</button>
        <label class="inline">Archive done tasks automatically
          <select class="select" value={afterDays} onchange={(e) => setAfter(Number(e.currentTarget.value))} aria-describedby="auto-saved">
            {#each ARCHIVE_AFTER as a (a.days)}<option value={a.days}>{a.label}</option>{/each}
          </select>
        </label>
      </div>
    {/if}
  </div>
  <p id="auto-saved" class="muted note" role="status">{view === 'archived' ? saved : ''}</p>
  <ul class="tasks">
    {#if view === 'archived'}
      {#each archived as task (task.id)}
        <li><div><a href={taskHref(task.projectId, task.id)}>{task.title}</a><p class="muted">{TASK_STATE_LABELS[task.state]} · Archived {timeAgo(task.archivedAt!, app.now)}</p></div>
          <div class="acts"><button class="btn small" disabled={!!busy} onclick={() => restore(task)}>{busy === task.id ? 'Working…' : 'Restore'}</button><button class="btn small quiet" disabled={!!busy} onclick={() => remove(task)}>Delete</button></div></li>
      {:else}<li class="muted">{search ? 'No archived tasks match.' : 'No archived work yet. Done tasks are archived with the button above, or by themselves if you choose so; Archive on a Done card does it for one.'}</li>{/each}
    {:else}
      {#each deleted as task (task.id)}
        <li><div><span class="name">{task.title}</span><p class="muted">Deleted {timeAgo(task.deletedAt!, app.now)} · {erasure(task.deletedAt!, app.now)}</p></div>
          <div class="acts"><button class="btn small" disabled={!!busy} onclick={() => untrash(task)}>{busy === task.id ? 'Working…' : 'Restore'}</button><button class="btn small danger" disabled={!!busy} onclick={() => erase(task)}>Erase now</button></div></li>
      {:else}<li class="muted">{search ? 'No deleted tasks match.' : 'Nothing deleted. A task you delete from the Archive waits here before it is erased.'}</li>{/each}
    {/if}
  </ul>
</section>
<style>
  .archive { padding: 0 24px 24px; min-height: 0; display: flex; flex-direction: column; gap: 14px; }
  .heading { display: flex; align-items: end; justify-content: space-between; gap: 20px; flex-wrap: wrap; }
  h2 { font-size: 20px; } .field { display: grid; gap: 4px; min-width: min(280px, 100%); }
  .bar { display: flex; align-items: center; justify-content: space-between; gap: 12px 20px; flex-wrap: wrap; }
  .n { margin-left: 4px; color: var(--text-2); font-variant-numeric: tabular-nums; }
  .auto { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
  .inline { display: flex; align-items: center; gap: 8px; font-size: 13px; }
  .inline .select { width: auto; min-height: 32px; }
  .note { margin: 0; min-height: 1.2em; font-size: 12px; }
  .tasks { list-style: none; padding: 0; margin: 0; overflow-y: auto; scrollbar-gutter: stable; }
  li { display: flex; justify-content: space-between; align-items: center; gap: 16px; padding: 14px 0; border-bottom: 1px solid var(--border); }
  li div { min-width: 0; } a, .name { font-weight: 500; overflow-wrap: anywhere; } p { font-size: 12px; margin: 2px 0 0; }
  .acts { display: flex; gap: 8px; flex: none; }
</style>
