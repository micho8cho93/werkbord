<script lang="ts">
  import Icon, { type IconName } from '../lib/Icon.svelte';
  import Mark from '../lib/Mark.svelte';
  import { counts } from './aggregate';
  import { model, type Page } from './model.svelte';
  import { activeKey, entries, listed } from './nav';

  // The window's one sidebar. Top to bottom: which workspace is open (and the switcher to change it), what spans every
  // workspace (My Work, Workspaces, Calendar), the open workspace's own places (its home, projects and so on), and last the
  // things about this computer (Devices & Hosting, Settings of the workspace open). The workspaces' own
  // pages carry no navigation of their own in this window.
  const everywhereShown = $derived(model.universal);
  const active = $derived(model.view?.items.find((i) => i.id === model.current));
  const kind = $derived(active?.kind ?? 'personal');
  const name = $derived(model.universal ? 'Universal' : kind === 'personal' ? 'Individual' : active?.name ?? 'Individual');
  // A workspace that cannot be shown (not running, too old) has nowhere to go to: the page says why instead.
  const usable = $derived(!!model.frames[model.current] || active?.state === 'ready');
  const summary = $derived(model.overview?.entries.find((e) => e.workspace.id === model.current)?.summary);
  const projects = $derived(listed(summary?.projects));
  const here = $derived(model.page === 'workspace' ? activeKey(kind, model.places[model.current]) : '');
  const needs = $derived(model.overview ? counts(model.overview).needsYou : 0);
  const reviews = $derived(kind === 'team' ? (summary?.attention.filter((a) => a.kind === 'review').length ?? 0) : 0);

  const everywhere: { id: Page; title: string; icon: IconName }[] = [
    { id: 'mywork', title: 'My Work', icon: 'check' },
    { id: 'inbox', title: 'Workspaces', icon: 'control' },
    { id: 'calendar', title: 'Calendar', icon: 'calendar' },
  ];

  // A narrow window keeps room for the workspace's pages (they change to a phone layout under about 900px): the sidebar
  // folds to icons.
  const query = typeof matchMedia === 'function' ? matchMedia('(max-width: 1107px)') : null;
  let narrow = $state(query?.matches ?? false);
  $effect(() => {
    const change = (e: MediaQueryListEvent) => (narrow = e.matches);
    query?.addEventListener('change', change);
    return () => query?.removeEventListener('change', change);
  });
  // At intermediate window widths the rail starts with icons to leave room for work, but the
  // person can reveal the labels and project list without needing to guess what an icon means.
  let labelsOpen = $state(false);
  const collapsed = $derived(narrow ? !labelsOpen : model.sidebarCollapsed);
  const sidebarToggleLabel = $derived(collapsed ? (narrow ? 'Show sidebar labels' : 'Expand sidebar') : (narrow ? 'Hide sidebar labels' : 'Collapse sidebar'));

  let switchButton: HTMLButtonElement | undefined = $state();
  $effect(() => {
    model.switcherButton = switchButton;
  });

  // Individual's assistant: the button it opens out of, and whether Werkbord can be reached.
  let assistantButton: HTMLButtonElement | undefined = $state();
  $effect(() => {
    model.assistantButton = assistantButton;
  });
  const assistantReachable = $derived(model.view?.items.find((i) => i.id === 'personal')?.state !== 'unavailable');
  const assistantLabel = $derived(
    !assistantReachable
      ? 'Salie, unavailable: Cannot reach Werkbord'
      : model.assistant.waiting > 0
        ? `Salie, ${model.assistant.waiting} ${model.assistant.waiting === 1 ? 'change is' : 'changes are'} waiting for you`
        : model.assistant.running
          ? 'Salie, working'
          : 'Salie',
  );
  function askAssistant() {
    const r = assistantButton?.getBoundingClientRect();
    if (r) void model.toggleAssistant({ x: r.left, y: r.top, width: r.width, height: r.height });
  }
</script>

{#snippet item(title: string, icon: IconName, current: boolean, go: () => void, id: string, count = 0)}
  <button class="nav" type="button" aria-current={current ? 'page' : undefined} onclick={go} {title} aria-label={id.startsWith('generated-') ? title : undefined} data-page={id}>
    <Icon name={icon} /><span class="label">{title}</span>
    {#if count}<span class="num" aria-label="{count} waiting for you">{count}</span>{/if}
  </button>
{/snippet}

<nav class="side" class:collapsed aria-label="Werkbord">
  <button class="toggle" type="button" onclick={() => { if (narrow) labelsOpen = !labelsOpen; else model.toggleSidebar(); }} aria-label={sidebarToggleLabel} title={sidebarToggleLabel} aria-expanded={!collapsed} data-testid="sidebar-label-toggle">
    <Icon name={collapsed ? 'right' : 'left'} size={14} />
  </button>

  <button bind:this={switchButton} class="switch" type="button" aria-haspopup="menu" aria-expanded={!!model.switcher} aria-label="Switch workspace. Showing {name}" title={name} onclick={() => model.toggleSwitcher()} data-testid="workspace-switcher">
    <span class="brand"><Mark height={20} /><span class="wm">werkbord</span></span>
    <span class="mode"><span class="mode-name">{name}</span><Icon name="down" size={14} /></span>
  </button>

  <div class="group ask">
    <button bind:this={assistantButton} class="nav assistant" type="button" aria-haspopup="dialog" aria-expanded={model.assistant.open} aria-label={assistantLabel} title={assistantReachable ? 'Salie' : 'Cannot reach Werkbord'} disabled={!assistantReachable} onclick={askAssistant} data-page="assistant">
      <span class="aico"><Icon name="assistant" /></span><span class="label">Salie</span>
      {#if model.assistant.waiting > 0}
        <span class="num" aria-hidden="true">{model.assistant.waiting}</span>
      {:else if model.assistant.running}
        <span class="live" aria-hidden="true"></span>
      {/if}
    </button>
  </div>

  {#if everywhereShown}
  <div class="group">

    {#each everywhere as p (p.id)}
      {@render item(p.title, p.icon, model.page === p.id, () => model.go(p.id), p.id, p.id === 'inbox' ? needs : 0)}
    {/each}
  </div>

  {/if}

  {#if !model.universal}
  <div class="group" aria-label={name}>
    <div class="lbl" title={name}>{name}</div>
    {#if usable}
      {#each entries(kind) as e (e.key)}
        {@render item(e.title, e.icon, here === e.key, () => model.visit(e.place), 'ws-' + e.key, e.key === 'reviews' ? reviews : !everywhereShown && e.key === 'home' ? needs : 0)}
        {#if e.key === 'projects'}
          {#each projects as p (p.id)}
            <button class="nav proj" type="button" aria-current={here === `project:${p.id}` ? 'page' : undefined} onclick={() => model.visit(p.href)} title={p.name} data-project={p.id}>
              <span class="sq" aria-hidden="true"></span><span class="label">{p.name}</span>
            </button>
          {/each}
          {#if kind === 'personal'}
            <button class="nav proj add" type="button" onclick={() => model.visit('#/projects')} title="Add a project" data-page="add-project">
              <Icon name="plus" size={14} /><span class="label">Add project</span>
            </button>
            {@render item('Analytics', 'analytics', here === 'analytics', () => model.visit('#/analytics'), 'ws-analytics')}
          {/if}
        {/if}
      {/each}
    {:else if !everywhereShown}
      {@render item('Workspace', 'control', model.page === 'inbox', () => model.go('inbox'), 'inbox', needs)}
    {/if}
  </div>

  {/if}
  <div class="group generated">
    {#if !model.universal}{@render item('Create with Salie', 'plus', false, () => { void model.customizeGenerated(); }, 'create-generated')}{/if}
    {#each model.visibleGenerated as workspace (workspace.id)}
      {@render item(workspace.name, workspace.icon, model.page === 'generated' && model.generatedID === workspace.id, () => model.openGenerated(workspace.id), 'generated-' + workspace.id)}
    {/each}
    {@render item('Personal data library', 'database', model.page === 'personal-library', () => model.go('personal-library'), 'personal-library')}
    {#if model.generatedError}<button class="nav" title={model.generatedError} aria-label="Saved workspaces unavailable. Retry" onclick={() => model.refreshGenerated()}><Icon name="reply" /><span class="label">Retry saved workspaces</span></button>{/if}
  </div>
  <div class="group bottom">
    <div class="lbl" aria-hidden="true"></div>
    {@render item('Devices & Hosting', 'runner', model.page === 'workspaces', () => model.go('workspaces'), 'workspaces')}
    {@render item('Settings', 'settings', model.page === 'settings', () => model.openSettings(), 'settings')}

  </div>
</nav>

<style>
  .side {
    display: flex;
    flex-direction: column;
    gap: 2px;
    width: 208px;
    flex: none;
    height: 100%;
    padding: 10px 8px;
    background: var(--surface);
    border-right: 1px solid var(--border);
    overflow-y: auto;
    overflow-x: hidden;
  }
  .side.collapsed {
    width: 48px;
    padding: 10px 6px;
  }
  .toggle {
    align-self: flex-end;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    margin-bottom: 2px;
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--text-2);
    cursor: pointer;
  }
  .collapsed .toggle {
    align-self: center;
  }
  .switch {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
    width: 100%;
    padding: 6px 8px;
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--text);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .brand,
  .mode {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
    max-width: 100%;
  }
  .wm {
    font-family: var(--mono);
    font-weight: 600;
    font-size: 17px;
    letter-spacing: -0.06em;
    line-height: 1;
  }
  .mode {
    gap: 6px;
    color: var(--text-2);
    font-size: 13px;
    font-weight: 500;
  }
  .mode-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .collapsed .switch {
    align-items: center;
    padding: 6px 0;
  }
  .collapsed .wm,
  .collapsed .mode {
    display: none;
  }
  .group {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .generated { border-top: 1px solid var(--border); margin-top: 6px; padding-top: 6px; }
  .generated :global(svg) { flex: none; }
  .bottom {
    margin-top: auto;
  }
  .ask {
    padding: 6px 0 0;
  }
  .nav.assistant {
    position: relative;
    border: 1px solid color-mix(in srgb, var(--accent) 45%, var(--border));
    background: var(--bg);
    color: var(--text);
    font-weight: 500;
  }
  .assistant:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .aico {
    display: inline-flex;
    color: var(--accent);
  }
  .live {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    animation: pulse 1.4s ease-in-out infinite;
  }
  .collapsed .live {
    position: absolute;
    top: 3px;
    right: 3px;
  }
  .lbl {
    min-height: 14px;
    padding: 14px 8px 4px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-2);
    font-family: var(--mono);
    font-size: 11.5px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
  }
  .collapsed .lbl {
    height: 1px;
    min-height: 0;
    margin: 6px 4px;
    padding: 0;
    background: var(--border);
    font-size: 0;
  }
  .nav {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    min-height: 32px;
    padding: 0 8px;
    border: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-2);
    font: inherit;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }
  .collapsed .nav {
    justify-content: center;
    padding: 0;
    position: relative;
  }
  .nav:hover,
  .toggle:hover,
  .switch:hover,
  .switch[aria-expanded='true'],
  .nav[aria-current='page'] {
    background: var(--surface-2);
    color: var(--text);
  }
  .nav:focus-visible,
  .toggle:focus-visible,
  .switch:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .proj {
    padding-block: 6px;
    padding-left: 12px;
  }
  .proj .label { white-space: normal; overflow-wrap: anywhere; }
  .sq {
    width: 7px;
    height: 7px;
    flex: none;
    background: var(--text-2);
  }
  .proj[aria-current='page'] .sq {
    background: var(--text);
  }
  .collapsed .proj {
    display: none;
  }
  .label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .collapsed .label {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  .num {
    min-width: 18px;
    padding: 0 5px;
    border-radius: 9px;
    background: var(--warn);
    color: var(--on-accent, #fff);
    font-family: var(--mono);
    font-size: 11.5px;
    line-height: 18px;
    text-align: center;
  }
  .collapsed .num {
    position: absolute;
    top: 1px;
    right: 1px;
    min-width: 14px;
    font-size: 11.5px;
    line-height: 14px;
    padding: 0 3px;
  }
</style>
