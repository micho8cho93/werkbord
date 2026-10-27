<script lang="ts">
  import { setupRouteAllowed } from './lib/setup/readiness';
  import WorkflowMonitor from './lib/mobile/WorkflowMonitor.svelte';
  import Icon, { type IconName } from './lib/Icon.svelte';
  import Mark from './lib/Mark.svelte';
  import Settling from './lib/Settling.svelte';
  import DeviceGit from './lib/DeviceGit.svelte';
  import RunnersPanel from './lib/RunnersPanel.svelte';
  import PendingInteraction from './lib/assistant/PendingInteraction.svelte';
  import Assistant from './lib/assistant/Assistant.svelte';
  import AssistantButton from './lib/assistant/AssistantButton.svelte';
  import { assistant } from './lib/assistant/sessions.svelte';
  import { embedded, hosted } from './lib/embed';
  import NeedsInputBanner from './lib/NeedsInputBanner.svelte';
  import { isPhone } from './lib/phones/capability';
  import PhoneWelcome from './lib/phones/PhoneWelcome.svelte';
  import ConnectionBanner from './lib/ConnectionBanner.svelte';
  import NewTaskDialog from './lib/NewTaskDialog.svelte';
  import ProjectSwitcher from './lib/ProjectSwitcher.svelte';
  import { pageTitle } from './lib/questions';
  import { GLOBAL_VIEWS, globalHref, hrefOf, inMore, inProject, isPhoneView, moreEntries, projectHref, resolved, router, sectionApplies } from './lib/router.svelte';
  import Sheet from './lib/Sheet.svelte';
  import ScheduleWatch from './lib/ScheduleWatch.svelte';
  import ProjectHeader from './lib/shell/ProjectHeader.svelte';
  import TabRow from './lib/shell/TabRow.svelte';
  import Rail from './lib/shell/Rail.svelte';
  import Toasts from './lib/shell/Toasts.svelte';
  import { app } from './lib/state.svelte';
  import Deck from './lib/mobile/Deck.svelte';
  import Agents from './lib/mobile/Agents.svelte';
  import Repositories from './lib/mobile/Repositories.svelte';
  import Reviews from './lib/mobile/Reviews.svelte';
  import Runners from './lib/mobile/Runners.svelte';
  import MyWork from './lib/mobile/MyWork.svelte';
  import Schedule from './lib/mobile/Schedule.svelte';
  import PhoneBack from './lib/mobile/PhoneBack.svelte';
  import PhoneNav from './lib/mobile/PhoneNav.svelte';
  import { titleOf } from './lib/mobile/nav';
  import { agentCards } from './lib/mobile/agents';
  import { phone } from './lib/mobile/phone.svelte';
  import { phoneSubtitle } from './lib/mobile/subtitle';
  import TokenPrompt from './lib/TokenPrompt.svelte';
  import UpdateBanner from './lib/UpdateBanner.svelte';
  import PersonalWorkspace from './routes/PersonalWorkspace.svelte';
  import Board from './routes/Board.svelte';
  import Scheduling from './routes/Scheduling.svelte';
  import ControlCenter from './routes/ControlCenter.svelte';
  import Git from './routes/Git.svelte';
  import Help from './routes/Help.svelte';
  import Onboarding from './routes/Onboarding.svelte';
  import Tour from './lib/Tour.svelte';
  import ProjectSettings from './routes/ProjectSettings.svelte';
  import ProjectOverview from './routes/ProjectOverview.svelte';
  import WorkOverview from './routes/WorkOverview.svelte';
  import Projects from './routes/Projects.svelte';
  import Runs from './routes/Runs.svelte';
  import Analytics from './routes/Analytics.svelte';
  import Settings from './routes/Settings.svelte';
  import TaskPanelHost from './routes/TaskPanelHost.svelte';

  // Inside the desktop app the window's own sidebar is the navigation: this page shows only the work.
  const framed = embedded();
  const WORKSPACE_TABS = [{ id: 'control', label: 'Workspace' }, { id: 'projects', label: 'Projects' }] as const;
  const loaded = $derived(app.projects.length > 0 || app.overview !== null);
  const inside = $derived(inProject(router.view));
  /** The project the page is in, or, on a global page, the one last used: what the phone's tabs point at. */
  const projectId = $derived(router.projectId || app.lastProjectId);
  const project = $derived(app.project(projectId));
  const scope = $derived(router.projectId ? app.scopes.get(router.projectId) : undefined);

  const globalTitle = $derived(router.view === 'onboarding' ? 'Set up Werkbord' : (GLOBAL_VIEWS.find((g) => g.id === router.view)?.label ?? titleOf(router.view)));
  /** The phone has its own navigation (the Control Deck and the Assistant). A narrow window of the desktop app keeps the desktop's. */
  const deck = $derived(phone.on);
  const subtitle = $derived(
    deck
      ? phoneSubtitle(router.view, { needsYou: app.needsYou, review: app.overview?.review.length ?? 0, cards: agentCards(app.agents, app.overview), runners: app.runners })
      : null,
  );
  const phoneMoreLocation = $derived(router.view === 'analytics' ? 'Analytics' : router.view === 'overview' ? 'Overview' : router.view === 'defaults' ? 'Project settings' : '');

  // The phone opens on the Control Deck: an address that names no page (the app opened from the home screen) lands there. On a wide
  // window the deck and the screens only the phone has do not exist, and an address that names one is Needs attention.
  $effect(() => {
    if (!loaded && app.onboarding === null) return;
    if (deck && !location.hash.replace(/^#\/?/, '')) router.go({ view: 'deck', projectId: '', taskId: '' }, true);
    else if (!deck && !framed && isPhoneView(router.view)) router.go({ view: 'control', projectId: '', taskId: '' }, true);
  });

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

  // Setup stays active until the completion review. Recovery and the example task remain reachable.
  $effect(() => {
    if (!app.needsOnboarding || isPhone() || hosted) return;
    if (framed) window.parent.postMessage({ type: 'werkbord.frame', event: 'onboarding', required: true, paused: app.onboardingPaused }, '*');
    if (!app.onboardingPaused && !setupRouteAllowed(router.view, router.taskId, app.onboarding?.example?.taskId)) {
      router.go({ view: 'onboarding', projectId: '', taskId: '' }, true);
    }
  });
  $effect(() => {
    if (framed && !hosted && app.onboarding && !app.needsOnboarding) window.parent.postMessage({ type: 'werkbord.frame', event: 'onboarding', required: false, paused: false }, '*');
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
    const el = e.target as HTMLElement | null;
    const typing = !!el && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName));
    if (typing || (app.needsOnboarding && !app.onboardingPaused && !isPhone())) return;
    if ((e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === 'k') {
      if (assistant.mounted) return;
      e.preventDefault();
      app.switcherOpen = !app.switcherOpen;
      return;
    }
    if (e.metaKey || e.ctrlKey || e.altKey || e.defaultPrevented || assistant.mounted) return;
    if (app.switcherOpen || app.newTaskOpen) return;
    if (e.key === 'n' && project) {
      e.preventDefault();
      app.newTaskOpen = true;
    }
  }

  /** The phone's tabs, as on the identity sheet: what needs you first, then the project's sections. */
  let moreOpen = $state(false);
  const more = $derived(moreEntries(project?.id ?? '').filter((e) => !isPhone() || e.id !== 'defaults'));
  // A page opened from the sheet, or by the back button, leaves the sheet behind.
  $effect(() => {
    void [router.view, router.projectId];
    moreOpen = false;
  });

  const phoneTabs = $derived<{ id: string; label: string; icon: IconName; href: string; current: boolean; count?: number }[]>([
    { id: 'control', label: 'Workspace', icon: 'control', href: globalHref('control'), current: router.view === 'control', count: app.needsYou },
    ...(['board', 'calendar', 'git', 'runs'] as const)
      .filter((s) => sectionApplies(s, project?.kind))
      .map((s) => ({
      id: s,
      label: { board: 'Board', calendar: 'Calendar', timeline: 'Timeline', git: 'Git', runs: 'Runs' }[s],
      icon: s,
      href: project ? projectHref(project.id, s) : globalHref('projects'),
      current: inside && (router.view === s || (s === 'board' && router.view === 'task') || (s === 'calendar' && router.view === 'timeline')),
    })),
  ]);
</script>

<svelte:window onkeydown={onKey} />

{#if app.connection === 'unauthorized'}
  <TokenPrompt />
{:else}
  <a class="skip-main" href="#main-content" onclick={(e) => { e.preventDefault(); mainEl?.focus({ preventScroll: true }); mainEl?.scrollIntoView({ block: 'start' }); }}>Skip to main content</a>
  <!-- The workspace stays mounted while the assistant is over it; inert keeps focus and screen readers on the assistant. -->
  <div class="shell" class:framed inert={assistant.mounted}>
    {#if !framed}<div class="rail-area"><Rail /></div>{/if}

    <div class="main-area">
      {#if inside && project}
        <ProjectHeader {project} />
      {:else if router.view !== 'personal' && (router.view !== 'deck' || !deck) && !(hosted && (router.view === 'settings' || router.view === 'help'))}
        <header class="ghead">
          <div class="gtitle">
            {#if deck}<PhoneBack />{:else if !framed}<span class="phone-mark"><Mark height={18} /></span>{/if}
            {#if subtitle}
              <div class="ttl">
                <h1>{globalTitle}{#if subtitle.badge}<span class="num pend" aria-label="{subtitle.badge} {subtitle.badge === 1 ? 'item needs' : 'items need'} attention">{subtitle.badge}</span>{/if}</h1>
                <p class="sub mm" data-testid="phone-subtitle">{#if subtitle.dot}<span class="dot" data-tone={subtitle.dot === 'ok' ? 'ok' : subtitle.dot === 'ask' ? 'ask' : 'neutral'} aria-hidden="true"></span>{/if}{subtitle.text}</p>
              </div>
            {:else}
              <h1>{globalTitle}</h1>
            {/if}
          </div>
          <div class="gacts">
            {#if !framed && (!app.needsOnboarding || app.onboardingPaused)}<span class="assistant-wide"><AssistantButton variant="toolbar" /></span>{/if}
            {#if !framed && !deck && (!app.needsOnboarding || app.onboardingPaused)}<span class="phone-only"><AssistantButton variant="icon" /></span>{/if}
            <button class="btn jump" type="button" onclick={() => (app.switcherOpen = true)} aria-label="Jump to">
              <Icon name="search" /><span class="wide">Jump to</span>
            </button>
          </div>
        </header>
        <!-- The workspace's own sub-areas, under its entry in the window's sidebar: the same row a project and a team have. -->
        {#if framed && !deck && (router.view === 'control' || router.view === 'projects')}
          <TabRow label="Workspace sections" current={router.view} tabs={WORKSPACE_TABS.map((t) => ({ ...t, href: globalHref(t.id) }))} />
        {/if}
      {/if}

      {#if deck}<WorkflowMonitor />{/if}
      <PhoneWelcome />
      {#if router.view !== 'personal'}<Tour surface="desktop" enabled={!deck && !isPhone() && !hosted && !app.needsOnboarding && router.view !== 'onboarding'} />{/if}
      {#if router.view !== 'task' || !scope || !project}<ConnectionBanner />{/if}
      <!-- The Control Deck exposes Needs attention through its configurable key. -->
      {#if !(deck && router.view === 'deck')}<NeedsInputBanner />{/if}
      <UpdateBanner />
      {#if app.needsOnboarding && !isPhone() && !framed && router.view !== 'onboarding'}
        <div class="setup-reminder" role="status">
          <span><strong>Setup is unfinished.</strong> Finish connecting your tools to get the full Werkbord experience.</span>
          <a class="btn primary" href={globalHref('onboarding')}>Resume setup</a>
        </div>
      {/if}

      <main id="main-content" tabindex="-1" bind:this={mainEl}>
        {#if phoneMoreLocation && !framed && !deck}<h2 class="phone-more-location">{phoneMoreLocation}</h2>{/if}
        {#if app.remote && ['defaults', 'onboarding', 'personal'].includes(router.view)}
          <div class="empty"><p>This desktop can control tasks and managed runs.</p><p>Private workspaces and controller settings are managed on the controller.</p></div>
        {:else if app.remote && router.view === 'settings'}
          <div class="page"><RunnersPanel /></div>
        {:else if router.view === 'personal'}
          {#key router.query}<PersonalWorkspace id={new URLSearchParams(router.query).get('id') ?? ''} />{/key}
        {:else if router.view === 'deck'}
          {#if deck}<Deck />{/if}
        {:else if router.view === 'reviews'}
          {#if deck}<Reviews />{/if}
        {:else if router.view === 'repositories'}
          {#if deck}<Repositories />{/if}
        {:else if router.view === 'runners'}
          {#if deck}<Runners />{/if}
        {:else if router.view === 'agents'}
          {#if deck}<Agents />{/if}
        {:else if router.view === 'mywork'}
          {#if deck}<MyWork />{/if}
        {:else if router.view === 'schedule'}
          {#if deck}<Schedule />{/if}
        {:else if router.view === 'control'}
          <ControlCenter />
        {:else if router.view === 'analytics'}
          <Analytics />
        {:else if router.view === 'projects'}
          <div class="page"><Projects /></div>
        {:else if router.view === 'settings'}
          <Settings />
        {:else if router.view === 'help'}
          <div class="page"><Help /></div>
        {:else if router.view === 'onboarding'}
          <div class="page"><Onboarding /></div>
        {:else if !router.projectId}
          {#if loaded}<p class="empty">Opening…</p>{:else}<Settling label="Opening Werkbord…" />{/if}
        {:else if !project}
          {#if loaded}
            <div class="empty">
              <p>This project was not found.</p>
              <p><a href={globalHref('projects')}>See all projects</a></p>
            </div>
          {:else}
            <Settling label="Opening Werkbord…" />
          {/if}
        {:else if !scope}
          <Settling label="Opening the project…" />
        {:else}
          <!-- Each project's page is its own: nothing typed or open in one carries over to another. -->
          {#key project.id}
            <ScheduleWatch {scope} />
            {#if router.view === 'overview'}
              {#if project.kind === 'work'}<WorkOverview {scope} {project} />{:else}<ProjectOverview {scope} {project} />{/if}
            {:else if router.view === 'calendar' || router.view === 'timeline'}
              <Scheduling {scope} {project} />
            {:else if router.view === 'git' && project.kind === 'work'}
              <p class="empty">This project has no Git repository. Add a repository project to inspect Git changes.</p>
            {:else if router.view === 'runs' && project.kind === 'work'}
              <p class="empty">This project has no execution history. Work is tracked on its Board and Calendar.</p>
            {:else if router.view === 'git'}
              {#if app.remote}<div class="page"><DeviceGit {project} /></div>{:else if router.sub}<div class="page"><Git {project} /></div>{:else}<Git {project} />{/if}
            {:else if router.view === 'runs'}
              <Runs {scope} {project} />
            {:else if router.view === 'defaults'}
              <div class="page"><ProjectSettings {project} /></div>
            {:else}
              <Board {scope} {project} />
              {#if router.view === 'task'}<TaskPanelHost {scope} {project} />{/if}
            {/if}
          {/key}
        {/if}
      </main>
    </div>

    {#if !deck && !framed}
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
      <button type="button" class="tab" aria-haspopup="dialog" aria-expanded={moreOpen} aria-current={inMore(router.view) ? 'page' : undefined} onclick={() => (moreOpen = true)}>
        <span class="icon"><Icon name="more" size={20} /></span>
        <span>More</span>
      </button>
    </nav>

    {#if moreOpen}
      <Sheet title="More" onclose={() => (moreOpen = false)}>
        <ul class="more">
          {#each more as m (m.id)}
            <li>
              <a href={m.href} aria-current={router.view === m.id ? 'page' : undefined}>
                <Icon name={m.id === 'defaults' ? 'defaults' : m.id} size={18} />{m.label}
              </a>
            </li>
          {/each}
        </ul>
      </Sheet>
    {/if}
    {/if}

    <ProjectSwitcher />
    {#if app.newTaskOpen && project}{#key project.id}<NewTaskDialog {project} />{/key}{/if}
    <Toasts />
  </div>
  <!-- In the desktop window the assistant is the shell's own frame over the whole window (lib/assistant/AssistantSurface). -->
  {#if !framed}<Assistant />
{#if !framed && deck && assistant.waiting > 0 && !assistant.isOpen && !assistant.compact}
  <button class="btn small mobile-attention" onclick={() => assistant.compact = true}>Needs attention · {assistant.waiting}</button>
{/if}
{#if !framed && assistant.compact && !assistant.isOpen}<PendingInteraction />{/if}{/if}
  <!-- Outside the shell, which is inert while the assistant is open: the phone's two places stay reachable over it. -->
  {#if deck && router.view !== 'onboarding'}<PhoneNav />{/if}
{/if}

<style>
  .mobile-attention { position: fixed; right: 16px; bottom: calc(var(--tabbar-h) + 16px + env(safe-area-inset-bottom)); z-index: 65; color: var(--warn-text); background: var(--surface); }
  .setup-reminder { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 24px; background: var(--surface-2); border-bottom: 1px solid var(--border); }
  .skip-main {
    position: fixed;
    z-index: 100;
    top: 8px;
    left: 8px;
    transform: translateY(calc(-100% - 12px));
    padding: 10px 14px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface);
    color: var(--text);
    box-shadow: var(--win-sh);
    font-weight: 600;
  }

  .skip-main:focus-visible {
    transform: translateY(0);
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  /* The skip link moves focus here, and a click on empty space does too: a ring round the whole page helps no one. */
  main:focus,
  main:focus-visible {
    outline: none;
  }

  /* Desktop: the window is the app. A rail, and a main column whose views fit the viewport. */
  .shell {
    height: 100dvh;
    display: grid;
    grid-template-columns: var(--rail-w) minmax(0, 1fr);
    overflow: hidden;
  }

  /* Embedded desktop panes keep the window layout even when Settings makes the frame narrow. */
  .shell.framed {
    grid-template-columns: minmax(0, 1fr);
    height: 100dvh;
    min-height: 0;
    display: grid;
    overflow: hidden;
    padding-bottom: 0;
  }

  .shell.framed .main-area {
    display: flex;
  }

  .shell.framed main {
    overflow-y: auto;
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

  .ttl {
    display: grid;
    gap: 2px;
    min-width: 0;
  }

  .ttl h1 {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .sub {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0;
  }

  .gacts {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .phone-only {
    display: none;
  }

  .assistant-wide {
    display: inline-flex;
  }

  .phone-more-location { display: none; }

  .tabbar {
    display: none;
  }

  .more {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
  }

  .more a {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 48px;
    padding: 0 4px;
    color: var(--text);
    text-decoration: none;
    font-size: 16px;
    font-weight: 500;
    border-bottom: 1px solid var(--border);
  }

  .more li:last-child a {
    border-bottom: 0;
  }

  .more a[aria-current='page'] {
    color: var(--accent-text);
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
      font-size: 20px;
    }

    .phone-mark {
      display: inline-flex;
    }

    .phone-only {
      display: inline-flex;
    }

    .assistant-wide {
      display: none;
    }

    .phone-more-location {
      display: block;
      margin: 12px 16px 0;
      font-size: 20px;
      font-weight: 600;
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
      /* One equal column per tab: a work project has fewer sections than a repository, and none may wrap out of sight.
         A column never goes narrower than its own label, so 11.5px text is never clipped on the narrowest phones. */
      grid-auto-flow: column;
      grid-auto-columns: minmax(min-content, 1fr);
      height: calc(var(--tabbar-h) + env(safe-area-inset-bottom));
      padding-bottom: env(safe-area-inset-bottom);
      background: color-mix(in srgb, var(--bg) 94%, transparent);
      backdrop-filter: blur(10px);
      border-top: 1px solid var(--border);
    }

    .tab {
      appearance: none;
      border: 0;
      background: none;
      font-family: inherit;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      gap: 4px;
      font-size: 12px;
      font-weight: 500;
      color: var(--text-2);
      text-decoration: none;
      min-width: 0;
    }

    .tab > span:last-child {
      max-width: 100%;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    /* Seven tabs on a phone: the labels sit a little closer rather than being clipped. */
    @media (max-width: 430px) {
      .tab {
        letter-spacing: -0.02em;
      }
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
      font-size: 12px;
      padding: 0 5px;
    }
  }
</style>
