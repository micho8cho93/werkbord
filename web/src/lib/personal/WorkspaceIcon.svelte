<script lang="ts">
  import { displayWorkspaceIcon } from './icons';
  import { workspaceIconComponents } from './icon-components';
  let { icon = 'auto', name = '', size = 16 }: { icon?: string; name?: string; size?: number } = $props();
  const identifier = $derived(displayWorkspaceIcon(icon, name));
  const Glyph = $derived(workspaceIconComponents[identifier as keyof typeof workspaceIconComponents]);
</script>

<span class="workspace-icon" aria-hidden="true">
  <Glyph {size} strokeWidth={2.1} data-workspace-icon={identifier} />
</span>

<style>
  .workspace-icon { display: inline-flex; flex: none; vertical-align: middle; }
</style>
