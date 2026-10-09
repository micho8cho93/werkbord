<script lang="ts">
  import { api } from './api';
  import FolderPicker from './FolderPicker.svelte';
  import Icon from './Icon.svelte';
  import { globalHref } from './router.svelte';
  import { app } from './state.svelte';
  import type { WaitingItem } from './types';

  // A Team ticket the person holds whose repository is not a project here yet. Nothing is done for them: they choose a
  // folder that is a clone of it, or clone it with their GitHub sign-in. Once it is here, the ticket becomes a task on its
  // board by itself (the Team service looks again within moments).
  let { item }: { item: WaitingItem } = $props();

  let path = $state('');
  let busy = $state(false);
  let error = $state('');
  let done = $state('');

  async function link(chosen: string): Promise<void> {
    busy = true;
    error = '';
    try {
      const p = await api.linkWaiting(chosen, item.repository);
      done = `${p.name} is added. The ticket appears on its board in a moment.`;
      app.refreshOverview();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      path = '';
    } finally {
      busy = false;
    }
  }

  async function clone(): Promise<void> {
    busy = true;
    error = '';
    try {
      const r = await api.githubAddRepo(item.github!);
      done = `${r.project.name} is ${r.cloned ? 'cloned and ' : ''}added. The ticket appears on its board in a moment.`;
      app.refreshOverview();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  $effect(() => {
    if (path && !busy && !done) void link(path);
  });
</script>

<li class="waiting" data-testid="waiting-repository">
  <div class="head">
    <span class="dot" data-tone="warn" aria-hidden="true"></span>
    <div class="what">
      <p class="title">{item.title}</p>
      <p class="mm">From {item.from || 'Team'} · needs <code>{item.repository}</code></p>
    </div>
  </div>
  {#if done}
    <p class="ok" role="status">{done}</p>
  {:else}
    <p class="muted small">Choose the folder where you cloned this repository{item.github ? ', or clone it now' : ''}. It becomes a task ready for you to start; nothing runs until you do.</p>
    <div class="actions">
      <FolderPicker bind:value={path} disabled={busy} />
      {#if item.github}
        <button class="btn" type="button" disabled={busy} onclick={clone}><Icon name="git" />Clone from GitHub</button>
      {/if}
    </div>
    {#if error}
      <p class="err" role="alert">{error}{#if item.github && /github/i.test(error)} <a href="{globalHref('settings')}?github">GitHub settings</a>{/if}</p>
    {/if}
  {/if}
</li>

<style>
  .waiting {
    list-style: none;
    padding: 14px 16px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .head {
    display: flex;
    gap: 10px;
    align-items: flex-start;
  }
  .dot {
    margin-top: 6px;
    flex: none;
  }
  .what {
    min-width: 0;
  }
  .title {
    margin: 0;
    font-weight: 500;
  }
  .mm {
    margin: 2px 0 0;
    font-size: 12px;
    color: var(--text-2);
    overflow-wrap: anywhere;
  }
  .muted {
    margin: 0;
    color: var(--text-2);
  }
  .small {
    font-size: 12px;
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .ok {
    margin: 0;
    color: var(--ok-text, var(--ok));
  }
  .err {
    margin: 0;
    color: var(--danger-text);
  }
</style>
