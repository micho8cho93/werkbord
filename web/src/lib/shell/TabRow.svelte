<script lang="ts">
  // One row of tabs for the sub-areas of a major part: a project's Board and Git, a workspace's Workspace and Projects. The window's
  // sidebar lists the major parts; this is the only other navigation, and everything that has sub-areas draws them here.
  let { label, tabs, current, deck = false }: {
    label: string;
    tabs: readonly { id: string; label: string; href: string; count?: number; countLabel?: string }[];
    current: string;
    /** On a phone with the Control Deck the row sits under the name instead of being hidden. */
    deck?: boolean;
  } = $props();
</script>

<nav class="tabs section-tabs" class:deck aria-label={label}>
  {#each tabs as t (t.id)}
    <a class="tab" href={t.href} aria-current={current === t.id ? 'page' : undefined}>
      {t.label}
      {#if t.count}<span class="num pend" aria-label={t.countLabel ?? String(t.count)}>{t.count}</span>{/if}
    </a>
  {/each}
</nav>

<style>
  .tabs { padding: 0 24px; }

  /* Phone: the tabs live in the bottom bar, except with the Control Deck, whose bottom bar has only two places. */
  @media (max-width: 899px) {
    .tabs {
      display: none;
    }

    .tabs.deck {
      display: flex;
      padding: 0 12px;
    }
  }
</style>
