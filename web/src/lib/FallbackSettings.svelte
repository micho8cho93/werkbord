<script lang="ts">
  import { api } from './api';
  import { agentName, timeAgo } from './format';
  import { app } from './state.svelte';
  import { fallbackLine, fallbackProblem, orderedChoices, type FallbackSettings, type FallbackView } from './fallback';

  let { projectId }: { projectId: string } = $props();
  let view = $state<FallbackView | null>(null);
  let draft = $state<FallbackSettings>({ enabled: false, providers: [], maxSwitches: 1, dailyLimit: 3 });
  let busy = $state(false);
  let error = $state('');
  let saved = $state(false);
  const take = (v: FallbackView) => { view = v; draft = { enabled: v.enabled, providers: [...v.providers], maxSwitches: v.maxSwitches, dailyLimit: v.dailyLimit }; };
  $effect(() => {
    let live = true;
    view = null;
    api.fallback(projectId).then(v => { if (live) take(v); }, e => { if (live) error = e.message; });
    return () => { live = false; };
  });
  const choices = $derived(orderedChoices(app.agents.filter(a => a.available || draft.providers.includes(a.id)).map(a => a.id), draft.providers));
  const name = (id: string) => agentName(app.agents, id);
  const problem = $derived(fallbackProblem(draft));
  const dirty = $derived(!!view && JSON.stringify({ e: draft.enabled, p: draft.providers, m: draft.maxSwitches, d: draft.dailyLimit }) !== JSON.stringify({ e: view.enabled, p: view.providers, m: view.maxSwitches, d: view.dailyLimit }));
  function toggle(id: string) { draft.providers = draft.providers.includes(id) ? draft.providers.filter(p => p !== id) : [...draft.providers, id]; }
  function move(id: string, by: number) {
    const list = [...draft.providers]; const i = list.indexOf(id);
    if (i < 0 || i + by < 0 || i + by >= list.length) return;
    [list[i], list[i + by]] = [list[i + by], list[i]]; draft.providers = list;
  }
  async function save() {
    if (problem || busy) return;
    busy = true; error = ''; saved = false;
    try { take(await api.setFallback(projectId, $state.snapshot(draft))); saved = true; }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = false; }
  }
</script>

<section class="pn" data-testid="fallback-settings">
  <h3>Automatic fallback</h3>
  <p class="muted">When an agent's run has ended because the provider rate-limited it or ran out of allowance, Werkbord can continue the work with another agent. It is off unless you turn it on here.</p>
  {#if !view && !error}<p class="muted" role="status">Loading…</p>{:else}
  <label class="check"><input type="checkbox" bind:checked={draft.enabled} />Continue failed runs with another agent</label>
  <fieldset disabled={!draft.enabled}><legend>Agents that may take over, in order</legend>
    {#each choices as id (id)}
      {@const on = draft.providers.includes(id)}
      <div class="pick"><label><input type="checkbox" checked={on} onchange={() => toggle(id)} />{name(id)}</label>
        {#if on}<span class="order"><button class="btn quiet small" type="button" aria-label="Move {name(id)} earlier" disabled={draft.providers.indexOf(id) === 0} onclick={() => move(id, -1)}>Up</button><button class="btn quiet small" type="button" aria-label="Move {name(id)} later" disabled={draft.providers.indexOf(id) === draft.providers.length - 1} onclick={() => move(id, 1)}>Down</button></span>{/if}
      </div>
    {:else}<p class="muted">No agents are installed.</p>{/each}
  </fieldset>
  <div class="caps">
    <label>Switches per task<input class="input narrow" type="number" min="1" max="3" bind:value={draft.maxSwitches} disabled={!draft.enabled} /></label>
    <label>Switches per day, in this project<input class="input narrow" type="number" min="1" max="20" bind:value={draft.dailyLimit} disabled={!draft.enabled} /></label>
  </div>
  <ul class="rules muted small">
    <li>Only a run that has already ended, never a session that is still open. Stop or wait for those yourself.</li>
    <li>Only when the controller read the work's Git state cleanly and there is recorded work to continue from. Otherwise it does nothing and says why below.</li>
    <li>The caps count starts, not money: what a provider bills is not known here. Each continuation bills to that agent's own account.</li>
    <li>Each failed run is considered once. If it is not continued, you can continue it yourself from the task.</li>
  </ul>
  {#if problem && draft.enabled}<p class="error" role="alert">{problem}</p>{/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  <div class="row"><button class="btn primary" disabled={busy || !dirty || !!problem} onclick={save}>{busy ? 'Saving…' : 'Save'}</button>{#if saved && !dirty}<span class="ok" role="status">Saved</span>{/if}</div>
  {#if view?.ledger.length}
    <h4>Recent</h4>
    <ul class="ledger">{#each [...view.ledger].reverse().slice(0, 10) as e (e.runId)}<li><span>{fallbackLine(e, name)}</span><span class="muted small">{timeAgo(e.at, app.now)}</span></li>{/each}</ul>
  {/if}
  {/if}
</section>

<style>
  .check{display:flex;gap:10px;align-items:center;margin:12px 0;}fieldset{border:1px solid var(--border);border-radius:6px;padding:10px 14px;display:grid;gap:6px;margin:0 0 12px;}legend{font-weight:500;padding:0 6px;}
  .pick{display:flex;justify-content:space-between;align-items:center;gap:10px;min-height:32px;}.pick label{display:flex;gap:10px;align-items:center;}.order{display:flex;gap:4px;}
  .caps{display:flex;gap:20px;flex-wrap:wrap;margin-bottom:12px;}.caps label{display:grid;gap:6px;}.rules{padding-left:18px;display:grid;gap:4px;margin:0 0 14px;}
  .ledger{list-style:none;margin:6px 0 0;padding:0;display:grid;gap:8px;}.ledger li{display:flex;justify-content:space-between;gap:12px;}h4{margin:18px 0 0;font-size:13px;}
</style>
