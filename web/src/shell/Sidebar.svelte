<script lang="ts">
  import Icon, { type IconName } from '../lib/Icon.svelte';
  import { theme } from '../lib/theme.svelte';
  import { counts } from './aggregate';
  import { model, type Page } from './model.svelte';

  // The window's own sidebar: what spans every workspace (My Work, Calendar, Needs you) and where they are managed. The
  // workspace on show keeps its own navigation beside it, exactly as it is when used on its own.
  const needs = $derived(model.overview ? counts(model.overview).needsYou : 0);
  const active = $derived(model.view?.items.find((i) => i.id === model.current));
  const pages: { id: Page; title: string; icon: IconName }[] = [
    { id: 'mywork', title: 'My Work', icon: 'check' },
    { id: 'calendar', title: 'Calendar', icon: 'calendar' },
    { id: 'inbox', title: 'Needs you', icon: 'control' },
  ];
  // A narrow window keeps room for the workspace's own full layout (its rail needs about 900px): the sidebar folds to icons.
  const query = typeof matchMedia === 'function' ? matchMedia('(max-width: 1179px)') : null;
  let narrow = $state(query?.matches ?? false);
  $effect(() => {
    const change = (e: MediaQueryListEvent) => (narrow = e.matches);
    query?.addEventListener('change', change);
    return () => query?.removeEventListener('change', change);
  });
  const collapsed = $derived(model.sidebarCollapsed || narrow);
</script>

<nav class="side" class:collapsed aria-label="Across workspaces">
  <button class="toggle" type="button" hidden={narrow} onclick={() => model.toggleSidebar()} aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'} title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'} aria-expanded={!collapsed}>
    <Icon name={collapsed ? 'right' : 'left'} size={14} />
  </button>

  <button class="nav" type="button" aria-current={model.page === 'workspace' ? 'page' : undefined} onclick={() => model.open(model.current)} title={active?.name ?? 'Individual'} data-page="workspace">
    <Icon name="board" /><span class="label">{active?.name ?? 'Individual'}</span>
  </button>

  <div class="rule" role="separator"></div>

  {#each pages as p (p.id)}
    <button class="nav" type="button" aria-current={model.page === p.id ? 'page' : undefined} onclick={() => model.go(p.id)} title={p.title} data-page={p.id}>
      <Icon name={p.icon} /><span class="label">{p.title}</span>
      {#if p.id === 'inbox' && needs}<span class="num" aria-label="{needs} need you">{needs}</span>{/if}
    </button>
  {/each}

  <div class="rule" role="separator"></div>

  <button class="nav" type="button" aria-current={model.page === 'workspaces' ? 'page' : undefined} onclick={() => model.go('workspaces')} title="Workspaces and devices" data-page="workspaces">
    <Icon name="runner" /><span class="label">Workspaces and devices</span>
  </button>

  <button class="nav foot" type="button" onclick={() => theme.toggle()} title={theme.dark ? 'Use light theme' : 'Use dark theme'} aria-label={theme.dark ? 'Use light theme' : 'Use dark theme'}>
    <Icon name={theme.dark ? 'sun' : 'moon'} /><span class="label">{theme.dark ? 'Light theme' : 'Dark theme'}</span>
  </button>
</nav>

<style>
  .side {
    display: flex;
    flex-direction: column;
    gap: 2px;
    width: 188px;
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
    margin-bottom: 6px;
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
  .nav[aria-current='page'] {
    background: var(--surface-2);
    color: var(--text);
  }
  .nav:focus-visible,
  .toggle:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
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
  .rule {
    height: 1px;
    margin: 6px 4px;
    background: var(--border);
  }
  .foot {
    margin-top: auto;
  }
</style>
