<script lang="ts">
  // Draws a reply as elements, never as HTML (see markdown.ts for why).
  import { blocks, type Inline } from './markdown';

  let { text }: { text: string } = $props();
  const tree = $derived(blocks(text));
</script>

{#snippet spans(list: Inline[])}
  {#each list as n, i (i)}
    {#if n.t === 'text'}{n.v}{:else if n.t === 'code'}<code>{n.v}</code>{:else if n.t === 'b'}<strong>{@render spans(n.c)}</strong>{:else}<em>{@render spans(n.c)}</em>{/if}
  {/each}
{/snippet}

<div class="md">
  {#each tree as b, i (i)}
    {#if b.t === 'p'}
      <p>{@render spans(b.c)}</p>
    {:else if b.t === 'h'}
      <p class="h">{@render spans(b.c)}</p>
    {:else if b.t === 'ul'}
      <ul>{#each b.items as it, j (j)}<li>{@render spans(it)}</li>{/each}</ul>
    {:else if b.t === 'ol'}
      <ol start={b.start}>{#each b.items as it, j (j)}<li>{@render spans(it)}</li>{/each}</ol>
    {:else if b.t === 'quote'}
      <blockquote>{@render spans(b.c)}</blockquote>
    {:else}
      <!-- A code block can scroll sideways, so the keyboard must be able to reach it. -->
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <pre class="well" tabindex="0" aria-label="Code"><code>{b.text}</code></pre>
    {/if}
  {/each}
</div>

<style>
  .md {
    display: flex;
    flex-direction: column;
    gap: 0.7em;
    overflow-wrap: anywhere;
  }
  p {
    white-space: pre-wrap;
  }
  .h {
    font-weight: 600;
  }
  ul,
  ol {
    margin: 0;
    padding-left: 1.3em;
    display: flex;
    flex-direction: column;
    gap: 0.25em;
  }
  blockquote {
    margin: 0;
    padding-left: 12px;
    border-left: 2px solid var(--border);
    color: var(--text-2);
    white-space: pre-wrap;
  }
  code {
    padding: 0.05em 0.35em;
    border-radius: 4px;
    background: var(--surface-2);
    font-size: max(0.88em, 11.5px);
  }
  pre {
    margin: 0;
    max-width: 100%;
    overflow-x: auto;
    white-space: pre;
  }
  pre code {
    padding: 0;
    background: none;
  }
</style>
