<script lang="ts">
  import { untrack } from 'svelte';
  import Icon from '../Icon.svelte';
  import TransferStatus from '../TransferStatus.svelte';
  import { acknowledge, surface, visibility } from '../motion';
  import { clock, size } from './words';
  import type { Staged } from './store.svelte';

  let { file, onremove }: { file: Staged; onremove: () => void } = $props();
  let previous = untrack(() => file.state);
  let arrived = $state(false);
  $effect(() => {
    const next = file.state;
    const fresh = next === 'ready' && previous !== next;
    previous = next;
    if (fresh) {
      arrived = true;
      const timer = setTimeout(() => arrived = false, 550);
      return () => { clearTimeout(timer); arrived = false; };
    }
  });
</script>

<li class="file" data-state={file.state} aria-busy={file.state === 'uploading'} use:visibility use:acknowledge={{ value: file.state, success: file.state === 'ready' }} in:surface={{ duration: 120, y: 0 }}>
  <span class="icon">{#if arrived}<TransferStatus state="done" />{:else}<Icon name="attach" size={13} />{/if}</span>
  <span class="fn">{file.name}</span>
  <span class="sz" role="status">
    {#if file.state === 'uploading'}Adding…{:else if file.state === 'error'}{file.error}{:else if file.state === 'expired'}Kept too long, removed{:else}{size(file.bytes)}{file.expiresAt ? ` · until ${clock(file.expiresAt)}` : ''}{/if}
  </span>
  <button class="x" type="button" onclick={onremove} aria-label="Remove {file.name}"><Icon name="close" size={12} /></button>
</li>

<style>
  .file { position: relative; display: grid; grid-template-columns: 16px minmax(0, 1fr) auto 26px; align-items: center; gap: 6px; max-width: 100%; min-height: 32px; padding: 0 4px 0 10px; border: 1px solid var(--border); border-radius: 999px; background: var(--surface); font-size: 13px; overflow: hidden; }
  .icon { display: grid; place-items: center; width: 16px; height: 18px; color: var(--text-2); }
  .fn { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sz { min-width: 0; color: var(--text-2); font-size: 12px; overflow-wrap: anywhere; }
  .file[data-state='error'], .file[data-state='expired'] { border-radius: 10px; }
  .file[data-state='error'] .sz, .file[data-state='expired'] .sz { grid-row: 2; grid-column: 2 / 4; padding-bottom: 6px; color: var(--danger-text); }
  .x { grid-column: 4; grid-row: 1; display: inline-grid; place-items: center; width: 26px; height: 26px; border: 0; border-radius: 50%; background: none; color: var(--text-2); }
  .x:hover { background: var(--surface-2); color: var(--text); }
  @media (prefers-reduced-motion: no-preference) {
    .file[data-state='uploading']::after { content: ''; position: absolute; left: 34px; right: 34px; bottom: 2px; height: 2px; background: linear-gradient(90deg, transparent 0 34%, var(--accent) 34% 66%, transparent 66%); background-size: 300% 100%; animation: attachment-current 1.2s linear infinite; }
  }
  @keyframes attachment-current { from { background-position: 100% 0; } to { background-position: 0 0; } }
  @media (pointer: coarse) { .file { min-height: 44px; grid-template-columns: 16px minmax(0, 1fr) auto 44px; } .x { width: 44px; height: 44px; } }
</style>
