<script lang="ts">
  import { callDesktop } from './desktop';
  import { desktop } from './desktop.svelte';
  import { app } from './state.svelte';

  /** v1.2.3 as people say it: 1.2.3. */
  const say = (v: string) => v.replace(/^v/, '');

  let busy = $state(false);
  let failure = $state('');

  const offer = $derived(app.updateOffer);

  // The desktop app does the installing, on this computer, when its person asks: it shows its own
  // confirmation, then runs `werkbord update` (checksum, backup, restart, and a way back). A page in
  // a browser or on a phone cannot make the controller install anything, so it says how instead.
  async function updateNow() {
    busy = true;
    failure = '';
    try {
      // Waits as long as it takes; when it works the window is reloaded on the new version.
      const result = await callDesktop<{ ok: boolean; message: string }>('RequestUpdate', [], 0);
      if (!result.ok && result.message) failure = result.message;
    } catch (err) {
      failure = err instanceof Error ? err.message : 'The update did not run.';
    } finally {
      busy = false;
    }
  }
</script>

{#if offer && offer.latest}
  <div role="status" class="banner">
    <span class="text">
      <strong>Werkbord {say(offer.latest)} is available</strong>
      <span class="what">
        {#if failure}
          {failure}
        {:else if desktop.available}
          You have {say(offer.current)}. Your data is backed up first.
        {:else}
          You have {say(offer.current)}. On the computer that runs Werkbord, run <code>werkbord update</code> in a terminal.
        {/if}
      </span>
    </span>
    {#if desktop.available}
      <button class="btn primary" type="button" onclick={updateNow} disabled={busy}>{busy ? 'Updating…' : failure ? 'Try again' : 'Update now'}</button>
    {/if}
    <button class="btn" type="button" onclick={() => app.dismissUpdate(offer.latest!)} disabled={busy}>Later</button>
  </div>
{/if}

<style>
  /* The accent's quiet strip: information, not something that needs you (that is the amber one). */
  .banner {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 40px;
    padding: 6px 24px;
    background: color-mix(in srgb, var(--accent) 9%, var(--bg));
    border-bottom: 1px solid color-mix(in srgb, var(--accent) 30%, var(--border));
    color: var(--text);
    font-size: 13px;
  }

  .text {
    display: flex;
    align-items: baseline;
    gap: 10px;
    min-width: 0;
    flex: 1;
  }

  strong {
    flex: none;
    font-weight: 600;
  }

  .what {
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .what code {
    font-family: var(--mono);
    font-size: 12px;
  }

  .btn {
    flex: none;
  }

  @media (max-width: 899px) {
    .banner {
      padding: 8px 16px;
      flex-wrap: wrap;
    }

    .text {
      display: grid;
      gap: 0;
      flex-basis: 100%;
    }

    .what {
      font-size: 12px;
      white-space: normal;
    }
  }
</style>
