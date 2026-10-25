<script lang="ts">
  import type { Snippet } from 'svelte';
  // One area of the page for one kind of thing: a coloured top edge and swatch say which kind, the title and caption say what
  // it shows. Panels sit in a grid of their own widths so the page reads as separate areas, not one long list.
  let { title, caption = '', tone = 'var(--accent)', span = 6, id = '', children, aside }: { title: string; caption?: string; tone?: string; span?: number; id?: string; children: Snippet; aside?: Snippet } = $props();
  const uid = $props.id();
</script>

<section class="panel" style:--tone={tone} style:--span={span} aria-labelledby={id || uid}>
  <header>
    <span class="swatch" aria-hidden="true"></span>
    <div class="head"><h2 id={id || uid}>{title}</h2>{#if caption}<p>{caption}</p>{/if}</div>
    {#if aside}<div class="aside">{@render aside()}</div>{/if}
  </header>
  <div class="body">{@render children()}</div>
</section>

<style>
  .panel {
    grid-column: span var(--span);
    min-width: 0;
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--card-sh);
    overflow: hidden;
  }
  header { display: flex; align-items: flex-start; gap: 10px; padding: 16px 16px 0; }
  .swatch { display: none; flex: none; width: 10px; height: 10px; margin-top: 4px; border-radius: 3px; background: var(--tone); box-shadow: 0 0 0 3px color-mix(in srgb, var(--tone) 22%, transparent); }
  :global(:root[data-style='classic']) .panel { border: 1px solid color-mix(in srgb, var(--tone) 38%, var(--border)); border-top: 3px solid var(--tone); border-radius: var(--radius-tray); }
  :global(:root[data-style='classic']) header { padding: 12px 16px; border-bottom: 1px solid color-mix(in srgb, var(--tone) 22%, var(--border)); background: color-mix(in srgb, var(--tone) 7%, var(--surface-2)); }
  :global(:root[data-style='classic']) .swatch { display: block; }
  .head { flex: 1; min-width: 0; }
  h2 { margin: 0; font-family: var(--font); font-size: 14px; font-weight: 600; letter-spacing: 0; text-transform: none; color: var(--text); }
  p { margin: 2px 0 0; color: var(--text-2); font-size: 12px; line-height: 1.5; }
  .aside { flex: none; }
  .body { flex: 1; min-width: 0; padding: 16px; }
  @media (max-width: 1100px) { .panel { grid-column: 1 / -1; } }
</style>
