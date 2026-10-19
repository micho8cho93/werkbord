<script lang="ts">
  import { onDestroy } from 'svelte';
  import { api } from '../api';
  import QrImage from '../QrImage.svelte';
  import type { PhoneEnrollment } from '../types';
  import { countdown } from './phones';

  /** "Add a phone": a QR code for one phone, good for five minutes. It carries a single-use code, never the access token. */
  let { onenrolled }: { onenrolled?: () => void } = $props();

  let code = $state<PhoneEnrollment | null>(null);
  let busy = $state(false);
  let error = $state('');
  let now = $state(Date.now());
  let before = 0;
  let timer: ReturnType<typeof setInterval> | undefined;

  const left = $derived(code ? countdown(code.expiresAt, now) : '');
  const expired = $derived(code ? new Date(code.expiresAt).getTime() <= now : false);

  function stop() {
    if (timer) clearInterval(timer);
    timer = undefined;
  }

  async function start() {
    busy = true;
    error = '';
    try {
      before = (await api.phones()).length;
      const made = await api.createPhoneCode();
      if (!made.qrSvg) throw new Error('There is no way for a phone to reach this computer: the phone relay is turned off and no private network is connected. Remove relay.disabled from config.json and restart Werkbord.');
      code = made;
      now = Date.now();
      stop();
      timer = setInterval(() => {
        now = Date.now();
        if (code && new Date(code.expiresAt).getTime() <= now) stop();
        else void watch();
      }, 1000);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  let checking = false;
  /** A phone that scanned the code shows up in the list: the code is spent, so the picture goes. */
  async function watch() {
    if (checking || Math.floor(now / 1000) % 3 !== 0) return;
    checking = true;
    try {
      if ((await api.phones()).length > before) {
        stop();
        code = null;
        onenrolled?.();
      }
    } catch {
      // The next tick tries again.
    } finally {
      checking = false;
    }
  }

  function close() {
    stop();
    code = null;
  }

  onDestroy(stop);
</script>

{#if code}
  <div class="add" data-testid="phone-enroll">
    <QrImage svg={code.qrSvg} size={176} label="QR code that adds your phone to Werkbord" />
    <div class="side">
      {#if expired}
        <p class="muted" role="status">This code has expired.</p>
        <div class="row"><button class="btn small primary" disabled={busy} onclick={start}>Make a new one</button><button class="btn small quiet" onclick={close}>Close</button></div>
      {:else}
        <p>
          Scan with your phone's camera. It opens Werkbord there, signed in{code.via === 'relay' ? '. No app is needed on the phone' : ''}.
        </p>
        <p class="muted small" role="timer" data-testid="phone-countdown">Works for one phone, for <span class="mono">{left}</span> more. It never contains your access token.</p>
        <div class="row"><button class="btn small quiet" onclick={close}>Cancel</button></div>
      {/if}
    </div>
  </div>
{:else}
  <div class="row"><button class="btn small primary" disabled={busy} onclick={start}>{busy ? 'Making a code…' : 'Add a phone'}</button></div>
{/if}
{#if error}<p class="error" role="alert">{error}</p>{/if}

<style>
  .add {
    display: flex;
    gap: 16px;
    flex-wrap: wrap;
    align-items: flex-start;
  }

  .side {
    display: grid;
    gap: 8px;
    max-width: 300px;
  }

  p {
    margin: 0;
  }

  .row {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .small {
    font-size: 0.8rem;
  }
</style>
