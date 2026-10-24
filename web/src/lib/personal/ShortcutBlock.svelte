<script lang="ts">
  // A button in a workspace that opens a place in the app: Projects, Analytics, the Calendar… A shortcut names a place from a
  // fixed list; the window the workspace is shown in decides whether it can open it, and says so when it cannot.
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import Icon from '../Icon.svelte';
  import { shortcutTargets, type ShortcutTarget } from './schema';
  import { workspaceSession } from './session.svelte';
  import { shortcutInfo } from './shortcuts';

  let { props }: ComponentRenderProps<{ target: string; label: string }> = $props();
  const session = workspaceSession();
  const target = $derived((shortcutTargets as readonly string[]).includes(props.target) ? (props.target as ShortcutTarget) : undefined);
  const info = $derived(target ? shortcutInfo[target] : undefined);
  const usable = $derived(!!target && !!session.navigate && session.navigable.includes(target));
</script>

{#if target && info}
  <button type="button" class="shortcut" disabled={!usable} onclick={() => session.navigate?.(target)} data-shortcut={target}
    title={usable ? undefined : 'This place cannot be opened from here.'}>
    <span class="glyph" aria-hidden="true"><Icon name={info.icon} size={18} /></span>
    <span class="text"><strong>{props.label || info.label}</strong><span>{usable ? info.about : 'Not available from here'}</span></span>
    <span class="go" aria-hidden="true"><Icon name="right" size={14} /></span>
  </button>
{/if}

<style>
  .shortcut { display: flex; align-items: center; gap: 12px; width: 100%; min-width: 0; padding: 12px 14px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); box-shadow: var(--card-sh); color: var(--text); font: inherit; text-align: left; cursor: pointer; transition: border-color 0.12s var(--ease), transform 0.12s var(--ease); }
  .shortcut:hover:not(:disabled) { border-color: var(--accent); transform: translateY(-1px); }
  .shortcut:active:not(:disabled) { transform: none; box-shadow: var(--press-sh); }
  .shortcut:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .shortcut:disabled { opacity: 0.6; cursor: default; }
  .glyph { flex: none; display: grid; place-items: center; width: 36px; height: 36px; border-radius: var(--radius-sm); background: var(--tint); color: var(--accent); }
  .text { display: grid; gap: 2px; flex: 1; min-width: 0; }
  .text strong { font-size: 14px; font-weight: 600; overflow-wrap: anywhere; }
  .text span { color: var(--text-2); font-size: 12.5px; }
  .go { flex: none; color: var(--text-2); }
  @media (max-width: 600px) { .shortcut { min-height: 56px; } }
</style>
