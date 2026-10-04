<script lang="ts">
  let { name }: { name: 'board' | 'control' | 'git' | 'activity' | 'projects' } = $props();
</script>

<svg
  width="22"
  height="22"
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="1.8"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
>
  {#if name === 'board'}
    <rect x="3" y="4" width="5" height="16" rx="1.5" />
    <rect x="10" y="4" width="5" height="11" rx="1.5" />
    <rect x="17" y="4" width="4" height="7" rx="1.5" />
  {:else if name === 'control'}
    <circle cx="12" cy="12" r="8" />
    <path d="M12 8v4l2.5 2.5" />
  {:else if name === 'activity'}
    <path d="M3 12h4l3-7 4 14 3-7h4" />
  {:else if name === 'projects'}
    <rect x="3" y="3" width="8" height="8" rx="2" />
    <rect x="13" y="3" width="8" height="8" rx="2" />
    <rect x="3" y="13" width="8" height="8" rx="2" />
    <rect x="13" y="13" width="8" height="8" rx="2" />
  {:else}
    <circle cx="6" cy="6" r="2.2" />
    <circle cx="6" cy="18" r="2.2" />
    <circle cx="18" cy="9" r="2.2" />
    <path d="M6 8.2v7.6M18 11.2c0 3.5-4 3.8-10 5.4" />
  {/if}
</svg>
