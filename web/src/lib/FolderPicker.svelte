<script lang="ts">
  import { api } from './api';
  import { callDesktop } from './desktop';
  import { desktop } from './desktop.svelte';
  import Icon from './Icon.svelte';
  import Sheet from './Sheet.svelte';

  let { value = $bindable(''), disabled = false }: { value?: string; disabled?: boolean } = $props();
  let open = $state(false);
  let busy = $state(false);
  let error = $state('');
  let listing = $state<Awaited<ReturnType<typeof api.folders>> | null>(null);
  let filter = $state('');
  const shown = $derived((listing?.folders ?? []).filter(f => f.name.toLowerCase().includes(filter.toLowerCase())));

  async function browse(path = '') {
    busy = true; error = ''; filter = '';
    try { listing = await api.folders(path); }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = false; }
  }
  async function choose() {
    if (desktop.available) {
      try { const path = await callDesktop<string>('ChooseDirectory', [], 0); if (path) value = path; return; }
      catch { /* Older desktop apps use the controller's folder browser. */ }
    }
    open = true;
    await browse(value);
  }
</script>

<button class="btn" type="button" {disabled} onclick={choose}><Icon name="folder" />Choose folder…</button>
{#if open}
  <Sheet title="Choose a folder" onclose={() => open = false}>
    <p class="muted">Folders on the computer running Werkbord. Choose your Git repository.</p>
    {#if listing}
      <div class="location">
        <button class="btn small" disabled={busy || listing.parent === listing.path} onclick={() => browse(listing!.parent)}><Icon name="left" />Up</button>
        <code>{listing.path}</code>
      </div>
      <label class="field">Find a folder<input class="input" type="search" bind:value={filter} placeholder="Filter folders" /></label>
    {/if}
    {#if error}<p class="error" role="alert">{error}</p><button class="btn" onclick={() => browse()}>Open home folder</button>{/if}
    <ul class="folders" aria-busy={busy}>
      {#each shown as f (f.path)}
        <li><button disabled={busy} onclick={() => browse(f.path)}><Icon name={f.repository ? 'git' : 'folder'} /><span>{f.name}</span>{#if f.repository}<span class="chip">Git</span>{/if}<Icon name="right" /></button></li>
      {:else}<li class="muted">{busy ? 'Loading folders…' : 'No folders here.'}</li>{/each}
    </ul>
    <div class="actions"><button class="btn" onclick={() => open = false}>Cancel</button><button class="btn primary" disabled={busy || !listing} onclick={() => { value = listing!.path; open = false; }}>Use this folder</button></div>
  </Sheet>
{/if}

<style>
  .location, .actions { display: flex; gap: 12px; align-items: center; }
  .location code { overflow-wrap: anywhere; min-width: 0; }
  .field { display: grid; gap: 4px; }
  .folders { list-style: none; padding: 0; margin: 0; max-height: 40vh; overflow-y: auto; scrollbar-gutter: stable; }
  .folders button { display: flex; align-items: center; width: 100%; padding: 12px; gap: 12px; border: 0; border-bottom: 1px solid var(--border); background: transparent; text-align: left; color: var(--text); }
  .folders button:hover { background: var(--surface-2); }
  .folders button span:first-of-type { flex: 1; }
  .actions { justify-content: end; }
</style>
