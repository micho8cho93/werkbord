<script lang="ts">
  import { historyCells } from './runs';
  import { app } from './state.svelte';
  import type { Run } from './types';

  // The last day, half an hour per cell, coloured by what the runs in it came to.

  let { runs }: { runs: readonly Run[] } = $props();

  // Recomputed once a minute, not every second: a cell is half an hour.
  const minute = $derived(Math.floor(app.now / 60_000));
  const cells = $derived(historyCells(runs, minute * 60_000));
  const time = (ms: number) => new Date(ms).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
  const word: Record<string, string> = { work: 'running', ask: 'needs you', idle: 'waiting for you', block: 'blocked', ok: 'finished', bad: 'failed', neutral: 'stopped' };
  const ran = $derived(cells.filter((c) => c.runs).length);
</script>

<div class="strip-wrap">
  <div class="strip" role="img" aria-label="Runs over the last 24 hours: work in {ran} of 48 half hours">
    {#each cells as c (c.start)}
      <i data-tone={c.tone || undefined} title={c.runs ? `${time(c.start)} · ${c.runs} ${c.runs === 1 ? 'run' : 'runs'} · ${word[c.tone] ?? ''}` : time(c.start)}></i>
    {/each}
  </div>
  <span class="mm">30 min per cell</span>
</div>

<style>
  .strip-wrap {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }

  .strip {
    display: flex;
    gap: 2px;
    min-width: 0;
    flex-wrap: wrap;
  }

  i {
    display: block;
    width: 10px;
    height: 14px;
    background: var(--surface-2);
    box-shadow: inset 0 1px 2px rgba(0, 0, 0, 0.22);
  }

  i[data-tone='ok'] {
    background: var(--ok);
  }

  i[data-tone='work'],
  i[data-tone='idle'] {
    background: var(--accent);
  }

  i[data-tone='block'] {
    background: var(--block);
  }

  i[data-tone='ask'] {
    background: var(--amber);
  }

  i[data-tone='bad'] {
    background: var(--danger);
  }

  i[data-tone='neutral'] {
    background: var(--text-2);
  }

  i[data-tone] {
    box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.3);
  }

  .mm {
    flex: none;
  }
</style>
