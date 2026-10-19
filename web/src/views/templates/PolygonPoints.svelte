<script lang="ts">
  import { gridStep, polygonPoints, polygonThrough, sinCos, snap, type TemplateDoc, type TemplateElement } from '../../lib/template';

  // The points of the selected polygon, laid over its box and turned with
  // it. A point drags, or moves with the arrow keys a grid step at a time
  // (Shift for ten), and Delete removes it while enough remain; the mark in
  // the middle of each side adds a point there.
  let {
    doc,
    e,
    style,
    onbegin,
    onupdate,
  }: {
    doc: TemplateDoc;
    e: TemplateElement;
    /** Where its box is, and how it is turned, as the canvas places it. */
    style: string;
    onbegin: () => void;
    onupdate: (e: TemplateElement) => void;
  } = $props();

  let box: HTMLDivElement | undefined = $state();
  const pts = $derived(polygonPoints(e));
  const closed = $derived(e.closed !== false);
  const least = $derived(closed ? 3 : 2);
  const pct = (v: number, total: number) => `${total ? (v / total) * 100 : 0}%`;

  let drag: { k: number; x0: number; y0: number; start: TemplateElement; moved: boolean } | null = null;

  /** Moves point k by (du, dv), in the template's units along the polygon's own box. */
  function moved(start: TemplateElement, k: number, du: number, dv: number, onGrid: boolean): TemplateElement {
    const next = polygonPoints(start).map((p, i): [number, number] => {
      if (i !== k) return p;
      const step = gridStep(doc.units);
      const u = p[0] + du;
      const v = p[1] + dv;
      return onGrid ? [snap(u, step, doc.units), snap(v, step, doc.units)] : [u, v];
    });
    return polygonThrough(doc, start, next);
  }

  function pointerdown(event: PointerEvent, k: number) {
    if (event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    (event.currentTarget as HTMLElement).focus();
    drag = { k, x0: event.clientX, y0: event.clientY, start: $state.snapshot(e) as TemplateElement, moved: false };
    (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
  }

  function pointermove(event: PointerEvent) {
    if (!drag || !box) return;
    if (!drag.moved) {
      drag.moved = true;
      onbegin();
    }
    // Template units per pixel, from the label's width on screen.
    const k = doc.width / (box.parentElement?.clientWidth || 1);
    const dx = (event.clientX - drag.x0) * k;
    const dy = (event.clientY - drag.y0) * k;
    // The drag turned back onto the polygon's own box.
    const [sin, cos] = sinCos(e.rotation ?? 0);
    onupdate(moved(drag.start, drag.k, dx * cos + dy * sin, -dx * sin + dy * cos, !event.altKey));
  }

  function pointerup(event: PointerEvent) {
    const target = event.currentTarget as HTMLElement;
    if (target.hasPointerCapture(event.pointerId)) target.releasePointerCapture(event.pointerId);
    drag = null;
  }

  function keydown(event: KeyboardEvent, k: number) {
    const step = gridStep(doc.units) * (event.shiftKey ? 10 : 1);
    const moves: Record<string, [number, number]> = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] };
    const move = moves[event.key];
    if (move) {
      event.preventDefault();
      event.stopPropagation();
      onbegin();
      onupdate(moved(e, k, move[0], move[1], false));
    } else if ((event.key === 'Delete' || event.key === 'Backspace') && pts.length > least) {
      event.preventDefault();
      event.stopPropagation();
      onbegin();
      onupdate(polygonThrough(doc, e, pts.filter((_, i) => i !== k)));
    }
  }

  /** Adds a point halfway along the side from point k. */
  function addAfter(k: number) {
    const a = pts[k]!;
    const b = pts[(k + 1) % pts.length]!;
    onbegin();
    onupdate(polygonThrough(doc, e, [...pts.slice(0, k + 1), [(a[0] + b[0]) / 2, (a[1] + b[1]) / 2], ...pts.slice(k + 1)]));
  }

  const sidesOf = $derived(pts.flatMap((_, k) => (closed || k < pts.length - 1 ? [k] : [])));
</script>

<div bind:this={box} class="pointer-events-none absolute" {style}>
  {#each pts as p, k (k)}
    <button
      type="button"
      class="point pointer-events-auto absolute rounded-full border-2 border-white bg-blue-600"
      style="left:calc({pct(p[0], e.width)} - var(--h) / 2);top:calc({pct(p[1], e.height)} - var(--h) / 2)"
      aria-label="Point {k + 1} of {pts.length}"
      onpointerdown={(ev) => pointerdown(ev, k)}
      onpointermove={pointermove}
      onpointerup={pointerup}
      onpointercancel={pointerup}
      onkeydown={(ev) => keydown(ev, k)}
    ></button>
  {/each}
  {#each sidesOf as k (k)}
    {@const a = pts[k]!}
    {@const b = pts[(k + 1) % pts.length]!}
    <button
      type="button"
      class="add pointer-events-auto absolute flex items-center justify-center rounded-full border border-blue-600 bg-white text-xs leading-none text-blue-700"
      style="left:calc({pct((a[0] + b[0]) / 2, e.width)} - var(--h) / 2);top:calc({pct((a[1] + b[1]) / 2, e.height)} - var(--h) / 2)"
      aria-label="Add a point between points {k + 1} and {((k + 1) % pts.length) + 1}"
      onpointerdown={(ev) => ev.stopPropagation()}
      onclick={() => addAfter(k)}>+</button
    >
  {/each}
</div>

<style>
  .point,
  .add {
    --h: 12px;
    width: var(--h);
    height: var(--h);
  }
  .add {
    --h: 14px;
  }
  @media (pointer: coarse) {
    .point,
    .add {
      --h: 24px;
    }
  }
</style>
