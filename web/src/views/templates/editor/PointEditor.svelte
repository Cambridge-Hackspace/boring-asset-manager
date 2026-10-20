<script lang="ts">
  import { sinCos, type TemplateElement } from '../../../lib/template';
  import { useEditor } from './editor.svelte';
  import { contoursOf, nearestOn, splitAt, toggled, withContours, type Contour, type Pt } from './nodes';
  import { snapPoint, snapNothing } from './snapping';

  // Editing a polygon's or path's points, as Krita's path tool does, laid
  // over the label in the overlay: drag a point, or a handle (a smooth
  // point's other handle following, unless Alt is held); Shift+click a point
  // to make it sharp or smooth; Delete removes the chosen point while enough
  // remain; a double-click on the outline adds one there (insertAt).
  let { index, scale, ox, oy }: { index: number; scale: number; ox: number; oy: number } = $props();

  const editor = useEditor();
  const doc = $derived(editor.doc);
  const e = $derived(doc.elements[index]);
  const contours = $derived(e ? contoursOf(e) : []);
  let chosen = $state<{ c: number; k: number } | null>(null);

  /** Local to the label, and to the screen, as the element lies turned. */
  function toLabel(el: TemplateElement, p: Pt): Pt {
    const [sin, cos] = sinCos(el.rotation ?? 0);
    const u = p[0] - el.width / 2;
    const v = p[1] - el.height / 2;
    return [el.x + el.width / 2 + u * cos - v * sin, el.y + el.height / 2 + u * sin + v * cos];
  }
  function toLocal(el: TemplateElement, q: Pt): Pt {
    const [sin, cos] = sinCos(el.rotation ?? 0);
    const dx = q[0] - (el.x + el.width / 2);
    const dy = q[1] - (el.y + el.height / 2);
    return [el.width / 2 + dx * cos + dy * sin, el.height / 2 - dx * sin + dy * cos];
  }
  const onLabel = (q: Pt): Pt => [Math.min(Math.max(q[0], 0), doc.width), Math.min(Math.max(q[1], 0), doc.height)];
  /** A point in the element's frame, moved onto the label where it lies off it. */
  const kept = (el: TemplateElement, p: Pt): Pt => toLocal(el, onLabel(toLabel(el, p)));

  const screen = (p: Pt): Pt => {
    const [x, y] = toLabel(e!, p);
    return [ox + x * scale, oy + y * scale];
  };

  type Drag = { what: 'p' | 'in' | 'out'; c: number; k: number; start: TemplateElement; contours: Contour[]; x0: number; y0: number; moved: boolean };
  let drag: Drag | null = null;

  function down(event: PointerEvent, what: Drag['what'], c: number, k: number) {
    if (event.button !== 0 || !e) return;
    event.preventDefault();
    event.stopPropagation();
    chosen = { c, k };
    if (what === 'p' && event.shiftKey) {
      // Sharp to smooth, or back.
      const nodes = contours[c]!.nodes;
      const next = contours.map((ct) => ({ ...ct, nodes: [...ct.nodes] }));
      const wrap = (j: number) => (contours[c]!.closed ? nodes[(j + nodes.length) % nodes.length] : nodes[j]);
      const t = toggled(nodes[k]!, wrap(k + 1), wrap(k - 1));
      next[c]!.nodes[k] = { ...t, ...(t.in ? { in: kept(e, t.in) } : {}), ...(t.out ? { out: kept(e, t.out) } : {}) };
      editor.update(index, withContours(doc, e, next), 'Make a point ' + (nodes[k]!.smooth ? 'sharp' : 'smooth'));
      return;
    }
    const start = $state.snapshot(e) as TemplateElement;
    drag = { what, c, k, start, contours: contoursOf(start), x0: event.clientX, y0: event.clientY, moved: false };
    (event.currentTarget as Element).setPointerCapture(event.pointerId);
  }

  function move(event: PointerEvent) {
    const d = drag;
    if (!d) return;
    if (!d.moved) {
      if (Math.abs(event.clientX - d.x0) + Math.abs(event.clientY - d.y0) < 2) return;
      d.moved = true;
      editor.begin('Move a point');
    }
    const node = d.contours[d.c]!.nodes[d.k]!;
    const from = d.what === 'p' ? node.p : (node[d.what] ?? node.p);
    // Where it is dragged to on the label, snapped, back in the element's frame.
    const [lx, ly] = toLabel(d.start, from);
    const target: Pt = [lx + (event.clientX - d.x0) / scale, ly + (event.clientY - d.y0) / scale];
    const snapped = snapPoint(doc, target[0], target[1], 6 / scale, event.shiftKey ? snapNothing : editor.snapTo, [index]);
    // Kept on the label, as the element's box must be.
    const to = toLocal(d.start, onLabel([snapped.x, snapped.y]));
    const next = d.contours.map((ct) => ({ ...ct, nodes: ct.nodes.map((n) => ({ ...n })) }));
    const n = next[d.c]!.nodes[d.k]!;
    if (d.what === 'p') {
      const dx = to[0] - n.p[0];
      const dy = to[1] - n.p[1];
      n.p = to;
      if (n.in) n.in = [n.in[0] + dx, n.in[1] + dy];
      if (n.out) n.out = [n.out[0] + dx, n.out[1] + dy];
    } else {
      n[d.what] = to;
      const other = d.what === 'in' ? 'out' : 'in';
      // A smooth point's other handle stays in line, its length kept; Alt breaks it.
      if (n.smooth && n[other] && !event.altKey) {
        const len = Math.hypot(n[other]![0] - n.p[0], n[other]![1] - n.p[1]);
        const vx = n.p[0] - to[0];
        const vy = n.p[1] - to[1];
        const k = Math.hypot(vx, vy) || 1;
        n[other] = [n.p[0] + (vx / k) * len, n.p[1] + (vy / k) * len];
      } else if (event.altKey) {
        n.smooth = false;
      }
      if (n[other]) n[other] = kept(d.start, n[other]!);
    }
    editor.doc.elements[index] = withContours(doc, d.start, next);
  }

  function up(event: PointerEvent) {
    const t = event.currentTarget as Element;
    if (t.hasPointerCapture(event.pointerId)) t.releasePointerCapture(event.pointerId);
    drag = null;
  }

  /** Delete: the chosen point, while enough remain; reports whether it did. */
  export function removeChosen(): boolean {
    if (!chosen || !e) return false;
    const c = contours[chosen.c];
    if (!c) return false;
    const least = e.shape === 'polygon' ? (c.closed ? 3 : 2) : 2;
    if (c.nodes.length <= least) return false;
    const next = contours.map((ct, i) => (i === chosen!.c ? { ...ct, nodes: ct.nodes.filter((_, k) => k !== chosen!.k) } : ct));
    editor.update(index, withContours(doc, e, next), 'Delete a point');
    chosen = null;
    return true;
  }

  /** A double-click on the outline, at a point on the label: a point added on the nearest segment. */
  export function insertAt(at: Pt): boolean {
    if (!e) return false;
    const q = toLocal(e, at);
    let best: { c: number; k: number; t: number; distance: number } | null = null;
    contours.forEach((ct, c) => {
      const n = ct.nodes.length;
      for (let k = 0; k < (ct.closed ? n : n - 1); k++) {
        const hit = nearestOn(ct.nodes[k]!, ct.nodes[(k + 1) % n]!, q);
        if (!best || hit.distance < best.distance) best = { c, k, ...hit };
      }
    });
    const b = best as { c: number; k: number; t: number; distance: number } | null;
    if (!b || b.distance * scale > 12) return false;
    const next = contours.map((ct) => ({ ...ct, nodes: [...ct.nodes] }));
    const nodes = next[b.c]!.nodes;
    const n = nodes.length;
    const split = splitAt(nodes[b.k]!, nodes[(b.k + 1) % n]!, b.t);
    nodes[b.k] = split.a;
    nodes[(b.k + 1) % n] = split.b;
    nodes.splice(b.k + 1, 0, split.mid);
    editor.update(index, withContours(doc, e, next), 'Add a point');
    chosen = { c: b.c, k: b.k + 1 };
    return true;
  }
</script>

{#if e}
  <g data-point-editor>
    {#each contours as ct, c (c)}
      {#each ct.nodes as n, k (k)}
        {@const p = screen(n.p)}
        {#each ['in', 'out'] as const as side (side)}
          {@const h = n[side]}
          {#if h}
            {@const s = screen(h)}
            <line x1={p[0]} y1={p[1]} x2={s[0]} y2={s[1]} class="handle-line" />
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <circle
              cx={s[0]}
              cy={s[1]}
              r="3.5"
              class="handle-dot"
              data-node-handle="{c}-{k}-{side}"
              onpointerdown={(ev) => down(ev, side, c, k)}
              onpointermove={move}
              onpointerup={up}
              onpointercancel={up}
            />
          {/if}
        {/each}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <rect
          x={p[0] - 4.5}
          y={p[1] - 4.5}
          width="9"
          height="9"
          class="node"
          class:chosen={chosen?.c === c && chosen.k === k}
          class:smooth={n.smooth}
          data-node="{c}-{k}"
          onpointerdown={(ev) => down(ev, 'p', c, k)}
          onpointermove={move}
          onpointerup={up}
          onpointercancel={up}
        />
      {/each}
    {/each}
  </g>
{/if}

<style>
  .node {
    fill: white;
    stroke: rgb(37 99 235);
    stroke-width: 1.5px;
    pointer-events: auto;
    cursor: move;
  }
  .node.smooth {
    rx: 4.5px;
  }
  .node.chosen {
    fill: rgb(37 99 235);
  }
  .handle-line {
    stroke: rgb(37 99 235 / 0.6);
    stroke-width: 1px;
  }
  .handle-dot {
    fill: rgb(37 99 235);
    pointer-events: auto;
    cursor: move;
  }
</style>
