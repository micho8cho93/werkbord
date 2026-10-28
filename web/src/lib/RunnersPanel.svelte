<script lang="ts">
  import DeviceTerminals from './DeviceTerminals.svelte';
  import HeldRunnerWork from './HeldRunnerWork.svelte';
  import { onMount } from 'svelte';
  import { api } from './api';
  import { formatDateTime, formatTime } from './clock.svelte';
  import { runnerConnection } from './format';
  import { app } from './state.svelte';
  import { isPhone } from './phones/capability';
  import type { Pairing, Runner } from './types';

  let runners = $state<Runner[]>([]);
  let savedRunners = $state<Record<string, string>>({});
  const settingsOf = (r: Runner) => JSON.stringify([r.name, r.capacity, r.automatic, r.disabled, [...r.projects].sort(), r.allowClone]);
  let loading = $state(true);
  let error = $state('');
  let busy = $state('');
  let adding = $state(false);
  let projects = $state<string[]>([]);
  let clone = $state(false);
  let pairing = $state<Pairing | null>(null);
  let copied = $state(false);
  let now = $state(Date.now());
  let held = $state<{ refresh(): Promise<void> }>();
 let expanded = $state<string[]>([]);
  const expired = $derived(pairing ? now >= Date.parse(pairing.expiresAt) : false);
  const size = (n?: number) => n === undefined ? 'Unknown' : `${(n / 1024 ** 3).toFixed(1)} GB`;

  async function refresh() {
    try { runners = await api.listRunners(); savedRunners = Object.fromEntries(runners.map(r => [r.id, settingsOf(r)])); app.runners = runners; error = ''; }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { loading = false; }
  }
  onMount(() => {
    void refresh();
    const timer = setInterval(() => { now = Date.now(); if (!busy && expanded.length===0 && document.visibilityState === 'visible') void refresh(); }, 10000);
    return () => clearInterval(timer);
  });
  async function pair() {
    busy = 'pair'; error = ''; copied = false;
    try { pairing = await api.pairRunner(projects, clone); }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = ''; }
  }
  async function save(r: Runner) {
    busy = r.id;
    try { await api.saveRunner(r); await refresh(); }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = ''; }
  }
  async function revoke(r: Runner) {
    if (!confirm(`Revoke ${r.name}? Its credentials stop working immediately. Offline-capable runs remain reserved, and appear under “Work held on revoked machines” below, until you have inspected that machine and confirmed they stopped.`)) return;
    busy = r.id;
    try { await api.revokeRunner(r.id); await refresh(); await held?.refresh(); }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = ""; }
  }
  async function remove(r: Runner) {
    busy = r.id;
    try { await api.removeRunner(r.id); await refresh(); }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = ''; }
  }
  async function copy() {
    if (!pairing || expired) return;
    try { await navigator.clipboard.writeText(`werkbord join ${pairing.code}${pairing.allowClone ? ' --allow-clone' : ''}`); copied = true; }
    catch { error = 'Select the join command and copy it manually.'; }
  }
</script>

<section class="pn runners" aria-labelledby="runners-title">
  <div class="heading"><div><h3 id="runners-title">Runners</h3><p class="muted">Your machines. Each uses its own Git and agent sign-ins.</p></div>{#if !isPhone() && !app.remote}<button class="btn" onclick={() => (adding = !adding)}>{adding ? 'Close pairing' : 'Add runner'}</button>{/if}</div>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if adding}
    <div class="pairing">
      <h4>Pair another machine</h4>
      <p class="muted">Select the projects it may execute. The code expires in five minutes and can be used once. Sign in to the same private network on that machine.</p>
      <fieldset disabled={!!pairing && !expired}><legend>Authorized projects</legend>
        {#each app.projects as p (p.id)}<label class="check"><input type="checkbox" bind:group={projects} value={p.id} />{p.name}</label>{/each}
        {#if !app.projects.length}<p class="muted">Add a project before pairing a runner.</p>{/if}
      </fieldset>
      <label class="check"><input type="checkbox" bind:checked={clone} disabled={!!pairing && !expired} />Allow cloning configured Git remotes</label>
      <button class="btn primary" disabled={!!busy || !projects.length || !!pairing && !expired} onclick={pair}>{busy === 'pair' ? 'Generating…' : pairing && !expired ? 'Pairing code ready' : 'Generate pairing code'}</button>
      {#if pairing}
        <label for="runner-join">Run on the other machine</label>
        <textarea id="runner-join" class="input" rows="3" readonly value={`werkbord join ${pairing.code}${pairing.allowClone ? ' --allow-clone' : ''}`}></textarea>
        <div class="heading"><span role="status" class="muted">{expired ? 'Expired. Generate a new code.' : `Expires at ${formatTime(pairing.expiresAt)}`}</span><button class="btn small" disabled={expired} onclick={copy}>{copied ? 'Copied' : 'Copy command'}</button></div>
        <p class="muted">For an existing clone, run <code>werkbord runner repo &lt;project-id&gt; &lt;clone-path&gt;</code>. Project IDs appear below.</p>
      {/if}
    </div>
  {/if}
  {#if loading}<p class="muted" role="status">Loading runners…</p>{/if}
  {#each runners as r (r.id)}
    <div class="runner">
      <div class="heading"><strong>{r.name}</strong><span class="state" data-online={app.connection === 'live' && r.online && !r.disabled}>{runnerConnection(r, app.connection === 'live')}</span></div>
      <p class="muted">{r.os}/{r.arch} · {(r.capabilities?.agents ?? []).filter(a=>a.available).map(a=>a.name).join(', ') || 'No available agents'}{r.kind === 'local' ? ' · Controller' : r.capabilities?.offlineContinuation ? ' · Continues running offline' : ' · Stops after a lost controller lease'}</p>
      {#if !isPhone() && !app.remote}
      <details ontoggle={e=>{expanded=e.currentTarget.open?[...expanded.filter(id=>id!==r.id),r.id]:expanded.filter(id=>id!==r.id)}}>
        <summary>Manage and inspect {r.name}</summary>
        <div class="controls">
          <label>Name<input class="input" bind:value={r.name} maxlength="120" /></label>
          <label>Capacity<input class="input" type="number" min="1" max="128" bind:value={r.capacity} /></label>
          <label class="check"><input type="checkbox" bind:checked={r.automatic} />Allow automatic routing</label>
          <label class="check"><input type="checkbox" bind:checked={r.disabled} />Disable new work (remote active work will stop)</label>
          {#if r.kind === 'remote'}
            <fieldset><legend>Authorized projects</legend>{#each app.projects as p (p.id)}<label class="check"><input type="checkbox" bind:group={r.projects} value={p.id} />{p.name} <code>{p.id}</code></label>{/each}</fieldset>
            <label class="check"><input type="checkbox" bind:checked={r.allowClone} />Allow cloning (runner must also opt in)</label>
          {/if}
          <div class="heading"><button class="btn primary" disabled={!!busy || settingsOf(r) === savedRunners[r.id] || !r.name.trim() || !Number.isInteger(r.capacity) || r.capacity < 1 || r.capacity > 128} onclick={() => save(r)}>{busy === r.id ? 'Saving…' : 'Save runner'}</button>{#if r.kind === 'remote'}<button class="btn danger" disabled={!!busy || r.currentRuns > 0} onclick={() => remove(r)}>Remove runner</button><button class="btn danger" disabled={!!busy} onclick={() => revoke(r)}>Revoke lost runner</button>{/if}</div>
          <dl><dt>CPU</dt><dd>{r.capabilities?.cpu || 'Unknown'} logical cores</dd><dt>RAM / available</dt><dd>{size(r.capabilities?.ramBytes)} / {size(r.capabilities?.availableRamBytes)}</dd><dt>Free storage</dt><dd>{size(r.capabilities?.storageBytes)}</dd><dt>Last seen</dt><dd>{r.kind === 'local' ? 'Werkbord is online' : r.lastSeenAt.startsWith('0001') ? 'Waiting for first heartbeat' : formatDateTime(r.lastSeenAt)}</dd></dl>
          {#if r.capabilities?.diagnostics}<p class="error">{r.capabilities.diagnostics}</p>{/if}
          {#each (r.capabilities?.agents ?? []).filter(a=>a.installed || a.available) as a (a.id)}<p class="muted">{a.name} {a.version ?? ''} · {a.available ? 'Available' : a.detail || 'Unavailable'}</p>{/each}
          {#if r.kind === 'remote'}<p class="muted">An offline runner keeps ownership of its runs. Reconnect it to stop work; removal is available after every run ends. For a permanently lost machine, revoke its identity. This blocks its credentials immediately. Work that may still be running stays reserved, and nothing releases it on a timer: inspect that machine, then confirm it stopped before running the task again.</p>{/if}
        </div>
      </details>
      {/if}
    </div>
  {/each}
</section>
{#if !isPhone() && !app.remote}<HeldRunnerWork bind:this={held} />{/if}
<DeviceTerminals />

<style>
  .runners { padding: 16px; display: grid; gap: 16px; }
  .heading { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 12px; }
  h3 { font-size: 16px; font-weight: 600; } h4 { margin: 0; }
  p { margin: 4px 0; font-size: 13px; }
  .runner { padding-top: 16px; border-top: 1px solid var(--border); display: grid; gap: 6px; }
  .state { font-family: var(--mono); font-size: 12px; font-variant-numeric: tabular-nums; color: var(--text-2); } .state[data-online='true'] { color: var(--ok-text); }
  .pairing, .controls { display: grid; gap: 12px; } .pairing { padding-block: 12px; }
  summary { cursor: pointer; font-size: 13px; font-weight: 500; color: var(--text-2); padding-block: 6px; } summary:hover { color: var(--text); }
  .controls { padding-top: 12px; } label { display: grid; gap: 6px; font-size: 13px; font-weight: 500; }
  .check { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  fieldset { border: 0; padding: 0; display: grid; gap: 8px; min-width: 0; } legend { margin-bottom: 8px; font-weight: 600; }
  dl { display: grid; grid-template-columns: minmax(100px, 1fr) 2fr; gap: 8px; font-size: .85rem; } dt { color: var(--text-2); } dd { margin: 0; overflow-wrap: anywhere; }
  textarea { overflow-wrap: anywhere; font-family: var(--mono, monospace); } code { overflow-wrap: anywhere; font-size: .8rem; }
</style>
