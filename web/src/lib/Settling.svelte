<script lang="ts">
  import Icon from './Icon.svelte';
  import Mark from './Mark.svelte';

  // What Werkbord shows while it finds out something it has to know before it can show the real thing: the moving mark, what it is
  // doing and, where there are several things to find out, each one ticking off as its answer arrives. Nothing is guessed meanwhile, so
  // nothing that appears here is taken back a moment later. Reduced motion stops the mark (lib/mark-animation) and the pulse below.
  let { label, items = [], testid = 'settling', compact = false }: {
    label: string;
    items?: readonly { label: string; done: boolean }[];
    testid?: string;
    /** Inline, for a panel or a column, instead of a screen of its own. */
    compact?: boolean;
  } = $props();
</script>

<section class="settling" class:compact role="status" aria-live="polite" data-testid={testid}>
  <Mark height={compact ? 28 : 40} activity="helix" />
  <p class="what">{label}</p>
  {#if items.length}
    <ul>
      {#each items as item (item.label)}
        <li data-done={item.done}>
          <span class="state" aria-hidden="true">{#if item.done}<Icon name="check" size={14} />{:else}<span class="pulse"></span>{/if}</span>
          <span>{item.label}</span>
          <span class="visually-hidden">{item.done ? ', checked' : ', checking'}</span>
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .settling {
    display: grid;
    justify-items: center;
    align-content: center;
    gap: 14px;
    min-height: 260px;
    padding: 32px 16px;
    text-align: center;
  }
  .settling.compact { min-height: 0; padding: 20px 12px; gap: 10px; }
  .what { margin: 0; color: var(--text-2); font-size: 14px; }
  ul { list-style: none; margin: 4px 0 0; padding: 0; width: min(100%, 320px); border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); text-align: left; }
  li { display: flex; align-items: center; gap: 10px; min-height: 40px; padding: 0 14px; font-size: 13px; color: var(--text-2); }
  li + li { border-top: 1px solid var(--border); }
  li[data-done='true'] { color: var(--text); }
  .state { display: inline-grid; place-items: center; width: 16px; height: 16px; flex: none; color: var(--ok); }
  .pulse { width: 7px; height: 7px; border-radius: 50%; background: var(--text-2); opacity: 0.55; }
  @media (prefers-reduced-motion: no-preference) {
    .pulse { animation: settle-pulse 1.1s ease-in-out infinite; }
  }
  @keyframes settle-pulse { 50% { opacity: 0.15; transform: scale(0.7); } }
</style>
