<script lang="ts">
  import { embedded, surface } from '../embed';
  import { phone } from '../mobile/phone.svelte';
  import { BASIS_LABEL, BASIS_TONE, CONFIDENCE_LABEL, INTENT_LABEL, TIER_LABEL, confirmationNote, deckHref, destination, evidenceMeta, feedbackNote, groupedByTier } from './recommend';
  import { recommendations } from './store.svelte';
  import type { Recommendation, RecVerdict } from './types';

  // One suggested next step. It is advice with its evidence: the button opens a screen that already exists and asks the person
  // itself (a workflow is only pointed at on the Control Deck, never started from here), and "Why this" says what it rests on
  // and which rule, policy or decision shaped it. The words below come from the controller's rules, not from a model.
  let {
    rec,
    storedIn = 'memory',
    onworkflow,
  }: {
    rec: Recommendation;
    /** Where a verdict is kept, as the controller said. */
    storedIn?: 'memory' | 'device';
    /** On the Control Deck itself: point at the workflow's button instead of leaving for the deck. */
    onworkflow?: (workflow: string) => void;
  } = $props();

  const dest = $derived(destination(rec));
  const note = $derived(confirmationNote(rec));
  const given = $derived(recommendations.given[rec.id]);
  const busy = $derived(!!recommendations.sending[rec.id]);
  const problem = $derived(recommendations.failed[rec.id]);

  // The Control Deck exists on a phone and inside the desktop app; a wide browser window has no deck to open.
  const deckReachable = $derived(phone.on || embedded());

  // In the desktop app the assistant is a frame of its own: a link there cannot move the window behind it, so it asks the window
  // to show the place (which closes the assistant). Anywhere else the page follows the link itself.
  function follow(e: MouseEvent, href: string) {
    if (!surface || !href.startsWith('#')) return;
    e.preventDefault();
    window.parent.postMessage({ type: 'werkbord.frame', event: 'open', target: 'personal', place: href }, '*');
  }

  function say(verdict: RecVerdict) {
    void recommendations.feedback(rec, verdict);
  }
</script>

<article class="card rec" data-basis={rec.basis} aria-label="{INTENT_LABEL[rec.intent]}: {rec.title}">
  <p class="lab kick">
    <span class="dot" data-tone={BASIS_TONE[rec.basis]} aria-hidden="true"></span>
    <span>{INTENT_LABEL[rec.intent]}</span>
    <span class="basis">{BASIS_LABEL[rec.basis]}</span>
  </p>
  <h3 class="title">{rec.title}</h3>
  <p class="detail">{rec.detail}</p>

  <div class="acts">
    {#if dest && 'href' in dest}
      <a class="btn small primary" href={dest.href} onclick={(e) => follow(e, dest.href)}>{rec.action.label}</a>
    {:else if dest && 'workflow' in dest}
      {#if onworkflow}
        <button class="btn small primary" type="button" onclick={() => onworkflow?.(dest.workflow)}>{rec.action.label}</button>
      {:else if deckReachable}
        <a class="btn small primary" href={deckHref(dest.workflow)} onclick={(e) => follow(e, deckHref(dest.workflow))}>{rec.action.label}</a>
      {:else}
        <span class="fine">Start it from the Control Deck on your phone or in the desktop app.</span>
      {/if}
    {:else}
      <span class="fine">Open it in its workspace.</span>
    {/if}
    {#if note && (dest !== null) && !(dest && 'workflow' in dest && !onworkflow && !deckReachable)}<span class="fine">{note}</span>{/if}
  </div>

  <details class="why">
    <summary>Why this</summary>
    {#if rec.applied.length}
      <p class="lab">What shaped it</p>
      <ul class="facts">
        {#each rec.applied as c (c.code + (c.ref ?? ''))}
          <li><span class="chip" data-tier={c.tier}>{TIER_LABEL[c.tier]}</span> {c.message}{#if c.ref} <span class="mm">({c.ref})</span>{/if}</li>
        {/each}
      </ul>
    {/if}
    <p class="lab">What it rests on</p>
    {#each groupedByTier(rec.evidence) as group (group.tier)}
      <ul class="facts">
        {#each group.items as e, i (group.tier + ':' + i)}
          <li>
            <span class="chip" data-tier={e.tier}>{group.label}</span> {e.label}
            <span class="mm">{evidenceMeta(e)}</span>
          </li>
        {/each}
      </ul>
    {/each}
    <p class="fine">{CONFIDENCE_LABEL[rec.confidence]}. Habits and feedback only choose between ways that policy, permissions and the repository already allow. {feedbackNote(storedIn)}</p>
  </details>

  <div class="fb" role="group" aria-label="Was this useful?">
    {#if given === 'useful'}
      <p class="fine" role="status">Noted as useful.</p>
    {:else}
      <button class="btn small quiet" type="button" disabled={busy} onclick={() => say('useful')}>Useful</button>
      <button class="btn small quiet" type="button" disabled={busy} onclick={() => say('not_useful')}>Not useful</button>
      <button class="btn small quiet" type="button" disabled={busy} onclick={() => say('dismissed')}>Dismiss</button>
    {/if}
  </div>
  {#if problem}<p class="fine problem" role="alert">It is hidden here, but could not be saved to your memory: {problem}</p>{/if}
</article>

<style>
  .rec {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 14px 16px;
    min-width: 0;
  }
  .rec[data-basis='required'] {
    border-color: color-mix(in srgb, var(--warn) 50%, var(--border));
  }
  .kick {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px 8px;
    line-height: 1.3;
  }
  .basis {
    margin-left: auto;
    letter-spacing: 0.04em;
    text-transform: none;
    font-size: 11.5px;
  }
  .title {
    font-size: 15px;
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .detail {
    font-size: 13px;
    color: var(--text-2);
    overflow-wrap: anywhere;
  }
  .acts {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px 12px;
  }
  .fine {
    font-size: 12px;
    line-height: 1.5;
    color: var(--text-2);
  }
  .problem {
    color: var(--danger-text);
  }
  .why {
    font-size: 12.5px;
  }
  .why summary {
    cursor: pointer;
    color: var(--text-2);
    min-height: 28px;
    display: flex;
    align-items: center;
  }
  .why summary:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
    border-radius: 2px;
  }
  .why .lab {
    margin: 10px 0 6px;
  }
  .facts {
    list-style: none;
    padding: 0;
    margin: 0 0 6px;
    display: grid;
    gap: 6px;
  }
  .facts li {
    overflow-wrap: anywhere;
    line-height: 1.5;
  }
  .facts .mm {
    display: block;
    margin-top: 1px;
  }
  .chip {
    display: inline-block;
    padding: 0 6px;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--surface-2);
    font-family: var(--mono);
    font-size: 11.5px;
    line-height: 18px;
    color: var(--text-2);
    white-space: nowrap;
  }
  .chip[data-tier='1'],
  .chip[data-tier='2'] {
    border-color: color-mix(in srgb, var(--warn) 55%, var(--border));
    color: var(--warn-text);
  }
  .fb {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 6px;
    border-top: 1px solid var(--border);
    margin-top: 2px;
    padding-top: 8px;
  }
</style>
