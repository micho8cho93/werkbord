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

<nav class="tabs" class:deck aria-label={label}>
  {#each tabs as t (t.id)}
    <a class="tab" href={t.href} aria-current={current === t.id ? 'page' : undefined}>
      {t.label}
      {#if t.count}<span class="num pend" aria-label={t.countLabel ?? String(t.count)}>{t.count}</span>{/if}
    </a>
  {/each}
</nav>

<style>
  .tabs {
    flex: none;
    display: flex;
    gap: 4px;
    height: 44px;
    padding: 0 24px;
    border-bottom: 1px solid var(--border);
    overflow-x: auto;
    scrollbar-width: none;
  }

  .tab {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 44px;
    padding: 0 12px;
    color: var(--text-2);
    text-decoration: none;
    font-weight: 500;
    font-size: 13px;
    border-bottom: 2px solid transparent;
    white-space: nowrap;
    transition: color 0.12s, border-color 0.12s;
  }

  .tab:hover {
    color: var(--text);
  }

  .tab[aria-current='page'] {
    color: var(--text);
    border-bottom-color: var(--accent);
  }

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
