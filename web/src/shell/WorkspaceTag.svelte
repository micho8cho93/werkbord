<script lang="ts">
  import type { Entry } from './types';
  let { workspace }: { workspace: Entry } = $props();
</script>

<span class="tag" data-kind={workspace.kind} title={workspace.kind === 'personal' ? 'Your own Werkbord on this computer' : 'A Team workspace'}>{workspace.name}</span>

<style>
  .tag {
    display: inline-block;
    max-width: 180px;
    padding: 1px 7px;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: var(--surface-2);
    color: var(--text-2);
    font-family: var(--mono);
    font-size: 10.5px;
    letter-spacing: 0.02em;
    overflow: hidden;
    text-overflow: ellipsis;
    vertical-align: middle;
    white-space: nowrap;
  }
  .tag[data-kind='team'] {
    background: var(--tint);
    color: var(--text);
  }
</style>
