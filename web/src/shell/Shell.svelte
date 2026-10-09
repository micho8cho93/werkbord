<script lang="ts">
  import { onMount } from 'svelte';
  import Mark from '../lib/Mark.svelte';
  import Icon from '../lib/Icon.svelte';
  import { theme } from '../lib/theme.svelte';
  import Switcher from './Switcher.svelte';
  import FrameHost from './FrameHost.svelte';
  import MyWork from './MyWork.svelte';
  import Calendar from './Calendar.svelte';
  import Inbox from './Inbox.svelte';
  import Workspaces from './Workspaces.svelte';
  import AddTeam from './AddTeam.svelte';
  import { model, type Page } from './model.svelte';
  import { onApp } from './native';
  import { counts } from './aggregate';

  const needs = $derived(model.overview ? counts(model.overview).needsYou : 0);
  const tabs: { id: Page; title: string }[] = [
    { id: 'workspace', title: 'Workspace' }, { id: 'mywork', title: 'My Work' },
    { id: 'calendar', title: 'Calendar' }, { id: 'inbox', title: 'Needs you' },
    { id: 'workspaces', title: 'Workspaces and devices' },
  ];
  function go(where: unknown): void {
    if (where === 'personal') void model.open('personal');
    else if (where === 'switch') model.switcherOpen = true;
    else if (where === 'invitation' || where === 'add-team') model.addOpen = true;
    else if (tabs.some(t => t.id === where)) model.go(where as Page);
  }
  onMount(() => {
    void model.start();
    const unsubscribe = onApp('shell:go', go);
    return () => { unsubscribe(); model.stop(); };
  });
  function key(e: KeyboardEvent): void {
    if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key.toLowerCase() === 'k') {
      e.preventDefault(); model.switcherOpen = !model.switcherOpen;
    }
  }
</script>

<svelte:window onkeydown={key} />
<div class="shell">
  <header class="toolbar">
    <button type="button" class="brand" onclick={() => model.open('personal')} aria-label="Personal workspace"><Mark height={20} /></button>
    <Switcher onadd={() => (model.addOpen = true)} />
    <nav aria-label="Across workspaces">
      {#each tabs as t (t.id)}
        <button type="button" class:chosen={model.page === t.id} aria-current={model.page === t.id ? 'page' : undefined} onclick={() => model.go(t.id)} data-page={t.id}>
          {t.title}{#if t.id === 'inbox' && needs}<span class="num pend">{needs}</span>{/if}
        </button>
      {/each}
    </nav>
    <button class="btn theme" type="button" title={theme.dark ? 'Use light theme' : 'Use dark theme'} aria-label={theme.dark ? 'Use light theme' : 'Use dark theme'} onclick={() => theme.toggle()}><Icon name={theme.dark ? 'sun' : 'moon'} size={16} /></button>
  </header>
  {#if model.error}
    <div class="message error" role="alert">{model.error}<button class="btn" type="button" onclick={() => (model.error = '')}>Dismiss</button></div>
  {/if}
  {#if model.notice}
    <div class="message" role="status">{model.notice}<button class="btn" type="button" onclick={() => (model.notice = '')}>Dismiss</button></div>
  {/if}
  {#if !model.loaded}<p class="loading" role="status">Opening your workspaces…</p>{/if}
  <main>
    <FrameHost shown={model.page === 'workspace'} />
    {#if model.page === 'mywork'}<MyWork overview={model.overview} />
    {:else if model.page === 'calendar'}<Calendar overview={model.overview} />
    {:else if model.page === 'inbox'}<Inbox overview={model.overview} />
    {:else if model.page === 'workspaces'}<Workspaces overview={model.overview} />{/if}
  </main>
  {#if model.addOpen}<AddTeam />{/if}
</div>

<style>
  .shell { height: 100dvh; display: flex; flex-direction: column; overflow: hidden; }
  .toolbar { display: flex; align-items: center; gap: 12px; padding: 8px 14px; background: var(--surface); border-bottom: 1px solid var(--border); flex-wrap: wrap; }
  .brand { display: flex; padding: 4px; background: none; border: 0; cursor: pointer; }
  nav { display: flex; gap: 3px; flex: 1; flex-wrap: wrap; }
  nav button { display: inline-flex; align-items: center; gap: 6px; padding: 6px 9px; background: none; color: var(--text-2); border: 0; border-radius: var(--radius-sm); font: inherit; font-size: 12px; cursor: pointer; }
  nav button:hover, nav button.chosen { background: var(--surface-2); color: var(--text); }
  button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .theme { margin-left: auto; }
  main { flex: 1; min-height: 0; display: flex; flex-direction: column; overflow: hidden; }
  .loading { padding: 20px; color: var(--text-2); }
  .message { display: flex; justify-content: space-between; align-items: center; gap: 12px; padding: 8px 16px; border-bottom: 1px solid var(--border); background: var(--surface-2); }
  .error { color: var(--danger-text); }
  @media (max-width: 900px) { nav { order: 3; flex-basis: 100%; } .toolbar { gap: 8px; } }
</style>
