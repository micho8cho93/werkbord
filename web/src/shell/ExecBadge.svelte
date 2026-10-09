<script lang="ts">
  import type { Execution } from './types';
  let { execution }: { execution?: Execution } = $props();
  const words: Record<string, string> = {
    queued: 'Queued',
    running: 'Agent working',
    needs_input: 'Needs you',
    blocked: 'Blocked',
    completed: 'Finished',
    failed: 'Failed',
    canceled: 'Stopped',
  };
  const tone = (e: string): string => ({ running: 'work', queued: 'work', needs_input: 'ask', blocked: 'block', failed: 'bad', completed: 'ok' })[e] ?? 'idle';
</script>

{#if execution}
  <span class="badge" data-tone={tone(execution)}><span class="dot" aria-hidden="true"></span>{words[execution] ?? execution}</span>
{/if}

<style>
  .badge {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
    white-space: nowrap;
  }
  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--text-2);
  }
  .badge[data-tone='work'] .dot {
    background: var(--accent);
  }
  .badge[data-tone='ask'] .dot {
    background: var(--amber);
  }
  .badge[data-tone='block'] .dot {
    background: var(--block);
  }
  .badge[data-tone='bad'] .dot {
    background: var(--danger);
  }
  .badge[data-tone='ok'] .dot {
    background: var(--ok);
  }
</style>
