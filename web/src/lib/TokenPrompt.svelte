<script lang="ts">
  import { setToken } from './api';
  import Mark from './Mark.svelte';
  import { enrollment } from './phones/enroll.svelte';
  import { app } from './state.svelte';

  let token = $state('');

  function submit(e: SubmitEvent) {
    e.preventDefault();
    setToken(token.trim());
    app.reconnect();
  }
</script>

<div class="wrap">
  <form class="card" onsubmit={submit}>
    <div class="brand"><Mark height={22} /><span class="wm">werkbord</span></div>
    <h1>Connect to Werkbord</h1>
    {#if enrollment.state === 'failed'}
      <p class="error" role="alert" data-testid="enroll-failed">{enrollment.error}</p>
    {:else}
      <p class="muted" data-testid="signed-out">
        On a phone, scan a new code: Settings → Phones on your computer. A phone that was revoked is signed out for good.
      </p>
    {/if}
    <p class="muted">
      On the computer running Werkbord, run <code>werkbord token</code> and paste the token here. Or run
      <code>werkbord token --url</code> to create a sign-in link for this browser.
    </p>
    <label for="token">Access token</label>
    <input
      id="token"
      class="input mono"
      type="password"
      autocomplete="off"
      placeholder="Access token"
      bind:value={token}
    />
    <button class="btn primary" type="submit" disabled={!token.trim()}>Sign in</button>
  </form>
</div>

<style>
  .wrap {
    min-height: 100dvh;
    display: grid;
    place-items: center;
    padding: 16px;
  }

  form {
    width: min(420px, 100%);
    display: grid;
    gap: 14px;
    padding: 24px;
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 6px;
  }

  .wm {
    font-family: var(--mono);
    font-weight: 600;
    font-size: 20px;
    letter-spacing: -0.06em;
  }

  p {
    font-size: 13px;
  }
</style>
