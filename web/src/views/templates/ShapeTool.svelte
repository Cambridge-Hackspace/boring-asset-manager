<script lang="ts">
  import { gridStep, newShape, polygonThrough, snap, tidy, type ShapeKind, type TemplateDoc, type TemplateElement } from '../../lib/template';

  // Drawing a shape on the canvas, laid over it while a tool is chosen. A
  // rectangle, ellipse or line is dragged out, Shift keeping it square,
  // round, or at a multiple of 45°; a click alone adds one there. A
  // polygon takes a point at each click, and is finished by a double click
  // or Enter, or closed by clicking its first point. Escape stops drawing.
  // Points are on the grid unless Alt is held.
  let {
    doc,
    tool,
    ondraw,
    oncancel,
  }: {
    doc: TemplateDoc;
    tool: ShapeKind;
    ondraw: (e: TemplateElement) => void;
    oncancel: () => void;
  } = $props();

  let layer: HTMLDivElement | undefined = $state();
  let from = $state<[number, number] | null>(null);
  let to = $state<[number, number] | null>(null);
  let points = $state<[number, number][]>([]);

  /** The pointer's place on the label, in its units, on the grid unless Alt is held. */
  function at(event: PointerEvent | MouseEvent): [number, number] {
    const r = layer!.getBoundingClientRect();
    const x = Math.min(Math.max(((event.clientX - r.left) / r.width) * doc.width, 0), doc.width);
    const y = Math.min(Math.max(((event.clientY - r.top) / r.height) * doc.height, 0), doc.height);
    if (event.altKey) return [tidy(x, doc.units), tidy(y, doc.units)];
    const step = gridStep(doc.units);
    return [snap(x, step, doc.units), snap(y, step, doc.units)];
  }

  /** Where a drag ends: square, round, or at 45°, with Shift. */
  function constrained(a: [number, number], b: [number, number], shift: boolean): [number, number] {
    if (!shift) return b;
    const dx = b[0] - a[0];
    const dy = b[1] - a[1];
    if (tool === 'line') {
      const angle = Math.round(Math.atan2(dy, dx) / (Math.PI / 4)) * (Math.PI / 4);
      const length = Math.hypot(dx, dy);
      return [tidy(a[0] + Math.cos(angle) * length, doc.units), tidy(a[1] + Math.sin(angle) * length, doc.units)];
    }
    const side = Math.max(Math.abs(dx), Math.abs(dy));
    return [tidy(a[0] + Math.sign(dx || 1) * side, doc.units), tidy(a[1] + Math.sign(dy || 1) * side, doc.units)];
  }

  function pointerdown(event: PointerEvent) {
    if (event.button !== 0) return;
    event.preventDefault();
    const p = at(event);
    if (tool === 'polygon') {
      const first = points[0];
      // Clicking the first point again closes it.
      const near = first && Math.hypot(first[0] - p[0], first[1] - p[1]) < gridStep(doc.units) * 1.5;
      if (near && points.length >= 3) finishPolygon();
      else points = [...points, p];
      return;
    }
    from = p;
    to = p;
    layer?.setPointerCapture(event.pointerId);
  }

  function pointermove(event: PointerEvent) {
    if (from) to = constrained(from, at(event), event.shiftKey);
  }

  function pointerup(event: PointerEvent) {
    if (!from || !to) return;
    if (layer?.hasPointerCapture(event.pointerId)) layer.releasePointerCapture(event.pointerId);
    const [a, b] = [from, to];
    from = to = null;
    const box = { x: Math.min(a[0], b[0]), y: Math.min(a[1], b[1]), width: tidy(Math.abs(b[0] - a[0]), doc.units), height: tidy(Math.abs(b[1] - a[1]), doc.units) };
    const step = gridStep(doc.units);
    if (box.width < step && box.height < step) {
      // A click alone: the shape at its usual size, there.
      const e = newShape(tool, doc);
      ondraw({ ...e, x: tidy(Math.min(a[0], doc.width - e.width), doc.units), y: tidy(Math.min(a[1], doc.height - e.height), doc.units) });
      return;
    }
    if (tool === 'line') {
      // From the bottom left up to the top right, or the other way.
      const rising = (b[0] - a[0]) * (b[1] - a[1]) < 0;
      ondraw({ ...newShape('line', doc, box), ...(rising ? { flip: true } : {}) });
      return;
    }
    ondraw(newShape(tool, doc, { ...box, width: Math.max(box.width, step), height: Math.max(box.height, step) }));
  }

  function finishPolygon() {
    // A double click's clicks each added a point; one is enough.
    points = points.filter((p, k) => k === 0 || p[0] !== points[k - 1]![0] || p[1] !== points[k - 1]![1]);
    if (points.length < 3) return;
    const xs = points.map((p) => p[0]);
    const ys = points.map((p) => p[1]);
    const box = { x: Math.min(...xs), y: Math.min(...ys), width: Math.max(...xs) - Math.min(...xs), height: Math.max(...ys) - Math.min(...ys) };
    const start = { ...newShape('polygon', doc, box), points: [] };
    ondraw(polygonThrough(doc, start, points.map(([x, y]) => [x - box.x, y - box.y])));
    points = [];
  }

  function keydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.preventDefault();
      points = [];
      from = to = null;
      oncancel();
    } else if (event.key === 'Enter' && tool === 'polygon' && points.length >= 3) {
      event.preventDefault();
      finishPolygon();
    }
  }

  const draft = $derived.by(() => {
    if (!from || !to) return null;
    return { x: Math.min(from[0], to[0]), y: Math.min(from[1], to[1]), w: Math.abs(to[0] - from[0]), h: Math.abs(to[1] - from[1]) };
  });
</script>

<svelte:window onkeydown={keydown} />

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  bind:this={layer}
  class="absolute inset-0 z-10 cursor-crosshair"
  onpointerdown={pointerdown}
  onpointermove={pointermove}
  onpointerup={pointerup}
  onpointercancel={() => (from = to = null)}
  ondblclick={(ev) => {
    ev.preventDefault();
    finishPolygon();
  }}
>
  <svg class="pointer-events-none h-full w-full" viewBox="0 0 {doc.width} {doc.height}" preserveAspectRatio="none" aria-hidden="true">
    {#if draft && from && to}
      {#if tool === 'rectangle'}
        <rect x={draft.x} y={draft.y} width={draft.w} height={draft.h} class="draft" />
      {:else if tool === 'ellipse'}
        <ellipse cx={draft.x + draft.w / 2} cy={draft.y + draft.h / 2} rx={draft.w / 2} ry={draft.h / 2} class="draft" />
      {:else if tool === 'line'}
        <line x1={from[0]} y1={from[1]} x2={to[0]} y2={to[1]} class="draft" />
      {/if}
    {/if}
    {#if points.length}
      <polyline points={points.map((p) => p.join(',')).join(' ')} class="draft" />
      {#each points as p, k (k)}
        <circle cx={p[0]} cy={p[1]} r={Math.min(doc.width, doc.height) / 80} class="dot" />
      {/each}
    {/if}
  </svg>
</div>

<style>
  .draft {
    fill: rgb(37 99 235 / 0.08);
    stroke: rgb(37 99 235);
    stroke-width: 1.5px;
    stroke-dasharray: 4 3;
    vector-effect: non-scaling-stroke;
  }
  .dot {
    fill: rgb(37 99 235);
  }
</style>
