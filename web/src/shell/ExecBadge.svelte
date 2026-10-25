<script lang="ts">
  import { executionLabel, recordedTime } from './aggregate';
  import type { Execution, ExecutionReport } from './types';
  let { execution, report, unavailable = false }: { execution?: Execution; report?: ExecutionReport; unavailable?: boolean } = $props();
  const stale = $derived(unavailable || report?.stale);
  const tone = (e: string): string => ({ running: 'work', starting: 'work', needs_input: 'ask', blocked: 'block', failed: 'bad', completed: 'ok' })[e] ?? 'idle';
  const recorded = $derived(recordedTime(report?.recordedAt));
  const requests: Record<string, string> = { send: 'Message delivery pending', respond: 'Answer delivery pending', stop: 'Stop requested', finish: 'Finish requested' };
</script>

{#if execution}
  <span class="report">
    <span class="badge" data-tone={stale ? 'idle' : tone(execution)}><span class="dot" aria-hidden="true"></span><span class="label">{stale ? 'Last reported: ' : 'Reported: '}{executionLabel(execution)}</span></span>
    {#if stale}<span class="note">Availability unknown</span>{/if}
    {#if recorded}<span class="note">State recorded {recorded}</span>{/if}
    {#each report?.pendingRequests ?? [] as request (request)}<span class="note" role="status">{requests[request]} · waiting for confirmation</span>{/each}
  </span>
{/if}

<style>
  .badge {
    display: inline-flex;
    align-items: baseline;
    min-width: 0;
    white-space: normal;
    gap: 6px;
    font-family: var(--mono);
    font-size: 12px;
    color: var(--text-2);
    overflow-wrap: anywhere;
  }
  .report { display: grid; gap: 3px; min-width: 0; }
  .label { min-width: 0; overflow-wrap: anywhere; }
  .note { font-size: 12px; color: var(--text-2); overflow-wrap: anywhere; }
  .dot {
    flex: 0 0 7px;
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
