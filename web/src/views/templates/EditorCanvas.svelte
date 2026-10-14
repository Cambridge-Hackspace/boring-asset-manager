<script lang="ts">
  import {
    describeElement,
    gridStep,
    handles,
    moveElements,
    resizeElement,
    type Handle,
    type TemplateDoc,
    type TemplateElement,
  } from '../../lib/template';

  // The label as the server renders it, with an interactive outline over
  // each element. Changes are reported through onchange; onbegin marks the
  // start of one undoable gesture.
  let {
    doc,
    selected,
    previewURL,
    previewStale,
    problemElements,
    onselect,
    onchange,
    onbegin,
    ondelete,
  }: {
    doc: TemplateDoc;
    selected: number[];
    previewURL: string;
    previewStale: boolean;
    problemElements: Set<number>;
    onselect: (indices: number[]) => void;
    onchange: (elements: TemplateElement[]) => void;
    onbegin: () => void;
    ondelete: () => void;
  } = $props();

  let surface: HTMLDivElement | undefined = $state();

  const pct = (v: number, total: number) => `${(v / total) * 100}%`;
  const boxStyle = (e: TemplateElement) =>
    `left:${pct(e.x, doc.width)};top:${pct(e.y, doc.height)};width:${pct(e.width, doc.width)};height:${pct(e.height, doc.height)}`;

  /** Template units per screen pixel. */
  function unitsPerPixel(): number {
    return surface ? doc.width / surface.clientWidth : 1;
  }

  type Gesture =
    | { kind: 'move'; x0: number; y0: number; start: TemplateElement[]; indices: number[]; moved: boolean }
    | { kind: 'resize'; x0: number; y0: number; start: TemplateElement[]; index: number; handle: Handle; moved: boolean };
  let gesture: Gesture | null = null;

  function pointerDownOnElement(event: PointerEvent, i: number) {
    if (event.button !== 0) return;
    event.preventDefault();
    (event.currentTarget as HTMLElement).focus();
    let next = selected;
    if (event.shiftKey) {
      next = selected.includes(i) ? selected.filter((s) => s !== i) : [...selected, i];
      onselect(next);
      return;
    }
    if (!selected.includes(i)) {
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

  function pointerMove(event: PointerEvent) {
    if (!gesture) return;
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

  function keydown(event: KeyboardEvent) {
    if (!selected.length) return;
    const step = gridStep(doc.units) * (event.shiftKey ? 10 : 1);
    const moves: Record<string, [number, number]> = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] };
    const move = moves[event.key];
    if (move) {
      event.preventDefault();
      onbegin();
      onchange(moveElements(doc, selected, $state.snapshot(doc.elements) as TemplateElement[], move[0], move[1], gridStep(doc.units)));
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
    {#each doc.elements as e, i (i)}
      {@const isSelected = selected.includes(i)}
      <div
        role="button"
        tabindex="0"
        aria-pressed={isSelected}
        aria-label={describeElement(e, i)}
        class="absolute cursor-move outline-offset-0 {isSelected
          ? 'outline-2 outline-blue-600'
          : 'outline-1 outline-dashed outline-zinc-400 hover:outline-blue-400'} {problemElements.has(i) ? 'bg-red-500/15 outline-red-600' : ''}"
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
              class="absolute h-2.5 w-2.5 border border-white bg-blue-600"
              style="cursor:{h}-resize;{h.includes('n') ? 'top:-5px' : h.includes('s') ? 'bottom:-5px' : 'top:calc(50% - 5px)'};{h.includes('w')
                ? 'left:-5px'
                : h.includes('e')
                  ? 'right:-5px'
                  : 'left:calc(50% - 5px)'}"
              onpointerdown={(ev) => pointerDownOnHandle(ev, i, h)}
            ></span>
          {/each}
        {/if}
      </div>
    {/each}
  </div>
  <p class="text-xs text-zinc-500">
    Drag to move, drag a corner to resize, Shift+click to select several. With an element focused: arrow keys move it by {gridStep(doc.units)}
    {doc.units} (Shift for ten times that), Delete removes it, Escape deselects. Hold Alt while dragging to leave the grid.
  </p>
</div>
