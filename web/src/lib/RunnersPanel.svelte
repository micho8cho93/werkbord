<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from './api';
  import { app } from './state.svelte';
  import type { Pairing, Runner } from './types';

  let runners = $state<Runner[]>([]);
  let loading = $state(true);
  let error = $state('');
  let busy = $state('');
  let adding = $state(false);
  let projects = $state<string[]>([]);
  let clone = $state(false);
  let pairing = $state<Pairing | null>(null);
  let copied = $state(false);
  let now = $state(Date.now());
 let expanded = $state<string[]>([]);
  const expired = $derived(pairing ? now >= Date.parse(pairing.expiresAt) : false);
  const size = (n?: number) => n === undefined ? 'Unknown' : `${(n / 1024 ** 3).toFixed(1)} GB`;

  async function refresh() {
    try { runners = await api.listRunners(); app.runners = runners; error = ''; }
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
    if (!confirm(`Revoke ${r.name}? Its credential is fenced immediately. Active capacity releases after 120 seconds. Inspect any work on that machine before running the task again.`)) return;
    busy = r.id;
    try { await api.revokeRunner(r.id); await refresh(); }
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
    try { await navigator.clipboard.writeText(`devboard join ${pairing.code}${pairing.allowClone ? ' --allow-clone' : ''}`); copied = true; }
    catch { error = 'Select the join command and copy it manually.'; }
  }
</script>

<section class="pn runners" aria-labelledby="runners-title">
  <div class="heading"><div><h3 id="runners-title">Runners</h3><p class="muted">Your machines. Each uses its own Git and agent sign-ins.</p></div><button class="btn" onclick={() => (adding = !adding)}>{adding ? 'Close pairing' : 'Add runner'}</button></div>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if adding}
    <div class="pairing">
      <h4>Pair another machine</h4>
      <p class="muted">Select the projects it may execute. The code expires in five minutes and can be used once. Sign in to the same private network on that machine.</p>
      <fieldset><legend>Authorized projects</legend>
        {#each app.projects as p (p.id)}<label class="check"><input type="checkbox" bind:group={projects} value={p.id} />{p.name}</label>{/each}
        {#if !app.projects.length}<p class="muted">Add a project before pairing a runner.</p>{/if}
      </fieldset>
      <label class="check"><input type="checkbox" bind:checked={clone} />Allow cloning configured Git remotes</label>
      <button class="btn primary" disabled={!!busy || !projects.length} onclick={pair}>{busy === 'pair' ? 'Generating…' : 'Generate pairing code'}</button>
      {#if pairing}
        <label for="runner-join">Run on the other machine</label>
        <textarea id="runner-join" class="input" rows="3" readonly value={`devboard join ${pairing.code}${pairing.allowClone ? ' --allow-clone' : ''}`}></textarea>
        <div class="heading"><span role="status" class="muted">{expired ? 'Expired. Generate a new code.' : `Expires at ${new Date(pairing.expiresAt).toLocaleTimeString()}`}</span><button class="btn small" disabled={expired} onclick={copy}>{copied ? 'Copied' : 'Copy command'}</button></div>
        <p class="muted">For an existing clone, run <code>devboard runner repo &lt;project-id&gt; &lt;clone-path&gt;</code>. Project IDs appear below.</p>
      {/if}
    </div>
  {/if}
  {#if loading}<p class="muted" role="status">Loading runners…</p>{/if}
  {#each runners as r (r.id)}
    <div class="runner">
      <div class="heading"><strong>{r.name}</strong><span class="state" data-online={r.online && !r.disabled}>{r.disabled ? 'Disabled' : !r.online ? 'Offline' : r.currentRuns ? 'Online' : 'Idle'} · {r.currentRuns}/{r.capacity}</span></div>
      <p class="muted">{r.os}/{r.arch} · {(r.capabilities?.agents ?? []).filter(a=>a.available).map(a=>a.name).join(', ') || 'No available agents'}{r.kind === 'local' ? ' · Controller machine' : ''}</p>
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
          <div class="heading"><button class="btn primary" disabled={!!busy} onclick={() => save(r)}>{busy === r.id ? 'Saving…' : 'Save runner'}</button>{#if r.kind === 'remote'}<button class="btn" disabled={!!busy || r.currentRuns > 0} onclick={() => remove(r)}>Remove runner</button><button class="btn" disabled={!!busy} onclick={() => revoke(r)}>Revoke lost runner</button>{/if}</div>
          <dl><dt>CPU</dt><dd>{r.capabilities?.cpu || 'Unknown'} logical cores</dd><dt>RAM / available</dt><dd>{size(r.capabilities?.ramBytes)} / {size(r.capabilities?.availableRamBytes)}</dd><dt>Free storage</dt><dd>{size(r.capabilities?.storageBytes)}</dd><dt>Last seen</dt><dd>{r.kind === 'local' ? 'This controller is online' : r.lastSeenAt.startsWith('0001') ? 'Waiting for first heartbeat' : new Date(r.lastSeenAt).toLocaleString()}</dd></dl>
          {#if r.capabilities?.diagnostics}<p class="error">{r.capabilities.diagnostics}</p>{/if}
          {#each r.capabilities?.agents ?? [] as a (a.id)}<p class="muted">{a.name} {a.version ?? ''} · {a.available ? 'Available' : a.detail || 'Unavailable'}</p>{/each}
          {#if r.kind === 'remote'}<p class="muted">An offline runner keeps ownership of its runs. Reconnect it to stop work; removal is available after every run ends. For a permanently lost machine, revoke its identity and wait for the fence window.</p>{/if}
        </div>
      </details>
    </div>
  {/each}
</section>

<style>
  .runners { padding: 16px; display: grid; gap: 16px; }
  .heading { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 12px; }
  h3 { font-size: 15px; font-weight: 600; } h4 { margin: 0; }
  p { margin: 4px 0; font-size: 13px; }
  .runner { padding-top: 16px; border-top: 1px solid var(--border); display: grid; gap: 6px; }
  .state { font-family: var(--mono); font-size: 11.5px; font-variant-numeric: tabular-nums; color: var(--text-2); } .state[data-online='true'] { color: var(--ok-text); }
  .pairing, .controls { display: grid; gap: 12px; } .pairing { padding-block: 12px; }
  summary { cursor: pointer; font-size: 13px; font-weight: 500; color: var(--text-2); padding-block: 6px; } summary:hover { color: var(--text); }
  .controls { padding-top: 12px; } label { display: grid; gap: 6px; font-size: 13px; font-weight: 500; }
  .check { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  fieldset { border: 0; padding: 0; display: grid; gap: 8px; min-width: 0; } legend { margin-bottom: 8px; font-weight: 600; }
  dl { display: grid; grid-template-columns: minmax(100px, 1fr) 2fr; gap: 8px; font-size: .85rem; } dt { color: var(--text-2); } dd { margin: 0; overflow-wrap: anywhere; }
  textarea { overflow-wrap: anywhere; font-family: var(--mono, monospace); } code { overflow-wrap: anywhere; font-size: .8rem; }
</style>
