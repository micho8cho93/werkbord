<script lang="ts">
  import { projectColor, projectInitial } from './projects';

  let { id, name, size = 28 }: { id: string; name: string; size?: number } = $props();
</script>

<span class="avatar" style:--c={projectColor(id)} style:--s="{size}px" aria-hidden="true">{projectInitial(name)}</span>

<style>
  /* A project's mark: its initial, in mono, on a raised tile. Colour is kept for status. */
  .avatar {
    flex: none;
    display: inline-grid;
    place-items: center;
    width: var(--s);
    height: var(--s);
    border-radius: min(6px, calc(var(--s) * 0.22));
    background: var(--btn-bg);
    border: 1px solid var(--border);
    box-shadow: var(--btn-sh);
    color: var(--text);
    font-family: var(--mono);
    font-size: calc(var(--s) * 0.48);
    font-weight: 600;
    line-height: 1;
  }
</style>
