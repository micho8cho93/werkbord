<script module lang="ts">
  import type { IconName } from './Icon.svelte';

  /** A place the sidebar names. With an address it is a link, without one it is a button that does what `go` says. */
  export interface SidebarItem {
    key: string;
    label: string;
    icon: IconName;
    current?: boolean;
    href?: string;
    go?: () => void;
    disabled?: boolean;
    /** What waits there: a count, which sort of attention it is, and the words for it. */
    badge?: { count: number; tone: string; label: string };
    title?: string;
    ariaLabel?: string;
    testid?: string;
  }
  export interface SidebarSalie {
    label: string;
    title: string;
    waiting: number;
    running: boolean;
    expanded: boolean;
    disabled: boolean;
    press: (rect: DOMRect, button: HTMLButtonElement) => void;
    testid?: string;
  }
</script>

<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import Mark from './Mark.svelte';
  import AssistantGlyph from './AssistantGlyph.svelte';
  import { sidebar } from './sidebar-fold.svelte';

  // The window's sidebar, drawn once for the Mac window page and for the web app: the brand, the project picker, Salie, the places the
  // person goes, and Settings and Help at the foot. It knows nothing about where the people or places come from. Each side says what is
  // listed, which place is current and what choosing one does; the look cannot differ between them, because there is only this one.
  let { brand, picker, salie = undefined, salieButton = $bindable(), destinations, bottom }: {
    brand: { label: string; href?: string; go?: () => void };
    picker: Snippet;
    /** Left out where Salie is not available. */
    salie?: SidebarSalie;
    salieButton?: HTMLButtonElement;
    destinations: SidebarItem[];
    bottom: SidebarItem[];
  } = $props();

  function key(e: KeyboardEvent): void {
    // ⌘B (Ctrl+B elsewhere) folds the sidebar away and brings it back, as it does in an editor.
    if ((e.metaKey || e.ctrlKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'b') {
      e.preventDefault();
      sidebar.toggle();
    }
  }
</script>

{#snippet entry(d: SidebarItem)}
  <Icon name={d.icon} /><span>{d.label}</span>
  {#if d.badge}{#key d.badge.count}<span class="pill" data-tone={d.badge.tone} aria-hidden="true" title={d.badge.label}>{d.badge.count}</span>{/key}{/if}
{/snippet}
{#snippet item(d: SidebarItem)}
  {#if d.href && !d.disabled}
    <a class="nav" href={d.href} title={d.title ?? d.label} aria-current={d.current ? 'page' : undefined} aria-label={d.ariaLabel} data-page={d.key} data-testid={d.testid} aria-describedby={d.badge ? `rail-${d.key}-waiting` : undefined}>{@render entry(d)}</a>
  {:else}
    <button class="nav" type="button" title={d.title ?? d.label} aria-current={d.current ? 'page' : undefined} aria-label={d.ariaLabel} disabled={d.disabled} onclick={() => d.go?.()} data-page={d.key} data-testid={d.testid} aria-describedby={d.badge ? `rail-${d.key}-waiting` : undefined}>{@render entry(d)}</button>
  {/if}
  {#if d.badge}<span id="rail-{d.key}-waiting" class="sr-only">{d.badge.count} waiting: {d.badge.label}</span>{/if}
{/snippet}

<svelte:window onkeydown={key} />

<nav class="side" class:collapsed={sidebar.collapsed} aria-label="Werkbord" data-testid="sidebar" data-collapsed={sidebar.collapsed}>
  <div class="top">
    {#if brand.href}
      <a class="brand" href={brand.href} aria-label={brand.label} title="Go to Work" data-testid="brand"><Mark height={20} /><span>werkbord</span></a>
    {:else}
      <button class="brand" type="button" aria-label={brand.label} title="Go to Work" onclick={() => brand.go?.()} data-testid="brand"><Mark height={20} /><span>werkbord</span></button>
    {/if}
    <button class="fold" type="button" onclick={() => sidebar.toggle()} aria-expanded={!sidebar.collapsed} aria-controls="sidebar-body" aria-label={sidebar.collapsed ? 'Expand sidebar' : 'Collapse sidebar'} title={sidebar.collapsed ? 'Expand sidebar (⌘B)' : 'Collapse sidebar (⌘B)'} data-testid="sidebar-toggle">
      <Icon name={sidebar.collapsed ? 'sidebarOpen' : 'sidebarClose'} />
    </button>
  </div>
  <div class="body" id="sidebar-body">
    {@render picker()}
    <!-- Salie sits under the project it is working in, and is drawn as a filled button so it reads as someone to ask, not a page to visit. -->
    {#if salie}
      <button bind:this={salieButton} class="salie" class:waiting={!!salie.waiting} class:busy={!salie.waiting && salie.running} type="button" aria-haspopup="dialog" aria-expanded={salie.expanded} aria-label={salie.label} title={salie.title} disabled={salie.disabled} onclick={() => {
        if (salieButton) salie.press(salieButton.getBoundingClientRect(), salieButton);
      }} data-testid={salie.testid ?? 'sidebar-salie'}>
        <span class="glyph"><AssistantGlyph /></span><span class="name">Salie</span>
        {#if salie.waiting}<span class="count">{salie.waiting}</span>{:else if salie.running}<span class="working" role="status" aria-label="Salie is working"></span>{/if}
      </button>
    {/if}
    <div class="destinations">
      {#each destinations as d (d.key)}{@render item(d)}{/each}
    </div>
    <div class="bottom">
      {#each bottom as d (d.key)}{@render item(d)}{/each}
    </div>
  </div>
</nav>

<style>
  .side { width: 224px; flex: none; height: 100%; display: flex; flex-direction: column; gap: 20px; padding: 20px 14px 16px; background: var(--frame); overflow: hidden auto; transition: width 220ms var(--ease), padding 220ms var(--ease); }
  a.nav, a.brand { text-decoration: none; }
  .top { display: flex; align-items: center; justify-content: space-between; gap: 4px; min-height: 32px; }
  .body { display: flex; flex-direction: column; flex: 1; min-height: 0; gap: 12px; }
  .brand { display: flex; align-items: center; gap: 10px; padding: 4px 10px; border: 0; background: transparent; color: var(--text); cursor: pointer; font-family: var(--mono); font-weight: 600; font-size: 16px; white-space: nowrap; overflow: hidden; }
  .fold { flex: none; display: grid; place-items: center; width: 32px; height: 32px; border: 0; border-radius: var(--radius-sm); background: transparent; color: var(--text-2); cursor: pointer; transition: background 120ms, color 120ms; }
  .fold:hover { background: color-mix(in srgb, var(--text) 6%, var(--frame)); color: var(--text); }
  .destinations { display: flex; flex-direction: column; gap: 4px; margin-top: 8px; }
  .nav { position: relative; display: flex; align-items: center; gap: 12px; min-height: 44px; width: 100%; padding: 8px 12px; border: 0; border-radius: var(--radius-sm); background: transparent; color: var(--text-2); font: inherit; font-size: 14px; text-align: left; cursor: pointer; white-space: nowrap; transition: background 120ms, color 120ms, transform 160ms var(--ease); }
  .nav :global(svg) { transition: transform 200ms var(--ease); }
  .nav:hover { background: color-mix(in srgb, var(--text) 6%, var(--frame)); color: var(--text); }
  .nav:hover :global(svg) { transform: scale(1.1); }
  .nav:active { transform: scale(.98); }
  .nav[aria-current='page'] { background: var(--surface); color: var(--text); font-weight: 600; box-shadow: 0 0 0 1px var(--tray-edge); }
  /* The place you are is marked by a bar that grows in from the edge, not just a change of fill. */
  .nav::before { content: ''; position: absolute; left: 0; top: 50%; width: 3px; height: 0; border-radius: 0 3px 3px 0; background: var(--accent); transform: translateY(-50%); transition: height 200ms var(--ease); }
  .nav[aria-current='page']::before { height: 20px; }
  .nav:disabled { cursor: default; color: var(--text-2); }
  button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }

  .salie { position: relative; isolation: isolate; overflow: hidden; display: flex; align-items: center; gap: 10px; min-height: 44px; width: 100%; padding: 8px 12px; border: 1px solid var(--pri-bd, var(--accent)); border-radius: var(--radius-sm); background: var(--pri-bg); color: var(--pri-text, var(--accent-text)); box-shadow: var(--pri-sh); font: inherit; font-size: 14px; font-weight: 600; text-align: left; cursor: pointer; white-space: nowrap; transition: transform 160ms var(--ease), filter 160ms, box-shadow 200ms; }
  /* A slow sheen crosses it when you point at it or Salie is working: a hint that she is alive, never a loop on an idle page. */
  .salie::after { content: ''; position: absolute; inset: 0; z-index: -1; background: linear-gradient(105deg, transparent 30%, rgb(255 255 255 / .28) 50%, transparent 70%); transform: translateX(-120%); }
  .salie:hover:not(:disabled) { filter: brightness(1.07); transform: translateY(-1px); }
  .salie:hover:not(:disabled)::after, .salie.busy::after { animation: sheen 1.6s var(--ease) infinite; }
  .salie:hover:not(:disabled)::after { animation-iteration-count: 1; }
  .salie:active:not(:disabled) { transform: translateY(0) scale(.98); }
  .salie:disabled { opacity: .5; cursor: default; }
  .salie .glyph { display: grid; place-items: center; flex: none; }
  .salie:hover:not(:disabled) .glyph :global(svg) { animation: twinkle 700ms var(--ease); }
  .salie.waiting { animation: ask 2.4s ease-in-out infinite; }
  .salie:focus-visible { outline-color: var(--text); }
  .count { margin-left: auto; min-width: 22px; padding: 2px 6px; border-radius: 999px; background: var(--amber); color: #1d1b18; font-size: 12px; font-weight: 600; text-align: center; font-variant-numeric: tabular-nums; }
  /* What waits in a place: amber for an agent asking, red for a failure, green for work to review. It pops once when the number changes and
     only an asking pill glows, so one thing at a time pulls the eye. The number is always there, so colour is never the only signal. */
  .pill { margin-left: auto; min-width: 22px; padding: 2px 6px; border-radius: 999px; font-size: 12px; font-weight: 600; text-align: center; font-variant-numeric: tabular-nums; color: #1d1b18; background: var(--amber); animation: pop 320ms var(--ease); }
  .pill[data-tone='bad'] { background: var(--danger); color: #fff; }
  .pill[data-tone='review'] { background: color-mix(in srgb, var(--ok) 22%, var(--surface)); color: var(--ok-text); }
  .pill[data-tone='ask'] { animation: pop 320ms var(--ease), ask 2.4s 320ms ease-in-out infinite; }
  @keyframes pop { 0% { transform: scale(.6); } 60% { transform: scale(1.18); } 100% { transform: scale(1); } }
  .working { margin-left: auto; width: 8px; height: 8px; border-radius: 50%; background: currentColor; animation: breathe 1.6s ease-in-out infinite; }
  .bottom { margin-top: auto; padding-top: 16px; border-top: 1px solid var(--border); display: flex; gap: 6px; }
  .bottom .nav { flex: 1 1 0; min-width: 0; padding-inline: 10px; gap: 8px; }
  .sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }

  @keyframes sheen { to { transform: translateX(120%); } }
  @keyframes twinkle { 0% { transform: rotate(0) scale(1); } 40% { transform: rotate(18deg) scale(1.25); } 100% { transform: rotate(0) scale(1); } }
  @keyframes breathe { 50% { opacity: .35; transform: scale(.75); } }
  /* A request that is waiting for you glows softly until you open it. */
  @keyframes ask { 0%, 100% { box-shadow: var(--pri-sh), 0 0 0 0 color-mix(in srgb, var(--amber) 0%, transparent); } 50% { box-shadow: var(--pri-sh), 0 0 0 4px color-mix(in srgb, var(--amber) 45%, transparent); } }

  /* The folded sidebar: a rail of icons. Names stay in the accessibility tree and as tooltips. */
  .side.collapsed { width: 64px; padding-inline: 8px; }
  .side.collapsed .top { flex-direction: column; gap: 8px; }
  .side.collapsed .brand { justify-content: center; padding-inline: 0; }
  .side.collapsed .brand > span, .side.collapsed .nav > span:not(.count):not(.working):not(.pill), .side.collapsed .salie .name { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
  .side.collapsed .count, .side.collapsed .working, .side.collapsed .pill { position: absolute; top: 4px; right: 4px; margin: 0; min-width: 0; }
  .side.collapsed .count { padding: 0 5px; font-size: 11px; line-height: 16px; }
  .side.collapsed .bottom { flex-direction: column; }
  .side.collapsed .nav, .side.collapsed .salie { justify-content: center; padding-inline: 8px; gap: 0; }
  @media (max-width: 640px) {
    /* A window this narrow is always folded. */
    .side { width: 64px; padding: 16px 8px; gap: 16px; }
    .fold { display: none; }
    .brand { justify-content: center; padding-inline: 0; }
    .brand > span, .nav > span:not(.count):not(.working), .salie .name { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
    .count, .working { position: absolute; top: 4px; right: 4px; margin: 0; min-width: 0; }
    .bottom { flex-direction: column; }
    .nav, .salie { justify-content: center; padding-inline: 8px; gap: 0; }
    }
  @media (prefers-reduced-motion: reduce) {
    .side, .nav, .nav :global(svg), .nav::before, .salie, .fold { transition: none; }
    .salie::after, .salie .glyph :global(svg), .salie.waiting, .working { animation: none !important; }
    .working { opacity: 1; }
    .salie.waiting { box-shadow: var(--pri-sh), 0 0 0 3px color-mix(in srgb, var(--amber) 45%, transparent); }
  }
</style>
