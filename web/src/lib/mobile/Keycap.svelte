<script lang="ts">
  import { onDestroy } from 'svelte';
  import Icon from '../Icon.svelte';
  import WorkspaceIcon from '../personal/WorkspaceIcon.svelte';
  import type { DeckKey } from './deck';
  import { KeyPress } from './press';

  let { key, target = '', workflow = false, editing = false, empty = false, disabled = false, onclick, onconfigure, ondrag, ondrop }:
    { key: DeckKey; target?: string; workflow?: boolean; editing?: boolean; empty?: boolean; disabled?: boolean; onclick?: () => void; onconfigure?: () => void; ondrag?: (x: number, y: number) => void; ondrop?: (cancelled?: boolean) => void } = $props();
  let pressed = $state(false);
  const gesture = new KeyPress(() => { pressed = false; onconfigure?.(); }, (x,y) => { if (editing) ondrag?.(x,y); });
  const release = () => { pressed = false; gesture.end(); };
  onDestroy(() => gesture.cancel());
  const href = $derived(!editing && !empty && key.href ? key.href : undefined);
</script>

<div class="key-holder">
  <svelte:element
    this={href ? 'a' : 'button'}
    type={href ? undefined : 'button'}
    role={href ? 'link' : 'button'}
    class="key-slot"
    class:editing
    class:empty
    class:workflow
    {href}
    disabled={href ? undefined : disabled}
    data-key={empty ? undefined : key.id}
    data-tone={key.tone}
    data-lit={key.count > 0 || !!key.note ? 'on' : undefined}
    data-pressed={pressed ? 'on' : undefined}
    aria-label={empty ? 'Empty slot. Add a button' : [key.label, workflow ? 'Workflow' : '', target, key.note, editing ? 'Replace or remove' : ''].filter(Boolean).join(', ')}
    title={onconfigure ? 'Hold or press Shift+F10 to configure. You can also use Edit Deck.' : undefined}
    onpointerdown={(e: PointerEvent) => { if (e.button !== 0 || disabled) return; pressed = true; gesture.start(e.clientX,e.clientY,!!onconfigure && !editing); if (editing) (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId); }}
    onpointermove={(e: PointerEvent) => gesture.move(e.clientX,e.clientY)}
    onpointerup={() => { release(); if (editing) ondrop?.(); }}
    onpointercancel={() => { pressed = false; gesture.cancel(); ondrop?.(true); }}
    onpointerleave={() => { if (!editing) { pressed = false; gesture.leave(); } }}
    onblur={release}
    oncontextmenu={(e: MouseEvent) => { if (onconfigure) { e.preventDefault(); gesture.cancel(); onconfigure(); } }}
    onclick={(e: MouseEvent) => { if (gesture.consumeClick()) { e.preventDefault(); return; } onclick?.(); }}
    onkeydown={(e: KeyboardEvent) => { if (onconfigure && (e.key === 'ContextMenu' || e.key === 'F10' && e.shiftKey)) { e.preventDefault(); onconfigure(); } else if (e.key === ' ' || e.key === 'Enter') { gesture.start(0,0,false); gesture.end(); pressed = true; } }}
    onkeyup={release}
  >
    <span class="cap">
      <span class="face">
        <span class="glyph">{#if !empty && key.workspaceIcon}<WorkspaceIcon icon={key.workspaceIcon} name={key.label} size={26} />{:else}<Icon name={empty ? 'plus' : key.icon} size={26} />{/if}</span>
        <span class="name">{empty ? 'Add button' : key.label}</span>
      </span>
      {#if key.count > 0}<span class="num" class:pend={key.tone === 'wait'} aria-hidden="true">{key.id !== 'agents' && key.count > 99 ? '99+' : key.count}</span>{/if}
    </span>
  </svelte:element>
  {#if editing && onconfigure}<button class="configure" type="button" aria-label="Configure {key.label}" onclick={onconfigure}><Icon name="settings" size={14} /></button>{/if}
</div>

<style>
  /* The well: the dark socket the keycap stands in. It is the tray of the design system, pressed a little deeper. */
  .key-holder { position: relative; min-width: 0; }
  .configure { position: absolute; top: -10px; right: -10px; width: 44px; height: 44px; display: grid; place-items: center; border: 0; background: transparent; color: var(--text); cursor: pointer; }
  .key-slot.editing { touch-action: none; cursor: grab; }
  .key-slot:disabled { cursor: wait; }
  .workflow .cap { background: linear-gradient(color-mix(in srgb, var(--accent) 12%, transparent), color-mix(in srgb, var(--accent) 12%, transparent)), var(--key-bg); border-color: color-mix(in srgb, var(--accent) 38%, var(--border)); }
  .workflow .glyph, .workflow .name { color: var(--accent); }
  .empty .cap { background: var(--surface-2); box-shadow: inset 0 1px 3px color-mix(in srgb, var(--text) 12%, transparent); transform: none; border-style: dashed; }
  .key-slot {
    --lift: 4px;
    position: relative;
    font: inherit;
    cursor: pointer;
    border: 0;
    width: 100%;
    text-align: center;
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
      transform 0.14s var(--ease),
      box-shadow 0.14s var(--ease);
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
    max-width: 100%;
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
    white-space: normal;
    overflow-wrap: anywhere;
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
    transition-duration: 0.08s;
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
