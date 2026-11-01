<script lang="ts">
  import { model } from './model.svelte';

  // Asking Salie for a brief of recent work, or for a plan of the day. Opening Werkbord asks her nothing: she is only asked when one of
  // these is pressed, and the question is sent at once because pressing it is the asking. The project she answers for is the one the
  // window is on (All Projects, or the one chosen), which AssistantFrame already tells her.
  const ranges = [
    { label: 'Last day', ask: 'Summarize the last day' },
    { label: 'Last 3 days', ask: 'Summarize the last 3 days' },
    { label: 'Last week', ask: 'Summarize the last week' },
  ];
  const scope = $derived(model.selectedProject ? model.contextLabel : 'all projects');
  const ask = (text: string) => void model.explainCatchUp(text, true);
</script>

{#if !model.remotePersonal}
  <section class="brief" aria-labelledby="brief-h" data-testid="work-brief">
    <h2 id="brief-h" class="section-title">Catch up</h2>
    <p class="note">A short brief from Salie on {scope}: what needs you, what is blocked, what was done, what is ahead. She is asked only when you press one.</p>
    <div class="ranges" role="group" aria-label="Summarize recent work">
      {#each ranges as r (r.label)}
        <button type="button" class="btn small" data-ask={r.ask} onclick={() => ask(r.ask)}>{r.label}</button>
      {/each}
    </div>
    <button type="button" class="btn primary plan" onclick={() => ask('Plan my day')}>Plan my day</button>
  </section>
{/if}

<style>
  .brief { border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); padding: 16px; display: grid; gap: 10px; }
    .note { margin: 0; font-size: 12px; line-height: 1.5; color: var(--text-2); }
  .ranges { display: flex; flex-wrap: wrap; gap: 8px; }
  .plan { justify-self: start; }
</style>
