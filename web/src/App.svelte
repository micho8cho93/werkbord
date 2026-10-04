<script lang="ts">
  import Icon from './lib/Icon.svelte';
  import TokenPrompt from './lib/TokenPrompt.svelte';
  import { ROUTES, router } from './lib/router.svelte';
  import { app } from './lib/state.svelte';
  import Board from './routes/Board.svelte';
  import ControlCenter from './routes/ControlCenter.svelte';
  import Git from './routes/Git.svelte';
  import TaskDetail from './routes/TaskDetail.svelte';

  const title = $derived(router.taskId ? 'Task' : (ROUTES.find((r) => r.id === router.current)?.label ?? ''));
  const statusLabel = $derived(
    { connecting: 'Connecting', live: 'Live', offline: 'Controller offline', unauthorized: 'Token required' }[
      app.connection
    ],
  );
</script>

{#if app.connection === 'unauthorized'}
  <TokenPrompt />
{:else}
  <div class="shell">
    <aside class="rail">
      <div class="brand">Devboard</div>
      <nav aria-label="Primary">
        {#each ROUTES as r (r.id)}
          <a href="#/{r.id}" class="rail-link" aria-current={router.current === r.id ? 'page' : undefined}>
            <Icon name={r.id} />
            {r.label}
            {#if r.id === 'control' && app.needsYou > 0}<span class="count" aria-label="{app.needsYou} waiting for you">{app.needsYou}</span>{/if}
          </a>
        {/each}
      </nav>
    </aside>

    <header class="topbar">
      <h1>{title}</h1>
      <span class="status" data-state={app.connection} title={statusLabel}>
        <span class="dot" aria-hidden="true"></span>
        <span class="status-label">{statusLabel}</span>
      </span>
    </header>

    <main>
      {#if app.error}
        <p class="error banner" role="alert">{app.error}</p>
      {/if}
      {#if router.taskId}
        <TaskDetail />
      {:else if router.current === 'board'}
        <Board />
      {:else if router.current === 'control'}
        <ControlCenter />
      {:else}
        <Git />
      {/if}
    </main>

    <nav class="tabbar" aria-label="Primary">
      {#each ROUTES as r (r.id)}
        <a href="#/{r.id}" class="tab" aria-current={router.current === r.id ? 'page' : undefined}>
          <span class="icon">
            <Icon name={r.id} />
            {#if r.id === 'control' && app.needsYou > 0}<span class="count" aria-label="{app.needsYou} waiting for you">{app.needsYou}</span>{/if}
          </span>
          <span>{r.id === 'control' ? 'Control' : r.label}</span>
        </a>
      {/each}
    </nav>
  </div>
{/if}

<style>
  /* Phone first: top bar, scrolling content, bottom tab bar in thumb reach. */
  .shell {
    min-height: 100dvh;
    display: grid;
    grid-template-rows: auto 1fr;
    grid-template-columns: minmax(0, 1fr);
    grid-template-areas: 'top' 'main';
  }

  .rail {
    display: none;
  }

  .topbar {
    grid-area: top;
    position: sticky;
    top: 0;
    z-index: 5;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: calc(env(safe-area-inset-top) + 10px) 16px 10px;
    background: color-mix(in srgb, var(--bg) 88%, transparent);
    backdrop-filter: blur(10px);
    border-bottom: 1px solid var(--border);
  }

  main {
    grid-area: main;
    min-width: 0;
    padding: 12px 16px calc(var(--tabbar-h) + env(safe-area-inset-bottom) + 16px);
  }

  .banner {
    margin-bottom: 12px;
  }

  .status {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 0.8rem;
    color: var(--text-2);
  }

  .status-label {
    display: none;
  }

  .dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--warn);
  }

  .status[data-state='live'] .dot {
    background: var(--ok);
  }

  .status[data-state='offline'] .dot {
    background: var(--danger);
  }

  .status[data-state='offline'] .status-label {
    display: inline;
  }

  .tabbar {
    position: fixed;
    inset: auto 0 0 0;
    z-index: 5;
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    height: calc(var(--tabbar-h) + env(safe-area-inset-bottom));
    padding-bottom: env(safe-area-inset-bottom);
    background: var(--surface);
    border-top: 1px solid var(--border);
  }

  .tab {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 2px;
    font-size: 0.72rem;
    font-weight: 550;
    color: var(--text-2);
    text-decoration: none;
  }

  .tab[aria-current='page'] {
    color: var(--accent);
  }

  .icon {
    position: relative;
    display: inline-flex;
  }

  .count {
    min-width: 18px;
    height: 18px;
    padding: 0 5px;
    border-radius: 999px;
    background: var(--warn);
    color: #fff;
    font-size: 0.7rem;
    font-weight: 700;
    line-height: 18px;
    text-align: center;
  }

  .icon .count {
    position: absolute;
    top: -6px;
    left: 60%;
  }

  .rail-link .count {
    margin-left: auto;
  }

  /* Tablet and desktop: persistent side rail, no tab bar. */
  @media (min-width: 900px) {
    .shell {
      grid-template-columns: var(--rail-w) minmax(0, 1fr);
      grid-template-areas: 'rail top' 'rail main';
    }

    .rail {
      grid-area: rail;
      display: flex;
      flex-direction: column;
      gap: 18px;
      position: sticky;
      top: 0;
      height: 100dvh;
      padding: 18px 12px;
      border-right: 1px solid var(--border);
      background: var(--surface);
    }

    .brand {
      padding: 0 10px;
      font-weight: 700;
      letter-spacing: -0.01em;
    }

    .rail nav {
      display: flex;
      flex-direction: column;
      gap: 2px;
    }

    .rail-link {
      display: flex;
      align-items: center;
      gap: 10px;
      padding: 8px 10px;
      border-radius: var(--radius-sm);
      color: var(--text-2);
      text-decoration: none;
      font-weight: 550;
    }

    .rail-link:hover {
      background: var(--surface-2);
    }

    .rail-link[aria-current='page'] {
      background: var(--surface-2);
      color: var(--text);
    }

    .topbar {
      padding: 14px 24px;
    }

    .status-label {
      display: inline;
    }

    main {
      padding: 20px 24px 32px;
    }

    .tabbar {
      display: none;
    }
  }
</style>
