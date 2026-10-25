<script lang="ts">
  import { onMount } from 'svelte';
  import { ask } from './native';
  import { plan } from './plan.svelte';

  // Free or Premium. There is no account and no licence: Premium is one unlock key, pasted here once, kept on this computer, and
  // given to each Team workspace you belong to. It lets you be in a team of three or more people; everything else is free.
  let key = $state('');
  const status = $derived(plan.status);

  onMount(() => void plan.refresh());

  async function unlock(e: SubmitEvent) {
    e.preventDefault();
    if (await plan.unlock(key)) key = '';
  }
</script>

<div class="wrap">
  <section class="pn" aria-labelledby="plan-h">
    <div class="head">
      <h3 id="plan-h">Your plan</h3>
      <p class="muted" data-testid="plan-current">
        {#if status.active}
          You are on <strong>Premium</strong>{status.holder ? ` (key for ${status.holder})` : ''}. You can create or join teams of any size.
        {:else}
          You are on the <strong>Free</strong> plan. Everything for working on your own is free, and so is a team of two people: you and a partner.
        {/if}
      </p>
    </div>
    <div class="plans">
      <div class="card" class:cur={!status.active}>
        <h4>Free {#if !status.active}<span class="chip" data-tone="ok">Your plan</span>{/if}</h4>
        <ul>
          <li>Unlimited personal projects, runs, Git and Salie</li>
          <li>Create or join teams of up to 2 people</li>
          <li>Home and Calendar across all your work</li>
          <li class="no">Teams of 3 or more people</li>
        </ul>
      </div>
      <div class="card" class:cur={status.active}>
        <h4>Premium <span class="chip">one-time</span> {#if status.active}<span class="chip" data-tone="ok">Your plan</span>{/if}</h4>
        <ul>
          <li>Everything in Free</li>
          <li>Create or join teams of any size</li>
          <li>As many teams as you need</li>
          <li>Works offline: there is no account or server to reach</li>
        </ul>
        {#if status.buyUrl}
          <button class="btn" type="button" onclick={() => ask((a) => a.OpenExternal(status.buyUrl))}>Get Premium</button>
        {:else if !status.active}
          <p class="muted small" data-testid="plan-not-on-sale">Premium is not on sale yet. If you were given an unlock key, enter it below.</p>
        {/if}
      </div>
    </div>
  </section>

  <section class="pn" aria-labelledby="key-h">
    <div class="head">
      <h3 id="key-h">Unlock key</h3>
      <p class="muted">
        Paste the key you were given. It is checked in this app and kept on this computer, and each team you are in checks it again and
        keeps only its number. It never expires.
      </p>
    </div>
    {#if !status.canCheck}
      <p class="note" role="status" data-testid="plan-cannot-check">This build of Werkbord cannot check unlock keys. Install the release from Werkbord’s download page to use one.</p>
    {/if}
    <form class="row" onsubmit={unlock}>
      <label class="field">
        <span class="sr">Unlock key</span>
        <input class="mono" name="unlock-key" placeholder="wbpremium-…" autocomplete="off" spellcheck="false" bind:value={key} disabled={plan.busy || !status.canCheck} />
      </label>
      <button class="btn primary" type="submit" disabled={plan.busy || !key.trim() || !status.canCheck}>{status.active ? 'Replace key' : 'Unlock Premium'}</button>
    </form>
    {#if plan.error}<p class="error" role="alert" data-testid="plan-error">{plan.error}</p>{/if}
  </section>
</div>

<style>
  .wrap {
    display: grid;
    gap: 12px;
    max-width: 760px;
    padding: 20px 24px 32px;
  }
  .head {
    display: grid;
    gap: 4px;
    margin-bottom: 10px;
  }
  .plans {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
    gap: 12px;
  }
  .card {
    display: grid;
    align-content: start;
    gap: 8px;
    padding: 14px 16px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
  }
  .card.cur {
    box-shadow: inset 0 0 0 1.5px var(--text);
  }
  h4 {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    margin: 0;
    font-size: 15px;
    font-weight: 600;
  }
  ul {
    display: grid;
    gap: 6px;
    margin: 0;
    padding: 0;
    list-style: none;
    font-size: 13px;
  }
  li::before {
    content: '✓ ';
    color: var(--ok-text, var(--text));
  }
  li.no {
    color: var(--text-2);
  }
  li.no::before {
    content: '– ';
    color: var(--text-2);
  }
  .chip {
    padding: 1px 8px;
    border-radius: 99px;
    background: var(--surface-2);
    color: var(--text-2);
    font-size: 12px;
    font-weight: 500;
  }
  .chip[data-tone='ok'] {
    color: var(--ok-text, var(--text));
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
  }
  .field {
    flex: 1 1 280px;
  }
  .field input {
    width: 100%;
    padding: 8px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface);
    color: var(--text);
    font-size: 12.5px;
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  .small {
    font-size: 12.5px;
  }
  .error {
    margin: 0;
    color: var(--danger-text, var(--danger));
  }
</style>
