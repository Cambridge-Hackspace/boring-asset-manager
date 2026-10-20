<script lang="ts">
  import type { Frame } from '../../../lib/live/client';
  import {
    boundsOf,
    describeElement,
    gridStep,
    handles,
    moveElements,
    newElement,
    newShape,
    polygonThrough,
    resizeElement,
    scaleElements,
    sinCos,
    snap,
    tidy,
    withGroups,
    type Box,
    type Handle,
    type PrintArea,
    type ShapeKind,
    type TemplateElement,
  } from '../../../lib/template';
  import { useEditor, type Tool } from './editor.svelte';

  // The label on its desk: the live renderer's drawing of it (or, until it
  // can, the server's), an outline over each element to take it by, and the
  // selection's handles, drawn the same size at every zoom, as in Krita.
  // The select tool takes, moves and resizes; the others draw what they
  // name, dragged out or clicked in place.
  let {
    frame,
    previewURL,
    previewStale,
    problemElements,
    area,
    generatorType,
    onfile,
  }: {
    frame: Frame | null;
    previewURL: string;
    previewStale: boolean;
    problemElements: Set<number>;
    /** Where the printer chosen prints, shading the rest; null with none. */
    area: PrintArea | null;
    /** The code type a new code is. */
    generatorType: string;
    /** An SVG file dropped on the label, to import. */
    onfile: (file: File) => void;
  } = $props();

  const editor = useEditor();
  const doc = $derived(editor.doc);

  let viewport = $state<HTMLDivElement | null>(null);
  let paper = $state<HTMLDivElement | null>(null);

  // The view: screen pixels per template unit, and where the label's top
  // left corner is in the viewport. It fits the label until changed.
  let scale = $state(4);
  let ox = $state(24);
  let oy = $state(24);
  let fitted = true;
  let viewW = $state(0);
  let viewH = $state(0);
  const pad = 32;

  function fit() {
    if (!viewW || !viewH) return;
    scale = Math.max(0.1, Math.min((viewW - 2 * pad) / doc.width, (viewH - 2 * pad) / doc.height));
    ox = (viewW - doc.width * scale) / 2;
    oy = (viewH - doc.height * scale) / 2;
  }

  $effect(() => {
    void doc.width;
    void doc.height;
    void viewW;
    void viewH;
    if (fitted) fit();
  });

  // Hiding everything but the canvas moves it on the screen; the label is
  // moved back, so it stays where it was under the eye.
  let lastOrigin: { left: number; top: number } | null = null;
  $effect(() => {
    void editor.canvasOnly;
    if (!viewport) return;
    const was = lastOrigin;
    queueMicrotask(() => {
      if (!viewport) return;
      const r = viewport.getBoundingClientRect();
      if (was && (r.left !== was.left || r.top !== was.top)) {
        fitted = false;
        ox += was.left - r.left;
        oy += was.top - r.top;
      }
      lastOrigin = { left: r.left, top: r.top };
    });
  });

  /** Where a pointer is on the label, in its units. */
  function onLabel(event: PointerEvent | MouseEvent): [number, number] {
    const r = paper!.getBoundingClientRect();
    return [((event.clientX - r.left) / r.width) * doc.width, ((event.clientY - r.top) / r.height) * doc.height];
  }

  /** A point on the label, in its units, on the grid unless Alt is held, kept on the label. */
  function gridPoint(event: PointerEvent | MouseEvent): [number, number] {
    const [x, y] = onLabel(event);
    const cx = Math.min(Math.max(x, 0), doc.width);
    const cy = Math.min(Math.max(y, 0), doc.height);
    if (event.altKey) return [tidy(cx, doc.units), tidy(cy, doc.units)];
    const step = gridStep(doc.units);
    return [snap(cx, step, doc.units), snap(cy, step, doc.units)];
  }

  // Screen geometry for the overlay, in viewport pixels.
  const sx = (x: number) => ox + x * scale;
  const sy = (y: number) => oy + y * scale;

  /** The corners and side midpoints of an element's box as it lies turned, on the screen. */
  function handlePoints(e: TemplateElement): Record<Handle, [number, number]> {
    const [sin, cos] = sinCos(e.rotation ?? 0);
    const cx = sx(e.x + e.width / 2);
    const cy = sy(e.y + e.height / 2);
    const at = (u: number, v: number): [number, number] => {
      const lx = (u * e.width * scale) / 2;
      const ly = (v * e.height * scale) / 2;
      return [cx + lx * cos - ly * sin, cy + lx * sin + ly * cos];
    };
    return { nw: at(-1, -1), n: at(0, -1), ne: at(1, -1), e: at(1, 0), se: at(1, 1), s: at(0, 1), sw: at(-1, 1), w: at(-1, 0) };
  }

  const cursors = ['ns-resize', 'nesw-resize', 'ew-resize', 'nwse-resize'];
  /** The resize cursor for a handle, turned with the element. */
  function cursorFor(handle: Handle, rotation = 0): string {
    const base = { n: 0, ne: 45, e: 90, se: 135, s: 180, sw: 225, w: 270, nw: 315 }[handle];
    const octant = Math.round((((base + rotation) % 180) + 180) % 180 / 45) % 4;
    return cursors[octant]!;
  }

  const single = $derived(editor.single);
  const selectionBox = $derived(editor.selected.length > 1 ? boundsOf(editor.selected.map((i) => doc.elements[i]!).filter(Boolean)) : null);
  const warned = $derived(new Set([...(area?.outside ?? []), ...(area?.nearEdge ?? [])]));

  /** The strips of the label outside what the printer prints. */
  const unprinted = $derived.by(() => {
    if (!area) return [];
    const { x, y, width, height } = area.printable;
    const W = doc.width;
    const H = doc.height;
    return [
      { x: 0, y: 0, width: W, height: y },
      { x: 0, y: y + height, width: W, height: H - y - height },
      { x: 0, y, width: x, height },
      { x: x + width, y, width: W - x - width, height },
    ].filter((r) => r.width > 0.001 && r.height > 0.001);
  });

  const pct = (v: number, total: number) => `${(v / total) * 100}%`;
  const boxStyle = (e: Box & { rotation?: number }) =>
    `left:${pct(e.x, doc.width)};top:${pct(e.y, doc.height)};width:${pct(e.width, doc.width)};height:${pct(e.height, doc.height)}` +
    (e.rotation ? `;transform:rotate(${e.rotation}deg)` : '');
  // A level or upright line's box has no height or width: its outline is
  // given some, to be seen and taken.
  const hitStyle = (e: TemplateElement) =>
    boxStyle(e) + (e.type === 'shape' && e.shape === 'line' && (!e.width || !e.height) ? ';padding:4px;margin:-4px;box-sizing:content-box' : '');

  // What a drag is doing.
  type Gesture =
    | { kind: 'move'; x0: number; y0: number; start: TemplateElement[]; indices: number[]; moved: boolean }
    | { kind: 'resize'; x0: number; y0: number; start: TemplateElement[]; index: number; handle: Handle; moved: boolean }
    | { kind: 'scale'; x0: number; y0: number; start: TemplateElement[]; indices: number[]; handle: Handle; box: Box; moved: boolean }
    | { kind: 'draw'; from: [number, number]; to: [number, number] };
  let gesture = $state<Gesture | null>(null);
  // A polygon's points so far, in the template's units.
  let points = $state<[number, number][]>([]);

  const toolNames: Record<Exclude<Tool, 'select'>, string> = {
    text: 'a text box',
    code: 'a code',
    image: 'an image',
    rectangle: 'a rectangle',
    ellipse: 'an ellipse',
    line: 'a line',
    polygon: 'a polygon',
    path: 'a path',
  };

  // What the status bar says can be done now.
  let overHandle = $state(false);
  $effect(() => {
    const g = gesture;
    const tool = editor.tool;
    if (g?.kind === 'move') editor.hint = 'Moving. Alt: off the grid. Esc: put it back.';
    else if (g?.kind === 'resize' || g?.kind === 'scale') editor.hint = 'Resizing. Alt: off the grid. Esc: put it back.';
    else if (g?.kind === 'draw') editor.hint = `Drawing ${toolNames[tool as Exclude<Tool, 'select'>] ?? 'a shape'}. Alt: off the grid. Esc: stop.`;
    else if (tool === 'polygon')
      editor.hint = points.length ? 'Click to place the next point. Double-click or Enter: finish. Click the first point: close it. Esc: stop.' : 'Click to place the first point.';
    else if (tool !== 'select') editor.hint = `Drag to draw ${toolNames[tool]}, or click to place one. Esc: back to selecting.`;
    else if (overHandle) editor.hint = 'Drag to resize.';
    else if (editor.selected.length) editor.hint = 'Drag to move. Shift+click: add or remove. Arrow keys: move (Shift: ten steps, Alt: a tenth). Esc: deselect.';
    else editor.hint = 'Click an element to select it. Shift+click: select several.';
  });

  function capture(event: PointerEvent) {
    viewport?.setPointerCapture(event.pointerId);
  }

  let byPointer = false;

  function downOnElement(event: PointerEvent, i: number) {
    if (event.button !== 0 || editor.tool !== 'select') return;
    event.preventDefault();
    event.stopPropagation();
    // Focused by the pointer, it is selected below, not by its focus.
    byPointer = true;
    (event.currentTarget as HTMLElement).focus();
    byPointer = false;
    // A click takes the element's whole group.
    const taken = withGroups(doc.elements, [i]);
    let next = editor.selected;
    if (event.shiftKey) {
      editor.selected = taken.every((k) => next.includes(k)) ? next.filter((s) => !taken.includes(s)) : [...new Set([...next, ...taken])];
      return;
    }
    if (!next.includes(i)) {
      next = taken;
      editor.selected = next;
    }
    gesture = { kind: 'move', x0: event.clientX, y0: event.clientY, start: editor.elements(), indices: next, moved: false };
    capture(event);
  }

  function downOnHandle(event: PointerEvent, handle: Handle) {
    if (event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    const start = editor.elements();
    if (single) {
      gesture = { kind: 'resize', x0: event.clientX, y0: event.clientY, start, index: editor.selected[0]!, handle, moved: false };
    } else if (selectionBox) {
      gesture = { kind: 'scale', x0: event.clientX, y0: event.clientY, start, indices: [...editor.selected], handle, box: { ...selectionBox }, moved: false };
    }
    capture(event);
  }

  function downOnDesk(event: PointerEvent) {
    if (event.button !== 0 || !paper) return;
    if (editor.tool === 'select') {
      // Off every element: nothing selected, or as it was with Shift.
      if (!event.shiftKey) editor.deselect();
      return;
    }
    event.preventDefault();
    const p = gridPoint(event);
    if (editor.tool === 'polygon') {
      const first = points[0];
      // Clicking the first point again closes it.
      const near = first && Math.hypot(first[0] - p[0], first[1] - p[1]) < gridStep(doc.units) * 1.5;
      if (near && points.length >= 3) finishPolygon();
      else points = [...points, p];
      return;
    }
    gesture = { kind: 'draw', from: p, to: p };
    capture(event);
  }

  function pointerMove(event: PointerEvent) {
    if (paper) {
      const [x, y] = onLabel(event);
      editor.pointer = x >= 0 && y >= 0 && x <= doc.width && y <= doc.height ? { x: tidy(x, doc.units), y: tidy(y, doc.units) } : null;
    }
    const g = gesture;
    if (!g) return;
    if (g.kind === 'draw') {
      g.to = gridPoint(event);
      return;
    }
    const dx = (event.clientX - g.x0) / scale;
    const dy = (event.clientY - g.y0) / scale;
    if (!g.moved) {
      if (Math.abs(event.clientX - g.x0) + Math.abs(event.clientY - g.y0) < 3) return;
      g.moved = true;
      editor.begin();
    }
    const step = event.altKey ? 0.001 : gridStep(doc.units);
    if (g.kind === 'move') {
      editor.doc.elements = moveElements(doc, g.indices, g.start, dx, dy, step);
    } else if (g.kind === 'scale') {
      const b = g.box;
      let [x0, y0, x1, y1] = [b.x, b.y, b.x + b.width, b.y + b.height];
      if (g.handle.includes('w')) x0 = Math.min(snap(x0 + dx, step, doc.units), x1 - step);
      if (g.handle.includes('e')) x1 = Math.max(snap(x1 + dx, step, doc.units), x0 + step);
      if (g.handle.includes('n')) y0 = Math.min(snap(y0 + dy, step, doc.units), y1 - step);
      if (g.handle.includes('s')) y1 = Math.max(snap(y1 + dy, step, doc.units), y0 + step);
      editor.doc.elements = scaleElements(doc, g.indices, g.start, b, { x: x0, y: y0, width: x1 - x0, height: y1 - y0 });
    } else {
      editor.doc.elements = g.start.map((e, i) => (i === g.index ? resizeElement(doc, e, g.handle, dx, dy, step) : e));
    }
  }

  function pointerUp(event: PointerEvent) {
    if (viewport?.hasPointerCapture(event.pointerId)) viewport.releasePointerCapture(event.pointerId);
    const g = gesture;
    gesture = null;
    if (g?.kind === 'draw') finishDrawing(g.from, g.to);
  }

  /** Adds what the tool draws, in the box dragged out, or at its usual size where clicked. */
  function finishDrawing(a: [number, number], b: [number, number]) {
    const tool = editor.tool;
    if (tool === 'select' || tool === 'polygon') return;
    const step = gridStep(doc.units);
    const box = { x: Math.min(a[0], b[0]), y: Math.min(a[1], b[1]), width: tidy(Math.abs(b[0] - a[0]), doc.units), height: tidy(Math.abs(b[1] - a[1]), doc.units) };
    const clicked = box.width < step && box.height < step;
    let e: TemplateElement;
    if (tool === 'line' && !clicked) {
      // From the bottom left up to the top right, or the other way.
      const rising = (b[0] - a[0]) * (b[1] - a[1]) < 0;
      e = { ...newShape('line', doc, box), ...(rising ? { flip: true } : {}) };
    } else {
      const made = tool === 'text' || tool === 'code' || tool === 'image' ? newElement(({ text: 'text', code: 'generator', image: 'static_image' } as const)[tool], doc, generatorType) : newShape(tool as ShapeKind, doc);
      e = clicked
        ? { ...made, x: tidy(Math.min(a[0], doc.width - made.width), doc.units), y: tidy(Math.min(a[1], doc.height - made.height), doc.units) }
        : { ...made, ...box, width: Math.max(box.width, step), height: Math.max(box.height, step) };
    }
    editor.add(e);
    editor.tool = 'select';
  }

  function finishPolygon() {
    // A double click's clicks each added a point; one is enough.
    const pts = points.filter((p, k) => k === 0 || p[0] !== points[k - 1]![0] || p[1] !== points[k - 1]![1]);
    points = [];
    if (pts.length < 3) return;
    const xs = pts.map((p) => p[0]);
    const ys = pts.map((p) => p[1]);
    const box = { x: Math.min(...xs), y: Math.min(...ys), width: Math.max(...xs) - Math.min(...xs), height: Math.max(...ys) - Math.min(...ys) };
    const start = { ...newShape('polygon', doc, box), points: [] };
    editor.add(polygonThrough(doc, start, pts.map(([x, y]) => [x - box.x, y - box.y])));
    editor.tool = 'select';
  }

  /** Escape: stops what is in progress, and reports whether there was any. */
  export function cancel(): boolean {
    const g = gesture;
    if (g) {
      gesture = null;
      if (g.kind !== 'draw' && g.moved) editor.cancel();
      return true;
    }
    if (points.length) {
      points = [];
      return true;
    }
    if (editor.tool !== 'select') {
      editor.tool = 'select';
      return true;
    }
    return false;
  }

  /** Enter finishes a polygon being drawn. */
  export function finish(): boolean {
    if (editor.tool === 'polygon' && points.length >= 3) {
      finishPolygon();
      return true;
    }
    return false;
  }

  $effect(() => {
    // Another tool chosen: a polygon begun is let go.
    if (editor.tool !== 'polygon') points = [];
  });

  // The live renderer's frame, drawn dot for dot.
  let frameCanvas = $state<HTMLCanvasElement | null>(null);
  $effect(() => {
    const f = frame;
    if (!f || !frameCanvas) return;
    frameCanvas.getContext('2d')?.putImageData(new ImageData(new Uint8ClampedArray(f.pixels), f.width, f.height), 0, 0);
  });
  // Dots larger than a screen pixel are shown as dots, not blurred.
  const crisp = $derived(scale / (doc.dpi * (doc.units === 'in' ? 1 : 1 / 25.4)) > 1.5);

  const draft = $derived.by(() => {
    const g = gesture;
    if (g?.kind !== 'draw') return null;
    return { x: Math.min(g.from[0], g.to[0]), y: Math.min(g.from[1], g.to[1]), w: Math.abs(g.to[0] - g.from[0]), h: Math.abs(g.to[1] - g.from[1]), g };
  });
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  bind:this={viewport}
  bind:clientWidth={viewW}
  bind:clientHeight={viewH}
  class="relative h-full w-full overflow-hidden bg-zinc-300 select-none dark:bg-zinc-800 {editor.tool === 'select' ? '' : 'cursor-crosshair'}"
  onpointerdown={downOnDesk}
  onpointermove={pointerMove}
  onpointerup={pointerUp}
  onpointercancel={pointerUp}
  onpointerleave={() => (editor.pointer = null)}
  ondblclick={(ev) => {
    if (editor.tool !== 'polygon') return;
    ev.preventDefault();
    finishPolygon();
  }}
  ondragover={(ev) => {
    if (ev.dataTransfer?.types.includes('Files')) ev.preventDefault();
  }}
  ondrop={(ev) => {
    const file = ev.dataTransfer?.files[0];
    if (!file) return;
    ev.preventDefault();
    if (file.type === 'image/svg+xml' || file.name.toLowerCase().endsWith('.svg')) onfile(file);
  }}
>
  <div
    bind:this={paper}
    class="absolute bg-white shadow-[0_1px_4px_rgb(0_0_0/0.35)]"
    style="left:{ox}px;top:{oy}px;width:{doc.width * scale}px;height:{doc.height * scale}px"
  >
    {#if frame}
      <div role="img" aria-label="Label preview" class="pointer-events-none absolute inset-0">
        <canvas bind:this={frameCanvas} class="h-full w-full" style:image-rendering={crisp ? 'pixelated' : 'auto'} width={frame.width} height={frame.height}></canvas>
      </div>
    {:else if previewURL}
      <img src={previewURL} alt="Label preview" class="pointer-events-none absolute inset-0 h-full w-full transition-opacity" class:opacity-40={previewStale} draggable="false" />
    {/if}
    {#each unprinted as r, i (i)}
      <div aria-hidden="true" class="unprinted pointer-events-none absolute" style={boxStyle(r)}></div>
    {/each}
    {#if area && area.edge > 0}
      <div
        aria-hidden="true"
        class="pointer-events-none absolute border border-dashed border-amber-500/70"
        style={boxStyle({ x: area.edge, y: area.edge, width: doc.width - 2 * area.edge, height: doc.height - 2 * area.edge })}
      ></div>
    {/if}
    {#each doc.elements as e, i (i)}
      <button
        type="button"
        data-element={i}
        class="element absolute"
        class:problem={problemElements.has(i)}
        class:warned={warned.has(i)}
        class:locked={e.locked}
        class:passive={editor.tool !== 'select'}
        style={hitStyle(e)}
        aria-label={describeElement(e, i) + (e.locked ? ', locked' : '')}
        aria-pressed={editor.selected.includes(i)}
        tabindex={e.locked ? -1 : 0}
        onpointerdown={(ev) => downOnElement(ev, i)}
        onfocus={() => {
          // Tabbed to, it is selected, as clicked.
          if (!byPointer && !editor.selected.includes(i) && !e.locked) editor.selected = withGroups(doc.elements, [i]);
        }}
      ></button>
    {/each}
  </div>

  <!-- The selection, the same size at every zoom. -->
  <svg class="pointer-events-none absolute inset-0 h-full w-full overflow-visible" aria-hidden="true">
    {#each editor.selected as i (i)}
      {@const e = doc.elements[i]}
      {#if e}
        {@const h = handlePoints(e)}
        <polygon points={[h.nw, h.ne, h.se, h.sw].map((p) => p.join(',')).join(' ')} class="outline" />
      {/if}
    {/each}
    {#if single && !single.locked}
      {@const h = handlePoints(single)}
      {#each handles as k (k)}
        <rect
          x={h[k][0] - 4}
          y={h[k][1] - 4}
          width="8"
          height="8"
          class="handle"
          data-handle={k}
          style:cursor={cursorFor(k, single.rotation)}
          onpointerdown={(ev) => downOnHandle(ev, k)}
          onpointerenter={() => (overHandle = true)}
          onpointerleave={() => (overHandle = false)}
        />
      {/each}
    {:else if selectionBox}
      {@const b = selectionBox}
      <rect x={sx(b.x)} y={sy(b.y)} width={b.width * scale} height={b.height * scale} class="outline group-box" />
      {#each handles as k (k)}
        {@const px = sx(b.x + (k.includes('w') ? 0 : k.includes('e') ? b.width : b.width / 2))}
        {@const py = sy(b.y + (k.includes('n') ? 0 : k.includes('s') ? b.height : b.height / 2))}
        <rect
          x={px - 4}
          y={py - 4}
          width="8"
          height="8"
          class="handle"
          data-handle={k}
          style:cursor={cursorFor(k)}
          onpointerdown={(ev) => downOnHandle(ev, k)}
          onpointerenter={() => (overHandle = true)}
          onpointerleave={() => (overHandle = false)}
        />
      {/each}
    {/if}
    {#if draft}
      {#if editor.tool === 'line'}
        <line x1={sx(draft.g.from[0])} y1={sy(draft.g.from[1])} x2={sx(draft.g.to[0])} y2={sy(draft.g.to[1])} class="draft" />
      {:else if editor.tool === 'ellipse'}
        <ellipse cx={sx(draft.x + draft.w / 2)} cy={sy(draft.y + draft.h / 2)} rx={(draft.w * scale) / 2} ry={(draft.h * scale) / 2} class="draft" />
      {:else}
        <rect x={sx(draft.x)} y={sy(draft.y)} width={draft.w * scale} height={draft.h * scale} class="draft" />
      {/if}
    {/if}
    {#if points.length}
      <polyline points={points.map(([x, y]) => `${sx(x)},${sy(y)}`).join(' ')} class="draft" />
      {#each points as [x, y], k (k)}
        <circle cx={sx(x)} cy={sy(y)} r="3.5" class="handle" />
      {/each}
    {/if}
  </svg>
</div>

<style>
  .element {
    border-radius: 1px;
    outline: none;
  }
  .element:hover,
  .element:focus-visible {
    box-shadow: 0 0 0 1px rgb(37 99 235 / 0.6);
  }
  .element.problem {
    box-shadow: 0 0 0 1.5px rgb(220 38 38);
  }
  .element.warned {
    box-shadow: 0 0 0 1.5px rgb(245 158 11);
  }
  .element.locked,
  .element.passive {
    pointer-events: none;
  }
  .unprinted {
    background: repeating-linear-gradient(45deg, rgb(113 113 122 / 0.35) 0 4px, rgb(113 113 122 / 0.12) 4px 8px);
  }
  .outline {
    fill: none;
    stroke: rgb(37 99 235);
    stroke-width: 1px;
  }
  .group-box {
    stroke-dasharray: 4 3;
  }
  .handle {
    fill: white;
    stroke: rgb(37 99 235);
    stroke-width: 1px;
    pointer-events: auto;
  }
  .draft {
    fill: rgb(37 99 235 / 0.08);
    stroke: rgb(37 99 235);
    stroke-width: 1.5px;
    stroke-dasharray: 4 3;
  }
</style>
