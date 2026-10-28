<script lang="ts">
  import Sheet from '../lib/Sheet.svelte';
  import { ask } from './native';
  import { model } from './model.svelte';
  import { mountProductMap, type MapSnapshot } from '../../../internal/uipresentation/static/product-map.js';
  import '../../../internal/uipresentation/static/product-map.css';
  let { workspace, project, name, onclose }: { workspace: string; project: string; name: string; onclose: () => void } = $props();
  let local = $state(''); let busy = $state(false); let error = $state(''); let reviewed = $state(false); let result = $state('');
  let preview = $state<{ token: string; snapshot: MapSnapshot } | null>(null);
  let viewer = $state<HTMLDivElement>();
  const projects = $derived(model.overview?.entries.find(e => e.workspace.id === 'personal')?.summary?.projects ?? []);
  $effect(() => { if (viewer && preview) return mountProductMap(viewer, preview.snapshot, [preview.snapshot]); });
  async function read() { busy = true; error = ''; try { preview = await ask(a => a.PreviewProductMapShare(local, workspace, project)); } catch (e) { error = (e as Error).message; } finally { busy = false; } }
  async function share() { if (!preview || !reviewed) return; busy = true; try { result = await ask(a => a.ConfirmProductMapShare(preview!.token)); } catch (e) { error = (e as Error).message; } finally { busy = false; } }
</script>
<Sheet title="Product Map · {name}" onclose={() => { if (!busy) onclose(); }} width="70rem" layer={70}>
  <div class="share-map">
    {#if result}<p>Map proposed. A project owner or reviewer can review and publish it for the team.</p><button class="btn primary" onclick={() => { model.goTo(workspace, `?tab=product-map&project=${project}`); onclose(); }}>Open Team Product Map</button>
    {:else if preview}
      <p>Review exactly what will be shared with <strong>{name}</strong>: feature descriptions, scenarios, relationships and source references.</p>
      <p class="muted">{preview.snapshot.repository} · {preview.snapshot.commit.slice(0, 12)}</p>
      <div bind:this={viewer}></div>
      <label><input type="checkbox" bind:checked={reviewed} /> I reviewed this snapshot and want to propose it to this Team project.</label>
      <div class="actions"><button class="btn" onclick={() => { preview = null; reviewed = false; }} disabled={busy}>Back</button><button class="btn primary" onclick={() => { void share(); }} disabled={busy || !reviewed}>Share map…</button></div>
    {:else}
      <p>Analyse this repository with your own connected agent, then share a reviewed snapshot. Other members can read the published map without another scan.</p>
      <label>Local repository project<select bind:value={local}><option value="">Choose a Personal project</option>{#each projects as p (p.id)}<option value={p.id}>{p.name}</option>{/each}</select></label>
      <div class="actions"><button class="btn" disabled={!local} onclick={() => { model.goTo('personal', `#/p/${local}/product-map`); onclose(); }}>Generate or update locally</button><button class="btn primary" disabled={!local || busy} onclick={() => { void read(); }}>Review saved map for sharing</button></div>
      <p class="muted">The local map must belong to the same repository. No Team service receives a local repository path or agent credentials.</p>
    {/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </div>
</Sheet>
<style>
  .share-map {display:flex;flex-direction:column;gap:16px;} p {margin:0;line-height:1.5;}.muted{color:var(--text-2);font-size:13px;}.actions{display:flex;flex-wrap:wrap;gap:8px;}label{display:flex;gap:8px;align-items:center;flex-wrap:wrap;}select{padding:8px;font:inherit;background:var(--surface);color:var(--text);border:1px solid var(--border);border-radius:var(--radius-sm);}.error{color:var(--danger-text);}
</style>
