<script lang="ts">
  import { onMount } from 'svelte';
  import Icon, { type IconName } from '../lib/Icon.svelte';
  import WorkspaceIcon from '../lib/personal/WorkspaceIcon.svelte';
  import Mark from '../lib/Mark.svelte';
  import AssistantGlyph from '../lib/AssistantGlyph.svelte';
  import { salie } from './salie.svelte';
  import { counts } from './aggregate';
  import { model } from './model.svelte';
  import { plan } from './plan.svelte';
  import { activeKey, entries, listed } from './nav';

  onMount(() => void plan.refresh());

  // The window's one sidebar. Top to bottom: which workspace is open (and the switcher to change it), the open workspace's own
  // places (its home, projects and so on) with Create with Salie and the workspaces Salie has made, and last, pinned to the
  // bottom whatever else is shown, the things about this computer: Devices & Hosting, the Data library and Settings.
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
  const collapsed = $derived(model.universal ? !model.universalSidebarOpen : narrow ? !labelsOpen : model.sidebarCollapsed);
  const sidebarToggleLabel = $derived(collapsed ? (narrow ? 'Show sidebar labels' : 'Expand sidebar') : (narrow ? 'Hide sidebar labels' : 'Collapse sidebar'));

  // Each time Universal is entered its sidebar starts folded: the page is the hub, the sidebar only switches and reaches the machine.
  $effect(() => {
    if (model.universal) model.universalSidebarOpen = false;
  });

  let switchButton: HTMLButtonElement | undefined = $state();
  let salieButton: HTMLButtonElement | undefined = $state();
  const sidebarSalie = $derived(salie.placements[model.universal ? 'universal' : 'workspace'] !== 'floating');
  $effect(() => {
    const button = salieButton;
    model.assistantSidebarButton = button;
    return () => { if (model.assistantSidebarButton === button) model.assistantSidebarButton = undefined; };
  });
  $effect(() => {
    model.switcherButton = switchButton;
  });

</script>

{#snippet item(title: string, icon: IconName | string, current: boolean, go: () => void, id: string, count = 0)}
  <button class="nav" type="button" aria-current={current ? 'page' : undefined} onclick={go} {title} aria-label={id.startsWith('generated-') ? title : undefined} data-page={id}>
    {#if id.startsWith('generated-')}<WorkspaceIcon {icon} name={title} />{:else}<Icon name={icon as IconName} />{/if}<span class="label">{title}</span>
    {#if count}<span class="num" aria-label="{count} waiting for you">{count}</span>{/if}
  </button>
{/snippet}

<nav class="side" class:collapsed aria-label="Werkbord">
  <button class="toggle" type="button" onclick={() => { if (model.universal || !narrow) model.toggleSidebar(); else labelsOpen = !labelsOpen; }} aria-label={sidebarToggleLabel} title={sidebarToggleLabel} aria-expanded={!collapsed} data-testid="sidebar-label-toggle">
    <Icon name={collapsed ? 'right' : 'left'} size={14} />
  </button>

  <button bind:this={switchButton} class="switch" type="button" aria-haspopup="menu" aria-expanded={!!model.switcher} aria-label="Switch workspace. Showing {name}" title={name} onclick={() => model.toggleSwitcher()} data-testid="workspace-switcher">
    <span class="brand"><Mark height={20} /><span class="wm">werkbord</span></span>
    <span class="mode"><span class="mode-name">{name}</span><Icon name="down" size={14} /></span>
  </button>

  <div class="top">
  {#if sidebarSalie}
    <button bind:this={salieButton} class="nav salie-nav" type="button" title="Salie" aria-haspopup="dialog" aria-expanded={model.assistant.open} disabled={model.view?.items.find(i => i.id === 'personal')?.state === 'unavailable'} onclick={() => {
      model.assistantButton = salieButton;
      const rect = salieButton?.getBoundingClientRect();
      if (rect) void model.toggleAssistant(rect);
    }} data-testid="sidebar-salie">
      <AssistantGlyph /><span class="label">Salie</span>
      {#if model.assistant.waiting}<span class="num" aria-label="{model.assistant.waiting} waiting for you">{model.assistant.waiting}</span>{:else if model.assistant.running}<span class="salie-working" role="status" aria-label="Salie is working"></span>{/if}
    </button>
  {/if}
  {#if !model.universal}
  <div class="group" aria-label={name}>
    <div class="lbl" title={name}>{name}</div>
    {#if usable}
      {#each entries(kind) as e (e.key)}
        {@render item(e.title, e.icon, here === e.key, () => model.visit(e.place), 'ws-' + e.key, e.key === 'reviews' ? reviews : e.key === 'home' ? needs : 0)}
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
    {:else}
      {@render item('Workspace', 'control', model.page === 'inbox', () => model.go('inbox'), 'inbox', needs)}
    {/if}
  </div>

  <div class="group generated">
    {@render item('Create with Salie', 'plus', false, () => { void model.customizeGenerated(); }, 'create-generated')}
    {#each model.visibleGenerated as workspace (workspace.id)}
      {@render item(workspace.name, workspace.icon, model.page === 'generated' && model.generatedID === workspace.id, () => model.openGenerated(workspace.id), 'generated-' + workspace.id)}
    {/each}
    {#if model.generatedError}<button class="nav" title={model.generatedError} aria-label="Saved workspaces unavailable. Retry" onclick={() => model.refreshGenerated()}><Icon name="reply" /><span class="label">Retry saved workspaces</span></button>{/if}
  </div>
  {/if}
  </div>
  <div class="group bottom">
    {@render item('Devices & Hosting', 'runner', model.page === 'workspaces', () => model.go('workspaces'), 'workspaces')}
    {@render item('Data library', 'database', model.page === 'personal-library', () => model.go('personal-library'), 'personal-library')}
    {@render item('Settings', 'settings', model.page === 'settings', () => model.openSettings(), 'settings')}
    <button class="plan" class:on={plan.premium} type="button" onclick={() => model.openSettings('general/plan')} data-testid="plan-chip" aria-label={plan.premium ? 'Plan: Premium' : 'Plan: Free. See Premium'}>
      <span class="label">{plan.premium ? 'Premium' : 'Free plan'}</span><span class="pill"><span class="full">{plan.premium ? 'Active' : 'Upgrade'}</span><span class="short">Free</span></span>
    </button>
  </div>
</nav>

<style>
  .plan {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 6px 4px 0;
    padding: 7px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg);
    color: var(--text);
    font: inherit;
    font-size: 13px;
    cursor: pointer;
  }
  .collapsed .plan { justify-content: center; margin: 6px 0 0; padding: 6px 0; }
  .collapsed .plan .label { display: none; }
  .collapsed .plan.on { display: none; }
  .plan .short { display: none; }
  .collapsed .plan .short { display: inline; }
  .collapsed .plan .full { display: none; }
  .collapsed .plan .pill { margin: 0; padding: 0 6px; font-size: 12px; }
  .plan:hover { background: var(--surface-2); }
  .plan:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .plan .pill { margin-left: auto; padding: 0 8px; border-radius: 99px; background: var(--surface-2); color: var(--text-2); font-size: 12px; font-weight: 600; }
  .salie-nav { flex: none; }
  .salie-working { width: 7px; height: 7px; border-radius: 50%; background: var(--accent); margin-left: auto; flex: none; }
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
    overflow: hidden;
  }
  /* What can grow (the workspace's places, the workspaces Salie made) scrolls on its own, so the bottom group never moves. */
  .top {
    display: flex;
    flex: 1 1 auto;
    flex-direction: column;
    gap: 2px;
    min-height: 0;
    overflow-x: hidden;
    overflow-y: auto;
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
    font-size: 16px;
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
    flex: none;
    margin-top: 6px;
    padding-top: 6px;
    border-top: 1px solid var(--border);
  }
  .lbl {
    min-height: 14px;
    padding: 14px 8px 4px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-2);
    font-family: var(--mono);
    font-size: 12px;
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
    border-radius: 10px;
    background: var(--warn);
    color: var(--on-accent, #fff);
    font-family: var(--mono);
    font-size: 12px;
    line-height: 18px;
    text-align: center;
  }
  .collapsed .num {
    position: absolute;
    top: 1px;
    right: 1px;
    min-width: 14px;
    font-size: 12px;
    line-height: 14px;
    padding: 0 3px;
  }
</style>
