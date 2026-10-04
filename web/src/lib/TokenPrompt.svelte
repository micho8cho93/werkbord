<script lang="ts">
  import { setToken } from './api';
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
    <h1>Connect to your controller</h1>
    <p class="muted">
      This controller requires an access token. On the computer running it, run <code>devboard token</code> to print
      it, or <code>devboard token --url</code> for a link that signs this browser in.
    </p>
    <label for="token" class="visually-hidden">Access token</label>
    <input
      id="token"
      class="input mono"
      type="password"
      autocomplete="off"
      placeholder="Access token"
      bind:value={token}
    />
    <button class="btn primary" type="submit" disabled={!token.trim()}>Connect</button>
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
    padding: 20px;
  }
</style>
