<svelte:options namespace="svg" />

<!--
  An isolex icon's shapes (docs/icons.md), as isolex's reference renderer
  draws them: the same shapes, in the same order, every number by the same
  rule, tokens painted with the palette's CSS properties. Drawn into an
  SVG: IsoIcon's own, or a scene's. A group of IR version 2 is drawn
  through its matrix, cut to its clip, and with its strokes kept at their
  width when fixed, as isolex 0.4 draws it; its clip paths are named for
  this drawing, so that an icon drawn many times on one page cuts each
  copy to its own.
-->
<script lang="ts">
  import type { IrShape } from '../lib/isolex/ir';
  import { formatNumber as n } from '../lib/isolex/ir';
  import { tokenVar } from '../lib/isolex/palette';

  let { shapes }: { shapes: IrShape[] } = $props();

  const uid = $props.id();
  // each clipped group's clip path, numbered in the order isolex numbers
  // them: as the shapes are drawn
  const clips = $derived.by(() => {
    const ids = new Map<IrShape, string>();
    const walk = (ss: IrShape[]) => {
      for (const s of ss) {
        if (s.kind !== 'group') continue;
        if (s.clip) ids.set(s, `iso-clip-${uid}-${ids.size + 1}`);
        walk(s.shapes ?? []);
      }
    };
    walk(shapes);
    return ids;
  });
  const points = (ps: [number, number][]) => ps.map(([x, y]) => `${n(x)},${n(y)}`).join(' ');
  const matrix = (m: number[]) => `matrix(${m.map(n).join(' ')})`;

  const paint = (token: string | undefined) => (!token || token === 'none' ? 'none' : tokenVar(token));
  // What isolex leaves out: an opacity of one, and a stroke's width and
  // dash where there is no stroke.
  const fillOpacity = (s: IrShape) => (s.fill && s.fill !== 'none' && s.fill_opacity !== 1 ? n(s.fill_opacity ?? 1) : undefined);
  const stroked = (s: IrShape) => !!s.stroke && s.stroke !== 'none';
  const strokeOpacity = (s: IrShape) => (stroked(s) && s.fill_opacity !== 1 ? n(s.fill_opacity ?? 1) : undefined);
</script>

{#snippet draw(shapes: IrShape[], fixed: boolean)}
  {#each shapes as s, k (k)}
    {#if s.kind === 'group' && s.matrix}
      {@const id = clips.get(s)}
      {#if id}
        <clipPath {id}><polygon points={points(s.clip ?? [])} /></clipPath>
        <g clip-path="url(#{id})"
          ><g transform={matrix(s.matrix)}>{@render draw(s.shapes ?? [], s.strokes === 'fixed')}</g></g
        >
      {:else}
        <g transform={matrix(s.matrix)}>{@render draw(s.shapes ?? [], s.strokes === 'fixed')}</g>
      {/if}
    {:else if s.kind === 'group'}
      <g transform="translate({n(s.tx ?? 0)} {n(s.ty ?? 0)}) scale({n(s.scale ?? 1)})">{@render draw(s.shapes ?? [], fixed)}</g>
    {:else if s.kind === 'polygon'}
      <polygon
        points={points(s.points ?? [])}
        fill={paint(s.fill)}
        fill-opacity={fillOpacity(s)}
        stroke={paint(s.stroke)}
        stroke-width={stroked(s) ? n(s.stroke_width ?? 0) : undefined}
        stroke-opacity={strokeOpacity(s)}
        stroke-dasharray={stroked(s) ? s.dash : undefined}
        vector-effect={fixed && stroked(s) ? 'non-scaling-stroke' : undefined}
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
        vector-effect={fixed && stroked(s) ? 'non-scaling-stroke' : undefined}
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
        vector-effect={fixed && stroked(s) ? 'non-scaling-stroke' : undefined}
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
        vector-effect={fixed && stroked(s) ? 'non-scaling-stroke' : undefined}
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
        vector-effect={fixed && stroked(s) ? 'non-scaling-stroke' : undefined}
      />
    {/if}
  {/each}
{/snippet}

{@render draw(shapes, false)}
