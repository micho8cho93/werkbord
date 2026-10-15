<script lang="ts">
  import Icon from '../Icon.svelte';
  import type { DeckKey } from './deck';

  // One key of the deck: a link to a page, drawn as a keycap that sits in its own well. It is a plain link, so it works from the
  // keyboard and a screen reader and opens in a new tab like any other; the keycap only moves while it is held down.
  let { key }: { key: DeckKey } = $props();
  let pressed = $state(false);
  const release = () => (pressed = false);
</script>

<a
  class="key-slot"
  href={key.href}
  data-key={key.id}
  data-tone={key.tone}
  data-lit={key.count > 0 ? 'on' : undefined}
  data-pressed={pressed ? 'on' : undefined}
  aria-label={key.note ? `${key.label}, ${key.note}` : key.label}
  onpointerdown={() => (pressed = true)}
  onpointerup={release}
  onpointercancel={release}
  onpointerleave={release}
  onblur={release}
  onkeydown={(e) => (e.key === ' ' || e.key === 'Enter') && (pressed = true)}
  onkeyup={release}
>
  <span class="cap">
    <span class="face">
      <span class="glyph"><Icon name={key.icon} size={26} /></span>
      <span class="name">{key.label}</span>
    </span>
    {#if key.count > 0}
      <span class="num" class:pend={key.tone === 'wait'} aria-hidden="true">{key.count > 99 ? '99+' : key.count}</span>
    {/if}
  </span>
</a>

<style>
  /* The well: the dark socket the keycap stands in. It is the tray of the design system, pressed a little deeper. */
  .key-slot {
    --lift: 4px;
    position: relative;
    display: block;
    aspect-ratio: 1 / 1;
    min-width: 0;
    padding: 5px;
    border-radius: 17px;
    background: var(--ground);
    box-shadow:
      inset 0 2px 5px color-mix(in srgb, var(--text) 26%, transparent),
      inset 0 0 0 1px color-mix(in srgb, var(--text) 8%, transparent),
      0 1px 0 var(--deck-edge-light);
    color: var(--text);
    text-decoration: none;
    -webkit-tap-highlight-color: transparent;
    touch-action: manipulation;
    user-select: none;
    -webkit-user-select: none;
    -webkit-touch-callout: none;
  }

  .key-slot:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 3px;
  }

  /* The cap: raised on its side wall, with a lit top edge and a faint dish worn into its face. */
  .cap {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100%;
    border-radius: 13px;
    border: 1px solid var(--border);
    background:
      radial-gradient(120% 90% at 50% 8%, color-mix(in srgb, var(--deck-edge-light) 70%, transparent), transparent 62%),
      var(--key-bg);
    box-shadow:
      inset 0 1px 0 var(--deck-edge-light),
      inset 0 -2px 3px color-mix(in srgb, var(--text) 7%, transparent),
      0 var(--lift) 0 var(--deck-wall),
      0 calc(var(--lift) + 3px) 7px -1px color-mix(in srgb, var(--text) 32%, transparent);
    transform: translateY(calc(var(--lift) * -1));
    transition:
      transform 0.16s var(--ease),
      box-shadow 0.16s var(--ease);
  }

  @media (max-width: 359px) {
    .name {
      font-size: 12px;
    }

    .face {
      padding: 0 2px;
    }
  }

  .face {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    min-width: 0;
    padding: 0 4px;
  }

  .glyph {
    display: inline-flex;
    color: var(--text-2);
    transition: color 0.16s;
  }

  .name {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 12.5px;
    font-weight: 500;
    letter-spacing: -0.005em;
    line-height: 1.1;
  }

  /* A key lights its glyph only when it means something: work waiting for the person is amber, work going on is cobalt. */
  [data-lit='on'][data-tone='wait'] .glyph {
    color: var(--warn);
  }

  [data-lit='on'][data-tone='work'] .glyph {
    color: var(--accent);
  }

  .num {
    position: absolute;
    top: -7px;
    right: -6px;
  }

  .key-slot:hover .cap,
  .key-slot:focus-visible .cap {
    --lift: 4px;
  }

  /* Held down: the cap drops onto its well, the wall disappears and the face goes in shade. */
  .key-slot[data-pressed='on'] .cap,
  .key-slot:active .cap {
    transform: translateY(0);
    transition-duration: 0.05s;
    box-shadow:
      inset 0 1px 3px color-mix(in srgb, var(--text) 22%, transparent),
      inset 0 0 0 1px color-mix(in srgb, var(--text) 5%, transparent),
      0 0 0 var(--deck-wall),
      0 1px 2px color-mix(in srgb, var(--text) 24%, transparent);
  }

  @media (prefers-reduced-motion: reduce) {
    .cap {
      transition: none;
    }

    /* Without movement the press still has to be seen: the face is shaded and the wall goes. */
    .key-slot[data-pressed='on'] .cap,
    .key-slot:active .cap {
      transform: translateY(calc(var(--lift) * -1));
      background: var(--surface-2);
      box-shadow:
        inset 0 1px 3px color-mix(in srgb, var(--text) 22%, transparent),
        0 0 0 var(--deck-wall);
    }
  }
</style>
