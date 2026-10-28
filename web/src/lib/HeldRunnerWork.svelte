<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from './api';
  import { formatDateTime } from './clock.svelte';
  import type { HeldRun } from './types';

  let runs = $state<HeldRun[]>([]);
  let error = $state('');
  let busy = $state('');
  let checked = $state<Record<string, boolean>>({});
  let released = $state<string[]>([]);

  export async function refresh() {
    try { runs = await api.heldRunnerRuns(); error = ''; }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
  }
  onMount(() => {
    void refresh();
    const timer = setInterval(() => { if (!busy && document.visibilityState === 'visible') void refresh(); }, 15000);
    return () => clearInterval(timer);
  });

  async function release(h: HeldRun) {
    busy = h.runId; error = '';
    try {
      await api.confirmRunnerStopped(h.runnerId, h.runId);
      released = [...released, h.taskTitle || h.taskId];
      await refresh();
    } catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = ''; }
  }
</script>

{#if runs.length || released.length || error}
  <section class="pn held" aria-labelledby="held-title">
    <div><h3 id="held-title">Work held on revoked machines</h3>
      <p class="muted">A revoked machine can no longer report to Werkbord, but it may still be running what it had started, so this work stays reserved. Nothing releases it on a timer. Inspect the machine, stop anything still running, then confirm below.</p></div>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    {#each released as title (title)}<p role="status" class="muted">Released “{title}”. Its work on that machine is unverified: reconcile its branch before running the task again.</p>{/each}
    {#each runs as h (h.runId)}
      <div class="run">
        <strong>{h.taskTitle || h.taskId}</strong>
        <p class="muted">On {h.runnerName}{h.hostname ? ` (${h.hostname})` : ''} · started {formatDateTime(h.startedAt)}</p>
        <label class="check"><input type="checkbox" bind:checked={checked[h.runId]} disabled={!!busy} />I inspected {h.runnerName} and nothing from this run is still executing.</label>
        <div><button class="btn danger" disabled={!!busy || !checked[h.runId]} onclick={() => release(h)}>{busy === h.runId ? 'Releasing…' : 'Release this work'}</button></div>
      </div>
    {/each}
  </section>
{/if}

<style>
  .held { padding: 16px; display: grid; gap: 16px; }
  h3 { font-size: 16px; font-weight: 600; }
  p { margin: 4px 0; font-size: 13px; }
  .run { padding-top: 16px; border-top: 1px solid var(--border); display: grid; gap: 8px; }
  .check { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; font-size: 13px; }
</style>
