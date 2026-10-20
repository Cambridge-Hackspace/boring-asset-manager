<script lang="ts">
  import type { Snippet } from 'svelte';
  import { appliedTurn, mirrorSides, turnedSize, type Mirror } from '../lib/template';

  // A label that prints mirrored or turned, shown as it prints, or as it
  // is once on: mirrored back, as it is read through the glass, and turned
  // so what is on it reads level, the label lying turned. What it shows is
  // the design, the right way round, or the label as printed.
  let {
    width,
    height,
    mirror,
    rotation,
    applied,
    content,
    maxHeight,
    children,
  }: {
    width: number;
    height: number;
    mirror: Mirror | undefined;
    rotation: number | undefined;
    applied: boolean;
    content: 'design' | 'printed';
    /** The most it is shown high, in rem. */
    maxHeight?: number;
    children: Snippet;
  } = $props();

  const sides = $derived(mirrorSides(mirror));
  const flip = $derived(`scale(${sides.across ? -1 : 1}, ${sides.down ? -1 : 1})`);
  const turn = $derived(rotation ?? 0);
  const lies = $derived(appliedTurn({ print_mirror: mirror, print_rotation: rotation }));
  const size = $derived<[number, number]>(applied ? turnedSize(width, height, lies) : [width, height]);
  // The label, turned as it lies once on; and what is on it.
  const label = $derived(applied ? (content === 'design' ? `rotate(${lies}deg)` : `${flip} rotate(${-turn}deg)`) : 'none');
  const onIt = $derived(content === 'design' ? (applied ? `rotate(${-lies}deg)` : `rotate(${turn}deg) ${flip}`) : 'none');
</script>

<div
  class="relative"
  style="aspect-ratio: {size[0]} / {size[1]}; width: {maxHeight ? `min(100%, ${(maxHeight * size[0]) / size[1]}rem)` : '100%'}"
  data-applied-label={applied ? 'applied' : 'printed'}
>
  <div
    class="absolute overflow-hidden border border-zinc-400 bg-white dark:border-zinc-600"
    style="left: {(1 - width / size[0]) * 50}%; top: {(1 - height / size[1]) * 50}%; width: {(width / size[0]) * 100}%; height: {(height / size[1]) * 100}%; transform: {label}"
  >
    <div class="h-full w-full" style="transform: {onIt}">{@render children()}</div>
  </div>
</div>
