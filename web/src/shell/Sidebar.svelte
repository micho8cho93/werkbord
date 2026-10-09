<script lang="ts">
  import Icon, { type IconName } from '../lib/Icon.svelte';
  import Mark from '../lib/Mark.svelte';
  import { theme } from '../lib/theme.svelte';
  import { counts } from './aggregate';
  import { model, type Page } from './model.svelte';
  import { activeKey, entries, listed, settingsPlace } from './nav';

  // The window's one sidebar. Top to bottom: which workspace is open (and the switcher to change it), what spans every
  // workspace (My Work, Calendar, Needs you), the open workspace's own places (its home, projects and so on), and last the
  // things about this computer (Workspaces and devices, Settings of the workspace open, the theme). The workspaces' own
  // pages carry no navigation of their own in this window.
  const active = $derived(model.view?.items.find((i) => i.id === model.current));
  const kind = $derived(active?.kind ?? 'personal');
  const name = $derived(active?.name ?? 'Individual');
  // A workspace that cannot be shown (not running, too old) has nowhere to go to: the page says why instead.
  const usable = $derived(!!model.frames[model.current] || active?.state === 'ready');
  const summary = $derived(model.overview?.entries.find((e) => e.workspace.id === model.current)?.summary);
  const projects = $derived(listed(summary?.projects));
  const here = $derived(model.page === 'workspace' ? activeKey(kind, model.places[model.current]) : '');
  const needs = $derived(model.overview ? counts(model.overview).needsYou : 0);
  const reviews = $derived(kind === 'team' ? (summary?.attention.filter((a) => a.kind === 'review').length ?? 0) : 0);

  const everywhere: { id: Page; title: string; icon: IconName }[] = [
    { id: 'mywork', title: 'My Work', icon: 'check' },
    { id: 'calendar', title: 'Calendar', icon: 'calendar' },
    { id: 'inbox', title: 'Needs you', icon: 'control' },
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
  const collapsed = $derived(model.sidebarCollapsed || narrow);

  let switchButton: HTMLButtonElement | undefined = $state();
  $effect(() => {
    model.switcherButton = switchButton;
  });
</script>

{#snippet item(title: string, icon: IconName, current: boolean, go: () => void, id: string, count = 0)}
  <button class="nav" type="button" aria-current={current ? 'page' : undefined} onclick={go} {title} data-page={id}>
    <Icon name={icon} /><span class="label">{title}</span>
    {#if count}<span class="num" aria-label="{count} waiting for you">{count}</span>{/if}
  </button>
{/snippet}

<nav class="side" class:collapsed aria-label="Werkbord">
  <button class="toggle" type="button" hidden={narrow} onclick={() => model.toggleSidebar()} aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'} title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'} aria-expanded={!collapsed}>
    <Icon name={collapsed ? 'right' : 'left'} size={14} />
  </button>

  <button bind:this={switchButton} class="switch" type="button" aria-haspopup="menu" aria-expanded={!!model.switcher} aria-label="Switch workspace. Showing {name}" title={name} onclick={() => model.toggleSwitcher()} data-testid="workspace-switcher">
    <span class="brand"><Mark height={20} /><span class="wm">werkbord</span></span>
    <span class="mode"><span class="mode-name">{name}</span><Icon name="down" size={14} /></span>
  </button>

  <div class="group">
    <div class="lbl">Everywhere</div>
    {#each everywhere as p (p.id)}
      {@render item(p.title, p.icon, model.page === p.id, () => model.go(p.id), p.id, p.id === 'inbox' ? needs : 0)}
    {/each}
  </div>

  <div class="group" aria-label={name}>
    <div class="lbl" title={name}>{name}</div>
    {#if usable}
      {#each entries(kind) as e (e.key)}
        {@render item(e.title, e.icon, here === e.key, () => model.visit(e.place), 'ws-' + e.key, e.key === 'reviews' ? reviews : 0)}
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
          {/if}
        {/if}
      {/each}
    {/if}
  </div>

  <div class="group bottom">
    <div class="lbl" aria-hidden="true"></div>
    {@render item('Workspaces and devices', 'runner', model.page === 'workspaces', () => model.go('workspaces'), 'workspaces')}
    {#if usable}
      {@render item('Settings', 'settings', here === 'settings', () => model.visit(settingsPlace(kind)), 'settings')}
    {/if}
    <button class="nav" type="button" onclick={() => theme.toggle()} title={theme.dark ? 'Use light theme' : 'Use dark theme'} aria-label={theme.dark ? 'Use light theme' : 'Use dark theme'}>
      <Icon name={theme.dark ? 'sun' : 'moon'} /><span class="label">{theme.dark ? 'Light theme' : 'Dark theme'}</span>
    </button>
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
  .toggle[hidden] {
    display: none;
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
  .bottom {
    margin-top: auto;
  }
  .lbl {
    min-height: 14px;
    padding: 14px 8px 4px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-2);
    font-family: var(--mono);
    font-size: 10px;
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
    background: none;
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
    padding-left: 12px;
  }
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
    font-size: 11px;
    line-height: 18px;
    text-align: center;
  }
  .collapsed .num {
    position: absolute;
    top: 1px;
    right: 1px;
    min-width: 14px;
    font-size: 9px;
    line-height: 14px;
    padding: 0 3px;
  }
</style>
