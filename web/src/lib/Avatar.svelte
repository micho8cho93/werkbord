<script lang="ts">
  import { avatarTone, initials } from './identity';

  // A person as initials on a tinted disc. No photo and no upload; the tone follows the member id, and the viewer's own disc is cobalt.
  let { id, name, self = false, size = 'md' }: { id: string; name: string; self?: boolean; size?: 'sm' | 'md' | 'lg' } = $props();
</script>

<span class="wb-avatar" data-size={size} data-tone={avatarTone(id)} data-self={self ? '' : undefined} role="img" aria-label={self ? name + ' (you)' : name}>{initials(name)}</span>
