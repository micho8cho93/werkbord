<script lang="ts">
  import { projectColor, projectInitial } from './projects';

  let { id, name, size = 28 }: { id: string; name: string; size?: number } = $props();
</script>

<span class="avatar" style:--c={projectColor(id)} style:--s="{size}px" aria-hidden="true">{projectInitial(name)}</span>

<style>
  .avatar {
    flex: none;
    display: inline-grid;
    place-items: center;
    width: var(--s);
    height: var(--s);
    border-radius: 8px;
    background: var(--c);
    color: #fff;
    font-size: calc(var(--s) * 0.5);
    font-weight: 700;
    line-height: 1;
    text-shadow: 0 1px 1px rgb(0 0 0 / 0.25);
  }
</style>
