<script lang="ts">
  import { agentName } from './format';
  import { app } from './state.svelte';
  import { budgetText, budgetsFrom, type UsageSettings } from './usage';
  import { usageStore } from './usage.svelte';
  import UsageMeter from './UsageMeter.svelte';

  // Usage: how Werkbord measures what your agents use, and the one thing it can do about it. A provider does not say what is left of a
  // plan, so the budgets are yours: roughly what you can use in a window before you would want to slow down. What is shown is what is
  // saved plus only what you have changed, so agents that load late, or a save from elsewhere, are never overwritten by a stale copy.
  type Field = 'fiveHour' | 'weekly';
  let modeEdit = $state<'standard' | 'saver' | null>(null);
  let saverEdit = $state<number | null>(null);
  let edits = $state<Record<string, Partial<Record<Field, string>>>>({});
  let busy = $state(false);
  let saved = $state(false);
  let error = $state('');
  $effect(() => { void usageStore.refresh(true); });
  const current = $derived(usageStore.view?.settings);
  const ids = $derived([...new Set([...app.agents.filter(a => a.installed).map(a => a.id), ...Object.keys(current?.budgets ?? {})])]);
  const value = (id: string, k: Field) => edits[id]?.[k] ?? budgetText(current?.budgets[id]?.[k]);
  const mode = $derived(modeEdit ?? current?.mode ?? 'standard');
  const saverAt = $derived(saverEdit ?? current?.saverAt ?? 80);
  const parsed = $derived(budgetsFrom(Object.fromEntries(ids.map(id => [id, { fiveHour: value(id, 'fiveHour'), weekly: value(id, 'weekly') }]))));
  const dirty = $derived(!!current && (current.mode !== mode || current.saverAt !== saverAt || JSON.stringify(current.budgets) !== JSON.stringify(parsed.budgets)));
  const noBudget = $derived(mode === 'saver' && Object.keys(parsed.budgets).length === 0);
  const type = (id: string, k: Field, v: string) => { edits[id] = { ...edits[id], [k]: v }; saved = false; };
  async function save() {
    if (busy || parsed.problem) return;
    busy = true; error = ''; saved = false;
    const body: UsageSettings = { mode, saverAt: Number(saverAt), budgets: parsed.budgets };
    try { await usageStore.save(body); edits = {}; modeEdit = null; saverEdit = null; saved = true; }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = false; }
  }
</script>

<section class="pn" data-testid="usage-settings">
  <h3>Usage</h3>
  <p class="muted">Werkbord adds up the tokens that runs it started reported, for the last five hours and the last seven days, for each agent. It cannot see what a provider has left of your plan, and it does not include work you do in a provider's own app. Percent is of the budget you set here.</p>
  <UsageMeter bare />
  {#if !current && !usageStore.error}<p class="muted" role="status">Loading…</p>{:else}
  <fieldset class="modes"><legend>Mode</legend>
    <label><input type="radio" name="usage-mode" value="standard" checked={mode === 'standard'} onchange={() => { modeEdit = 'standard'; saved = false; }} />Standard<span class="muted">Measure and show. Nothing is held back.</span></label>
    <label><input type="radio" name="usage-mode" value="saver" checked={mode === 'saver'} onchange={() => { modeEdit = 'saver'; saved = false; }} />Usage saver<span class="muted">When an agent reaches the share of its budget below, work that would start by itself on that agent waits: scheduled tasks and plan steps, and automatic fallback does not move work onto it. Anything you start yourself is never held. It does not change models or reasoning.</span></label>
  </fieldset>
  <label class="at">Hold automatic work at <input class="input at-input" type="number" min="50" max="99" value={saverAt} oninput={(e) => { saverEdit = Number(e.currentTarget.value); saved = false; }} disabled={mode !== 'saver'} aria-label="Saver threshold, percent of a budget" /> % of a budget</label>
  <table class="budgets"><caption class="visually-hidden">Budgets in tokens</caption>
    <thead><tr><th scope="col">Agent</th><th scope="col">Per 5 hours</th><th scope="col">Per week</th></tr></thead>
    <tbody>
      {#each ids as id (id)}
        <tr><th scope="row">{agentName(app.agents, id)}</th>
          <td><input class="input" value={value(id, 'fiveHour')} oninput={(e) => type(id, 'fiveHour', e.currentTarget.value)} placeholder="No budget" inputmode="text" aria-label="{agentName(app.agents, id)} budget per 5 hours, in tokens" /></td>
          <td><input class="input" value={value(id, 'weekly')} oninput={(e) => type(id, 'weekly', e.currentTarget.value)} placeholder="No budget" inputmode="text" aria-label="{agentName(app.agents, id)} budget per week, in tokens" /></td></tr>
      {:else}<tr><td colspan="3" class="muted">No agents are installed.</td></tr>{/each}
    </tbody>
  </table>
  <p class="muted small">Write a budget as 2M, 500k or 2,000,000 tokens (input plus output; cached tokens are not added). Leave it empty for no budget. Pick numbers from what you have seen your plan allow: a budget you set too high never holds anything.</p>
  {#if noBudget}<p class="note" role="note">Usage saver needs at least one budget to act on. Until you set one it only measures.</p>{/if}
  {#if parsed.problem}<p class="error" role="alert">{parsed.problem}</p>{/if}
  {#if error || usageStore.error}<p class="error" role="alert">{error || usageStore.error}</p>{/if}
  <div class="row"><button class="btn primary" disabled={busy || !dirty || !!parsed.problem} onclick={save}>{busy ? 'Saving…' : 'Save'}</button>{#if saved && !dirty}<span class="ok" role="status">Saved{current && Object.keys(current.budgets).length ? ` · ${Object.keys(current.budgets).length === 1 ? 'one budget' : `${Object.keys(current.budgets).length} budgets`}` : ''}</span>{/if}</div>
  {/if}
</section>

<style>
  .modes { border: 1px solid var(--border); border-radius: 6px; padding: 10px 14px; display: grid; gap: 10px; margin: 14px 0 12px; }
  .modes legend { font-weight: 500; padding: 0 6px; }
  .modes label { display: grid; grid-template-columns: auto 1fr; gap: 2px 10px; align-items: start; }
  .modes label .muted { grid-column: 2; font-size: 13px; }
  .at-input { width: 5rem; }
  .row { display: flex; align-items: center; gap: 12px; }
  .at { display: flex; align-items: center; gap: 8px; margin-bottom: 14px; }
  .budgets { width: 100%; border-collapse: collapse; margin-bottom: 8px; }
  .budgets th, .budgets td { text-align: left; padding: 6px 8px 6px 0; font-weight: 400; }
  .budgets thead th { font-size: 12px; color: var(--text-2); }
  .budgets tbody th { font-weight: 500; white-space: nowrap; }
  .note { padding: 8px 12px; border: 1px solid var(--border); border-radius: 6px; background: var(--surface-2); font-size: 13px; }
  .small { font-size: 12px; }
</style>
