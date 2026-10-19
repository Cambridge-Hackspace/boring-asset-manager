<script lang="ts">
  import {
    describeElement,
    elementsAt,
    gridStep,
    handles,
    moveElements,
    resizeElement,
    rotateElements,
    sides,
    sinCos,
    snap,
    withMargin,
    type CodeLayout,
    type Handle,
    type Side,
    type PrintArea,
    type Restack,
    type TemplateDoc,
    type TemplateElement,
  } from '../../lib/template';

  // The label as the server renders it, with an interactive outline over
  // each element. Changes are reported through onchange; onbegin marks the
  // start of one undoable gesture. With a printer chosen, what it does not
  // print is shaded, and elements reaching into it are outlined in amber.
  // Outlines are stacked as the elements are drawn, so a click takes the top
  // one; Alt+click takes the one beneath the selected one there. Outlines
  // turn with their elements, and the handle above one turns it, snapping to
  // multiples of 45° unless that is switched off.

  let {
    doc,
    selected,
    previewURL,
    previewStale,
    problemElements,
    area = null,
    onselect,
    onchange,
    onbegin,
    ondelete,
    onrestack,
    codeLayouts = {},
  }: {
    doc: TemplateDoc;
    selected: number[];
    previewURL: string;
    previewStale: boolean;
    problemElements: Set<number>;
    area?: PrintArea | null;
    onselect: (indices: number[]) => void;
    onchange: (elements: TemplateElement[]) => void;
    onbegin: () => void;
    ondelete: () => void;
    onrestack: (how: Restack) => void;
    /** How the server laid out each code, by element index, for its margins. */
    codeLayouts?: Record<number, CodeLayout>;
  } = $props();

  let surface: HTMLDivElement | undefined = $state();

  // Whether turning snaps to multiples of 45°, remembered in this browser.
  const snapKey = 'bam.editor-snap-angles';
  let snapAngles = $state(readSnap());
  function readSnap(): boolean {
    try {
      return localStorage.getItem(snapKey) !== 'off';
    } catch {
      return true;
    }
  }
  function setSnap(on: boolean) {
    snapAngles = on;
    try {
      localStorage.setItem(snapKey, on ? 'on' : 'off');
    } catch {
      // Storage unavailable: it lasts as long as the page.
    }
  }

  const pct = (v: number, total: number) => `${(v / total) * 100}%`;
  const boxStyle = (e: TemplateElement) =>
    `left:${pct(e.x, doc.width)};top:${pct(e.y, doc.height)};width:${pct(e.width, doc.width)};height:${pct(e.height, doc.height)}` +
    (e.rotation ? `;transform:rotate(${e.rotation}deg)` : '');

  const warned = $derived(new Set([...(area?.outside ?? []), ...(area?.nearEdge ?? [])]));

  /** The strips of the label outside what the printer prints. */
  const unprinted = $derived.by(() => {
    if (!area) return [];
    const { x, y, width, height } = area.printable;
    const W = doc.width;
    const H = doc.height;
    const eps = 0.001;
    return [
      { left: 0, top: 0, width: W, height: y },
      { left: 0, top: y + height, width: W, height: H - y - height },
      { left: 0, top: y, width: x, height },
      { left: x + width, top: y, width: W - x - width, height },
    ].filter((r) => r.width > eps && r.height > eps);
  });

  /** Template units per screen pixel. */
  function unitsPerPixel(): number {
    return surface ? doc.width / surface.clientWidth : 1;
  }

  type Gesture =
    | { kind: 'move'; x0: number; y0: number; start: TemplateElement[]; indices: number[]; moved: boolean }
    | { kind: 'resize'; x0: number; y0: number; start: TemplateElement[]; index: number; handle: Handle; moved: boolean }
    | { kind: 'rotate'; x0: number; y0: number; start: TemplateElement[]; index: number; cx: number; cy: number; moved: boolean }
    | { kind: 'margin'; x0: number; y0: number; start: TemplateElement[]; index: number; side: Side; from: number; moved: boolean };
  let gesture: Gesture | null = null;

  function pointerDownOnElement(event: PointerEvent, i: number) {
    if (event.button !== 0) return;
    event.preventDefault();
    let next = selected;
    if (event.altKey && surface) {
      // The next element down under the pointer, round to the top again.
      const r = surface.getBoundingClientRect();
      const stack = elementsAt(doc, ((event.clientX - r.left) / r.width) * doc.width, ((event.clientY - r.top) / r.height) * doc.height);
      const from = selected.length === 1 && stack.includes(selected[0]!) ? selected[0]! : i;
      i = stack[(stack.indexOf(from) + 1) % stack.length] ?? i;
      next = [i];
      onselect(next);
      surface.querySelector<HTMLElement>(`[data-element="${i}"]`)?.focus();
    } else {
      (event.currentTarget as HTMLElement).focus();
    }
    if (event.shiftKey) {
      next = selected.includes(i) ? selected.filter((s) => s !== i) : [...selected, i];
      onselect(next);
      return;
    }
    if (!next.includes(i)) {
      next = [i];
      onselect(next);
    }
    gesture = { kind: 'move', x0: event.clientX, y0: event.clientY, start: $state.snapshot(doc.elements) as TemplateElement[], indices: next, moved: false };
    surface?.setPointerCapture(event.pointerId);
  }

  function pointerDownOnHandle(event: PointerEvent, i: number, handle: Handle) {
    if (event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    gesture = { kind: 'resize', x0: event.clientX, y0: event.clientY, start: $state.snapshot(doc.elements) as TemplateElement[], index: i, handle, moved: false };
    surface?.setPointerCapture(event.pointerId);
  }

  function pointerDownOnRotate(event: PointerEvent, i: number) {
    if (event.button !== 0 || !surface) return;
    event.preventDefault();
    event.stopPropagation();
    const r = surface.getBoundingClientRect();
    const e = doc.elements[i]!;
    const cx = r.left + ((e.x + e.width / 2) / doc.width) * r.width;
    const cy = r.top + ((e.y + e.height / 2) / doc.height) * r.height;
    gesture = { kind: 'rotate', x0: event.clientX, y0: event.clientY, start: $state.snapshot(doc.elements) as TemplateElement[], index: i, cx, cy, moved: false };
    surface.setPointerCapture(event.pointerId);
  }

  /**
   * A code with one side's margin set from a length in the template's units:
   * whole modules, or on the grid in length.
   */
  function marginFrom(e: TemplateElement, side: Side, length: number, layout: CodeLayout): TemplateElement {
    const across = side === 'left' || side === 'right';
    const most = (across ? e.width : e.height) / 2;
    const value =
      e.margin_unit === 'length'
        ? Math.min(Math.max(0, snap(length, gridStep(doc.units), doc.units)), most)
        : Math.min(Math.max(0, Math.round(length / layout.module)), 20);
    return withMargin(e, [side], value);
  }

  function pointerDownOnMargin(event: PointerEvent, i: number, side: Side) {
    const layout = codeLayouts[i];
    if (event.button !== 0 || !layout) return;
    event.preventDefault();
    event.stopPropagation();
    gesture = { kind: 'margin', x0: event.clientX, y0: event.clientY, start: $state.snapshot(doc.elements) as TemplateElement[], index: i, side, from: layout.margin[side], moved: false };
    surface?.setPointerCapture(event.pointerId);
  }

  /** Margin handles are sliders: arrow keys move them a module, or a grid step in length. */
  function keydownOnMargin(event: KeyboardEvent, i: number, side: Side) {
    const layout = codeLayouts[i];
    const e = doc.elements[i];
    const step = { ArrowUp: 1, ArrowRight: 1, ArrowDown: -1, ArrowLeft: -1 }[event.key];
    if (!layout || !e || !step) return;
    event.preventDefault();
    event.stopPropagation();
    const by = e.margin_unit === 'length' ? gridStep(doc.units) : layout.module;
    onbegin();
    onchange(doc.elements.map((x, k) => (k === i ? marginFrom(x, side, layout.margin[side] + step * by, layout) : x)));
  }

  function pointerMove(event: PointerEvent) {
    if (!gesture) return;
    if (gesture.kind === 'margin') {
      const g = gesture;
      const e = g.start[g.index]!;
      const layout = codeLayouts[g.index];
      if (!layout) return;
      const k = unitsPerPixel();
      // The drag along the code's own sides, turned back.
      const [sin, cos] = sinCos(e.rotation ?? 0);
      const dx = (event.clientX - g.x0) * k;
      const dy = (event.clientY - g.y0) * k;
      const along = dx * cos + dy * sin;
      const downward = -dx * sin + dy * cos;
      const inward = { left: along, right: -along, top: downward, bottom: -downward }[g.side];
      if (!g.moved) {
        g.moved = true;
        onbegin();
      }
      onchange(g.start.map((x, i) => (i === g.index ? marginFrom(x, g.side, g.from + inward, layout) : x)));
      return;
    }
    if (gesture.kind === 'rotate') {
      const g = gesture;
      if (!g.moved) {
        g.moved = true;
        onbegin();
      }
      // The angle the pointer has swept about the element's center.
      const delta = ((Math.atan2(event.clientY - g.cy, event.clientX - g.cx) - Math.atan2(g.y0 - g.cy, g.x0 - g.cx)) * 180) / Math.PI;
      onchange(rotateElements(doc, [g.index], g.start, delta, snapAngles && !event.altKey));
      return;
    }
    const k = unitsPerPixel();
    const dx = (event.clientX - gesture.x0) * k;
    const dy = (event.clientY - gesture.y0) * k;
    if (!gesture.moved) {
      if (Math.abs(event.clientX - gesture.x0) + Math.abs(event.clientY - gesture.y0) < 3) return;
      gesture.moved = true;
      onbegin();
    }
    const step = event.altKey ? 0.001 : gridStep(doc.units);
    if (gesture.kind === 'move') {
      onchange(moveElements(doc, gesture.indices, gesture.start, dx, dy, step));
    } else {
      const g = gesture;
      onchange(g.start.map((e, i) => (i === g.index ? resizeElement(doc, e, g.handle, dx, dy, step) : e)));
    }
  }

  function pointerUp(event: PointerEvent) {
    if (gesture && surface?.hasPointerCapture(event.pointerId)) surface.releasePointerCapture(event.pointerId);
    gesture = null;
  }

  function pointerDownOnSurface(event: PointerEvent) {
    if (event.target === event.currentTarget || (event.target as Element).tagName === 'IMG') onselect([]);
  }

  const stackKeys: Record<string, Restack> = { PageUp: 'forward', PageDown: 'backward', Home: 'front', End: 'back' };

  function keydown(event: KeyboardEvent) {
    if (!selected.length) return;
    // [ and ] turn by 15°, or 1° with Shift; by key position, as Shift changes the character.
    const turn = { BracketLeft: -1, BracketRight: 1 }[event.code];
    if (turn) {
      event.preventDefault();
      onbegin();
      onchange(rotateElements(doc, selected, $state.snapshot(doc.elements) as TemplateElement[], turn * (event.shiftKey ? 1 : 15)));
      return;
    }
    const step = gridStep(doc.units) * (event.shiftKey ? 10 : 1);
    const moves: Record<string, [number, number]> = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] };
    const move = moves[event.key];
    if (move) {
      event.preventDefault();
      onbegin();
      onchange(moveElements(doc, selected, $state.snapshot(doc.elements) as TemplateElement[], move[0], move[1], gridStep(doc.units)));
    } else if (stackKeys[event.key]) {
      event.preventDefault();
      onrestack(stackKeys[event.key]!);
    } else if (event.key === 'Delete' || event.key === 'Backspace') {
      event.preventDefault();
      ondelete();
    } else if (event.key === 'Escape') {
      onselect([]);
    }
  }
</script>

<div class="space-y-2">
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    bind:this={surface}
    class="relative w-full touch-none select-none border border-zinc-400 bg-white shadow-sm dark:border-zinc-600"
    style="aspect-ratio: {doc.width} / {doc.height}"
    onpointerdown={pointerDownOnSurface}
    onpointermove={pointerMove}
    onpointerup={pointerUp}
    onpointercancel={pointerUp}
  >
    {#if previewURL}
      <img
        src={previewURL}
        alt="Label preview"
        class="pointer-events-none absolute inset-0 h-full w-full transition-opacity"
        class:opacity-40={previewStale}
        draggable="false"
      />
    {/if}
    {#each unprinted as r, i (i)}
      <div
        aria-hidden="true"
        class="unprinted pointer-events-none absolute"
        style="left:{pct(r.left, doc.width)};top:{pct(r.top, doc.height)};width:{pct(r.width, doc.width)};height:{pct(r.height, doc.height)}"
      ></div>
    {/each}
    {#if area && area.edge > 0}
      <div
        aria-hidden="true"
        class="pointer-events-none absolute border border-dashed border-amber-500/70"
        style="left:{pct(area.edge, doc.width)};top:{pct(area.edge, doc.height)};right:{pct(area.edge, doc.width)};bottom:{pct(area.edge, doc.height)}"
      ></div>
    {/if}
    {#each doc.elements as e, i (i)}
      {@const isSelected = selected.includes(i)}
      <div
        role="button"
        tabindex="0"
        aria-pressed={isSelected}
        aria-label={describeElement(e, i)}
        data-element={i}
        class="absolute cursor-move outline-offset-0 {isSelected
          ? 'outline-2 outline-blue-600'
          : 'outline-1 outline-dashed outline-zinc-400 hover:outline-blue-400'} {problemElements.has(i)
          ? 'bg-red-500/15 outline-red-600'
          : warned.has(i)
            ? 'bg-amber-400/15 outline-amber-500'
            : ''}"
        style={boxStyle(e)}
        onpointerdown={(ev) => pointerDownOnElement(ev, i)}
        onkeydown={keydown}
        onfocus={() => {
          if (!isSelected && !gesture) onselect([i]);
        }}
      >
        {#if isSelected && selected.length === 1}
          {#each handles as h (h)}
            <span
              aria-hidden="true"
              class="handle absolute border border-white bg-blue-600"
              style="cursor:{h}-resize;{h.includes('n') ? 'top:calc(var(--h) / -2)' : h.includes('s') ? 'bottom:calc(var(--h) / -2)' : 'top:calc(50% - var(--h) / 2)'};{h.includes('w')
                ? 'left:calc(var(--h) / -2)'
                : h.includes('e')
                  ? 'right:calc(var(--h) / -2)'
                  : 'left:calc(50% - var(--h) / 2)'}"
              onpointerdown={(ev) => pointerDownOnHandle(ev, i, h)}
            ></span>
          {/each}
          <span aria-hidden="true" class="rotate-stem absolute bg-blue-600"></span>
          <span
            aria-hidden="true"
            class="handle rotate-handle absolute rounded-full border border-white bg-blue-600"
            title="Drag to turn"
            onpointerdown={(ev) => pointerDownOnRotate(ev, i)}
          ></span>
        {/if}
      </div>
    {/each}
    {#if selected.length === 1 && doc.elements[selected[0]!]?.type === 'generator' && codeLayouts[selected[0]!]}
      {@const i = selected[0]!}
      {@const e = doc.elements[i]!}
      {@const layout = codeLayouts[i]!}
      <!-- The selected code's margin, shaded, with a handle on each inner edge. -->
      <div class="pointer-events-none absolute" style={boxStyle(e)}>
        {#each sides as side (side)}
          {@const m = layout.margin[side]}
          {@const across = side === 'left' || side === 'right'}
          <span
            aria-hidden="true"
            class="code-margin absolute"
            style="{side}:0;{across ? `top:0;bottom:0;width:${pct(m, e.width)}` : `left:0;right:0;height:${pct(m, e.height)}`}"
          ></span>
          <span
            role="slider"
            tabindex="0"
            aria-label="{side[0]!.toUpperCase() + side.slice(1)} margin"
            aria-valuemin={0}
            aria-valuemax={e.margin_unit === 'length' ? (across ? e.width : e.height) / 2 : 20}
            aria-valuenow={e.margin_unit === 'length' ? m : Math.round(m / layout.module)}
            aria-valuetext={e.margin_unit === 'length' ? `${m} ${doc.units}` : `${Math.round(m / layout.module)} modules`}
            class="margin-handle pointer-events-auto absolute border border-white bg-emerald-600"
            style="{across ? `top:calc(50% - var(--h) / 2);${side}:calc(${pct(m, e.width)} - var(--h) / 2)` : `left:calc(50% - var(--h) / 2);${side}:calc(${pct(m, e.height)} - var(--h) / 2)`};cursor:{across ? 'ew' : 'ns'}-resize"
            onpointerdown={(ev) => pointerDownOnMargin(ev, i, side)}
            onkeydown={(ev) => keydownOnMargin(ev, i, side)}
          ></span>
        {/each}
      </div>
    {/if}
  </div>
  <label class="flex items-center gap-2 text-sm">
    <input type="checkbox" checked={snapAngles} onchange={(ev) => setSnap(ev.currentTarget.checked)} />
    Snap angles to 45°
  </label>
  {#if unprinted.length || (area && area.edge > 0)}
    <p class="text-xs text-zinc-600 dark:text-zinc-400">
      {#if unprinted.length}Shaded: where the printer does not print.{/if}
      {#if area && area.edge > 0}Dashed: within {area.edge} {doc.units} of the edge, which may be cut off.{/if}
    </p>
  {/if}
  <p class="hidden text-xs text-zinc-600 pointer-coarse:block dark:text-zinc-400">
    Drag to move, and drag a corner to resize; or set an element's position and size exactly in its settings.
  </p>
  <p class="text-xs text-zinc-600 pointer-coarse:hidden dark:text-zinc-400">
    Drag to move, drag a corner to resize, drag the round handle to turn, Shift+click to select several, Alt+click to select what is beneath. With an
    element focused: arrow keys move it by {gridStep(doc.units)} {doc.units} (Shift for ten times that), [ and ] turn it 15° (Shift for 1°), Page Up and
    Page Down bring it forward and send it backward, Home and End to the front and back, Delete removes it, Escape deselects. Hold Alt while dragging to
    leave the grid, or while turning not to snap.
  </p>
</div>

<style>
  /* Resize handles: larger where a finger, not a mouse, takes them. */
  .handle {
    --h: 10px;
    width: var(--h);
    height: var(--h);
  }
  @media (pointer: coarse) {
    .handle {
      --h: 22px;
    }
  }
  /* A code's margin, and the handles that set it. */
  .code-margin {
    background: rgb(16 185 129 / 0.18);
  }
  .margin-handle {
    --h: 10px;
    width: var(--h);
    height: var(--h);
    transform: rotate(45deg);
  }
  @media (pointer: coarse) {
    .margin-handle {
      --h: 22px;
    }
  }
  /* The turning handle, on a stem above the top edge. */
  .rotate-handle {
    left: calc(50% - var(--h) / 2);
    top: calc(-1 * var(--h) - 16px);
    cursor: grab;
  }
  .rotate-stem {
    left: calc(50% - 1px);
    top: -16px;
    width: 2px;
    height: 16px;
  }
  .unprinted {
    background: repeating-linear-gradient(45deg, rgb(113 113 122 / 0.35) 0 4px, rgb(113 113 122 / 0.12) 4px 8px);
  }
</style>
