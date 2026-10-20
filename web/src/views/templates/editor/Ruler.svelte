<script lang="ts">
  import { boundsOf, rulerStep, type Units } from '../../../lib/template';
  import { useEditor } from './editor.svelte';

  // A ruler along the top or down the left of the label, as in Krita: its
  // numbers spaced to suit the zoom, a mark where the pointer is, and the
  // selection's extent shaded. Right-click it to change its units.
  // Pressed and dragged onto the label, it gives a guide (onguide).
  let { axis, onguide }: { axis: 'x' | 'y'; onguide?: (event: PointerEvent) => void } = $props();

  const editor = useEditor();
  let w = $state(0);
  let h = $state(0);
  const length = $derived(axis === 'x' ? w : h);
  let menu = $state<{ x: number; y: number } | null>(null);

  const units = $derived(editor.rulerUnits ?? editor.doc.units);
  /** Template units in one ruler unit. */
  const per = $derived(units === editor.doc.units ? 1 : units === 'in' ? 25.4 : 1 / 25.4);
  const pxPerUnit = $derived(editor.view.scale * per);
  const origin = $derived(axis === 'x' ? editor.view.x : editor.view.y);
  const major = $derived(rulerStep(pxPerUnit, 56));
  const minor = $derived(rulerStep(pxPerUnit, 7));
  const fmt = (v: number) => String(Math.round(v * 1000) / 1000);

  /** The ticks in view: their place on the ruler, and a number for the major ones. */
  const ticks = $derived.by(() => {
    if (!length || !(pxPerUnit > 0)) return [];
    const out: { at: number; label?: string; long: boolean }[] = [];
    const first = Math.floor(-origin / pxPerUnit / minor) * minor;
    const last = (length - origin) / pxPerUnit;
    for (let v = first, n = 0; v <= last && n < 2000; v += minor, n++) {
      const value = Math.round(v / minor) * minor;
      const isMajor = Math.abs(value / major - Math.round(value / major)) < 1e-6;
      out.push({ at: Math.round(origin + value * pxPerUnit) + 0.5, long: isMajor, ...(isMajor ? { label: fmt(value) } : {}) });
    }
    return out;
  });

  const pointer = $derived(editor.pointer ? origin + (axis === 'x' ? editor.pointer.x : editor.pointer.y) * editor.view.scale : null);
  const extent = $derived.by(() => {
    const els = editor.selected.map((i) => editor.doc.elements[i]).filter((e) => e !== undefined);
    if (!els.length) return null;
    const b = boundsOf(els);
    const s = editor.view.scale;
    return axis === 'x' ? { from: origin + b.x * s, size: b.width * s } : { from: origin + b.y * s, size: b.height * s };
  });

  function choose(u: Units) {
    editor.setRulerUnits(u);
    menu = null;
  }
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && menu && (menu = null)} onpointerdown={() => menu && (menu = null)} />

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  bind:clientWidth={w}
  bind:clientHeight={h}
  title="Drag onto the label for a guide. Right-click for its units."
  class="relative h-full w-full cursor-default overflow-hidden bg-zinc-100 text-[0.6rem] text-zinc-600 select-none dark:bg-zinc-900 dark:text-zinc-400"
  aria-hidden="true"
  data-ruler={axis}
  onpointerdown={(e) => e.button === 0 && onguide?.(e)}
  oncontextmenu={(e) => {
    e.preventDefault();
    menu = { x: e.clientX, y: e.clientY };
  }}
>
  <svg class="absolute inset-0 h-full w-full">
    {#if extent}
      {#if axis === 'x'}
        <rect x={extent.from} y="0" width={extent.size} height="100%" class="fill-blue-500/20" />
      {:else}
        <rect x="0" y={extent.from} width="100%" height={extent.size} class="fill-blue-500/20" />
      {/if}
    {/if}
    {#each ticks as t, i (i)}
      {#if axis === 'x'}
        <line x1={t.at} x2={t.at} y1={t.long ? 6 : 14} y2="20" class="stroke-zinc-400 dark:stroke-zinc-600" />
        {#if t.label}<text x={t.at + 2} y="9" class="fill-current">{t.label}</text>{/if}
      {:else}
        <line y1={t.at} y2={t.at} x1={t.long ? 6 : 14} x2="20" class="stroke-zinc-400 dark:stroke-zinc-600" />
        {#if t.label}<text x="9" y={t.at + 2} class="fill-current" transform="rotate(-90 9 {t.at + 2})">{t.label}</text>{/if}
      {/if}
    {/each}
    {#if pointer !== null}
      {#if axis === 'x'}
        <line x1={pointer} x2={pointer} y1="0" y2="20" class="stroke-blue-600" />
      {:else}
        <line y1={pointer} y2={pointer} x1="0" x2="20" class="stroke-blue-600" />
      {/if}
    {/if}
  </svg>
</div>

{#if menu}
  <div
    role="menu"
    aria-label="Ruler units"
    tabindex="-1"
    class="fixed z-50 min-w-36 rounded border border-zinc-300 bg-white py-1 text-xs shadow-lg dark:border-zinc-700 dark:bg-zinc-900"
    style="left:{menu.x}px;top:{menu.y}px"
    onpointerdown={(e) => e.stopPropagation()}
  >
    {#each [['mm', 'Millimeters'], ['in', 'Inches']] as const as [u, name] (u)}
      <button type="button" role="menuitemradio" aria-checked={units === u} class="block w-full px-3 py-1 text-left hover:bg-zinc-100 aria-checked:font-semibold dark:hover:bg-zinc-800" onclick={() => choose(u)}>{name}</button>
    {/each}
  </div>
{/if}
