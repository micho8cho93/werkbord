<script lang="ts">
  import ProductSwitch from './ProductSwitch.svelte';
  import { embedded } from '../embed';

  const framed = embedded();
  import Icon from '../Icon.svelte';
  import Mark from '../Mark.svelte';
  import { shortPath } from '../projects';
  import { PROJECT_TABS, projectHref, router } from '../router.svelte';
  import { app } from '../state.svelte';
  import type { Project } from '../types';

  // Where you are, and the two things you do most from anywhere in a project: jump somewhere,
  // and add work. The tabs below are the project's sections, all one click away.

  let { project }: { project: Project } = $props();

  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);
  const active = $derived(router.view === 'task' ? 'board' : router.view);
  const activity = $derived(app.overview?.projects.find((p) => p.projectId === project.id));
  /** Repository findings that want attention: what the Git tab's amber count says. */
  const gitItems = $derived(activity?.repoAttention ?? 0);
  const branch = $derived(project.repository?.currentBranch ?? '');
</script>

<header class="head">
  <div class="where">
    <!-- On a phone the project name opens the switcher: it is the only way between projects there. -->
    <div class="name-row">
      {#if framed}<span class="phone-mark"><ProductSwitch variant="compact" /></span>{/if}
      <button class="name" type="button" onclick={() => (app.switcherOpen = true)} aria-label="Switch project, current: {project.name}">
        {#if !framed}<span class="phone-mark"><Mark height={18} /></span>{/if}
        <h1>{project.name}</h1>
        <span class="chev"><Icon name="down" size={14} /></span>
      </button>
    </div>
    <p class="path" title={project.repoPath}>{shortPath(project.repoPath)}{branch ? ` · ${branch}` : ''}</p>
  </div>
  <div class="acts">
    <button class="btn jump" type="button" onclick={() => (app.switcherOpen = true)} aria-label="Jump to">
      <Icon name="search" /><span class="wide">Jump to</span><kbd class="key wide">{isMac ? '⌘' : 'Ctrl'} K</kbd>
    </button>
    <a
      class="btn icon gear"
      href={projectHref(project.id, 'defaults')}
      aria-label="Project settings: defaults for {project.name}"
      title="Project settings"
      aria-current={router.view === 'defaults' ? 'page' : undefined}
    >
      <Icon name="defaults" />
    </a>
    <button class="btn primary new" type="button" onclick={() => (app.newTaskOpen = true)} title="New task (N)">
      <Icon name="plus" /><span>New task</span>
    </button>
  </div>
</header>

<nav class="tabs" aria-label="{project.name} sections">
  {#each PROJECT_TABS as s (s.id)}
    <a class="tab" href={projectHref(project.id, s.id)} aria-current={active === s.id ? 'page' : undefined}>
      {s.label}
      {#if s.id === 'git' && gitItems > 0}<span class="num pend" aria-label="{gitItems} Git items need attention">{gitItems}</span>{/if}
    </a>
  {/each}
</nav>

<style>
  .head {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    min-height: 64px;
    padding: 8px 24px;
  }

  .where {
    min-width: 0;
    display: flex;
    flex-direction: column-reverse;
  }

  .name-row {
    display: flex;
    align-items: center;
    gap: 2px;
    min-width: 0;
  }

  .name {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    max-width: 100%;
    padding: 0;
    border: 0;
    background: none;
    color: var(--text);
    text-align: left;
  }

  h1 {
    margin-top: 2px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .chev {
    display: inline-flex;
    color: var(--text-2);
    transition: transform 0.15s var(--ease);
  }

  .name:hover .chev {
    transform: translateY(1px);
    color: var(--text);
  }

  .phone-mark {
    display: none;
  }

  .path {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .acts {
    display: flex;
    gap: 10px;
    flex: none;
  }

  .gear[aria-current='page'] {
    box-shadow: var(--press-sh);
    background: var(--surface-2);
  }

  .tabs {
    flex: none;
    display: flex;
    gap: 4px;
    height: 44px;
    padding: 0 24px;
    border-bottom: 1px solid var(--border);
    overflow-x: auto;
    scrollbar-width: none;
  }

  .tab {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 44px;
    padding: 0 12px;
    color: var(--text-2);
    text-decoration: none;
    font-weight: 500;
    font-size: 13px;
    border-bottom: 2px solid transparent;
    white-space: nowrap;
    transition: color 0.12s, border-color 0.12s;
  }

  .tab:hover {
    color: var(--text);
  }

  .tab[aria-current='page'] {
    color: var(--text);
    border-bottom-color: var(--accent);
  }

  /* Phone: the mark and project name are the heading; the tabs live in the bottom bar. */
  @media (max-width: 899px) {
    .head {
      position: sticky;
      top: 0;
      z-index: 5;
      padding: calc(10px + env(safe-area-inset-top)) 16px 8px;
      min-height: 0;
      background: color-mix(in srgb, var(--bg) 90%, transparent);
      backdrop-filter: blur(10px);
    }

    .where {
      flex-direction: column;
    }

    h1 {
      font-size: 20px;
    }

    .phone-mark {
      display: inline-flex;
      margin-right: 4px;
    }

    .wide,
    .new span,
    .gear {
      display: none;
    }

    .jump {
      width: 44px;
      padding: 0;
    }

    /* The phone has a floating New task button over the board instead. */
    .new {
      display: none;
    }

    .tabs {
      display: none;
    }
  }
</style>
