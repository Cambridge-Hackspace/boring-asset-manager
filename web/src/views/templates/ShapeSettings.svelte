<script lang="ts">
  import { defaultStroke, hairline, withProp, type Paint, type TemplateDoc, type TemplateElement } from '../../lib/template';

  // A shape's paint: its stroke, how wide, and its fill, in black, white,
  // which paints over what is beneath it, or none; and what applies to its
  // kind: a rectangle's corners, which way a line runs, whether a polygon is
  // closed.
  let { doc, e, index, onupdate }: { doc: TemplateDoc; e: TemplateElement; index: number; onupdate: (e: TemplateElement) => void } = $props();

  const id = (name: string) => `el-${index}-${name}`;
  const paints: [Paint, string][] = [
    ['black', 'Black'],
    ['white', 'White'],
    ['none', 'None'],
  ];
  const fillable = $derived(e.shape !== 'line' && !(e.shape === 'polygon' && e.closed === false));
  const drawsNothing = $derived(e.stroke === 'none' && (e.fill ?? 'none') === 'none');

  function setStroke(paint: Paint) {
    let next = withProp(e, 'stroke', paint);
    if (paint === 'none') next = withProp(next, 'stroke_width', undefined);
    else if (!next.stroke_width) next = withProp(next, 'stroke_width', defaultStroke(doc.units));
    onupdate(next);
  }

  function setNumber(key: 'stroke_width' | 'corner_radius', raw: string) {
    const n = Number(raw);
    if (raw === '' || !(Number.isFinite(n) && n >= 0)) return;
    onupdate(withProp(e, key, n || undefined));
  }
</script>

<div class="grid grid-cols-2 gap-3 sm:grid-cols-3">
  <div>
    <label class="label" for={id('stroke')}>Stroke</label>
    <select id={id('stroke')} class="input" value={e.stroke ?? 'black'} onchange={(ev) => setStroke(ev.currentTarget.value as Paint)}>
      {#each paints as [value, label] (value)}<option {value}>{label}</option>{/each}
    </select>
  </div>
  {#if e.stroke !== 'none'}
    <div>
      <label class="label" for={id('stroke-width')}>Stroke width ({doc.units})</label>
      <input id={id('stroke-width')} class="input" type="number" min="0" step="any" value={e.stroke_width ?? ''} onchange={(ev) => setNumber('stroke_width', ev.currentTarget.value)} />
    </div>
  {/if}
  {#if fillable}
    <div>
      <label class="label" for={id('fill')}>Fill</label>
      <select id={id('fill')} class="input" value={e.fill ?? 'none'} onchange={(ev) => onupdate(withProp(e, 'fill', ev.currentTarget.value as Paint))}>
        {#each paints as [value, label] (value)}<option {value}>{label}</option>{/each}
      </select>
    </div>
  {/if}
  {#if e.shape === 'rectangle'}
    <div>
      <label class="label" for={id('radius')}>Corner radius ({doc.units})</label>
      <input id={id('radius')} class="input" type="number" min="0" step="any" value={e.corner_radius ?? 0} onchange={(ev) => setNumber('corner_radius', ev.currentTarget.value)} />
    </div>
  {:else if e.shape === 'line'}
    <label class="flex items-center gap-2 self-end pb-2 text-sm">
      <input type="checkbox" checked={e.flip ?? false} onchange={(ev) => onupdate(withProp(e, 'flip', ev.currentTarget.checked || undefined))} /> Rising, from bottom left to top right
    </label>
  {:else if e.shape === 'polygon'}
    <label class="flex items-center gap-2 self-end pb-2 text-sm">
      <input
        type="checkbox"
        checked={e.closed !== false}
        onchange={(ev) => {
          // An open polygon has no inside to fill.
          const closed = ev.currentTarget.checked;
          onupdate(closed ? withProp(e, 'closed', undefined) : { ...withProp(e, 'closed', false), fill: 'none' });
        }}
      /> Closed
    </label>
  {/if}
</div>
{#if e.shape === 'polygon'}
  <p class="text-xs text-zinc-600 dark:text-zinc-400">Drag its points on the label, or focus one and use the arrow keys; Delete removes it. The marks halfway along its sides add points.</p>
{/if}
{#if drawsNothing}
  <p class="error-text" role="alert">With no stroke and no fill, it draws nothing.</p>
{:else if hairline(e, doc)}
  <p class="rounded-md border border-amber-300 bg-amber-50 px-2 py-1 text-sm dark:border-amber-800 dark:bg-amber-950" role="status">
    Its stroke is thinner than one printer dot at {doc.dpi} DPI, so it prints one dot wide.
  </p>
{/if}
