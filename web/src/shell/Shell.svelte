<script lang="ts">
  import { onMount } from 'svelte';
  import Sidebar from './Sidebar.svelte';
  import Palette from './Palette.svelte';
  import FrameHost from './FrameHost.svelte';
  import WorkspaceProblem from './WorkspaceProblem.svelte';
  import Calendar from './Calendar.svelte';
  import Analytics from './Analytics.svelte';
  import Reviews from './Reviews.svelte';
  import Agents from './Agents.svelte';
  import ContextStatus from './ContextStatus.svelte';
  import ReturnBar from './ReturnBar.svelte';
  import { remembered } from './view-state.svelte';
  import Hub from './Hub.svelte';
  import { catchUpSession } from './catchup-session.svelte';
  $effect(() => { catchUpSession.ingest(model.overview); });
  import Workspaces from './Workspaces.svelte';
  import AddTeam from './AddTeam.svelte';
  import AssistantFrame from './AssistantFrame.svelte';
  import SettingsNav from './SettingsNav.svelte';
  import SettingsNative from './SettingsNative.svelte';
  import SettingsPlan from './SettingsPlan.svelte';
  import Icon from '../lib/Icon.svelte';
  import { model, PANE_MIN, type Page } from './model.svelte';
  import { onApp } from './native';

  // One project context and destination list surround the original workspace-owned detail screens.
  const pages: Page[] = ['workspace', 'calendar', 'inbox', 'reviews', 'analytics', 'workspaces', 'settings', 'help', 'agents'];
  let mainEl: HTMLElement | undefined;
  // The person's own Werkbord cannot be shown (too old, or not running): say so instead of a blank frame.
  const problem = $derived(model.current === 'personal' && model.view?.items.find((i) => i.id === 'personal')?.state === 'unavailable');
  // What a page that shows a workspace's page for Settings or Help needs: that workspace to be there to show.
  const hostedWorkspace = $derived(model.page === 'settings' || model.page === 'help' ? model.shownWorkspace : undefined);
  const hostedGone = $derived(!!hostedWorkspace && model.view?.items.find((i) => i.id === hostedWorkspace)?.state === 'unavailable');
  const native = $derived(model.page === 'settings' ? model.settingsShown.where : undefined);

  $effect(() => {
    if (!model.setupRequired || model.setupPaused || model.page === 'settings' || model.page === 'help') return;
    // Individual guards its own routes, including the exact example task. Do not race its pause report against a place report.
    if (model.page === 'workspace' && model.current === 'personal') return;
    void model.open('personal', '#/onboarding');
  });

  function go(where: unknown): void {
    if (where === 'personal') void model.open('personal');
    else if (where === 'switch' || where === 'goto') model.togglePalette();
    else if (where === 'invitation' || where === 'add-team') model.addOpen = true;
    else if (pages.includes(where as Page)) model.go(where as Page);
  }
  onMount(() => {
    const stopCatchUp = catchUpSession.start();
    void model.start();
    const unsubscribe = onApp('shell:go', go);
    // Work shows a task or session beside its list only while the window is wide enough for both.
    const wide = matchMedia(`(min-width: ${PANE_MIN}px)`);
    const resized = () => model.setWide(wide.matches);
    resized();
    wide.addEventListener('change', resized);
    return () => { stopCatchUp(); unsubscribe(); wide.removeEventListener('change', resized); model.stop(); };
  });
  /** What the pane shows, named for the bar over it. */
  const paneTitle = $derived(model.pane && model.pane.place.includes('/task/') ? 'Task' : model.pane && /sessions/.test(model.pane.place) ? 'Agent session' : 'Details');
  function expandPane(): void {
    const open = model.pane;
    if (!open) return;
    const where = model.places[open.workspace] || open.place;
    model.pane = null;
    void model.goTo(open.workspace, where.replace(/([?&])pane=1&?/, '$1').replace(/[?&]$/, ''));
  }
  function key(e: KeyboardEvent): void {
    // ⌘K goes to a project, a place or a page; ⌘⇧K still does, for whoever learnt it when it switched workspace.
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      model.togglePalette();
    }
  }
</script>

<svelte:window onkeydown={key} />
<a class="skip-main" href="#shell-main" onclick={(e) => { e.preventDefault(); mainEl?.focus({ preventScroll: true }); }}>Skip to main content</a>
<!-- While the assistant is over the window, what is under it is not reachable by keyboard or screen reader either. -->
<div class="shell" inert={model.assistantShown}>
  <Sidebar />

  <div class="content">
    {#if model.error}
      <div class="message error" role="alert">{model.error}<button class="btn" type="button" onclick={() => (model.error = '')}>Dismiss</button></div>
    {/if}
    {#if model.notice}
      <div class="message" role="status">{model.notice}<button class="btn" type="button" onclick={() => (model.notice = '')}>Dismiss</button></div>
    {/if}
    {#if !model.loaded}<p class="loading" role="status">Opening your workspaces…</p>{/if}
    {#if model.setupRequired && !(model.page === 'workspace' && model.current === 'personal' && model.places.personal?.startsWith('#/onboarding'))}
      <div class="message" role="status"><span><strong>Setup is unfinished.</strong> Finish connecting your tools to get the full Werkbord experience.</span><button class="btn primary" onclick={() => { model.setupPaused = false; void model.open('personal', '#/onboarding'); }}>Resume setup</button></div>
    {/if}
    <main id="shell-main" tabindex="-1" bind:this={mainEl} class:with-nav={model.page === 'settings'}>
      {#if model.page === 'settings'}<SettingsNav />{/if}
      <div class="stage">
        <ContextStatus />
        <ReturnBar />
        <!-- Agents: what workspaces report sits above the person's own sessions, which are a page of their own Werkbord. -->
        {#if model.agentsInFrame}<Agents strip />{/if}
        <div class="split-area" class:split={model.paneOpen}>
          <div class="frame-slot" class:pane={model.paneOpen}>
            {#if model.paneOpen}
              <header class="pane-bar" data-testid="work-pane-bar">
                <span class="pane-title">{paneTitle}</span>
                <button class="pane-btn" type="button" onclick={expandPane} aria-label="Open {paneTitle.toLowerCase()} as a full page" title="Open as a full page"><Icon name="expand" size={14} /></button>
                <button class="pane-btn" type="button" onclick={() => model.closePane()} aria-label="Close {paneTitle.toLowerCase()}" title="Close" data-testid="work-pane-close"><Icon name="close" size={14} /></button>
              </header>
            {/if}
            <FrameHost shown={(model.page === 'workspace' && !problem) || model.agentsInFrame || model.paneOpen || (!!hostedWorkspace && !hostedGone && !(native && 'native' in native))} />
          </div>
        {#if model.page === 'workspace' && problem}
          <div class="page-area" use:remembered={model.page}><WorkspaceProblem /></div>
        {:else if hostedGone}
          <div class="page-area" use:remembered={model.page}><WorkspaceProblem /></div>
        {:else if native && 'native' in native}
          <div class="page-area" use:remembered={model.page}>{#if native.native === 'plan'}<SettingsPlan />{:else if native.native === 'workspaces'}<Workspaces overview={model.overview} embedded />{:else}<SettingsNative section={native.native} />{/if}</div>
        {:else if model.page !== 'workspace' && !model.agentsInFrame && !hostedWorkspace}
          <div class="page-area" use:remembered={model.page}>
            {#if model.page === 'calendar'}<Calendar overview={model.contextOverview} />
            {:else if model.page === 'agents'}<Agents />
            {:else if model.page === 'reviews'}<Reviews />
            {:else if model.page === 'analytics'}<Analytics />
            {:else if model.page === 'inbox'}<Hub overview={model.contextOverview} compact={model.paneOpen} />
            {:else if model.page === 'workspaces'}<Workspaces overview={model.overview} />{/if}
          </div>
        {/if}
        </div>
      </div>
    </main>
  </div>
  {#if model.paletteOpen}<Palette />{/if}
  {#if model.addOpen}<AddTeam />{/if}
</div>
<AssistantFrame />

<style>
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

  .shell {
    height: 100dvh;
    display: flex;
    overflow: hidden;
    background: var(--bg);
  }
  .content {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  main {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: row;
    overflow: hidden;
  }
  /* Settings has its own list of sections beside the section on show; on a narrow window the list becomes a menu above it. */
  @media (max-width: 899px) {
    main.with-nav {
      flex-direction: column;
    }
  }
  .stage {
    flex: 1;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .page-area {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }
  /* The frame and the page it shows are one area. With a task or session open on Work the area is two columns: the list, then the open page. */
  .split-area {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .split-area.split {
    flex-direction: row;
  }
  .split-area.split .page-area {
    order: -1;
    flex: 0 0 clamp(360px, 38%, 520px);
    border-right: 1px solid var(--border);
  }
  .frame-slot:not(.pane) {
    display: contents;
  }
  .frame-slot.pane {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    animation: pane-in 280ms var(--ease) both;
  }
  .pane-bar {
    flex: none;
    display: flex;
    align-items: center;
    gap: 4px;
    min-height: 40px;
    padding: 0 8px 0 16px;
    border-bottom: 1px solid var(--border);
    background: var(--surface);
  }
  .pane-title {
    flex: 1;
    font-size: 13px;
    font-weight: 600;
    color: var(--text-2);
  }
  .pane-btn {
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
    border: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-2);
    cursor: pointer;
    transition: background 120ms, color 120ms;
  }
  .pane-btn:hover {
    background: var(--surface-2);
    color: var(--text);
  }
  .pane-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  @keyframes pane-in {
    from { opacity: 0; transform: translateX(20px); }
    to { opacity: 1; transform: none; }
  }
  @media (prefers-reduced-motion: reduce) {
    .frame-slot.pane { animation: none; }
  }
  .loading {
    padding: 20px;
    color: var(--text-2);
  }
  .message {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    padding: 8px 16px;
    border-bottom: 1px solid var(--border);
    background: var(--surface-2);
  }
  .error {
    color: var(--danger-text);
  }
</style>
