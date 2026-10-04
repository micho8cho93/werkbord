<script lang="ts">
  import Icon from './lib/Icon.svelte';
  import NeedsInputBanner from './lib/NeedsInputBanner.svelte';
  import ProjectSwitcher from './lib/ProjectSwitcher.svelte';
  import SwitcherButton from './lib/SwitcherButton.svelte';
  import { pageTitle } from './lib/questions';
  import { projectColor } from './lib/projects';
  import {
    GLOBAL_VIEWS,
    PROJECT_SECTIONS,
    globalHref,
    hrefOf,
    inProject,
    projectHref,
    resolved,
    router,
  } from './lib/router.svelte';
  import { app } from './lib/state.svelte';
  import TokenPrompt from './lib/TokenPrompt.svelte';
  import Activity from './routes/Activity.svelte';
  import Board from './routes/Board.svelte';
  import ControlCenter from './routes/ControlCenter.svelte';
  import Git from './routes/Git.svelte';
  import Projects from './routes/Projects.svelte';
  import TaskDetail from './routes/TaskDetail.svelte';

  const loaded = $derived(app.projects.length > 0 || app.overview !== null);
  const inside = $derived(inProject(router.view));
  /** The project the page is in, or, on a global page, the one last used: what the project tabs point at. */
  const projectId = $derived(router.projectId || app.lastProjectId);
  const project = $derived(app.project(projectId));
  const scope = $derived(router.projectId ? app.scopes.get(router.projectId) : undefined);
  const activeSection = $derived(router.view === 'task' ? 'board' : router.view);

  const title = $derived(
    inside
      ? (PROJECT_SECTIONS.find((s) => s.id === activeSection)?.label ?? 'Task')
      : (GLOBAL_VIEWS.find((g) => g.id === router.view)?.label ?? ''),
  );
  const statusLabel = $derived(
    { connecting: 'Connecting', live: 'Live', offline: 'Controller offline', unauthorized: 'Token required' }[
      app.connection
    ],
  );

  // An address that names no project (an old link, or none) is settled to the project last used. Until the
  // projects are known there is nothing to settle it against.
  $effect(() => {
    if (!loaded) return;
    const loc = router.location;
    const fixed = resolved(
      loc,
      app.projects.map((p) => p.id),
      app.lastProjectId,
    );
    if (hrefOf(fixed) !== hrefOf(loc)) router.go(fixed, true);
  });

  // Being in a project makes it the current one and has its data ready, fetched the first time.
  $effect(() => {
    if (router.projectId && app.project(router.projectId)) app.enter(router.projectId);
  });

  // A new page starts at its top, not wherever the last one was scrolled to.
  $effect(() => {
    void [router.view, router.projectId, router.taskId, router.sub];
    window.scrollTo(0, 0);
  });

  // Questions are what the user must not miss. The tab title and, for an installed app, the icon badge say
  // so even while the page is in the background.
  $effect(() => {
    document.title = pageTitle(app.needsInput);
    const nav = navigator as Navigator & { setAppBadge?: (n?: number) => Promise<void>; clearAppBadge?: () => Promise<void> };
    try {
      const done = app.needsInput > 0 ? nav.setAppBadge?.(app.needsInput) : nav.clearAppBadge?.();
      done?.catch(() => {});
    } catch {
      // Badging is not available everywhere.
    }
  });

  // Ctrl/⌘ K opens the project switcher from anywhere.
  $effect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        app.switcherOpen = !app.switcherOpen;
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });
</script>

{#if app.connection === 'unauthorized'}
  <TokenPrompt />
{:else}
  <div class="shell" data-scope={inside ? 'project' : 'global'} style:--project={inside && project ? projectColor(project.id) : 'transparent'}>
    <aside class="rail">
      <div class="brand">Devboard</div>
      <SwitcherButton variant="rail" />

      <nav aria-label="Everywhere">
        <p class="group">Everywhere</p>
        {#each GLOBAL_VIEWS as g (g.id)}
          <a href={globalHref(g.id)} class="rail-link" aria-current={router.view === g.id ? 'page' : undefined}>
            <Icon name={g.id} />
            {g.label}
            {#if g.id === 'control' && app.needsYou > 0}<span class="count" aria-label="{app.needsYou} waiting for you">{app.needsYou}</span>{/if}
          </a>
        {/each}
      </nav>

      {#if project}
        <nav class="project-nav" aria-label="{project.name}" style:--project={projectColor(project.id)}>
          <p class="group">{project.name}</p>
          {#each PROJECT_SECTIONS as s (s.id)}
            <a
              href={projectHref(project.id, s.id)}
              class="rail-link"
              aria-current={inside && router.projectId === project.id && activeSection === s.id ? 'page' : undefined}
            >
              <Icon name={s.id} />
              {s.label}
            </a>
          {/each}
        </nav>
      {/if}
    </aside>

    <div class="top">
      <div class="stripe" aria-hidden="true"></div>
      <header class="topbar">
        {#if inside}
          <h1 class="in-project">
            <SwitcherButton variant="title" />
            <span class="section">{title}</span>
          </h1>
        {:else}
          <h1>{title}</h1>
          {#if project}<SwitcherButton variant="chip" />{/if}
        {/if}
        <span class="status" data-state={app.connection} title={statusLabel}>
          <span class="dot" aria-hidden="true"></span>
          <span class="status-label">{statusLabel}</span>
        </span>
      </header>
      <NeedsInputBanner />
    </div>

    <main>
      {#if app.error}
        <p class="error banner" role="alert">{app.error}</p>
      {/if}
      {#if app.notice}
        <p class="notice" role="status">
          <span>{app.notice}</span>
          <button class="btn small quiet" onclick={() => app.dismissNotice()}>Dismiss</button>
        </p>
      {/if}

      {#if router.view === 'control'}
        <ControlCenter />
      {:else if router.view === 'projects'}
        <Projects />
      {:else if !router.projectId}
        <p class="card empty">{loaded ? 'Opening…' : 'Loading…'}</p>
      {:else if !project}
        <div class="card empty">
          <p>{loaded ? 'This project was not found.' : 'Loading…'}</p>
          {#if loaded}<p><a href={globalHref('projects')}>See all projects</a></p>{/if}
        </div>
      {:else if !scope}
        <p class="card empty">Loading…</p>
      {:else}
        <!-- Each project's page is its own: nothing typed or open in one carries over to another. -->
        {#key project.id}
          {#if router.view === 'task'}
            <TaskDetail {scope} {project} />
          {:else if router.view === 'git'}
            <Git {project} />
          {:else if router.view === 'activity'}
            <Activity {scope} {project} />
          {:else}
            <Board {scope} {project} />
          {/if}
        {/key}
      {/if}
    </main>

    <nav class="tabbar" aria-label="Primary" style:--tabs={1 + PROJECT_SECTIONS.length}>
      <a href={globalHref('control')} class="tab" aria-current={router.view === 'control' ? 'page' : undefined}>
        <span class="icon">
          <Icon name="control" />
          {#if app.needsYou > 0}<span class="count" aria-label="{app.needsYou} waiting for you">{app.needsYou}</span>{/if}
        </span>
        <span>Control</span>
      </a>
      {#each PROJECT_SECTIONS as s (s.id)}
        <a
          href={project ? projectHref(project.id, s.id) : globalHref('projects')}
          class="tab"
          aria-current={inside && activeSection === s.id ? 'page' : undefined}
        >
          <span class="icon"><Icon name={s.id} /></span>
          <span>{s.label}</span>
        </a>
      {/each}
    </nav>

    <ProjectSwitcher />
  </div>
{/if}

<style>
  /* Phone first: top bar, scrolling content, bottom tab bar in thumb reach. */
  .shell {
    min-height: 100dvh;
    display: grid;
    grid-template-rows: auto 1fr;
    grid-template-columns: minmax(0, 1fr);
    grid-template-areas: 'top' 'main';
  }

  .rail {
    display: none;
  }

  .top {
    grid-area: top;
    position: sticky;
    top: 0;
    z-index: 5;
  }

  /* Inside a project the whole top of the page is that project's colour: there is no mistaking where you are. */
  .stripe {
    height: 4px;
    background: var(--project);
  }

  .topbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 6px 16px 6px 10px;
    min-height: 52px;
    background: color-mix(in srgb, var(--bg) 88%, transparent);
    backdrop-filter: blur(10px);
    border-bottom: 1px solid var(--border);
  }

  .topbar h1 {
    min-width: 0;
    padding-left: 6px;
  }

  .topbar h1.in-project {
    display: flex;
    align-items: center;
    gap: 2px;
    padding-left: 0;
  }

  .section {
    flex: none;
    margin-left: 4px;
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--text-2);
  }

  main {
    grid-area: main;
    min-width: 0;
    padding: 12px 16px calc(var(--tabbar-h) + env(safe-area-inset-bottom) + 16px);
  }

  .banner {
    margin-bottom: 12px;
  }

  .notice {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 12px;
    padding: 10px 12px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-left: 3px solid var(--accent);
    border-radius: var(--radius);
    font-size: 0.9rem;
  }

  .status {
    display: inline-flex;
    flex: none;
    align-items: center;
    gap: 6px;
    font-size: 0.8rem;
    color: var(--text-2);
  }

  .status-label {
    display: none;
  }

  .dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--warn);
  }

  .status[data-state='live'] .dot {
    background: var(--ok);
  }

  .status[data-state='offline'] .dot {
    background: var(--danger);
  }

  .status[data-state='offline'] .status-label {
    display: inline;
  }

  .tabbar {
    position: fixed;
    inset: auto 0 0 0;
    z-index: 5;
    display: grid;
    grid-template-columns: repeat(var(--tabs), 1fr);
    height: calc(var(--tabbar-h) + env(safe-area-inset-bottom));
    padding-bottom: env(safe-area-inset-bottom);
    background: var(--surface);
    border-top: 1px solid var(--border);
  }

  .tab {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 2px;
    font-size: 0.72rem;
    font-weight: 550;
    color: var(--text-2);
    text-decoration: none;
  }

  .tab[aria-current='page'] {
    color: var(--accent);
  }

  .icon {
    position: relative;
    display: inline-flex;
  }

  .count {
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

  .icon .count {
    position: absolute;
    top: -6px;
    left: 60%;
  }

  .rail-link .count {
    margin-left: auto;
  }

  /* Tablet and desktop: persistent side rail, no tab bar. */
  @media (min-width: 900px) {
    .shell {
      grid-template-columns: var(--rail-w) minmax(0, 1fr);
      grid-template-areas: 'rail top' 'rail main';
    }

    .rail {
      grid-area: rail;
      display: flex;
      flex-direction: column;
      gap: 14px;
      position: sticky;
      top: 0;
      height: 100dvh;
      padding: 18px 12px;
      overflow-y: auto;
      border-right: 1px solid var(--border);
      background: var(--surface);
    }

    .brand {
      padding: 0 10px;
      font-weight: 700;
      letter-spacing: -0.01em;
    }

    .rail nav {
      display: flex;
      flex-direction: column;
      gap: 2px;
    }

    .group {
      padding: 0 10px 4px;
      font-size: 0.7rem;
      font-weight: 650;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      color: var(--text-2);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    /* A project's own links hang off a bar in its colour. */
    .project-nav {
      padding-left: 8px;
      border-left: 3px solid var(--project);
      border-radius: 2px;
    }

    .rail-link {
      display: flex;
      align-items: center;
      gap: 10px;
      padding: 8px 10px;
      border-radius: var(--radius-sm);
      color: var(--text-2);
      text-decoration: none;
      font-weight: 550;
    }

    .rail-link:hover {
      background: var(--surface-2);
    }

    .rail-link[aria-current='page'] {
      background: var(--surface-2);
      color: var(--text);
    }

    .topbar {
      padding: 6px 24px 6px 18px;
    }

    .status-label {
      display: inline;
    }

    main {
      padding: 20px 24px 32px;
    }

    .tabbar {
      display: none;
    }
  }
</style>
