<script lang="ts">
  import ProjectAvatar from './ProjectAvatar.svelte';
  import { attentionCount } from './projects';
  import { router } from './router.svelte';
  import { app } from './state.svelte';

  /**
   * `title`: the project's name is the heading of the page, as it is inside a project.
   * `rail`: the wide button at the head of the side rail.
   * `chip`: a small one, for a page that is not in a project.
   */
  let { variant = 'chip' }: { variant?: 'title' | 'rail' | 'chip' } = $props();

  const id = $derived(router.projectId || app.lastProjectId);
  const project = $derived(app.project(id));
  const others = $derived(
    (app.overview?.projects ?? []).filter((p) => p.projectId !== id).reduce((n, p) => n + attentionCount(p), 0),
  );
  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);
</script>

<button
  class="switch"
  data-variant={variant}
  aria-haspopup="dialog"
  aria-expanded={app.switcherOpen}
  onclick={() => (app.switcherOpen = true)}
  title="Switch project ({isMac ? '⌘' : 'Ctrl'} K)"
>
  {#if project}
    <ProjectAvatar id={project.id} name={project.name} size={variant === 'chip' ? 22 : 30} />
    <span class="name">{project.name}</span>
  {:else}
    <span class="name muted">Choose a project</span>
  {/if}
  <svg class="chev" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path d="m7 10 5 5 5-5" />
  </svg>
  {#if others > 0}<span class="other" aria-label="{others} need you in other projects">{others}</span>{/if}
  {#if variant === 'rail'}<kbd>{isMac ? '⌘' : 'Ctrl'} K</kbd>{/if}
</button>

<style>
  .switch {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    max-width: 100%;
    min-height: 40px;
    padding: 0 8px 0 6px;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    background: transparent;
    font-weight: 650;
  }

  .switch:hover {
    background: var(--surface-2);
  }

  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .chev {
    flex: none;
    color: var(--text-2);
  }

  /* The heading of a project's page: big, and unmistakable. */
  [data-variant='title'] {
    font-size: 1.15rem;
    letter-spacing: -0.01em;
  }

  [data-variant='chip'] {
    min-height: 34px;
    border-color: var(--border);
    font-size: 0.85rem;
  }

  [data-variant='rail'] {
    width: 100%;
    padding: 8px;
    border-color: var(--border);
    background: var(--surface-2);
  }

  [data-variant='rail'] .name {
    flex: 1;
    text-align: left;
  }

  kbd {
    flex: none;
    padding: 1px 6px;
    border: 1px solid var(--border);
    border-radius: 5px;
    background: var(--surface);
    font: 600 0.68rem var(--font);
    color: var(--text-2);
  }

  /* Something needs you in a project that is not the one on screen. */
  .other {
    min-width: 18px;
    height: 18px;
    padding: 0 5px;
    border-radius: 999px;
    background: var(--warn);
    color: #1a1204;
    font-size: 0.7rem;
    font-weight: 700;
    line-height: 18px;
    text-align: center;
  }
</style>
