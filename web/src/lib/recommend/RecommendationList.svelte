<script lang="ts">
  import RecommendationCard from './RecommendationCard.svelte';
  import { gaps } from './recommend';
  import { recommendations } from './store.svelte';

  // The suggestions for one project, at most two, in a place that already exists. It says nothing when there is nothing to
  // suggest, and nothing about its own failures: it is a convenience and never gets in the way of the screen it sits on.
  let {
    projectId,
    title = 'Suggested next',
    onworkflow,
    onsuggest,
  }: {
    projectId: string;
    title?: string;
    onworkflow?: (workflow: string) => void;
    /** Tells the host which workflow, if any, is being suggested, so it can mark that button. */
    onsuggest?: (workflows: string[]) => void;
  } = $props();

  const entry = $derived(recommendations.entry(projectId));
  const items = $derived(recommendations.shown(projectId));
  const storedIn = $derived(entry?.set?.feedbackStoredIn ?? 'memory');
  const notes = $derived(entry?.set ? gaps(entry.set) : []);
  let announcement = $state('');

  $effect(() => {
    void recommendations.load(projectId);
    const refresh = () => {
      if (document.visibilityState === 'visible') void recommendations.load(projectId);
    };
    document.addEventListener('visibilitychange', refresh);
    return () => document.removeEventListener('visibilitychange', refresh);
  });

  $effect(() => {
    onsuggest?.(items.filter((r) => r.action.kind === 'open_workflow' && r.action.workflow).map((r) => r.action.workflow!));
  });

  // Say it when a suggestion leaves, for someone who cannot see it go.
  let count = -1;
  $effect(() => {
    const now = items.length;
    if (count >= 0 && now < count) announcement = 'Suggestion hidden.';
    count = now;
  });
</script>

{#if items.length > 0}
  <section class="recs" aria-label={title}>
    <header>
      <h2 class="lab">{title}</h2>
    </header>
    {#each items as rec (rec.id)}
      <RecommendationCard {rec} {storedIn} {onworkflow} />
    {/each}
    {#if notes.length > 0}<p class="gap">{notes[0]}</p>{/if}
  </section>
{/if}
<span class="visually-hidden" role="status" aria-live="polite">{announcement}</span>

<style>
  .recs {
    display: grid;
    gap: 10px;
    min-width: 0;
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .gap {
    font-size: 12px;
    color: var(--text-2);
  }
</style>
