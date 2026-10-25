<!--
  An isolex icon (README §3.7), drawn from its IR as isolex's reference
  renderer draws it: the same shapes, in the same order, every number by
  the same rule. Tokens are painted with the palette's CSS properties
  (lib/isolex/palette), and category with the category's own color.
-->
<script lang="ts">
  import type { IrIcon, IrShape } from '../lib/isolex/ir';
  import { formatNumber as n } from '../lib/isolex/ir';
  import { tokenVar } from '../lib/isolex/palette';

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

  const paint = (token: string | undefined) => (!token || token === 'none' ? 'none' : tokenVar(token));
  // What isolex leaves out: an opacity of one, and a stroke's width and
  // dash where there is no stroke.
  const fillOpacity = (s: IrShape) => (s.fill && s.fill !== 'none' && s.fill_opacity !== 1 ? n(s.fill_opacity ?? 1) : undefined);
  const stroked = (s: IrShape) => !!s.stroke && s.stroke !== 'none';
  const strokeOpacity = (s: IrShape) => (stroked(s) && s.fill_opacity !== 1 ? n(s.fill_opacity ?? 1) : undefined);
</script>

{#snippet draw(shapes: IrShape[])}
  {#each shapes as s, k (k)}
    {#if s.kind === 'group'}
      <g transform="translate({n(s.tx ?? 0)} {n(s.ty ?? 0)}) scale({n(s.scale ?? 1)})">{@render draw(s.shapes ?? [])}</g>
    {:else if s.kind === 'polygon'}
      <polygon
        points={(s.points ?? []).map(([x, y]) => `${n(x)},${n(y)}`).join(' ')}
        fill={paint(s.fill)}
        fill-opacity={fillOpacity(s)}
        stroke={paint(s.stroke)}
        stroke-width={stroked(s) ? n(s.stroke_width ?? 0) : undefined}
        stroke-opacity={strokeOpacity(s)}
        stroke-dasharray={stroked(s) ? s.dash : undefined}
      />
    {:else if s.kind === 'path'}
      <path
        d={s.d}
        fill={paint(s.fill)}
        fill-opacity={fillOpacity(s)}
        stroke={paint(s.stroke)}
        stroke-width={stroked(s) ? n(s.stroke_width ?? 0) : undefined}
        stroke-opacity={strokeOpacity(s)}
        stroke-dasharray={stroked(s) ? s.dash : undefined}
      />
    {:else if s.kind === 'ellipse'}
      <ellipse
        cx={n(s.cx ?? 0)}
        cy={n(s.cy ?? 0)}
        rx={n(s.rx ?? 0)}
        ry={n(s.ry ?? 0)}
        fill={paint(s.fill)}
        fill-opacity={fillOpacity(s)}
        stroke={paint(s.stroke)}
        stroke-width={stroked(s) ? n(s.stroke_width ?? 0) : undefined}
        stroke-opacity={strokeOpacity(s)}
        stroke-dasharray={stroked(s) ? s.dash : undefined}
      />
    {:else if s.kind === 'circle'}
      <circle
        cx={n(s.cx ?? 0)}
        cy={n(s.cy ?? 0)}
        r={n(s.r ?? 0)}
        fill={paint(s.fill)}
        fill-opacity={fillOpacity(s)}
        stroke={paint(s.stroke)}
        stroke-width={stroked(s) ? n(s.stroke_width ?? 0) : undefined}
        stroke-opacity={strokeOpacity(s)}
        stroke-dasharray={stroked(s) ? s.dash : undefined}
      />
    {:else if s.kind === 'rect'}
      <rect
        x={n(s.x ?? 0)}
        y={n(s.y ?? 0)}
        width={n(s.w ?? 0)}
        height={n(s.h ?? 0)}
        rx={(s.rx ?? 0) > 0 ? n(s.rx ?? 0) : undefined}
        fill={paint(s.fill)}
        fill-opacity={fillOpacity(s)}
        stroke={paint(s.stroke)}
        stroke-width={stroked(s) ? n(s.stroke_width ?? 0) : undefined}
        stroke-opacity={strokeOpacity(s)}
        stroke-dasharray={stroked(s) ? s.dash : undefined}
      />
    {/if}
  {/each}
{/snippet}

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
  class="inline-block shrink-0 align-middle">{@render draw(icon.shapes)}</svg
>
