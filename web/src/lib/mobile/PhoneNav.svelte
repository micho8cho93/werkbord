<script lang="ts">
  import { assistant } from '../assistant/sessions.svelte';
  import { originOf } from '../assistant/transition';
  import Icon from '../Icon.svelte';
  import { globalHref } from '../router.svelte';

  // The phone's two places: the Control Deck, and the Assistant. Everything else is reached from the deck and has a Back. It stays
  // at the bottom on every page, over the Assistant too, so that the person is never more than one touch from either.
  let assistantTab = $state<HTMLButtonElement>();
  const waiting = $derived(assistant.waiting);
  const assistantLabel = $derived(
    waiting > 0 ? `Assistant, ${waiting} ${waiting === 1 ? 'change is' : 'changes are'} waiting for you` : assistant.running ? 'Assistant, working' : 'Assistant',
  );

  function toDeck() {
    // Opening the deck closes the assistant: the page under it is not put back, the deck is the destination.
    if (assistant.isOpen) assistant.hide();
  }
  function toAssistant() {
    if (assistantTab) assistant.toggle(originOf(assistantTab.getBoundingClientRect()), assistantTab);
  }
</script>

<nav class="phone-nav" class:over={assistant.mounted} aria-label="Primary" data-phone-nav>
  <a class="tab" href={globalHref('deck')} aria-current={!assistant.isOpen ? 'page' : undefined} onclick={toDeck} data-testid="nav-deck">
    <span class="mark"><Icon name="control" size={22} /></span>
    <span class="label">Control Deck</span>
  </a>
  <button bind:this={assistantTab} class="tab" type="button" aria-haspopup="dialog" aria-expanded={assistant.isOpen} aria-current={assistant.isOpen ? 'page' : undefined} aria-label={assistantLabel} onclick={toAssistant} data-testid="nav-assistant">
    <span class="mark asst"><Icon name="assistant" size={22} /></span>
    <span class="label">Assistant</span>
    {#if waiting > 0}
      <span class="num pend" aria-hidden="true">{waiting}</span>
    {:else if assistant.running}
      <span class="live" aria-hidden="true"></span>
    {/if}
  </button>
</nav>

<style>
  .phone-nav {
    position: fixed;
    inset: auto 0 0 0;
    /* Under sheets (50) like the tab bar it replaces, so a sheet's own buttons are never covered. */
    z-index: 10;
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    height: calc(var(--tabbar-h) + env(safe-area-inset-bottom));
    padding-bottom: env(safe-area-inset-bottom);
    background: color-mix(in srgb, var(--bg) 94%, transparent);
    backdrop-filter: blur(10px);
    border-top: 1px solid var(--border);
  }

  /* While the assistant is open, over it (it is 60): it covers the page, not this. */
  .phone-nav.over {
    z-index: 70;
  }

  .tab {
    appearance: none;
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 4px;
    border: 0;
    background: none;
    font-family: inherit;
    font-size: 11.5px;
    font-weight: 500;
    color: var(--text-2);
    text-decoration: none;
    -webkit-tap-highlight-color: transparent;
    touch-action: manipulation;
  }

  /* The place you are in is cobalt with the tab strip's own underline (as a project's tabs mark theirs). */
  .tab[aria-current='page'] {
    color: var(--accent);
  }

  .tab[aria-current='page']::after {
    content: '';
    position: absolute;
    left: 22%;
    right: 22%;
    bottom: 0;
    height: 2px;
    border-radius: 2px 2px 0 0;
    background: var(--accent);
  }

  .mark {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 52px;
    height: 30px;
    border-radius: 15px;
    color: inherit;
    transition: background 0.16s;
  }

  .tab:active .mark {
    background: var(--surface-2);
    box-shadow: var(--press-sh);
  }

  /* The assistant keeps its own colour, as it does wherever else it is drawn. */
  .tab:not([aria-current='page']) .asst {
    color: var(--accent);
  }

  .tab:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -4px;
    border-radius: var(--radius);
  }

  .num {
    position: absolute;
    top: 6px;
    left: calc(50% + 10px);
    min-width: 18px;
    height: 18px;
    padding: 0 5px;
    font-size: 11.5px;
  }

  .live {
    position: absolute;
    top: 9px;
    left: calc(50% + 14px);
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    animation: pulse 1.4s ease-in-out infinite;
  }

  @media (prefers-reduced-motion: reduce) {
    .mark {
      transition: none;
    }
    .live {
      animation: none;
    }
  }
</style>
