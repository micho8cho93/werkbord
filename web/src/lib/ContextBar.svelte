<script lang="ts">
  import type { Snippet } from 'svelte';

  // The header over a page: where the person is (the project, then the page, then what is open), how many agents are running and how
  // many things need them, and room for what each side can say beside that. Drawn once for the Mac window page and for the web app.
  let { crumbs, running, needsYou, tag, notices, children, compact }: {
    crumbs: { label: string; title?: boolean }[];
    running: number;
    needsYou: number;
    /** Beside the crumbs: which workspace the project belongs to, when there are several. */
    tag?: Snippet;
    /** Statements about what cannot be shown. */
    notices?: Snippet;
    /** The popovers and actions at the right, in a row. */
    children?: Snippet;
    /** What the single "Context" button holds on a narrow window; it is handed the counts to draw. */
    compact?: Snippet<[Snippet]>;
  } = $props();
</script>

{#snippet counts()}
  <span class="count" data-testid="header-running"><span class="dot run" aria-hidden="true"></span>{running} running</span>
  <span class="count" data-testid="header-needs"><span class="dot ask" aria-hidden="true"></span>{needsYou} {needsYou === 1 ? 'needs' : 'need'} you</span>
{/snippet}

<div class="context" aria-label="Current project">
  <nav class="crumbs" aria-label="Where you are">
    <ol>{#each crumbs as c, i (i)}<li aria-current={i === crumbs.length - 1 ? 'location' : undefined} class:title={c.title}>{#if i > 0}<span class="sep" aria-hidden="true">›</span>{/if}<span class="crumb">{c.label}</span></li>{/each}</ol>
    {@render tag?.()}
  </nav>
  {@render notices?.()}

  <div class="status inline" data-testid="header-status">
    {@render counts()}
    {@render children?.()}
  </div>

  <!-- Below 640px one button holds every section. -->
  {#if compact}<div class="status compact" data-testid="header-context">{@render compact(counts)}</div>{/if}
</div>

<style>
  .context { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 20px; flex: none; min-height: 40px; box-sizing: border-box; padding: 6px 24px; border-bottom: 1px solid var(--border); color: var(--text-2); font-size: 13px; }
  .crumbs { display: flex; align-items: center; gap: 10px; min-width: 0; }
  ol { display: flex; align-items: center; flex-wrap: wrap; gap: 0 6px; list-style: none; margin: 0; padding: 0; min-width: 0; }
  li { display: inline-flex; align-items: center; gap: 6px; min-width: 0; }
  .sep { opacity: 0.6; }
  li[aria-current='location'] .crumb { color: var(--text); font-weight: 500; }
  .crumb { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 28ch; }
  .status { display: flex; align-items: center; gap: 12px; margin-left: auto; min-width: 0; }
  .count { display: inline-flex; align-items: center; gap: 6px; white-space: nowrap; font-variant-numeric: tabular-nums; }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--text-2); }
  .dot.run { background: var(--accent); }
  .dot.ask { background: var(--warn); }
  .compact { display: none; }
  @media (max-width: 639px) {
    .context { padding: 6px 12px; }
    .inline { display: none; }
    .compact { display: flex; }
    .crumb { max-width: 16ch; }
  }
</style>
