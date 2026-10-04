<script lang="ts">
  /** A QR code from the controller's own SVG, shown as an image so that nothing in it can run. */
  let { svg, size = 200, label = 'QR code' }: { svg: string; size?: number; label?: string } = $props();
  const src = $derived(`data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`);
</script>

<img {src} alt={label} width={size} height={size} class="qr" />

<style>
  .qr {
    display: block;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    /* The code is black on white whatever the theme: that is what a camera reads. */
    background: #fff;
    image-rendering: pixelated;
    max-width: 100%;
    height: auto;
  }
</style>
