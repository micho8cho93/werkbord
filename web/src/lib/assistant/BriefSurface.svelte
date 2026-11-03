<script lang="ts">
  import { onMount } from 'svelte';
  import { onBriefRequest, reportBrief } from '../embed';
  import BriefView from './BriefView.svelte';

  // The brief's own page in the desktop window (embed.ts, `briefSurface`): the write-up (BriefView) alone on a page, drawn inside a card
  // of Work so it can be read without leaving it. What it writes stays here: the window is told a state and a height, never a word of it.
  let view: BriefView | undefined = $state();
  onMount(() => onBriefRequest((r) => {
    if (r.action === 'clear') void view?.clear();
    else if (r.prompt) void view?.ask(r.prompt, r.projectContext);
  }));
</script>

<BriefView bind:this={view} onreport={reportBrief} />

<style>
  :global(html.brief-surface), :global(html.brief-surface body), :global(html.brief-surface #app) { background: transparent; height: auto; min-height: 0; overflow: hidden; }
</style>
