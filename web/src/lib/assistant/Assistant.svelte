<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import Icon from '../Icon.svelte';
  import SessionContent from './SessionContent.svelte';
  import ModelPicker from './ModelPicker.svelte';
  import SessionTabs from './SessionTabs.svelte';
  import { phone } from '../mobile/phone.svelte';
  import { assistant } from './sessions.svelte';
  import { colourOf, plan, prefersReducedMotion, Reveal } from './transition';

  // The assistant's whole-window surface. It opens out of the button that was pressed and closes back into it. The workspace
  // underneath stays mounted and untouched (the page keeps its place, its drafts, its running agents); this only covers it.
  // Closing the surface does not stop anything either: a reply keeps streaming, and changes waiting for the person keep waiting,
  // with the button showing that they do.

  let surface: HTMLDivElement | undefined = $state();
  let fxEdge: HTMLDivElement | undefined = $state();
  let fxBloom: HTMLDivElement | undefined = $state();
  let reveal: Reveal | null = null;
  let announcement = $state('');

  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);

  // ---- opening and closing ----
  $effect(() => {
    const phase = assistant.phase;
    const el = surface;
    if (!el) return;
    untrack(() => {
      if (phase === 'opening') {
        if (!reveal || reveal.el !== el) {
          reveal?.destroy();
          const from = colourOf(el, '--asst-from', '#ffffff');
          const to = colourOf(el, '--asst-to', '#f5f5f2');
          reveal = new Reveal(el, plan(assistant.origin, window.innerWidth, window.innerHeight, prefersReducedMotion(), from, to), { edge: fxEdge, bloom: fxBloom });
        }
        void reveal.open().then(() => assistant.opened());
      } else if (phase === 'closing') {
        // A surface nobody can see (the desktop window moved to another page, or the tab is hidden) has no movement to show:
        // it is simply closed, and an animation that cannot run does not hold it open.
        if (!reveal || el.getClientRects().length === 0 || document.hidden) {
          reveal?.destroy();
          reveal = null;
          assistant.closed();
        } else void reveal.close().then(() => assistant.closed());
      }
    });
  });
  $effect(() => {
    if (!assistant.mounted && reveal) {
      reveal.destroy();
      reveal = null;
    }
  });
  onDestroy(() => reveal?.destroy());

  // ---- the page behind it ----
  $effect(() => {
    document.documentElement.classList.toggle('asst-open', assistant.mounted);
    return () => document.documentElement.classList.remove('asst-open');
  });

  // On a phone the on-screen keyboard shrinks the visual viewport but not the layout viewport; keep the box above it.
  $effect(() => {
    if (!assistant.mounted) return;
    const vv = window.visualViewport;
    if (!vv) return;
    const fit = () => {
      surface?.style.setProperty('--vvh', `${vv.height}px`);
      surface?.style.setProperty('--vvt', `${vv.offsetTop}px`);
      // The phone's bottom bar stays under the assistant. With the on-screen keyboard up it is behind the keyboard, and the box
      // takes the room.
      surface?.style.setProperty('--asst-nav', !phone.on || window.innerHeight - vv.height > 100 ? '0px' : 'calc(var(--tabbar-h) + env(safe-area-inset-bottom))');
    };
    fit();
    vv.addEventListener('resize', fit);
    vv.addEventListener('scroll', fit);
    return () => {
      vv.removeEventListener('resize', fit);
      vv.removeEventListener('scroll', fit);
    };
  });

  // ---- for people who cannot see it move: say what happens, in words ----
  let lastRunning = false;
  let lastWaiting = 0;
  $effect(() => {
    const running = assistant.running;
    const waiting = assistant.waiting;
    if (running && !lastRunning) announcement = 'The assistant is working.';
    else if (!running && lastRunning) announcement = waiting > 0 ? `The assistant has finished. ${waiting === 1 ? 'A change is' : `${waiting} changes are`} waiting for your confirmation.` : 'The assistant has finished.';
    else if (waiting > lastWaiting) announcement = 'A change is waiting for your confirmation.';
    lastRunning = running;
    lastWaiting = waiting;
  });

  const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]):not([aria-hidden="true"]), select:not([disabled]), textarea:not([disabled]), summary, [tabindex]:not([tabindex="-1"])';

  /** Keeps Tab inside the assistant: past the last control it goes to the first, before the first it goes to the last. */
  function trap(e: KeyboardEvent) {
    if (!surface) return;
    // On the phone the bottom bar's two places are part of the dialog's order: they are how the person leaves it.
    const inside = [...surface.querySelectorAll<HTMLElement>(FOCUSABLE), ...(phone.on ? document.querySelectorAll<HTMLElement>('[data-phone-nav] ' + FOCUSABLE.split(', ').join(', [data-phone-nav] ')) : [])];
    const items = inside.filter((el) => el.offsetParent !== null || el === document.activeElement);
    if (items.length === 0) return;
    const first = items[0];
    const last = items[items.length - 1];
    const at = document.activeElement as HTMLElement | null;
    if (!at || !(surface.contains(at) || (phone.on && at.closest('[data-phone-nav]')))) {
      e.preventDefault();
      first.focus();
    } else if (e.shiftKey && at === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && at === last) {
      e.preventDefault();
      first.focus();
    }
  }

  function onKey(e: KeyboardEvent) {
    if (!assistant.mounted || e.defaultPrevented) return;
    if (e.key === 'Tab') return trap(e);
    if (e.key !== 'Escape') return;
    e.preventDefault();
    assistant.hide();
  }

</script>

<svelte:window onkeydown={onKey} />

{#if assistant.mounted}
  <!-- The light that travels with the edge of the circle as it opens (see transition.ts); it takes no part once the surface is open. -->
  <div class="fx bloom" bind:this={fxBloom} aria-hidden="true"></div>
  <div
    bind:this={surface}
    class="surface"
    data-phase={assistant.phase}
    role="dialog"
    aria-modal={phone.on ? undefined : 'true'}
    data-nav={phone.on ? 'on' : undefined}
    aria-labelledby="asst-title"
    inert={assistant.phase === 'closing'}
    data-testid="assistant"
  >
    <div class="inner">
      <header class="top">
        <div class="who">
          <h1 id="asst-title">Assistant</h1>
          {#key assistant.activeId}<ModelPicker assistant={assistant.active} />{/key}
        </div>
        <div class="acts">
          <button class="btn quiet close" class:phone-nav={phone.on} type="button" onclick={() => assistant.hide()} aria-label="Close assistant" title="Close ({isMac ? '' : 'Esc'})" data-testid="assistant-close">
            <Icon name="close" size={16} /><span class="wide">Close</span><kbd class="key wide">Esc</kbd>
          </button>
        </div>
      </header>

      <SessionTabs />
      <div class="session-panel" id="sally-panel" role="tabpanel" aria-labelledby="sally-tab-{assistant.activeId}">
      {#key assistant.activeId}
        <SessionContent store={assistant.active} />
      {/key}
      </div>
    </div>
    <p class="visually-hidden" role="status" aria-live="polite" data-testid="assistant-announce">{announcement}</p>
  </div>
  <div class="fx edge" bind:this={fxEdge} aria-hidden="true"></div>
{/if}

<style>
  .surface {
    --asst-w: 46rem;
    --asst-from: var(--surface);
    --asst-to: var(--bg);
    position: fixed;
    top: var(--vvt, 0px);
    left: 0;
    width: 100%;
    height: calc(var(--vvh, 100dvh) - var(--asst-nav, 0px));
    z-index: 60;
    background: var(--asst-to);
    color: var(--text);
    overflow: hidden;
  }
  .surface[data-nav='on'] {
    --asst-nav: calc(var(--tabbar-h) + env(safe-area-inset-bottom));
  }
  .session-panel { display:flex; flex-direction:column; min-height:0; flex:1; }
  .inner {
    display: flex;
    flex-direction: column;
    height: 100%;
  }
  /* The content arrives after the circle has mostly grown, one part after another from the top down, each rising a little into place,
     so the eye is led through it; it leaves as soon as the surface starts to close. With reduced motion the animation is switched
     off, and the content is simply there. */
  .surface[data-phase='opening'] .inner > :global(*),
  .surface[data-phase='open'] .inner > :global(*) {
    animation: arrive 760ms var(--ease, cubic-bezier(0.16, 1, 0.3, 1)) backwards;
  }
  .surface .inner > :global(:nth-child(1)) {
    animation-delay: 540ms;
  }
  .surface .inner > :global(:nth-child(2)) {
    animation-delay: 640ms;
  }
  .surface .inner > :global(:nth-child(3)) {
    animation-delay: 740ms;
  }
  .surface[data-phase='closing'] .inner {
    opacity: 0;
    transition: opacity 0.16s ease;
  }
  @keyframes arrive {
    from {
      opacity: 0;
      transform: translateY(18px);
    }
  }
  /* The light that travels with the edge. Each layer is the size of the window and draws its gradients from --r, the circle's radius,
     which transition.ts animates. They are invisible until they move and again once the surface is open. */
  .fx {
    position: fixed;
    inset: 0;
    z-index: 61;
    pointer-events: none;
    opacity: 0;
    --r: 0px;
    --ox: 0px;
    --oy: 0px;
  }
  .edge {
    background: radial-gradient(
      circle at var(--ox) var(--oy),
      transparent calc(var(--r) - 150px),
      color-mix(in srgb, var(--accent) 12%, transparent) calc(var(--r) - 48px),
      color-mix(in srgb, var(--accent) 30%, transparent) calc(var(--r) - 16px),
      color-mix(in srgb, white 55%, var(--accent)) calc(var(--r) - 4px),
      color-mix(in srgb, white 78%, var(--accent)) calc(var(--r) - 0.5px),
      color-mix(in srgb, white 45%, var(--accent)) calc(var(--r) + 3px),
      color-mix(in srgb, var(--accent) 40%, transparent) calc(var(--r) + 9px),
      color-mix(in srgb, var(--accent) 16%, transparent) calc(var(--r) + 32px),
      transparent calc(var(--r) + 96px)
    );
  }
  .bloom {
    background: radial-gradient(
      circle at var(--ox) var(--oy),
      color-mix(in srgb, var(--accent) 38%, transparent) 0,
      color-mix(in srgb, var(--accent) 14%, transparent) calc(var(--r) * 0.35 + 36px),
      transparent calc(var(--r) * 0.7 + 90px)
    );
  }
  .top {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: calc(8px + env(safe-area-inset-top)) 12px 8px 16px;
  }
  .who {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  h1 {
    font-size: 15px;
    font-weight: 600;
    letter-spacing: -0.01em;
  }
  .acts {
    display: flex;
    align-items: center;
    gap: 4px;
    flex: none;
  }
  .close {
    gap: 8px;
  }
  /* With the phone's bottom bar the way out is the bar: its Assistant tab closes the assistant and its other tab leaves it. */
  .close.phone-nav {
    display: none;
  }
  @media (max-width: 640px) {
    .wide {
      display: none;
    }
    .close {
      width: 44px;
      padding: 0;
    }
  }
</style>
