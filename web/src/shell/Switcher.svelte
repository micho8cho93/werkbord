<script lang="ts">
  import Icon from '../lib/Icon.svelte';
  import { model } from './model.svelte';
  import type { Item } from './types';

  // The switcher: Individual first, then each Team workspace, then adding one. It opens under the product name the person
  // clicked, in the header of whichever page is showing (a workspace's own page tells the shell where its name is).
  // Choosing one changes what the window shows and nothing else: whatever is running in any workspace keeps running.

  let { onadd }: { onadd: () => void } = $props();

  const items = $derived(model.view?.items.filter((i) => i.state !== 'setup' || i.id === model.current) ?? []);
  const personal = $derived(items.filter((i) => i.kind === 'personal'));
  const teams = $derived(items.filter((i) => i.kind === 'team'));
  const team = $derived(model.view?.team);
  const tone = (i: Item): string =>
    i.state === 'ready' ? 'ok' : i.state === 'offline' || i.state === 'unavailable' ? 'bad' : 'wait';
  const sub = (i: Item): string => {
    if (i.kind === 'personal') return model.personalOutdated ? 'Needs an update' : i.state === 'unavailable' ? 'Not running' : 'This computer';
    const role = i.role ? i.role[0].toUpperCase() + i.role.slice(1) : '';
    return ['Team', role, i.state !== 'ready' ? i.state : ''].filter(Boolean).join(' · ');
  };
  // Team itself needs something before any of its workspaces can be listed: say so where the workspaces would be.
  const teamProblem = $derived(
    teams.length === 0 && team && team.state !== 'ready' && team.state !== 'not_installed'
      ? { outdated: 'Needs an update', stopped: 'Not running', refused: 'Not available to this app' }[team.state]
      : '',
  );

  const pos = $derived.by(() => {
    const a = model.switcher;
    if (!a) return { left: 8, top: 8 };
    const width = 300;
    const vw = typeof innerWidth === 'number' ? innerWidth : 1024;
    return { left: Math.max(8, Math.min(a.x, vw - width - 8)), top: Math.max(8, a.y + a.height + 6) };
  });

  let menu: HTMLElement | undefined = $state();

  $effect(() => {
    if (model.switcher)
      queueMicrotask(() =>
        (menu?.querySelector<HTMLElement>('[role="menuitem"][aria-current="true"]') ?? menu?.querySelector<HTMLElement>('[role="menuitem"]'))?.focus(),
      );
  });

  function onkey(e: KeyboardEvent): void {
    if (e.key === 'Escape') {
      e.preventDefault();
      model.closeSwitcher(true);
      return;
    }
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
    const all = [...(menu?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [])];
    const at = all.indexOf(document.activeElement as HTMLElement);
    all[(at + (e.key === 'ArrowDown' ? 1 : -1) + all.length) % all.length]?.focus();
    e.preventDefault();
  }
</script>

{#snippet row(i: Item)}
  {@const chosen = i.id === model.current && model.page === 'workspace'}
  <button class="item" type="button" role="menuitem" aria-current={chosen} onclick={() => model.open(i.id)} data-workspace={i.id}>
    <span class="check" aria-hidden="true">{#if chosen}<Icon name="check" size={14} />{/if}</span>
    <span class="text">
      <span class="name">{i.name}</span>
      <span class="sub">{sub(i)}</span>
    </span>
    <span class="dot" data-tone={tone(i)} aria-hidden="true"></span>
  </button>
{/snippet}

<div class="scrim" onclick={() => model.closeSwitcher()} role="presentation"></div>
<div class="menu" role="menu" aria-label="Switch between Individual and Team" tabindex="-1" bind:this={menu} onkeydown={onkey} style:left="{pos.left}px" style:top="{pos.top}px" data-testid="switcher-menu">
  {#each personal as i (i.id)}{@render row(i)}{/each}
  {#if teams.length || teamProblem}
    <div class="rule" role="separator"></div>
    {#each teams as i (i.id)}{@render row(i)}{/each}
    {#if teamProblem}
      <button class="item" type="button" role="menuitem" onclick={() => model.go('workspaces')} data-testid="team-problem">
        <span class="check" aria-hidden="true"></span>
        <span class="text"><span class="name">Team</span><span class="sub">{teamProblem}</span></span>
        <span class="dot" data-tone="bad" aria-hidden="true"></span>
      </button>
    {/if}
  {/if}
  <div class="rule" role="separator"></div>
  <button class="item add" type="button" role="menuitem" onclick={() => { model.closeSwitcher(); onadd(); }} data-testid="add-team">
    <span class="check" aria-hidden="true"><Icon name="plus" size={14} /></span><span class="name">Add a Team…</span>
  </button>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 40;
  }
  .menu {
    position: fixed;
    z-index: 41;
    width: 300px;
    max-width: calc(100vw - 16px);
    max-height: calc(100dvh - 24px);
    overflow-y: auto;
    padding: 6px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    box-shadow: var(--win-sh);
  }
  .item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    min-height: 42px;
    padding: 4px 10px 4px 6px;
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--text);
    font: inherit;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }
  .item:hover,
  .item:focus-visible {
    background: var(--surface-2);
  }
  .item:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .check {
    display: inline-flex;
    justify-content: center;
    width: 18px;
    flex: none;
    color: var(--text-2);
  }
  .item[aria-current='true'] .check {
    color: var(--accent);
  }
  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    flex: 1;
  }
  .name {
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sub {
    color: var(--text-2);
    font-size: 12px;
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
