<script lang="ts">
  import { onMount } from 'svelte';
  import { app } from './state.svelte';
  import { desktop } from './desktop.svelte';
  import { bindingLabel, connectProblem, connectionSummary, deviceSetup, normalizeDeviceStatus, type DeviceStatus } from './device-setup';

  let status = $state<DeviceStatus>(normalizeDeviceStatus(undefined));
  let loaded = $state(false);
  let error = $state('');
  let busy = $state('');
  let controller = $state('');
  let name = $state('');
  let code = $state('');
  let runner = $state(false);
  let runnerCode = $state('');

  const working = $derived(status.job.running || busy !== '');
  const problem = $derived(connectProblem({ controller, name, code, runner, runnerCode }, status.clientPaired));
  const projectName = (id: string) => app.projects.find((p) => p.id === id)?.name ?? id;

  async function refresh() {
    try { status = await deviceSetup.status(); error = ''; }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { loaded = true; }
  }
  onMount(() => {
    void refresh();
    // While a change is running the app moves the services; ask often, otherwise rarely.
    const timer = setInterval(() => { if (document.visibilityState === 'visible') void refresh(); }, 2000);
    return () => clearInterval(timer);
  });

  async function run(kind: string, action: () => Promise<unknown>) {
    busy = kind; error = '';
    try { await action(); code = ''; runnerCode = ''; await refresh(); }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = ''; }
  }
  const connect = () => run('connect', () => deviceSetup.connect({ controller, name, code, runner, runnerCode }));
  const useLocal = () => run('local', () => deviceSetup.useLocal());
  const bind = (project: string) => run(project, () => deviceSetup.bind(project));
</script>

{#if desktop.available}
  <section class="pn device-setup" aria-labelledby="device-setup-title">
    <div class="heading"><h3 id="device-setup-title">Connect to existing Werkbord</h3></div>
    {#if !loaded}<p class="muted" role="status">Checking this computer…</p>{/if}
    {#if loaded}<p data-testid="device-summary">{connectionSummary(status)}</p>{/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    {#if status.job.running}
      <p role="status" class="muted">{status.job.action === 'local' ? 'Switching back…' : status.job.action === 'bind' ? 'Saving the clone…' : 'Connecting…'} Do not quit Werkbord; this keeps running if you close the window.</p>
    {:else if status.job.phase === 'failed'}
      <p class="error" role="alert">{status.job.error || 'That did not finish.'} Nothing on this computer was deleted. Codes work once and expire after five minutes, so ask the other computer for new ones before trying again.</p>
    {:else if status.job.phase === 'done' && status.job.action !== 'bind'}
      <p role="status" class="muted">Done.</p>
    {/if}

    {#if status.mode === 'remote'}
      <div class="stack">
        <p class="muted">The board, history and approvals live on the other computer. If it cannot be reached, this window waits instead of falling back to a different board.</p>
        <div><button class="btn" disabled={working} onclick={useLocal}>{busy === 'local' ? 'Switching back…' : 'Use this computer’s Werkbord'}</button></div>
        <p class="muted">Switching back waits until agents and terminals started from the other computer have finished here, then restores this computer’s own board as you left it.</p>
      </div>
    {:else}
      <form class="stack" onsubmit={(e) => { e.preventDefault(); if (!problem && !working) void connect(); }}>
        <p class="muted">On the other computer open Settings → Runners and choose <strong>Pair desktop</strong>. It shows an address and a code that works once for five minutes.</p>
        <label>Address of the other Werkbord<input class="input" bind:value={controller} placeholder="https://name.tailnet.ts.net:8443" autocomplete="off" spellcheck="false" /></label>
        <label>Desktop pairing code<input class="input" type="password" bind:value={code} autocomplete="off" spellcheck="false" placeholder={status.clientPaired ? 'Not needed again: this computer is already paired' : ''} /></label>
        <label>Name of this computer<input class="input" bind:value={name} maxlength="120" placeholder="Laptop" /></label>
        <label class="check"><input type="checkbox" bind:checked={runner} />Also let that Werkbord run agents on this computer</label>
        {#if runner}
          <label>Runner pairing code<input class="input" type="password" bind:value={runnerCode} autocomplete="off" spellcheck="false" placeholder={status.runnerPaired ? 'Not needed again: this runner is already paired' : 'From Add runner on the other computer'} /></label>
          <p class="muted">Running agents uses this computer’s own Git and agent sign-ins. Controlling and running are paired and revoked separately.</p>
        {/if}
        <div><button class="btn primary" type="submit" disabled={working || !!problem}>{busy === 'connect' ? 'Connecting…' : 'Connect'}</button></div>
        {#if problem && (controller || code)}<p class="muted" role="status">{problem}</p>{/if}
        <p class="muted">Work that is running here has to finish first. This computer’s own board is kept, not deleted, and you can switch back at any time.</p>
      </form>
    {/if}

    {#if status.runner || status.runnerPaired}
      <div class="stack">
        <h4>Where each project lives on this computer</h4>
        {#each status.projects as id (id)}
          <div class="binding">
            <div><strong>{projectName(id)}</strong><p class="muted path">{bindingLabel(status.bindings[id])}</p></div>
            <button class="btn small" disabled={working} onclick={() => bind(id)}>{busy === id ? 'Choosing…' : status.bindings[id] ? 'Change clone' : 'Choose clone'}</button>
          </div>
        {:else}<p class="muted">The other computer has not authorized any project for this runner yet.</p>{/each}
        <p class="muted">Agents work in a separate worktree of that clone. Werkbord never commits, pushes or copies your files unless you confirm it.</p>
      </div>
    {/if}

    <details class="why">
      <summary>If something goes wrong</summary>
      <dl>
        <dt>The other computer is unreachable</dt>
        <dd>Work changes, new runs and Git on that machine need it, so they are unavailable until it returns. Nothing is retried for you and this computer never starts a board of its own.</dd>
        <dt>Agents already running here</dt>
        <dd>They keep running while the connection is down, and what they report is kept and delivered when it returns. No new work starts while this computer’s permission has expired.</dd>
        <dt>Stop, finish or reply while a runner is offline</dt>
        <dd>The command waits and is delivered once the runner reconnects; it is shown as pending until it is acknowledged.</dd>
        <dt>A computer was lost or revoked</dt>
        <dd>Its access ends at once. Work that may still be running stays reserved until you have checked that machine and confirmed it stopped; nothing is released by a timer. The controller’s Settings → Runners lists that work under “Work held on revoked machines”.</dd>
        <dt>A pairing code failed</dt>
        <dd>Codes work once. Ask the other computer for a new code; nothing is saved from a failed attempt that would block trying again.</dd>
      </dl>
    </details>
  </section>
{/if}

<style>
  .device-setup { padding: 16px; display: grid; gap: 16px; }
  .heading { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 12px; }
  h3 { font-size: 16px; font-weight: 600; } h4 { margin: 0; font-size: 13px; }
  p { margin: 4px 0; font-size: 13px; }
  .stack { display: grid; gap: 12px; }
  label { display: grid; gap: 6px; font-size: 13px; font-weight: 500; }
  .check { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .binding { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding-top: 12px; border-top: 1px solid var(--border); flex-wrap: wrap; }
  .path { overflow-wrap: anywhere; font-family: var(--mono); font-size: 12px; }
  summary { cursor: pointer; font-size: 13px; font-weight: 500; color: var(--text-2); padding-block: 6px; } summary:hover { color: var(--text); }
  dl { display: grid; gap: 4px; margin: 8px 0 0; font-size: 13px; } dt { font-weight: 600; margin-top: 8px; } dd { margin: 0; color: var(--text-2); }
</style>
