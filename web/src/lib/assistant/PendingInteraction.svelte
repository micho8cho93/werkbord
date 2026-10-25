<script lang="ts">
  import { assistant } from './sessions.svelte';
  import { app } from '../state.svelte';
  import QuestionCard from '../QuestionCard.svelte';
  import ProposalInteraction from './ProposalInteraction.svelte';
  let { floating = true }: { floating?: boolean } = $props();
  const current = $derived(assistant.interactions[0]);
</script>
<svelte:window onkeydown={e => { if (e.key === 'Escape' && assistant.compact) { e.preventDefault(); assistant.compact = false; } }} />
{#if current}
  <aside class:floating aria-label="Salie pending request">
    <header><strong>Needs you · {assistant.waiting} pending</strong><button class="btn quiet small" aria-label="Close request popup" onclick={() => assistant.compact = false}>Close</button></header>
    {#key current.key}
      {#if current.kind === 'question'}
        <p class="source">{app.project(current.question.projectId)?.name ?? current.question.projectId} · Run {current.question.runId}</p>
        <QuestionCard question={current.question} />
        <button class="btn small" onclick={() => assistant.openRun(current.question.projectId, current.question.runId)}>Open Session</button>
      {:else}
        <p class="source">{assistant.title(assistant.tabs.find(t => t.id === current.tabId)!)}</p>
        <ProposalInteraction store={current.store} item={current.item} />
        <button class="btn small" onclick={() => { assistant.select(current.tabId); assistant.show(); }}>Open conversation</button>
      {/if}
    {/key}
  </aside>
{/if}
<style>
  aside { display: grid; gap: 12px; background: var(--surface); padding: 16px; overflow: auto; max-height: 100dvh; }
  aside.floating { position: fixed; right: 16px; bottom: 80px; width: min(360px, calc(100vw - 32px)); max-height: min(500px, calc(100dvh - 110px)); z-index: 65; border: 1px solid var(--border); border-radius: var(--radius); }
  header { display: flex; justify-content: space-between; gap: 8px; align-items: center; }
  header strong { font-size: 13px; }
  .source { font: 12px var(--mono); color: var(--text-2); overflow-wrap: anywhere; }
</style>
