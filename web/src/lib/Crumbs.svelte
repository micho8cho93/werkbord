<script lang="ts">
  // The pages above this one, oldest first, the nearest last: that last one is where Back goes. The page's own title is the heading
  // below, so it is not repeated here. Each step is a link when it is an address, so it can be followed like any other.
  import { originHref, goOrigin } from './origin-nav';
  import { labelOf, trail, type Names, type Origin } from './origin';

  let { chain, names = {}, fromTask = '' }: { chain: readonly Origin[]; names?: Names; /** The task this page is, so that a Calendar it leads back to opens on its entry. */ fromTask?: string } = $props();
  const steps = $derived(trail(chain).map((o, i, all) => ({ o, rest: chain.slice(all.length - i), label: labelOf(o, names) })));
</script>

{#if steps.length}
  <nav class="crumbs" aria-label="Breadcrumb" data-testid="crumbs">
    <ol>
      {#each steps as s, i (i)}
        {@const href = originHref(s.o, s.rest, fromTask)}
        <li>
          {#if href}<a class="crumb" class:back={i === steps.length - 1} {href} data-origin={s.o.kind}>{#if i === steps.length - 1}<span class="chev" aria-hidden="true">‹</span>{/if}{s.label}</a>
          {:else}<button class="crumb" class:back={i === steps.length - 1} type="button" data-origin={s.o.kind} onclick={() => goOrigin(s.o, s.rest, fromTask)}>{#if i === steps.length - 1}<span class="chev" aria-hidden="true">‹</span>{/if}{s.label}</button>{/if}
        </li>
      {/each}
    </ol>
  </nav>
{/if}

<style>
  .crumbs ol { display: flex; flex-wrap: wrap; align-items: center; gap: 2px 6px; margin: 0; padding: 0; list-style: none; font-size: 13px; }
  li + li::before { content: '›'; margin-right: 6px; color: var(--text-2); }
  .crumb { display: inline; padding: 2px 0; border: 0; background: none; font: inherit; color: var(--text-2); text-decoration: none; cursor: pointer; max-width: 28ch; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; vertical-align: bottom; }
  .crumb:hover { color: var(--text); text-decoration: underline; }
  .chev { margin-right: 4px; }
  .crumb.back { color: var(--text); font-weight: 500; }
  .crumb:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; border-radius: 2px; }
  @media (max-width: 899px) { .crumb { min-height: 44px; display: inline-flex; align-items: center; } }
</style>
