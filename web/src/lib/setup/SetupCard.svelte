<script lang="ts">
  import type { Snippet } from 'svelte';

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

  const mark = $derived({ done: '✓', wait: '…', todo: '○', off: '–' }[status]);
  const word = $derived({ done: 'Done', wait: 'In progress', todo: 'To do', off: 'Skipped' }[status]);
</script>

<section class="card setup" data-status={status}>
  <header>
    <span class="mark" role="img" aria-label={word}>{mark}</span>
    <h3>{title}</h3>
    {#if summary}<span class="summary">{summary}</span>{/if}
  </header>
  {#if children}
    <div class="body">{@render children()}</div>
  {/if}
</section>

<style>
  .setup {
    display: grid;
    gap: 10px;
    padding: 14px 16px;
    min-width: 0;
  }

  header {
    display: flex;
    align-items: baseline;
    gap: 10px;
    flex-wrap: wrap;
  }

  h3 {
    font-size: 1rem;
    font-weight: 650;
  }

  .mark {
    flex: none;
    width: 22px;
    height: 22px;
    display: inline-grid;
    place-items: center;
    border-radius: 50%;
    border: 1.5px solid var(--text-2);
    color: var(--text-2);
    font-size: 0.8rem;
    font-weight: 700;
    align-self: center;
  }

  [data-status='done'] .mark {
    background: var(--ok);
    border-color: var(--ok);
    color: var(--accent-text);
  }

  [data-status='wait'] .mark {
    border-color: var(--accent);
    color: var(--accent);
  }

  .summary {
    color: var(--text-2);
    font-size: 0.88rem;
    overflow-wrap: anywhere;
  }

  .body {
    display: grid;
    gap: 10px;
    min-width: 0;
  }
</style>
