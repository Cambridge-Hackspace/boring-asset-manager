<!--
  The icon palette as CSS custom properties (lib/isolex/palette), in the
  page's head, for IsoIcon to paint with: light and dark, and the
  iso-light and iso-dark classes that force either.
-->
<script lang="ts">
  import { paletteCss, type IconColor } from '../lib/isolex/palette';

  let { palette }: { palette: IconColor[] } = $props();
  const css = $derived(paletteCss(palette));
</script>

<svelte:head>
  {@html `<style data-icon-palette>${css}</style>`}
</svelte:head>
