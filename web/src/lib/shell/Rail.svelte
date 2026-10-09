<script lang="ts">
  import Icon from '../Icon.svelte';
  import ProductSwitch from './ProductSwitch.svelte';
  import { attentionCount } from '../projects';
  import { globalHref, hrefOf, router, switchedTo } from '../router.svelte';
  import { app } from '../state.svelte';
  import { theme } from '../theme.svelte';

  // The rail is the map of everything: what needs you, every project, every machine. One click
  // reaches any of them; nothing here hides behind a menu.

  const current = $derived(router.projectId);
  const activity = (id: string) => app.overview?.projects.find((p) => p.projectId === id);

  const statusLabel = $derived(
    { connecting: 'Connecting', live: 'Live', offline: 'Controller offline', unauthorized: 'Token required' }[app.connection],
  );

  /** Switching keeps the section you are in, where the other project has one. */
  const hrefFor = (id: string): string => hrefOf(switchedTo(router.location, id));
</script>

<aside class="rail" aria-label="Werkbord">
  <ProductSwitch />

  <nav class="group" aria-label="Everywhere">
    <a class="nav" href={globalHref('control')} aria-current={router.view === 'control' ? 'page' : undefined}>
      <Icon name="control" />
      Control Center
      {#if app.needsYou > 0}<span class="num" aria-label="{app.needsYou} waiting for you">{app.needsYou}</span>{/if}
    </a>
  </nav>

  <nav class="group" aria-label="Projects">
    <div class="sec">
      <a href={globalHref('projects')} class="sec-link" aria-current={router.view === 'projects' ? 'page' : undefined}>
        <Icon name="folder" />Projects
      </a>
      <a class="add" href={globalHref('projects')} aria-label="Add a project" title="Add a project"><Icon name="plus" size={14} /></a>
    </div>
    {#each app.projects as p (p.id)}
      {@const a = activity(p.id)}
      {@const n = attentionCount(a)}
      <a class="nav project" href={hrefFor(p.id)} aria-current={p.id === current ? 'page' : undefined} title={p.repoPath}>
        <span class="sq" aria-hidden="true"></span>
        <span class="name">{p.name}</span>
        {#if n > 0}
          <span class="num pend" aria-label="{n} need you">{n}</span>
        {:else if a?.running}
          <span class="n" aria-label="{a.running} running">{a.running}</span>
        {/if}
      </a>
    {:else}
      <a class="nav" href={globalHref('projects')}><Icon name="plus" />Add a repository</a>
    {/each}
  </nav>

  {#if app.runners.length}
    <nav class="group" aria-label="Runners">
      <div class="sec">
        <a href="{globalHref('settings')}?runners" class="sec-link"><Icon name="runner" />Runners</a>
      </div>
      {#each app.runners as r (r.id)}
        <a class="nav runner" href="{globalHref('settings')}?runners" title={r.hostname}>
          <span class="dot" data-tone={r.disabled || !r.online ? 'neutral' : 'ok'} class:off={r.disabled || !r.online} aria-hidden="true"></span>
          <span class="name">{r.kind === 'local' ? 'this computer' : r.name}</span>
          <span class="n">{r.disabled ? 'off' : r.online ? `${r.currentRuns}/${r.capacity}` : 'offline'}</span>
        </a>
      {/each}
    </nav>
  {/if}

  <div class="foot">
    <a class="nav" href={globalHref('settings')} aria-current={router.view === 'settings' ? 'page' : undefined}>
      <Icon name="settings" />Settings
    </a>
    <div class="row">
      <span class="status" data-state={app.connection} title={statusLabel}>
        <span class="dot" aria-hidden="true"></span>{statusLabel}
      </span>
      <button class="btn quiet small icon" type="button" onclick={() => theme.toggle()} aria-label={theme.dark ? 'Switch to light' : 'Switch to dark'} title={theme.dark ? 'Light' : 'Dark'}>
        <Icon name={theme.dark ? 'sun' : 'moon'} />
      </button>
    </div>
  </div>
</aside>

<style>
  .rail {
    display: flex;
    flex-direction: column;
    gap: 2px;
    height: 100%;
    padding: 18px 12px 12px;
    border-right: 1px solid var(--border);
    overflow-y: auto;
    overscroll-behavior: contain;
  }

  .group {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .nav {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 34px;
    padding: 0 10px;
    border-radius: var(--radius-sm);
    color: var(--text-2);
    text-decoration: none;
    font-weight: 500;
    font-size: 13px;
    transition: color 0.12s, background 0.12s;
  }

  .nav:hover {
    color: var(--text);
  }

  .nav[aria-current='page'] {
    background: var(--surface-2);
    color: var(--text);
    box-shadow: var(--press-sh);
  }

  .nav .num,
  .nav .n {
    margin-left: auto;
  }

  .n {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
  }

  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sq {
    width: 7px;
    height: 7px;
    flex: none;
    background: var(--text-2);
  }

  .project[aria-current='page'] .sq {
    background: var(--text);
  }

  .sec {
    display: flex;
    align-items: center;
    padding: 18px 4px 6px 10px;
  }

  .sec-link {
    display: flex;
    align-items: center;
    gap: 10px;
    font-family: var(--mono);
    font-weight: 500;
    font-size: 10px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-2);
    text-decoration: none;
  }

  .sec-link:hover,
  .sec-link[aria-current='page'] {
    color: var(--text);
  }

  .add {
    margin-left: auto;
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    border-radius: 4px;
    color: var(--text-2);
  }

  .add:hover {
    background: var(--surface-2);
    color: var(--text);
  }

  .runner .dot.off {
    opacity: 0.5;
  }

  .foot {
    margin-top: auto;
    padding-top: 14px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 4px 4px 0 10px;
  }

  .status {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
  }

  .status .dot {
    width: 7px;
    height: 7px;
    background: var(--warn);
  }

  .status[data-state='live'] .dot {
    background: var(--ok);
  }

  .status[data-state='offline'] {
    color: var(--danger-text);
  }

  .status[data-state='offline'] .dot {
    background: var(--danger);
  }
</style>
