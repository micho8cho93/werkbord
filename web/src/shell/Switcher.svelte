<script lang="ts">
  import Icon from '../lib/Icon.svelte';
  import { model } from './model.svelte';
  import type { Item } from './types';

  // The persistent switcher. Personal is always first and always there; Team workspaces follow. Choosing one changes what the
  // window shows and nothing else: whatever is running in any workspace keeps running.

  let { onadd }: { onadd: () => void } = $props();

  const items = $derived(model.view?.items.filter((i) => i.state !== 'setup' || i.id === model.current) ?? []);
  const active = $derived(items.find((i) => i.id === model.current));
  const tone = (i: Item): string => (i.state === 'ready' ? 'ok' : i.state === 'offline' || i.state === 'unavailable' ? 'bad' : 'wait');
  const role = (i: Item): string => (i.kind === 'personal' ? 'This computer' : i.role ? i.role[0].toUpperCase() + i.role.slice(1) : 'Team');

  let menu: HTMLElement | undefined = $state();
  let button: HTMLButtonElement | undefined = $state();

  $effect(() => {
    if (model.switcherOpen) queueMicrotask(() => (menu?.querySelector<HTMLElement>('[role="menuitem"][aria-current="true"]') ?? menu?.querySelector<HTMLElement>('[role="menuitem"]'))?.focus());
  });

  function onkey(e: KeyboardEvent): void {
    if (e.key === 'Escape') {
      model.switcherOpen = false;
      button?.focus();
      return;
    }
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
    const all = [...(menu?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [])];
    const at = all.indexOf(document.activeElement as HTMLElement);
    all[(at + (e.key === 'ArrowDown' ? 1 : -1) + all.length) % all.length]?.focus();
    e.preventDefault();
  }
</script>

<div class="switcher">
  <button
    bind:this={button}
    class="current"
    type="button"
    aria-haspopup="menu"
    aria-expanded={model.switcherOpen}
    onclick={() => (model.switcherOpen = !model.switcherOpen)}
    data-testid="workspace-switcher"
  >
    {#if active}<span class="dot" data-tone={tone(active)} aria-hidden="true"></span>{/if}
    <span class="name">{active?.name ?? 'Personal'}</span>
    <span class="kind">{active ? role(active) : ''}</span>
    <Icon name="down" size={14} />
  </button>

  {#if model.switcherOpen}
    <div class="scrim" onclick={() => (model.switcherOpen = false)} role="presentation"></div>
    <div class="menu" role="menu" aria-label="Workspaces" tabindex="-1" bind:this={menu} onkeydown={onkey}>
      {#each items as i (i.id)}
        <button class="item" type="button" role="menuitem" aria-current={i.id === model.current && model.page === 'workspace'} onclick={() => model.open(i.id)} data-workspace={i.id}>
          <span class="dot" data-tone={tone(i)} aria-hidden="true"></span>
          <span class="text">
            <span class="name">{i.name}</span>
            <span class="sub">{role(i)}{i.state !== 'ready' && i.detail ? ` · ${i.detail}` : ''}</span>
          </span>
          {#if i.state === 'offline' || i.state === 'unavailable'}<span class="flag">{i.state}</span>{/if}
        </button>
      {/each}
      <div class="rule" role="separator"></div>
      <button class="item add" type="button" role="menuitem" onclick={() => { model.switcherOpen = false; onadd(); }} data-testid="add-team">
        <Icon name="plus" size={14} /><span class="name">Add a Team…</span>
      </button>
      <button class="item add" type="button" role="menuitem" onclick={() => model.go('workspaces')}>
        <Icon name="settings" size={14} /><span class="name">Workspaces and devices</span>
      </button>
    </div>
  {/if}
</div>

<style>
  .switcher {
    position: relative;
  }
  .current {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    min-height: 32px;
    max-width: 320px;
    padding: 0 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--btn-bg);
    box-shadow: var(--btn-sh);
    color: var(--text);
    font: inherit;
    font-weight: 600;
    font-size: 13px;
    cursor: pointer;
  }
  .current:focus-visible,
  .item:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .kind {
    color: var(--text-2);
    font-family: var(--mono);
    font-size: 11px;
    font-weight: 400;
  }
  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dot {
    width: 7px;
    height: 7px;
    flex: none;
    border-radius: 50%;
    background: var(--text-2);
  }
  .dot[data-tone='ok'] {
    background: var(--ok);
  }
  .dot[data-tone='bad'] {
    background: var(--danger);
  }
  .dot[data-tone='wait'] {
    background: var(--warn);
  }
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 20;
  }
  .menu {
    position: absolute;
    top: calc(100% + 6px);
    left: 0;
    z-index: 21;
    min-width: 300px;
    max-width: 380px;
    padding: 6px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    box-shadow: var(--win-sh);
  }
  .item {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    min-height: 40px;
    padding: 4px 10px;
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--text);
    font: inherit;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }
  .item:hover {
    background: var(--surface-2);
  }
  .item[aria-current='true'] {
    background: var(--surface-2);
    box-shadow: var(--press-sh);
  }
  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .sub {
    color: var(--text-2);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .flag {
    margin-left: auto;
    font-family: var(--mono);
    font-size: 10px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--danger-text);
  }
  .add {
    min-height: 34px;
    color: var(--text-2);
  }
  .rule {
    height: 1px;
    margin: 6px 4px;
    background: var(--border);
  }
</style>
