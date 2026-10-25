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
  import { activeKey, entries, listed, sectionColour, sectionItems } from './nav';
  import { reviewCount } from './reviews';
  import type { Item } from './types';

  onMount(() => void plan.refresh());

  // The window's one sidebar. Top to bottom: Salie, Home, Calendar, Reviews and Analytics (what spans everything, each one page with
  // its own choice of where to look), then a section for Personal and one
  // for each team the person belongs to, each with its own projects, then Create with Salie, and last, pinned to the
  // bottom whatever else is shown, the things about this computer: Devices & Hosting, the Data library, Settings and the plan.
  // There is no switcher and no mode: a project is reached from the section it belongs to.
  const sections = $derived(sectionItems(model.view?.items));
  const needs = $derived(model.overview ? counts(model.overview).needsYou : 0);
  const summaryOf = (id: string) => model.overview?.entries.find((e) => e.workspace.id === id)?.summary;
  const nameOf = (i: Item): string => (i.kind === 'personal' ? 'Personal' : i.name);
  // The place in a workspace the person is at now, as the section it belongs to: only for the workspace that is on show.
  const hereIn = (i: Item): string => (model.page === 'workspace' && model.current === i.id ? activeKey(i.kind, model.places[i.id]) : '');
  const reviews = $derived(reviewCount(model.overview));
  const needsOf = (i: Item): number => (summaryOf(i.id)?.attention ?? []).filter((a) => a.severity !== 'info').length;
  // A workspace that cannot be shown (not running, too old) has nowhere to go to inside it: its header opens the page that says why.
  const usable = (i: Item): boolean => !!model.frames[i.id] || i.state === 'ready';
  const generatedIn = (i: Item) => model.generatedFor(i.id);

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

  let salieButton: HTMLButtonElement | undefined = $state();
  const sidebarSalie = $derived(salie.placements[model.universal ? 'universal' : 'workspace'] !== 'floating');
  $effect(() => {
    const button = salieButton;
    model.assistantSidebarButton = button;
    return () => { if (model.assistantSidebarButton === button) model.assistantSidebarButton = undefined; };
  });
</script>

{#snippet item(title: string, icon: IconName | string, current: boolean, go: () => void, id: string, count = 0)}
  <button class="nav" type="button" aria-current={current ? 'page' : undefined} onclick={go} {title} aria-label={id.startsWith('generated-') ? title : undefined} data-page={id}>
    {#if id.startsWith('generated-')}<WorkspaceIcon {icon} name={title} />{:else}<Icon name={icon as IconName} />{/if}<span class="label">{title}</span>
    {#if count}<span class="num" aria-label="{count} waiting for you">{count}</span>{/if}
  </button>
{/snippet}

<nav class="side" class:collapsed aria-label="Werkbord">
  <button class="toggle" type="button" onclick={() => { if (!narrow) model.toggleSidebar(); else labelsOpen = !labelsOpen; }} aria-label={sidebarToggleLabel} title={sidebarToggleLabel} aria-expanded={!collapsed} data-testid="sidebar-label-toggle">
    <Icon name={collapsed ? 'right' : 'left'} size={14} />
  </button>

  <button class="brand-row" type="button" aria-label="werkbord. Go to Home" title="Home" onclick={() => model.go('inbox')} data-testid="brand">
    <span class="brand"><Mark height={20} /><span class="wm">werkbord</span></span>
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

  <div class="group" aria-label="Everything">
    {@render item('Home', 'control', model.page === 'inbox', () => model.go('inbox'), 'inbox', needs)}
    {@render item('Calendar', 'calendar', model.page === 'calendar', () => model.go('calendar'), 'calendar')}
    {@render item('Reviews', 'merge', model.page === 'reviews', () => model.go('reviews'), 'reviews', reviews)}
    {@render item('Analytics', 'analytics', model.page === 'analytics', () => model.go('analytics'), 'analytics')}
    {#each model.generatedFor('universal') as workspace (workspace.id)}
      {@render item(workspace.name, workspace.icon, model.page === 'generated' && model.generatedID === workspace.id, () => model.openGenerated(workspace.id), 'generated-' + workspace.id)}
    {/each}
    {@render item('Create with Salie', 'plus', false, () => { void model.customizeGenerated(); }, 'create-generated')}
    {#if model.generatedError}<button class="nav" title={model.generatedError} aria-label="Saved workspaces unavailable. Retry" onclick={() => model.refreshGenerated()}><Icon name="reply" /><span class="label">Retry saved workspaces</span></button>{/if}
  </div>

  {#each sections as i (i.id)}
    {@const kind = i.kind}
    {@const folded = model.folded.includes(i.id)}
    {@const here = hereIn(i)}
    {@const home = entries(kind)[0].place}
    <div class="group section" data-section={i.id} data-kind={kind} style:--sc={sectionColour(kind, i.id)} role="group" aria-label={nameOf(i)}>
      <div class="shead">
        <button class="fold" type="button" aria-expanded={!folded} aria-label="{folded ? 'Show' : 'Hide'} {nameOf(i)}" onclick={() => model.toggleSection(i.id)} data-testid="fold-{i.id}">
          <Icon name={folded ? 'right' : 'down'} size={12} />
        </button>
        <button class="open" type="button" title={nameOf(i)} aria-current={here && !here.startsWith('project:') ? 'page' : undefined} onclick={() => model.goTo(i.id, usable(i) ? home : undefined)} data-open={i.id} data-page="ws-home">
          <span class="swatch" aria-hidden="true"></span><span class="label">{nameOf(i)}</span>
          {#if !usable(i)}<span class="meta">{i.state === 'unavailable' ? 'not running' : i.state}</span>
          {:else if needsOf(i)}<span class="num" aria-label="{needsOf(i)} waiting for you">{needsOf(i)}</span>{/if}
        </button>
      </div>
      {#if usable(i) && !folded}
        {#each listed(summaryOf(i.id)?.projects) as p (p.id)}
          <button class="nav proj" type="button" aria-current={here === `project:${p.id}` ? 'page' : undefined} onclick={() => model.goTo(i.id, p.href)} title={p.name} data-project={p.id}>
            <span class="sq" aria-hidden="true"></span><span class="label">{p.name}</span>
          </button>
        {/each}
        {#if kind === 'personal'}
          <button class="nav proj add" type="button" onclick={() => model.goTo(i.id, '#/projects')} title="New personal project" data-page="add-project">
            <Icon name="plus" size={14} /><span class="label">New personal project</span>
          </button>
        {/if}
        {#each generatedIn(i) as workspace (workspace.id)}
          {@render item(workspace.name, workspace.icon, model.page === 'generated' && model.generatedID === workspace.id, () => model.openGenerated(workspace.id), 'generated-' + workspace.id)}
        {/each}
      {/if}
    </div>
  {/each}

  </div>
  <div class="group bottom">
    <button class="nav" type="button" onclick={() => (model.addOpen = true)} title="New team or join…" data-testid="add-team" data-page="add-team">
      <Icon name="plus" /><span class="label">New team or join…</span>
    </button>
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
  .brand-row {
    display: flex;
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
  .brand {
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
  .collapsed .brand-row {
    justify-content: center;
    padding: 6px 0;
  }
  .collapsed .wm {
    display: none;
  }
  /* A section: Personal, or one team. Its header names it in its colour; its projects and places sit under it. */
  .section {
    margin-top: 8px;
    padding-top: 6px;
    border-top: 1px solid var(--border);
  }
  .shead {
    display: flex;
    align-items: center;
    gap: 2px;
  }
  .fold {
    flex: none;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 28px;
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--text-2);
    cursor: pointer;
  }
  .open {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1;
    min-width: 0;
    min-height: 30px;
    padding: 0 6px;
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--text);
    font: inherit;
    font-size: 13px;
    font-weight: 600;
    text-align: left;
    cursor: pointer;
  }
  .swatch {
    width: 10px;
    height: 10px;
    flex: none;
    border-radius: 3px;
    background: var(--sc, var(--accent));
  }
  .meta {
    color: var(--text-2);
    font-family: var(--mono);
    font-size: 12px;
    font-weight: 500;
  }
  .collapsed .fold, .collapsed .meta { display: none; }
  .collapsed .open { justify-content: center; padding: 0; }
  .collapsed .section { margin-top: 6px; padding-top: 6px; }
  .group {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .bottom {
    flex: none;
    margin-top: 6px;
    padding-top: 6px;
    border-top: 1px solid var(--border);
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
  .brand-row:hover,
  .open:hover,
  .fold:hover,
  .open[aria-current='page'],
  .nav[aria-current='page'] {
    background: var(--surface-2);
    color: var(--text);
  }
  .nav:focus-visible,
  .toggle:focus-visible,
  .brand-row:focus-visible,
  .open:focus-visible,
  .fold:focus-visible {
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
