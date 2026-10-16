<script lang="ts">
  import { onMount } from 'svelte';
  import { api, ApiError, setToken } from '../api';
  import { formatDateTime } from '../clock.svelte';
  import type { PhoneEntry, RelayStatus } from '../types';
  import { isPhone } from './capability';
  import PhoneEnroll from './PhoneEnroll.svelte';
  import { lastSeen } from './phones';

  /** Settings → Phones: who can reach this Werkbord from a phone, when they were last seen, and a way to take any away. */
  let phones = $state<PhoneEntry[] | null>(null);
  let relay = $state<RelayStatus | null>(null);
  let error = $state('');
  let busy = $state('');
  let now = $state(Date.now());
  let editing = $state('');
  let draft = $state('');
  /** This browser is itself an enrolled phone: it may see and sign out itself, and nothing else here. */
  let self = $state<PhoneEntry | null>(null);

  async function refresh() {
    try {
      if (isPhone()) {
        self = await api.phoneSelf();
        return;
      }
      phones = await api.phones();
      now = Date.now();
      error = '';
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        try {
          self = await api.phoneSelf();
          error = '';
          return;
        } catch {
          // fall through to the plain error
        }
      }
      error = err instanceof Error ? err.message : String(err);
    }
  }

  onMount(() => {
    void refresh();
    if (!isPhone()) {
      void api.relay().then((r) => (relay = r)).catch(() => {});
    }
    const t = setInterval(() => {
      if (document.visibilityState === 'visible' && !self) void refresh();
    }, 10000);
    return () => clearInterval(t);
  });

  async function revoke(p: PhoneEntry) {
    if (!confirm(`Revoke ${p.name}? It is signed out at once and cannot reach Werkbord again until you add it with a new code.`)) return;
    busy = p.id;
    try {
      await api.revokePhone(p.id);
      await refresh();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = '';
    }
  }

  async function rename(p: PhoneEntry) {
    busy = p.id;
    try {
      await api.renamePhone(p.id, draft.trim());
      editing = '';
      await refresh();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = '';
    }
  }

  async function signOut() {
    if (!confirm('Sign this phone out of Werkbord? You will need a new code from your computer to come back.')) return;
    try {
      await api.signOutSelf();
    } catch {
      // Signed out already, or unreachable: either way this browser should forget its token.
    }
    setToken('');
    location.reload();
  }
</script>

<section class="pn" aria-labelledby="phones-title">
  <div class="head">
    <h3 id="phones-title">Phones</h3>
    {#if self}
      <p class="muted">This phone is signed in to Werkbord by its own code. Phones are managed on your computer.</p>
    {:else}
      <p class="muted">
        A phone you add can see your work, answer agents, use the Assistant, run configured workflows and use Git actions. General settings, adding repositories and managing runners stay on this computer.
        Each phone has its own sign-in that you can take away.
      </p>
    {/if}
  </div>

  {#if self}
    <div class="phone" data-testid="phone-self">
      <div class="who"><strong>{self.name}</strong><span class="muted small">added {formatDateTime(self.createdAt)}</span></div>
      <button class="btn small" onclick={signOut}>Sign out this phone</button>
    </div>
  {:else}
    {#if phones === null && !error}
      <p class="muted">Checking…</p>
    {:else if phones && phones.length === 0}
      <p class="muted" data-testid="phones-empty">No phone yet.</p>
    {/if}
    {#if phones?.length}
      <ul class="list" data-testid="phones-list">
        {#each phones as p (p.id)}
          <li class="phone" data-phone={p.id}>
            {#if editing === p.id}
              <form class="rename" onsubmit={(e) => (e.preventDefault(), rename(p))}>
                <input class="input" aria-label="Name of this phone" maxlength="60" bind:value={draft} />
                <button class="btn small primary" type="submit" disabled={busy === p.id || !draft.trim()}>Save</button>
                <button class="btn small quiet" type="button" onclick={() => (editing = '')}>Cancel</button>
              </form>
            {:else}
              <div class="who">
                <strong>{p.name}</strong>
                <span class="muted small">added {formatDateTime(p.createdAt)} · last seen {lastSeen(p.lastUsedAt, now)}</span>
              </div>
              <div class="acts">
                <button class="btn small quiet" onclick={() => ((editing = p.id), (draft = p.name))}>Rename</button>
                <button class="btn small" disabled={busy === p.id} onclick={() => revoke(p)}>Revoke</button>
              </div>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
    {#if relay && !relay.enabled}
      <p class="muted">Phones reach this computer through a relay in your own Cloudflare account, and it is not set up yet. Add its addresses to <code>config.json</code> as <code>relay.url</code> and <code>relay.connectorUrl</code>, then restart Werkbord. The steps are in <code>docs/RELAY.md</code>.</p>
    {:else if relay && !relay.connected}
      <p class="muted" role="status">Connecting to your relay…{relay.error ? ` ${relay.error}` : ''}</p>
    {:else}
      <PhoneEnroll onenrolled={refresh} />
    {/if}
  {/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}
</section>

<style>
  .head {
    display: grid;
    gap: 4px;
    margin-bottom: 8px;
  }

  .list {
    list-style: none;
    margin: 0 0 12px;
    padding: 0;
    display: grid;
    gap: 6px;
  }

  .phone {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
    padding: 8px 0;
    border-top: 1px solid var(--border);
  }

  .who {
    display: grid;
    gap: 2px;
    min-width: 0;
  }

  .acts,
  .rename {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    align-items: center;
  }

  .rename .input {
    min-width: 12ch;
  }

  .small {
    font-size: 0.8rem;
  }
</style>
