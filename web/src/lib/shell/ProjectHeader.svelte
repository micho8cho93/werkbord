<script lang="ts">
  import { embedded } from '../embed';

  const framed = embedded();
  import Icon from '../Icon.svelte';
  import { shortPath } from '../projects';
  import TabRow from './TabRow.svelte';
  import ContextBar from '../ContextBar.svelte';
  import { attentionCount } from '../projects';
  import PhoneBack from '../mobile/PhoneBack.svelte';
  import { phone } from '../mobile/phone.svelte';
  import { projectHref, router, tabsFor } from '../router.svelte';
  import { app } from '../state.svelte';
  import type { Project } from '../types';

  // Where you are, and the two things you do most from anywhere in a project: jump somewhere,
  // and add work. The tabs below are the project's sections, all one click away.

  let { project }: { project: Project } = $props();

  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);
  const active = $derived(router.view === 'task' ? 'work' : router.view);
  const activity = $derived(app.overview?.projects.find((p) => p.projectId === project.id));
  /** Repository findings that want attention: what the Git tab's amber count says. */
  const gitItems = $derived(activity?.repoAttention ?? 0);
  const SECTION: Record<string, string> = { work: 'Work', task: 'Work', git: 'Git', analytics: 'Analytics', defaults: 'Project settings' };
  const trail = $derived([{ label: `${project.name} · Personal` }, ...(SECTION[router.view] ? [{ label: SECTION[router.view] }] : []), ...(router.view === 'task' ? [{ label: 'Task' }] : [])]);
  /** Every project page but Work has the heading: Work's own page says what it is, and its actions sit in the bar. */
  const headed = $derived(router.view !== 'work');
  const branch = $derived(project.repository?.currentBranch ?? '');
</script>

{#if phone.on || framed || headed}
<!-- A page of the project other than Work is headed by its name and what can be done from it, under the window's bar. In the window the
     bar is the window's own and this is the framed page's; in the web app the bar is the shared one, drawn here, so the two read the same. -->
{#if !phone.on && !framed}<ContextBar crumbs={trail} running={activity?.running ?? 0} needsYou={attentionCount(activity)} />{/if}
<div class="bar" class:deck={phone.on}>
<header class="head" class:deck={phone.on}>
  {#if phone.on}<PhoneBack />{/if}
  <div class="where">
    <!-- On a phone the project name opens the switcher: it is the only way between projects there. -->
    <div class="name-row">
      {#if phone.on}
        <button class="name" type="button" onclick={() => (app.switcherOpen = true)} aria-label="Switch project, current: {project.name}">
          <h1>{project.name}</h1>
          <span class="chev"><Icon name="down" size={14} /></span>
        </button>
      {:else}
        <h1>{project.name}</h1>
      {/if}
    </div>
    {#if project.kind === 'work'}
      <p class="path">Work project · no Git repository</p>
    {:else}
      <p class="path" title={project.repoPath}>{shortPath(project.repoPath)}{branch ? ` · ${branch}` : ''}</p>
    {/if}
  </div>
  <div class="acts">
    {#if !phone.on}<button class="btn jump" type="button" onclick={() => (app.switcherOpen = true)} aria-label="Jump to">
      <Icon name="search" /><span class="wide">Jump to</span><kbd class="key wide">{isMac ? '⌘' : 'Ctrl'} K</kbd>
    </button>
    {/if}
    {#if !app.remote}<a
      class="btn icon gear"
      href={projectHref(project.id, 'defaults')}
      aria-label="Project settings: defaults for {project.name}"
      title="Project settings"
      aria-current={router.view === 'defaults' ? 'page' : undefined}
    >
      <Icon name="defaults" />
    </a>{/if}
    <button class="btn primary new" type="button" onclick={() => (app.newTaskOpen = true)} title="New task (N)">
      <Icon name="plus" /><span>New task</span>
    </button>
  </div>
</header>

<!-- In the window, Work and Git are entries of the sidebar: a second row of the same two names above the page is only a way to lose
     one's place. Outside it (a browser, a phone) the row is how a project's sections are reached. -->
{#if phone.on}
<TabRow
  label="{project.name} sections"
  tabs={[
    ...tabsFor(project.kind).map((t) => ({ id: t.id, label: t.label, href: projectHref(project.id, t.id), ...(t.id === 'git' ? { count: gitItems, countLabel: `${gitItems} Git items need attention` } : {}) })),
    ...(phone.on ? [{ id: 'defaults', label: 'Settings', href: projectHref(project.id, 'defaults') }] : []),
  ]}
  current={active}
  deck={phone.on}
/>
{/if}
</div>
{:else}
  <!-- The same header as the Mac window's (in the window's own page the window draws it, so a framed page keeps the plain one above): where you are, and what is running and waiting. Work and Git are entries of the sidebar. -->
  <ContextBar crumbs={trail} running={activity?.running ?? 0} needsYou={attentionCount(activity)}>
    {#if !app.remote}<a class="btn icon gear" href={projectHref(project.id, 'defaults')} aria-label="Project settings: defaults for {project.name}" title="Project settings" aria-current={router.view === 'defaults' ? 'page' : undefined}><Icon name="defaults" /></a>{/if}
    <button class="btn primary new" type="button" onclick={() => (app.newTaskOpen = true)} title="New task (N)"><Icon name="plus" /><span>New task</span></button>
  </ContextBar>
{/if}

<style>
  /* The heading and the tabs are one piece; on a wide window the wrapper is not there, as far as layout goes. */
  .bar {
    display: contents;
  }

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
    white-space: normal;
    overflow-wrap: anywhere;
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

  .path {
    font-family: var(--mono);
    font-size: 12px;
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

    /* With the Control Deck the bottom bar has only two places, so the project's sections are a compact row of tabs under its name. */
    .head.deck {
      gap: 4px;
      padding-bottom: 4px;
    }

    .head.deck .where {
      flex: 1;
      min-width: 0;
    }

    .head.deck .gear,
    .head.deck .new {
      display: none;
    }

    .bar.deck {
      display: block;
      position: sticky;
      top: 0;
      z-index: 5;
      background: color-mix(in srgb, var(--bg) 90%, transparent);
      backdrop-filter: blur(10px);
    }

    .head.deck {
      position: static;
      background: none;
      backdrop-filter: none;
    }
  }
</style>
