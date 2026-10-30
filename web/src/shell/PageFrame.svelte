<script lang="ts">
  import type { Snippet } from 'svelte';

  // The frame of a page that is the window's own (Reviews, Analytics): its title, one line under it, then the page, which scrolls by
  // itself. Every such page is drawn in it, so they line up and change together.
  let { id, title, lede, children }: { id: string; title: string; lede: string; children: Snippet } = $props();
</script>

<section class="frame" aria-labelledby={id}>
  <header>
    <h1 {id}>{title}</h1>
    <p class="lede">{lede}</p>
  </header>
  {@render children()}
</section>

<style>
  .frame { display: flex; flex-direction: column; gap: 24px; padding: 28px clamp(18px, 3vw, 40px) 32px; height: 100%; overflow: auto; }
  .frame > :global(*) { flex: none; }
  h1 { margin: 0; font-size: 24px; font-weight: 600; letter-spacing: -.025em; }
  .lede { margin: 4px 0 0; color: var(--text-2); }
  @media (max-width: 899px) { .frame { height: auto; overflow: visible; padding: 20px 16px 24px; } }
</style>
