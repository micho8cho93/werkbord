<script lang="ts">
  import { tick } from 'svelte';
  import Icon from '../lib/Icon.svelte';
  import { projectHref } from '../lib/router.svelte';
  import type { ProjectScope } from '../lib/scope.svelte';
  import type { Project } from '../lib/types';
  import TaskPanel from './TaskPanel.svelte';

  // The task panel behind an error boundary: something in a task that cannot be drawn costs the panel, not the board
  // or the rest of the app, and says so with a way out.

  let { scope, project }: { scope: ProjectScope; project: Project } = $props();
  const boardHref = $derived(projectHref(project.id, 'board'));

  let failedHeading = $state<HTMLElement>();
  $effect(() => {
    if (failedHeading) void tick().then(() => failedHeading?.focus());
  });
  let failed = $state(false);
  let retrying = $state(false);
  let retryError = $state('');
  async function retry(reset: () => void) {
    if (retrying) return;
    retrying = true; retryError = '';
    try {
      await scope.load(); // Refresh evidence only; never replay a task, run or answer action.
      failed = false; reset();
    } catch (error) {
      retryError = `Could not refresh this task. Reconnect, then try again. ${reason(error)}`;
    } finally { retrying = false; }
  }
  function report(error: unknown) {
    failed = true;
    console.error('The task panel failed to render', error);
  }
  const reason = (error: unknown) => (error instanceof Error ? error.message : String(error));
</script>

<svelte:window onkeydown={(e) => { if (failed && e.key === 'Escape') location.hash = boardHref; }} />

<svelte:boundary onerror={report}>
  <TaskPanel {scope} {project} />

  {#snippet failed(error, reset)}
    <a class="scrim" href={boardHref} aria-hidden="true" tabindex="-1"></a>
    <section class="panel" aria-labelledby="task-failed-title" role="alert">
      <header class="head">
        <h2 id="task-failed-title" tabindex="-1" bind:this={failedHeading}>This task could not be shown</h2>
        <a class="btn quiet small icon" href={boardHref} aria-label="Close panel" title="Close (Esc)"><Icon name="close" /></a>
      </header>
      <div class="body">
        <p>The task panel could not load. Your task and runs are unaffected. Refresh the task or return to the board.</p>
        <p class="well">{reason(error)}</p>
        {#if retryError}<p class="error" role="alert">{retryError}</p>{/if}
        <div class="row">
          <button class="btn primary" disabled={retrying} onclick={() => retry(reset)}>{retrying ? 'Refreshing task…' : 'Refresh task'}</button>
          <a class="btn" href={boardHref}>Back to the board</a>
        </div>
      </div>
    </section>
  {/snippet}
</svelte:boundary>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 20;
    background: color-mix(in srgb, var(--bg) 35%, transparent);
    cursor: default;
  }
  .panel {
    position: fixed;
    z-index: 21;
    top: 0;
    right: 0;
    bottom: 0;
    width: min(560px, calc(100vw - var(--rail-w) - 120px));
    display: flex;
    flex-direction: column;
    background: var(--bg);
    border-left: 1px solid var(--border);
    box-shadow: var(--win-sh);
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 12px 20px 14px 24px;
    border-bottom: 1px solid var(--border);
  }
  .head h2 {
    font-family: inherit;
    text-transform: none;
    letter-spacing: -0.015em;
    color: var(--text);
    font-size: 20px;
    font-weight: 600;
  }
  .head h2:focus {
    outline: none;
  }
  .body {
    display: grid;
    gap: 14px;
    align-content: start;
    padding: 20px 24px;
    overflow-wrap: anywhere;
    overflow-y: auto;
    min-height: 0;
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  @media (max-width: 899px), (pointer: coarse) { .row .btn { min-height: 44px; } }
  @media (max-width: 899px) {
    .panel {
      width: 100vw;
      border-left: 0;
    }
  }
</style>
