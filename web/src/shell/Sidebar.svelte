<script lang="ts">
  import Icon, { type IconName } from '../lib/Icon.svelte';
  import Mark from '../lib/Mark.svelte';
  import AssistantGlyph from '../lib/AssistantGlyph.svelte';
  import { model, type Page } from './model.svelte';
  import { contextKey, parseContext } from './project-context';
  import { sidebar } from './nav-fold.svelte';
  import { railBadges } from './rail';

  const destinations: { label: string; page: Page; icon: IconName }[] = [
    { label: 'Work', page: 'inbox', icon: 'control' },
    { label: 'Calendar', page: 'calendar', icon: 'calendar' },
    { label: 'Agents', page: 'agents', icon: 'activity' },
    { label: 'Analytics', page: 'analytics', icon: 'analytics' },
  ];
  // Help is Individual's guides and shortcuts, which describe every part of Werkbord, so it is there wherever the person is.
  // What is waiting in each place, so a glance at the rail says whether something needs the person and what sort.
  const badges = $derived(railBadges(model.overview));
  const helpUnavailable = $derived(model.view?.items.find((i) => i.id === 'personal')?.state === 'unavailable');
  let salieButton: HTMLButtonElement | undefined = $state();
  const selected = $derived(contextKey(model.selectedProject));
  const missing = $derived(!!selected && !model.projects.some(p => contextKey(p) === selected));
  const salieLabel = $derived(model.assistant.waiting ? `Salie · ${model.assistant.waiting} ${model.assistant.waiting === 1 ? 'request is' : 'requests are'} waiting for you` : 'Salie');
  $effect(() => {
    model.assistantSidebarButton = salieButton;
    return () => { model.assistantSidebarButton = undefined; };
  });
  function key(e: KeyboardEvent): void {
    // ⌘B (Ctrl+B elsewhere) folds the sidebar away and brings it back, as it does in an editor.
    if ((e.metaKey || e.ctrlKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'b') {
      e.preventDefault();
      sidebar.toggle();
    }
  }
</script>

<svelte:window onkeydown={key} />

<nav class="side" class:collapsed={sidebar.collapsed} aria-label="Werkbord" data-testid="sidebar" data-collapsed={sidebar.collapsed}>
  <div class="top">
    <button class="brand" type="button" aria-label="werkbord. Go to Work" title="Go to Work" onclick={() => model.go('inbox')} data-testid="brand">
      <Mark height={20} /><span>werkbord</span>
    </button>
    <button class="fold" type="button" onclick={() => sidebar.toggle()} aria-expanded={!sidebar.collapsed} aria-controls="sidebar-body" aria-label={sidebar.collapsed ? 'Expand sidebar' : 'Collapse sidebar'} title={sidebar.collapsed ? 'Expand sidebar (⌘B)' : 'Collapse sidebar (⌘B)'} data-testid="sidebar-toggle">
      <Icon name={sidebar.collapsed ? 'sidebarOpen' : 'sidebarClose'} />
    </button>
  </div>
  <div class="body" id="sidebar-body">
    <label class="project-picker">
      <span class="sr-only">Project context</span><span class="picker-icon"><Icon name="folder" /></span>
      <select value={selected} onchange={e => model.selectProject(parseContext(e.currentTarget.value))} title={model.contextLabel} aria-label="Project context" data-testid="project-context">
        <option value="">All Projects</option>
        {#each ['personal', 'team'] as kind (kind)}
          <optgroup label={kind === 'personal' ? 'Personal projects' : 'Team projects'}>
            {#each model.projects.filter(p => p.kind === kind) as p (contextKey(p))}
              <option value={contextKey(p)}>{p.name}{kind === 'team' ? ` · ${p.owner}` : ''}{model.view?.items.find(i => i.id === p.workspace)?.state !== 'ready' ? ' · Offline' : ''}</option>
            {/each}
          </optgroup>
        {/each}
        {#if missing}<option value={selected}>Project unavailable</option>{/if}
      </select>
    </label>
    <!-- Salie sits under the project it is working in, and is drawn as a filled button so it reads as someone to ask, not a page to visit. -->
    <button bind:this={salieButton} class="salie" class:waiting={!!model.assistant.waiting} class:busy={!model.assistant.waiting && !!model.assistant.running} type="button" aria-haspopup="dialog" aria-expanded={model.assistantShown} aria-label={salieLabel} title={model.remotePersonal ? 'Salie requires the local controller' : salieLabel} disabled={model.remotePersonal || model.view?.items.find(i => i.id === 'personal')?.state === 'unavailable'} onclick={() => {
      const rect = salieButton?.getBoundingClientRect();
      if (rect) void model.toggleAssistant(rect);
    }} data-testid="sidebar-salie">
      <span class="glyph"><AssistantGlyph /></span><span class="name">Salie</span>
      {#if model.assistant.waiting}<span class="count">{model.assistant.waiting}</span>{:else if model.assistant.running}<span class="working" role="status" aria-label="Salie is working"></span>{/if}
    </button>
    <div class="destinations">
      {#each destinations as d (d.page)}
        {@const b = d.page === 'inbox' ? badges.inbox : d.page === 'agents' ? badges.agents : undefined}
        <button class="nav" type="button" title={d.label} aria-current={model.page === d.page || d.page === 'inbox' && model.page === 'workspace' ? 'page' : undefined} onclick={() => model.go(d.page)} data-page={d.page}>
          <Icon name={d.icon} /><span>{d.label}</span>
          {#if b}{#key b.count}<span class="pill" data-tone={b.tone} role="status" aria-label={b.label} title={b.label}>{b.count}</span>{/key}{/if}
        </button>
      {/each}
    </div>
    <div class="bottom">
      <button class="nav" type="button" title="Settings" aria-current={model.page === 'settings' ? 'page' : undefined} onclick={() => model.openSettings()} data-page="settings"><Icon name="settings" /><span>Settings</span></button>
      <button class="nav" type="button" aria-current={model.page === 'help' ? 'page' : undefined} disabled={helpUnavailable} onclick={() => model.openHelp()} aria-label="Help and keyboard shortcuts" title="Help and keyboard shortcuts" data-testid="help-button" data-page="help"><Icon name="help" /><span>Help</span></button>
    </div>
  </div>
</nav>

<style>
  .side { width: 224px; flex: none; height: 100%; display: flex; flex-direction: column; gap: 20px; padding: 20px 14px 16px; background: var(--bg); border-right: 1px solid var(--border); overflow: hidden auto; transition: width 220ms var(--ease), padding 220ms var(--ease); }
  .top { display: flex; align-items: center; justify-content: space-between; gap: 4px; min-height: 32px; }
  .body { display: flex; flex-direction: column; flex: 1; min-height: 0; gap: 12px; }
  .brand { display: flex; align-items: center; gap: 10px; padding: 4px 10px; border: 0; background: transparent; color: var(--text); cursor: pointer; font-family: var(--mono); font-weight: 600; font-size: 16px; white-space: nowrap; overflow: hidden; }
  .fold { flex: none; display: grid; place-items: center; width: 32px; height: 32px; border: 0; border-radius: var(--radius-sm); background: transparent; color: var(--text-2); cursor: pointer; transition: background 120ms, color 120ms; }
  .fold:hover { background: var(--surface-2); color: var(--text); }
  .project-picker { display: block; min-width: 0; position: relative; }
  .picker-icon { display: none; }
  select { width: 100%; min-height: 40px; padding: 8px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; font-size: 13px; text-overflow: ellipsis; transition: border-color 120ms; }
  select:hover { border-color: color-mix(in srgb, var(--accent) 40%, var(--border)); }
  .destinations { display: flex; flex-direction: column; gap: 4px; margin-top: 8px; }
  .nav { position: relative; display: flex; align-items: center; gap: 12px; min-height: 44px; width: 100%; padding: 8px 12px; border: 0; border-radius: var(--radius-sm); background: transparent; color: var(--text-2); font: inherit; font-size: 14px; text-align: left; cursor: pointer; white-space: nowrap; transition: background 120ms, color 120ms, transform 160ms var(--ease); }
  .nav :global(svg) { transition: transform 200ms var(--ease); }
  .nav:hover { background: var(--surface-2); color: var(--text); }
  .nav:hover :global(svg) { transform: scale(1.1); }
  .nav:active { transform: scale(.98); }
  .nav[aria-current='page'] { background: var(--surface); color: var(--text); font-weight: 600; }
  /* The place you are is marked by a bar that grows in from the edge, not just a change of fill. */
  .nav::before { content: ''; position: absolute; left: 0; top: 50%; width: 3px; height: 0; border-radius: 0 3px 3px 0; background: var(--accent); transform: translateY(-50%); transition: height 200ms var(--ease); }
  .nav[aria-current='page']::before { height: 20px; }
  .nav:disabled { cursor: default; color: var(--text-2); }
  button:focus-visible, select:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }

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
  .side.collapsed select { font-size: 16px; color: transparent; appearance: none; cursor: pointer; }
  .side.collapsed option, .side.collapsed optgroup { color: var(--text); }
  .side.collapsed .picker-icon { display: flex; position: absolute; inset: 0; align-items: center; justify-content: center; pointer-events: none; color: var(--text); }
  @media (max-width: 640px) {
    /* A window this narrow is always folded. */
    .side { width: 64px; padding: 16px 8px; gap: 16px; }
    .fold { display: none; }
    .brand { justify-content: center; padding-inline: 0; }
    .brand > span, .nav > span:not(.count):not(.working), .salie .name { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
    .count, .working { position: absolute; top: 4px; right: 4px; margin: 0; min-width: 0; }
    .bottom { flex-direction: column; }
    .nav, .salie { justify-content: center; padding-inline: 8px; gap: 0; }
    select { font-size: 16px; color: transparent; appearance: none; cursor: pointer; }
    option, optgroup { color: var(--text); }
    .picker-icon { display: flex; position: absolute; inset: 0; align-items: center; justify-content: center; pointer-events: none; color: var(--text); }
  }
  @media (prefers-reduced-motion: reduce) {
    .side, .nav, .nav :global(svg), .nav::before, .salie, .fold { transition: none; }
    .salie::after, .salie .glyph :global(svg), .salie.waiting, .working { animation: none !important; }
    .working { opacity: 1; }
    .salie.waiting { box-shadow: var(--pri-sh), 0 0 0 3px color-mix(in srgb, var(--amber) 45%, transparent); }
  }
</style>
