<script lang="ts" module>
  /** Every icon the interface draws: one 16px grid, one 1.4px stroke, round caps. */
  export type IconName =
    | 'control'
    | 'overview'
    | 'board'
    | 'calendar'
    | 'git'
    | 'activity'
    | 'runs'
    | 'projects'
    | 'folder'
    | 'runner'
    | 'settings'
    | 'defaults'
    | 'search'
    | 'plus'
    | 'left'
    | 'right'
    | 'down'
    | 'close'
    | 'sun'
    | 'moon'
    | 'play'
    | 'stop'
    | 'check'
    | 'reply'
    | 'merge'
    | 'copy'
    | 'more'
    | 'expand'
    | 'branch'
    | 'link'
    | 'grip';
</script>

<script lang="ts">
  let { name, size = 16 }: { name: IconName; size?: number } = $props();
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 16 16"
  fill="none"
  stroke="currentColor"
  stroke-width="1.4"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
>
  {#if name === 'control'}
    <rect x="2" y="2" width="5" height="5" rx="1" /><rect x="9" y="2" width="5" height="5" rx="1" /><rect x="2" y="9" width="5" height="5" rx="1" /><rect x="9" y="9" width="5" height="5" rx="1" />
  {:else if name === 'overview'}
    <rect x="2" y="2" width="12" height="12" rx="1.5" /><path d="M2 6.5h12M6.5 6.5V14" />
  {:else if name === 'board'}
    <rect x="2" y="2.5" width="3.2" height="11" rx="0.8" /><rect x="6.4" y="2.5" width="3.2" height="7.5" rx="0.8" /><rect x="10.8" y="2.5" width="3.2" height="5" rx="0.8" />
  {:else if name === 'calendar'}
    <rect x="2" y="3" width="12" height="11" rx="1.2" /><path d="M5 1.8v2.4M11 1.8v2.4M2 7h12" />
  {:else if name === 'git' || name === 'branch'}
    <circle cx="4.5" cy="3.5" r="1.5" /><circle cx="4.5" cy="12.5" r="1.5" /><circle cx="11.5" cy="5.5" r="1.5" /><path d="M4.5 5v6M11.5 7c0 2.5-3 2.8-7 4" />
  {:else if name === 'activity'}
    <path d="M1.8 8h2.6l2-5 3.2 10 2-5h2.6" />
  {:else if name === 'runs'}
    <path d="M2.5 4h11M2.5 8h11M2.5 12h7" /><circle cx="12.5" cy="12" r="1.2" />
  {:else if name === 'projects'}
    <rect x="2" y="2" width="5" height="5" rx="1" /><rect x="9" y="2" width="5" height="5" rx="1" /><rect x="2" y="9" width="5" height="5" rx="1" /><rect x="9" y="9" width="5" height="5" rx="1" />
  {:else if name === 'folder'}
    <path d="M2 4.5A1 1 0 0 1 3 3.5h3L7.5 5H13a1 1 0 0 1 1 1v5.5a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1z" />
  {:else if name === 'runner'}
    <rect x="2" y="2.5" width="12" height="8.5" rx="1" /><path d="M5.5 13.5h5M8 11v2.5" />
  {:else if name === 'settings' || name === 'defaults'}
    <path d="M2 4.5h7M12 4.5h2M2 11.5h2M7 11.5h7" /><circle cx="10.5" cy="4.5" r="1.5" /><circle cx="5.5" cy="11.5" r="1.5" />
  {:else if name === 'search'}
    <circle cx="7" cy="7" r="4.5" /><path d="M10.5 10.5L14 14" />
  {:else if name === 'plus'}
    <path d="M8 3v10M3 8h10" />
  {:else if name === 'left'}
    <path d="M10 3L5 8l5 5" />
  {:else if name === 'right'}
    <path d="M6 3l5 5-5 5" />
  {:else if name === 'down'}
    <path d="M4 6l4 4 4-4" />
  {:else if name === 'close'}
    <path d="M4 4l8 8M12 4l-8 8" />
  {:else if name === 'sun'}
    <circle cx="8" cy="8" r="3" /><path d="M8 1.5V3M8 13v1.5M1.5 8H3M13 8h1.5M3.4 3.4l1 1M11.6 11.6l1 1M3.4 12.6l1-1M11.6 4.4l1-1" />
  {:else if name === 'moon'}
    <path d="M13.5 9.5A5.5 5.5 0 0 1 6.5 2.5a5.5 5.5 0 1 0 7 7z" />
  {:else if name === 'play'}
    <path d="M4.5 3v10l8.5-5z" />
  {:else if name === 'stop'}
    <rect x="4" y="4" width="8" height="8" rx="1" />
  {:else if name === 'check'}
    <path d="M3 8.5l3 3 7-7" />
  {:else if name === 'reply'}
    <path d="M6.5 4L2.5 8l4 4" /><path d="M2.5 8H10a3.5 3.5 0 0 1 3.5 3.5V13" />
  {:else if name === 'merge'}
    <circle cx="4.5" cy="3.5" r="1.5" /><circle cx="4.5" cy="12.5" r="1.5" /><circle cx="11.5" cy="10.5" r="1.5" /><path d="M4.5 5v6M4.5 5c0 3 2.5 5.5 5.5 5.5" />
  {:else if name === 'copy'}
    <rect x="5" y="5" width="8.5" height="8.5" rx="1.2" /><path d="M3 10.5V3.8A1.3 1.3 0 0 1 4.3 2.5H10.5" />
  {:else if name === 'more'}
    <circle cx="3.5" cy="8" r="0.6" /><circle cx="8" cy="8" r="0.6" /><circle cx="12.5" cy="8" r="0.6" />
  {:else if name === 'expand'}
    <path d="M9.5 2.5h4v4M6.5 13.5h-4v-4M13.5 2.5L9 7M2.5 13.5L7 9" />
  {:else if name === 'link'}
    <path d="M9 2.5h4.5V7M13.5 2.5L7.5 8.5M12 9.5v3a1 1 0 0 1-1 1H3.5a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1h3" />
  {:else if name === 'grip'}
    <circle cx="6" cy="4" r="0.5" /><circle cx="10" cy="4" r="0.5" /><circle cx="6" cy="8" r="0.5" /><circle cx="10" cy="8" r="0.5" /><circle cx="6" cy="12" r="0.5" /><circle cx="10" cy="12" r="0.5" />
  {/if}
</svg>
