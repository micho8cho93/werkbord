<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from '../Icon.svelte';

  /** One thing to set up, with how it stands: done, in progress, needs you, or left alone. */
  let {
    title,
    status = 'todo',
    summary = '',
    children,
  }: {
    title: string;
    status?: 'done' | 'wait' | 'todo' | 'off';
    /** One line on how it stands, beside the title. */
    summary?: string;
    children?: Snippet;
  } = $props();

  const word = $derived({ done: 'Done', wait: 'In progress', todo: 'To do', off: 'Skipped' }[status]);
</script>

<section class="pn setup" data-status={status}>
  <header>
    <span class="mark" title={word}>
      {#if status === 'done'}<Icon name="check" size={12} />{:else}<span class="pip"></span>{/if}
      <span class="visually-hidden">{word}</span>
    </span>
    <h3>{title}</h3>
    {#if summary}<span class="summary">{summary}</span>{/if}
  </header>
  {#if children}
    <div class="body">{@render children()}</div>
  {/if}
</section>

<style>
  .setup {
    min-width: 0;
  }

  header {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }

  h3 {
    font-size: 15px;
  }

  .mark {
    flex: none;
    width: 20px;
    height: 20px;
    display: inline-grid;
    place-items: center;
    border-radius: 5px;
    background: var(--surface-2);
    box-shadow: var(--tray-sh);
    color: var(--text-2);
  }

  .pip {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--text-2);
    opacity: 0.6;
  }

  [data-status='done'] .mark {
    background: var(--ok);
    box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.3);
    color: #fff;
  }

  [data-status='wait'] .pip {
    background: var(--accent);
    opacity: 1;
    animation: pulse 2.4s ease-in-out infinite;
  }

  .summary {
    font-family: var(--mono);
    font-size: 11.5px;
    color: var(--text-2);
    overflow-wrap: anywhere;
  }

  .body {
    display: grid;
    gap: 10px;
    min-width: 0;
    font-size: 13px;
  }
</style>
