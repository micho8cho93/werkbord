<script lang="ts">
  import Icon, { type IconName } from '../lib/Icon.svelte';
  import Mark from '../lib/Mark.svelte';
  import AssistantGlyph from '../lib/AssistantGlyph.svelte';
  import { model, type Page } from './model.svelte';
  import { contextKey, parseContext } from './project-context';

  const destinations: { label: string; page: Page; icon: IconName }[] = [
    { label: 'Work', page: 'inbox', icon: 'control' },
    { label: 'Calendar', page: 'calendar', icon: 'calendar' },
    { label: 'Agents', page: 'agents', icon: 'activity' },
    { label: 'Analytics', page: 'analytics', icon: 'analytics' },
  ];
  let salieButton: HTMLButtonElement | undefined = $state();
  const selected = $derived(contextKey(model.selectedProject));
  const missing = $derived(!!selected && !model.projects.some(p => contextKey(p) === selected));
  $effect(() => {
    model.assistantSidebarButton = salieButton;
    return () => { model.assistantSidebarButton = undefined; };
  });
</script>

<nav class="side" aria-label="Werkbord" data-testid="sidebar">
  <button class="brand" type="button" aria-label="werkbord. Go to Work" onclick={() => model.go('inbox')} data-testid="brand">
    <Mark height={20} /><span>werkbord</span>
  </button>
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
  <div class="destinations">
    {#each destinations as d (d.page)}
      <button class="nav" type="button" aria-current={model.page === d.page || d.page === 'inbox' && model.page === 'workspace' ? 'page' : undefined} onclick={() => model.go(d.page)} data-page={d.page}>
        <Icon name={d.icon} /><span>{d.label}</span>
      </button>
    {/each}
    <button bind:this={salieButton} class="nav" type="button" aria-haspopup="dialog" aria-expanded={model.assistantShown} aria-label={model.assistant.waiting ? `Salie · ${model.assistant.waiting} ${model.assistant.waiting === 1 ? 'request is' : 'requests are'} waiting for you` : 'Salie'} title={model.remotePersonal ? 'Salie requires the local controller' : 'Salie'} disabled={model.remotePersonal || model.view?.items.find(i => i.id === 'personal')?.state === 'unavailable'} onclick={() => {
      const rect = salieButton?.getBoundingClientRect();
      if (rect) void model.toggleAssistant(rect);
    }} data-testid="sidebar-salie">
      <AssistantGlyph /><span>Salie</span>
      {#if model.assistant.waiting}<span class="count">{model.assistant.waiting}</span>{:else if model.assistant.running}<span class="working" role="status" aria-label="Salie is working"></span>{/if}
    </button>
  </div>
  <div class="bottom">
    <button class="nav" type="button" aria-current={model.page === 'settings' ? 'page' : undefined} onclick={() => model.openSettings()} data-page="settings"><Icon name="settings" /><span>Settings</span></button>
  </div>
</nav>

<style>
  .side { width: 224px; flex: none; height: 100%; display: flex; flex-direction: column; gap: 28px; padding: 24px 14px 16px; background: var(--bg); border-right: 1px solid var(--border); overflow-y: auto; }
  .brand { display: flex; align-items: center; gap: 10px; padding: 4px 10px; border: 0; background: transparent; color: var(--text); cursor: pointer; font-family: var(--mono); font-weight: 600; font-size: 16px; }
  .project-picker { display: block; min-width: 0; position: relative; }
  .picker-icon { display: none; }
  select { width: 100%; min-height: 40px; padding: 8px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; font-size: 13px; text-overflow: ellipsis; }
  .destinations { display: flex; flex-direction: column; gap: 6px; }
  .nav { display: flex; align-items: center; gap: 12px; min-height: 44px; width: 100%; padding: 8px 12px; border: 0; border-radius: var(--radius-sm); background: transparent; color: var(--text-2); font: inherit; font-size: 14px; text-align: left; cursor: pointer; }
  .nav:hover { background: var(--surface-2); color: var(--text); }
  .nav[aria-current='page'] { background: var(--surface); color: var(--text); }
  .nav[aria-current='page'] { font-weight: 600; }
  .nav:disabled { cursor: default; color: var(--text-2); }
  button:focus-visible, select:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .bottom { margin-top: auto; padding-top: 16px; border-top: 1px solid var(--border); }
  .count { margin-left: auto; min-width: 22px; padding: 2px 6px; border-radius: 4px; background: var(--surface-2); color: var(--warn-text); font-size: 12px; text-align: center; font-variant-numeric: tabular-nums; }
  .working { margin-left: auto; width: 7px; height: 7px; border-radius: 50%; background: var(--accent); }
  .sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
  @media (max-width: 640px) {
    .side { width: 64px; padding: 16px 8px; gap: 20px; }
    .brand { justify-content: center; padding-inline: 0; }
    .brand > span, .nav > span:not(.count):not(.working) { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
    .count, .working { margin-left: 0; }
    .nav { flex-wrap: wrap; justify-content: center; padding-inline: 8px; gap: 0; }
    select { font-size: 16px; color: transparent; appearance: none; cursor: pointer; }
    option, optgroup { color: var(--text); }
    .picker-icon { display: flex; position: absolute; inset: 0; align-items: center; justify-content: center; pointer-events: none; color: var(--text); }
  }
</style>
