<script lang="ts">
  import { onMount } from 'svelte';
  import Sidebar from './Sidebar.svelte';
  import Switcher from './Switcher.svelte';
  import FrameHost from './FrameHost.svelte';
  import WorkspaceProblem from './WorkspaceProblem.svelte';
  import MyWork from './MyWork.svelte';
  import Calendar from './Calendar.svelte';
  import Inbox from './Inbox.svelte';
  import Workspaces from './Workspaces.svelte';
  import AddTeam from './AddTeam.svelte';
  import { model, type Page } from './model.svelte';
  import { onApp } from './native';

  // The window: one sidebar (switching workspace, what spans every workspace, and the navigation of the workspace on show)
  // beside the workspace's own pages, which carry no navigation of their own here.
  const pages: Page[] = ['workspace', 'mywork', 'calendar', 'inbox', 'workspaces'];
  // The person's own Werkbord cannot be shown (too old, or not running): say so instead of a blank frame.
  const problem = $derived(model.current === 'personal' && model.view?.items.find((i) => i.id === 'personal')?.state === 'unavailable');

  function go(where: unknown): void {
    if (where === 'personal') void model.open('personal');
    else if (where === 'switch') model.toggleSwitcher();
    else if (where === 'invitation' || where === 'add-team') model.addOpen = true;
    else if (pages.includes(where as Page)) model.go(where as Page);
  }
  onMount(() => {
    void model.start();
    const unsubscribe = onApp('shell:go', go);
    return () => { unsubscribe(); model.stop(); };
  });
  function key(e: KeyboardEvent): void {
    if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      model.toggleSwitcher();
    }
  }
</script>

<svelte:window onkeydown={key} />
<div class="shell">
  <Sidebar />
  <div class="content">
    {#if model.error}
      <div class="message error" role="alert">{model.error}<button class="btn" type="button" onclick={() => (model.error = '')}>Dismiss</button></div>
    {/if}
    {#if model.notice}
      <div class="message" role="status">{model.notice}<button class="btn" type="button" onclick={() => (model.notice = '')}>Dismiss</button></div>
    {/if}
    {#if !model.loaded}<p class="loading" role="status">Opening your workspaces…</p>{/if}
    <main>
      <FrameHost shown={model.page === 'workspace' && !problem} />
      {#if model.page === 'workspace' && problem}
        <div class="page-area"><WorkspaceProblem /></div>
      {:else if model.page !== 'workspace'}
        <div class="page-area">
          {#if model.page === 'mywork'}<MyWork overview={model.overview} />
          {:else if model.page === 'calendar'}<Calendar overview={model.overview} />
          {:else if model.page === 'inbox'}<Inbox overview={model.overview} />
          {:else if model.page === 'workspaces'}<Workspaces overview={model.overview} />{/if}
        </div>
      {/if}
    </main>
  </div>
  {#if model.switcher}<Switcher onadd={() => (model.addOpen = true)} />{/if}
  {#if model.addOpen}<AddTeam />{/if}
</div>

<style>
  .shell {
    height: 100dvh;
    display: flex;
    overflow: hidden;
    background: var(--bg);
  }
  .content {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  main {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .page-area {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }
  .loading {
    padding: 20px;
    color: var(--text-2);
  }
  .message {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    padding: 8px 16px;
    border-bottom: 1px solid var(--border);
    background: var(--surface-2);
  }
  .error {
    color: var(--danger-text);
  }
</style>
