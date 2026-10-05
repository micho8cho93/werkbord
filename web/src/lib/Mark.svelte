<script lang="ts" module>
  // The pixel mark: three slanted bars (ink, cobalt, amber), eight rows each, one flat shade
  // per row. The light ramps sit on paper; the dark ramps on ink. `light-dark()` follows the
  // page's color-scheme, so the mark changes with the theme without any script.
  const LIGHT = [
    ['#3A3A38', '#333331', '#2C2C2B', '#252524', '#1F1F1E', '#181817', '#111111', '#0A0A0A'],
    ['#5B7BFF', '#5272FC', '#4A6AF9', '#4161F6', '#3959F3', '#3050F0', '#2848ED', '#1F3FEA'],
    ['#FFCB6B', '#FCC35E', '#F8BB51', '#F5B344', '#F2AA36', '#EFA229', '#EB9A1C', '#E8920F'],
  ];
  const DARK = [
    ['#FFFFFF', '#F7F7F6', '#EEEEEC', '#E6E6E3', '#DDDDDA', '#D5D5D1', '#CCCCC7', '#C4C4BE'],
    ['#A3B4FF', '#99ACFF', '#8EA4FF', '#849CFF', '#7A93FF', '#708BFF', '#6583FF', '#5B7BFF'],
    ['#FFD582', '#FDCF78', '#FBC86E', '#F9C264', '#F8BC59', '#F6B64F', '#F4AF45', '#F2A93B'],
  ];
  const CELLS = [0, 1, 2].flatMap((bar) =>
    Array.from({ length: 8 }, (_, row) => {
      const x = 36 * (bar + 1) - 12 * Math.floor(row / 2);
      const fill = `light-dark(${LIGHT[bar][row]}, ${DARK[bar][row]})`;
      return [
        { x, y: 12 * row, fill },
        { x: x + 12, y: 12 * row, fill },
      ];
    }).flat(),
  );
</script>

<script lang="ts">
  let { height = 22, label = '' }: { height?: number; label?: string } = $props();
</script>

<svg
  class="mark"
  viewBox="0 0 130 94"
  height={height}
  width={(height * 130) / 94}
  role={label ? 'img' : undefined}
  aria-label={label || undefined}
  aria-hidden={label ? undefined : 'true'}
>
  {#each CELLS as c, i (i)}<rect x={c.x} y={c.y} width="10" height="10" style:fill={c.fill} />{/each}
</svg>

<style>
  .mark {
    display: block;
    flex: none;
  }
</style>
