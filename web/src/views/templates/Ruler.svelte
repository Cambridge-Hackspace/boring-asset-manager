<script lang="ts">
  import { rulerStep } from '../../lib/template';

  // A ruler along the top of the canvas (x) or down its side (y), in the
  // template's units, moving as the canvas scrolls. Dragging from it onto
  // the label places a guide.
  let {
    axis,
    length,
    pxPerUnit,
    offset,
    onguide,
  }: {
    axis: 'x' | 'y';
    /** The label's side along the ruler, in its units. */
    length: number;
    pxPerUnit: number;
    /** How far the canvas has scrolled along the ruler, in pixels. */
    offset: number;
    /** A pointer pressed on the ruler, to drag a guide out of it. */
    onguide: (event: PointerEvent) => void;
  } = $props();

  const step = $derived(rulerStep(pxPerUnit));
  // Every fifth tick is long and numbered.
  const ticks = $derived.by(() => {
    const out: { at: number; major: boolean }[] = [];
    const n = Math.floor(length / step + 1e-9);
    for (let k = 0; k <= n && k < 2000; k++) out.push({ at: k * step, major: k % 5 === 0 });
    return out;
  });
  const label = (v: number) => String(Math.round(v * 1000) / 1000);
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="ruler relative overflow-hidden bg-zinc-100 text-zinc-700 select-none dark:bg-zinc-800 dark:text-zinc-300 {axis === 'x' ? 'h-5 cursor-s-resize' : 'w-5 cursor-e-resize'}"
  title={axis === 'x' ? 'Drag down onto the label for a horizontal guide' : 'Drag across onto the label for a vertical guide'}
  onpointerdown={onguide}
>
  <svg
    aria-hidden="true"
    class="absolute top-0 left-0"
    width={axis === 'x' ? length * pxPerUnit + 1 : 20}
    height={axis === 'x' ? 20 : length * pxPerUnit + 1}
    style="transform: translate{axis === 'x' ? 'X' : 'Y'}({-offset}px)"
  >
    {#each ticks as t (t.at)}
      {@const p = Math.round(t.at * pxPerUnit) + 0.5}
      {#if axis === 'x'}
        <line x1={p} x2={p} y1={t.major ? 8 : 14} y2={20} stroke="currentColor" />
        {#if t.major}<text x={p + 2} y={9} font-size="9" fill="currentColor">{label(t.at)}</text>{/if}
      {:else}
        <line y1={p} y2={p} x1={t.major ? 8 : 14} x2={20} stroke="currentColor" />
        {#if t.major}<text x={9} y={p + 2} font-size="9" fill="currentColor" transform="rotate(-90 9 {p + 2})">{label(t.at)}</text>{/if}
      {/if}
    {/each}
  </svg>
</div>
