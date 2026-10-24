<script lang="ts">
  import type { Entry } from './types';
  let { workspace }: { workspace: Entry } = $props();
</script>

<!-- Individual is blue, a Team is violet, wherever a workspace is named across the window. -->
<span class="tag" data-kind={workspace.kind} title={workspace.kind === 'personal' ? 'Your own Werkbord on this computer' : 'A Team workspace'}>
  <span class="dot" aria-hidden="true"></span>{workspace.kind === 'personal' ? 'Individual' : workspace.name}
</span>

<style>
  .tag {
    --kind: var(--accent);
    display: inline-flex;
    align-items: center;
    gap: 6px;
    max-width: 180px;
    padding: 1px 8px 1px 7px;
    border: 1px solid color-mix(in srgb, var(--kind) 55%, var(--border));
    border-radius: 999px;
    background: color-mix(in srgb, var(--kind) 12%, var(--surface));
    color: var(--text);
    font-family: var(--mono);
    font-size: 11.5px;
    letter-spacing: 0.02em;
    overflow: hidden;
    text-overflow: ellipsis;
    vertical-align: middle;
    white-space: nowrap;
  }
  .tag[data-kind='team'] {
    --kind: var(--block);
  }
  .dot {
    flex: none;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--kind);
  }
</style>
