<script lang="ts">
  import { displayWorkspaceIcon, workspaceIconCells } from './icons';
  let { icon = 'auto', name = '', size = 16 }: { icon?: string; name?: string; size?: number } = $props();
  const cells = $derived(workspaceIconCells(icon, name));
</script>

<svg class="workspace-icon" width={size} height={size} viewBox="0 0 16 16" fill="currentColor" aria-hidden="true" data-workspace-icon={displayWorkspaceIcon(icon, name)}>
  {#each cells as cell (cell)}<rect x={1.5 + cell % 5 * 2.6} y={1.5 + Math.floor(cell / 5) * 2.6} width="2" height="2" rx="0.35" />{/each}
</svg>

<style>
  .workspace-icon { display: inline-block; flex: none; vertical-align: middle; }
</style>
