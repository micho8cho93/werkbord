<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { request } from '../lib/api';
  import type { Project } from '../lib/types';
  import { mountProductMap, type MapSnapshot } from '../../../internal/uipresentation/static/product-map.js';
  import '../../../internal/uipresentation/static/product-map.css';
  let { project }: { project: Project } = $props();
  interface Job { id: string; state: string; stage: string; mode: string; reason?: string; filesRead: number; error?: string }
  interface View { disclosures: Record<string, boolean>; snapshot: MapSnapshot | null; history: MapSnapshot[]; jobs: Job[]; branch: string; commit: string; freshness: string; detail?: string; models: Record<string, { id: string; name: string }[]>; branches: string[]; providers: { id: string; name: string; available: boolean; detail?: string; auth?: string; usesApiKey?: boolean }[] }
  let view = $state<View | null>(null);
  let error = $state(''); let busy = $state(false); let provider = $state(''); let model = $state(''); let branch = $state(''); let consent = $state(false); let selectedVersion = $state(0);
  let consentProvider = '';
  $effect(() => { if (provider !== consentProvider) { consentProvider = provider; consent = false; } });
  $effect(() => { if (!view?.models[provider]?.some(m => m.id === model)) model = ''; });
  let viewer = $state<HTMLDivElement>();
  const running = $derived(view?.jobs.find(j => j.state === 'running'));
  const selected = $derived(view?.history.find(s => s.version === selectedVersion) ?? view?.snapshot);
  const engine = $derived(view?.providers.find(p => p.id === provider));
  const canAnalyse = $derived(!busy && !!engine?.available && !!view?.commit && view.branch === branch && (consent || !!view.disclosures?.[provider]));
  const labels: Record<string, string> = { preparing_source: 'Preparing committed source', discovering_features: 'Discovering features', analysing_scenarios: 'Analysing scenarios and relationships', validating_evidence: 'Validating source evidence', saving: 'Saving map', completed: 'Map saved', unchanged: 'No permitted source changes — no model calls', interrupted: 'Analysis interrupted' };
  const base = () => `/api/projects/${encodeURIComponent(project.id)}/product-map`;
  async function refresh() { try { view = await request<View>('GET', base() + (branch ? `?branch=${encodeURIComponent(branch)}` : '')); error = ''; if (!provider) provider = view.providers.find(p => p.available)?.id ?? ''; if (!branch) branch = view.branch; } catch (e) { error = (e as Error).message; } }
  async function start(mode: 'generate' | 'update' | 'rebuild') { busy = true; error = ''; try { await request('POST', base() + '/jobs', { mode, provider, model, branch, expectedVersion: view?.snapshot?.version ?? 0, expectedCommit: view?.commit, disclosure: consent }); selectedVersion = 0; await refresh(); } catch (e) { error = (e as Error).message; } finally { busy = false; } }
  async function cancel() { if (!running) return; busy = true; try { await request('POST', `${base()}/jobs/${encodeURIComponent(running.id)}/cancel`, {}); await refresh(); } catch (e) { error = (e as Error).message; } finally { busy = false; } }
  onMount(() => { void refresh(); const timer = setInterval(() => { if (!document.hidden) void refresh(); }, 5000); return () => clearInterval(timer); });
  const renderKey = $derived(selected?.id);
  $effect(() => { if (!viewer || !renderKey) return; const el = viewer; return untrack(() => selected ? mountProductMap(el, selected, view?.history) : undefined); });
</script>

<section class="map-page" aria-labelledby="product-map-title">
  <header><h1 id="product-map-title">Product Map</h1><p class="muted">Understand your codebase visually. Explore implemented features, behaviours and their relationships.</p></header>
  {#if error}<p class="error" role="alert">{error} <button class="btn small" onclick={() => { void refresh(); }}>Retry</button></p>{/if}
  {#if view}
    <details class="scan-settings" open={!view.snapshot}>
      <summary>Analysis settings{engine ? ` · ${engine.name}` : ''}{view.branch ? ` · ${view.branch}` : ''}</summary>
    <div class="configuration">
      <label>Analysis agent<select bind:value={provider} disabled={!!running}>{#each view.providers as p (p.id)}<option value={p.id} disabled={!p.available}>{p.name}{!p.available ? ' — unavailable' : ''}</option>{/each}</select></label>
      <label>Model <select bind:value={model} disabled={!!running}><option value="">Agent default</option>{#each view.models[provider] ?? [] as m (m.id)}{#if m.id}<option value={m.id}>{m.name}</option>{/if}{/each}</select></label>
      <label>Target branch <select bind:value={branch} onchange={() => { void refresh(); }} disabled={!!running}><option value="" disabled>Select a branch</option>{#each view.branches as name (name)}<option value={name}>{name}</option>{/each}</select></label>
    </div>
    {#if engine}<p class="muted">{engine.detail || `Uses your connected ${engine.name} runtime${engine.usesApiKey ? ' with API billing' : engine.auth ? ` (${engine.auth})` : ''}.`}</p>{/if}
    {#each view.providers.filter(p => !p.available) as p (p.id)}<p class="muted">{p.name}: {p.detail || 'Connect an eligible agent in Settings → Agents.'}</p>{/each}
    {#if view.commit}<p class="revision">{view.branch} · {view.commit.slice(0, 12)} · committed source only</p>{:else}<p class="muted">{view.detail || 'Select a committed target branch before generating a map.'}</p>{/if}
    {#if !view.disclosures?.[provider]}<label class="disclosure"><input type="checkbox" bind:checked={consent} /> Starting analysis sends permitted source-code context to the selected model provider. Account limits or API charges may apply. Credential files, repository instructions and uncommitted work are excluded.</label>{:else}<p class="muted">Analysis sends permitted source context to this provider using the disclosure you acknowledged for this repository. Account limits or API charges may apply.</p>{/if}
    </details>
    <div class="actions">
      {#if running}<p role="status">{labels[running.stage] ?? running.stage} · {running.filesRead} files read</p><button class="btn small" onclick={() => { void cancel(); }} disabled={busy}>Cancel analysis</button>
      {:else}
        {#if !view.snapshot}<button class="btn primary" onclick={() => { void start('generate'); }} disabled={!canAnalyse}>Generate Product Map</button>
        {:else}<span class="status">{view.freshness === 'current' ? 'Current as of the local target revision' : view.freshness === 'updates_available' ? 'Updates available' : 'Freshness unknown'}</span><button class="btn primary" onclick={() => { void start('update'); }} disabled={!canAnalyse}>Update Map</button><button class="btn" onclick={() => { void start('rebuild'); }} disabled={!canAnalyse}>Rebuild Map</button>{/if}
      {/if}
    </div>
    {#if !running && view.jobs[0] && view.jobs[0].state !== 'completed'}<p class="muted" role="status">{view.jobs[0].error || labels[view.jobs[0].stage] || view.jobs[0].state}{view.jobs[0].reason ? ` · ${view.jobs[0].reason}` : ''}</p>{/if}
    {#if view.snapshot}
      <label class="history">Recent snapshots<select bind:value={selectedVersion}><option value={0}>Latest · version {view.snapshot.version}</option>{#each view.history as s (s.version)}<option value={s.version}>Version {s.version} · {new Date(s.createdAt).toLocaleString()}</option>{/each}</select></label>
      {#if selected}<p class="muted">Version {selected.version} · {selected.provider} / {selected.model || 'provider default'} · {selected.filesRead} of {selected.filesAvailable} permitted files read · {selected.commit.slice(0, 12)}</p>{/if}
      <div bind:this={viewer}></div>
    {:else if !running}<p class="empty">Generate a map to explore this project. Nothing runs until you start analysis.</p>{/if}
  {:else if !error}<p class="muted">Loading Product Map…</p>{/if}
</section>
<style>
  .map-page {padding:24px;display:flex;flex-direction:column;gap:12px;max-width:1600px;margin:auto;}
  h1,p {margin:0;} h1 {font-size:24px;} header p {margin-top:6px;}
  .muted,.revision {color:var(--text-2);font-size:13px;line-height:1.5;} .revision {font-family:var(--mono);}
  .scan-settings {border-block:1px solid var(--border);padding-block:10px;}
  .scan-settings summary {cursor:pointer;font-size:13px;}
  .scan-settings[open] summary {margin-bottom:12px;}
  .scan-settings > p,.scan-settings > label {margin-top:10px;}
  .configuration,.actions {display:flex;align-items:center;flex-wrap:wrap;gap:12px;}
  .configuration label,.history {display:flex;flex-direction:column;gap:5px;font-size:13px;}
  .configuration select,.history select {font:inherit;color:var(--text);background:var(--surface);border:1px solid var(--border);border-radius:var(--radius-sm);padding:8px;}
  .disclosure {display:flex;align-items:flex-start;gap:8px;font-size:13px;line-height:1.5;max-width:85ch;}
  .disclosure input {margin-top:4px;} .status {font-size:13px;} .error {color:var(--danger-text);}
  .empty {padding:40px 0;color:var(--text-2);} .history {max-width:360px;}
  @media(max-width:899px){.map-page{padding:16px;}.configuration{align-items:stretch;}.configuration label{flex:1;min-width:160px;}.configuration select{font-size:16px;}}
</style>
