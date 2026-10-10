<script lang="ts">
  import Icon, { type IconName } from './lib/Icon.svelte';
  import Mark from './lib/Mark.svelte';
  import { embedded } from './lib/embed';
  import NeedsInputBanner from './lib/NeedsInputBanner.svelte';
  import NewTaskDialog from './lib/NewTaskDialog.svelte';
  import ProjectSwitcher from './lib/ProjectSwitcher.svelte';
  import { pageTitle } from './lib/questions';
  import { GLOBAL_VIEWS, globalHref, hrefOf, inProject, projectHref, resolved, router, sectionApplies } from './lib/router.svelte';
  import ScheduleWatch from './lib/ScheduleWatch.svelte';
  import ProjectHeader from './lib/shell/ProjectHeader.svelte';
  import Rail from './lib/shell/Rail.svelte';
  import Toasts from './lib/shell/Toasts.svelte';
  import { app } from './lib/state.svelte';
  import TokenPrompt from './lib/TokenPrompt.svelte';
  import UpdateBanner from './lib/UpdateBanner.svelte';
  import Board from './routes/Board.svelte';
  import Calendar from './routes/Calendar.svelte';
  import ControlCenter from './routes/ControlCenter.svelte';
  import Git from './routes/Git.svelte';
  import Onboarding from './routes/Onboarding.svelte';
  import ProjectSettings from './routes/ProjectSettings.svelte';
  import ProjectOverview from './routes/ProjectOverview.svelte';
  import WorkOverview from './routes/WorkOverview.svelte';
  import Projects from './routes/Projects.svelte';
  import Runs from './routes/Runs.svelte';
  import Timeline from './routes/Timeline.svelte';
  import Settings from './routes/Settings.svelte';
  import TaskPanel from './routes/TaskPanel.svelte';

  // Inside the desktop app the window's own sidebar is the navigation: this page shows only the work.
  const framed = embedded();
  const loaded = $derived(app.projects.length > 0 || app.overview !== null);
  const inside = $derived(inProject(router.view));
  /** The project the page is in, or, on a global page, the one last used: what the phone's tabs point at. */
  const projectId = $derived(router.projectId || app.lastProjectId);
  const project = $derived(app.project(projectId));
  const scope = $derived(router.projectId ? app.scopes.get(router.projectId) : undefined);

  const globalTitle = $derived(router.view === 'onboarding' ? 'Set up Werkbord' : (GLOBAL_VIEWS.find((g) => g.id === router.view)?.label ?? ''));

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

  // A work project has no calendar, Git or runs: an address that names one lands on its board.
  $effect(() => {
    const v = router.view;
    if (project && inProject(v) && v !== 'task' && !sectionApplies(v, project.kind)) {
      router.go({ view: 'board', projectId: project.id, taskId: '' }, true);
    }
  });

  // First-time setup opens by itself, once, until it is finished or skipped. It is a page like any other: nothing is
  // blocked behind it, and a computer that already has projects never sees it.
  let setupOffered = false;
  $effect(() => {
    if (!loaded && app.onboarding === null) return;
    if (app.needsOnboarding && !setupOffered && router.view !== 'onboarding') {
      setupOffered = true;
      router.go({ view: 'onboarding', projectId: '', taskId: '' });
    }
  });

  // Being in a project makes it the current one and has its data ready, fetched the first time.
  $effect(() => {
    if (router.projectId && app.project(router.projectId)) app.enter(router.projectId);
  });

  // A new page starts at its top, not wherever the last one was scrolled to. Opening or closing a task keeps the board where it was.
  let mainEl = $state<HTMLElement>();
  $effect(() => {
    void [router.view === 'task' ? 'board' : router.view, router.projectId, router.sub];
    window.scrollTo(0, 0);
    mainEl?.scrollTo(0, 0);
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

  // ⌘/Ctrl K: Jump to, from anywhere. N: a new task in the project you are in.
  function onKey(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      app.switcherOpen = !app.switcherOpen;
      return;
    }
    if (e.metaKey || e.ctrlKey || e.altKey || e.defaultPrevented) return;
    const el = e.target as HTMLElement | null;
    const typing = !!el && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName));
    if (typing || app.switcherOpen || app.newTaskOpen) return;
    if (e.key === 'n' && project) {
      e.preventDefault();
      app.newTaskOpen = true;
    }
  }

  /** The phone's tabs, as on the identity sheet: what needs you first, then the project's sections. */
  const phoneTabs = $derived<{ id: string; label: string; icon: IconName; href: string; current: boolean; count?: number }[]>([
    { id: 'control', label: 'Needs you', icon: 'control', href: globalHref('control'), current: router.view === 'control', count: app.needsYou },
    ...(['board', 'calendar', 'timeline', 'git', 'runs'] as const)
      .filter((s) => sectionApplies(s, project?.kind))
      .map((s) => ({
      id: s,
      label: { board: 'Board', calendar: 'Calendar', timeline: 'Timeline', git: 'Git', runs: 'Runs' }[s],
      icon: s,
      href: project ? projectHref(project.id, s) : globalHref('projects'),
      current: inside && (router.view === s || (s === 'board' && router.view === 'task')),
    })),
  ]);
</script>

<svelte:window onkeydown={onKey} />

{#if app.connection === 'unauthorized'}
  <TokenPrompt />
{:else}
  <div class="shell" class:framed>
    {#if !framed}<div class="rail-area"><Rail /></div>{/if}

    <div class="main-area">
      {#if inside && project}
        <ProjectHeader {project} />
      {:else}
        <header class="ghead">
          <div class="gtitle">
            {#if !framed}<span class="phone-mark"><Mark height={18} /></span>{/if}
            <h1>{globalTitle}</h1>
          </div>
          <button class="btn jump" type="button" onclick={() => (app.switcherOpen = true)} aria-label="Jump to">
            <Icon name="search" /><span class="wide">Jump to</span>
          </button>
        </header>
      {/if}

      <NeedsInputBanner />
      <UpdateBanner />

      <main bind:this={mainEl}>
        {#if router.view === 'control'}
          <ControlCenter />
        {:else if router.view === 'projects'}
          <div class="page"><Projects /></div>
        {:else if router.view === 'settings'}
          <Settings />
        {:else if router.view === 'onboarding'}
          <div class="page"><Onboarding /></div>
        {:else if !router.projectId}
          <p class="empty">{loaded ? 'Opening…' : 'Loading…'}</p>
        {:else if !project}
          <div class="empty">
            <p>{loaded ? 'This project was not found.' : 'Loading…'}</p>
            {#if loaded}<p><a href={globalHref('projects')}>See all projects</a></p>{/if}
          </div>
        {:else if !scope}
          <p class="empty">Loading…</p>
        {:else}
          <!-- Each project's page is its own: nothing typed or open in one carries over to another. -->
          {#key project.id}
            <ScheduleWatch {scope} />
            {#if router.view === 'overview'}
              {#if project.kind === 'work'}<WorkOverview {scope} {project} />{:else}<ProjectOverview {scope} {project} />{/if}
            {:else if router.view === 'calendar'}
              <Calendar {scope} {project} />
            {:else if router.view === 'timeline'}
              <Timeline {scope} {project} />
            {:else if router.view === 'git'}
              {#if router.sub}<div class="page"><Git {project} /></div>{:else}<Git {project} />{/if}
            {:else if router.view === 'runs'}
              <Runs {scope} {project} />
            {:else if router.view === 'defaults'}
              <div class="page"><ProjectSettings {project} /></div>
            {:else}
              <Board {scope} {project} />
              {#if router.view === 'task'}<TaskPanel {scope} {project} />{/if}
            {/if}
          {/key}
        {/if}
      </main>
    </div>

    <nav class="tabbar" aria-label="Primary">
      {#each phoneTabs as t (t.id)}
        <a href={t.href} class="tab" aria-current={t.current ? 'page' : undefined}>
          <span class="icon">
            <Icon name={t.icon} size={20} />
            {#if t.count}<span class="num" aria-label="{t.count} waiting for you">{t.count}</span>{/if}
          </span>
          <span>{t.label}</span>
        </a>
      {/each}
    </nav>

    <ProjectSwitcher />
    {#if app.newTaskOpen && project}<NewTaskDialog {project} />{/if}
    <Toasts />
  </div>
{/if}

<style>
  /* Desktop: the window is the app. A rail, and a main column whose views fit the viewport. */
  .shell {
    height: 100dvh;
    display: grid;
    grid-template-columns: var(--rail-w) minmax(0, 1fr);
    overflow: hidden;
  }

  .shell.framed {
    grid-template-columns: minmax(0, 1fr);
  }

  .rail-area {
    min-height: 0;
  }

  .main-area {
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  main {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  .page {
    padding: 20px 24px 32px;
  }

  .ghead {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    min-height: 64px;
    padding: 8px 24px;
    border-bottom: 1px solid var(--border);
  }

  .gtitle {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }

  .phone-mark {
    display: none;
  }

  .tabbar {
    display: none;
  }

  /* Phone: a heading, the page, and the tabs in thumb reach. The document scrolls, as phones expect. */
  @media (max-width: 899px) {
    .shell {
      display: block;
      height: auto;
      min-height: 100dvh;
      overflow: visible;
      padding-bottom: calc(var(--tabbar-h) + env(safe-area-inset-bottom));
    }

    .rail-area {
      display: none;
    }

    .main-area {
      display: block;
    }

    main {
      overflow: visible;
    }

    .page {
      padding: 12px 16px 24px;
    }

    .ghead {
      position: sticky;
      top: 0;
      z-index: 5;
      min-height: 56px;
      padding: calc(8px + env(safe-area-inset-top)) 16px 8px;
      background: color-mix(in srgb, var(--bg) 90%, transparent);
      backdrop-filter: blur(10px);
    }

    .ghead h1 {
      font-size: 22px;
    }

    .phone-mark {
      display: inline-flex;
    }

    .wide {
      display: none;
    }

    .jump {
      width: 44px;
      padding: 0;
    }

    .tabbar {
      position: fixed;
      inset: auto 0 0 0;
      z-index: 10;
      display: grid;
      grid-template-columns: repeat(5, minmax(0, 1fr));
      height: calc(var(--tabbar-h) + env(safe-area-inset-bottom));
      padding-bottom: env(safe-area-inset-bottom);
      background: color-mix(in srgb, var(--bg) 94%, transparent);
      backdrop-filter: blur(10px);
      border-top: 1px solid var(--border);
    }

    .tab {
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      gap: 4px;
      font-size: 11px;
      font-weight: 500;
      color: var(--text-2);
      text-decoration: none;
    }

    .tab[aria-current='page'] {
      color: var(--text);
    }

    .icon {
      position: relative;
      display: inline-flex;
    }

    .icon .num {
      position: absolute;
      top: -7px;
      left: calc(100% - 4px);
      min-width: 18px;
      height: 18px;
      font-size: 10.5px;
      padding: 0 5px;
    }
  }
</style>
