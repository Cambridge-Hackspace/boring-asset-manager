<!--
  An isolex icon (README §3.7), drawn from its IR as isolex's reference
  renderer draws it: the same shapes, in the same order, every number by
  the same rule. Tokens are painted with the palette's CSS properties
  (lib/isolex/palette), and category with the category's own color.
-->
<script lang="ts">
  import type { IrIcon } from '../lib/isolex/ir';
  import { formatNumber as n } from '../lib/isolex/ir';
  import IsoShapes from './IsoShapes.svelte';

  interface Props {
    icon: IrIcon;
    /** Its height on the page, in CSS pixels; its width follows. */
    height?: number;
    /** What it shows, for those who cannot see it; empty for decoration. */
    label?: string;
    /** The color the category token paints with. */
    categoryColor?: string;
  }

  let { icon, height, label = '', categoryColor = '' }: Props = $props();

  const vb = $derived(icon.viewbox);
  const shown = $derived(height ?? vb[3]);
  const width = $derived((shown * vb[2]) / vb[3]);
  const colorRule = /^#(?:[0-9a-f]{3}|[0-9a-f]{6}|[0-9a-f]{8})$/iu;
  const style = $derived(colorRule.test(categoryColor) ? `--iso-category:${categoryColor}` : undefined);
</script>

<svg
  xmlns="http://www.w3.org/2000/svg"
  viewBox="{n(vb[0])} {n(vb[1])} {n(vb[2])} {n(vb[3])}"
  width={width}
  height={shown}
  role={label ? 'img' : undefined}
  aria-label={label || undefined}
  aria-hidden={label ? undefined : 'true'}
  data-icon={icon.name}
  {style}
  class="inline-block shrink-0 align-middle"><IsoShapes shapes={icon.shapes} /></svg
>
