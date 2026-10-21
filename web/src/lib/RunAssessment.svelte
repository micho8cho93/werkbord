<script lang="ts">
  import { api } from './api';
  import { app } from './state.svelte';
  import type { Run } from './types';
  import { acknowledge } from './motion';

  let { run }: { run: Run } = $props();
  let error = $state('');
  let busy = $state(false);

  async function assess(acceptance: 'accepted' | 'rejected') {
    busy = true;
    error = '';
    try {
      const updated = await api.assessRun(run, acceptance);
      app.scopes.get(updated.projectId)?.upsertRun(updated);
      run = updated;
      app.analyticsRevision++;
      app.notify(acceptance === 'accepted' ? 'Work accepted.' : 'Work rejected.');
      void app.refreshOverview();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }
</script>

<div class="assessment" use:acknowledge={{ value: run.usage?.acceptance, success: !!run.usage?.acceptance }} role="group" aria-label="Your assessment of this run" aria-describedby="assess-help-{run.id}">
  <span role="status">{busy ? 'Saving assessment…' : 'Your assessment: '}<strong>{!busy ? (run.usage?.acceptance === 'accepted' ? 'Accepted' : run.usage?.acceptance === 'rejected' ? 'Rejected' : 'Not recorded') : ''}</strong></span>
  <button class="btn small" aria-pressed={run.usage?.acceptance === 'accepted'} disabled={busy || run.usage?.acceptance === 'accepted'} onclick={() => assess('accepted')}>Accept work</button>
  <button class="btn small" aria-pressed={run.usage?.acceptance === 'rejected'} disabled={busy || run.usage?.acceptance === 'rejected'} onclick={() => assess('rejected')}>Reject work</button>
</div>
<p class="explanation muted small" id="assess-help-{run.id}">This records your assessment. It does not merge, move or archive the task. You can change it later.</p>
{#if error}<p class="error" role="alert">{error}</p>{/if}

<style>
  .assessment { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-top: 4px; border-top: 1px solid var(--border); padding-top: 8px; }
  .explanation { margin: 0; font-size: .8rem; }
  @media (max-width: 899px), (pointer: coarse) {
    .assessment { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .assessment > [role="status"] { grid-column: 1 / -1; }
    .assessment .btn { min-height: 44px; }
  }
</style>
