<script lang="ts">
  // The keys that work everywhere in Werkbord, and the ones one side adds. Each side says what its own are; the page and the look are one.
  let { keys, note = 'Inside a task or a plan, a page lists its own where they apply.' }: { keys: { keys: string; what: string }[]; note?: string } = $props();
</script>

<section class="pn" aria-labelledby="keys-h">
  <div class="head">
    <h3 id="keys-h">Keyboard shortcuts</h3>
    <p class="muted">{note}</p>
  </div>
  <dl class="keys" data-testid="shortcut-list">
    {#each keys as k (k.keys)}<div><dt><kbd>{k.keys}</kbd></dt><dd>{k.what}</dd></div>{/each}
  </dl>
</section>

<style>
  .head { display: grid; gap: 4px; margin-bottom: 10px; }
  .keys { display: grid; margin: 0; }
  .keys div { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 9px 0; border-top: 1px solid var(--border); }
  .keys div:first-child { border-top: 0; }
  .keys dt { order: 2; }
  .keys dd { margin: 0; order: 1; font-size: 13.5px; }
  kbd { display: inline-block; min-width: 28px; padding: 2px 8px; border: 1px solid var(--border); border-bottom-width: 2px; border-radius: 6px; background: var(--surface-2); color: var(--text); font: 600 12.5px var(--mono); text-align: center; }
</style>
