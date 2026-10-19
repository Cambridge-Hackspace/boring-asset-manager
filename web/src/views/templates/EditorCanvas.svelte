<script lang="ts">
  import { tick } from 'svelte';
  import {
    bounds,
    boundsOf,
    describeElement,
    elementsAt,
    gridStep,
    handles,
    moveElements,
    PX_PER_MM,
    resizeElement,
    rotateElements,
    scaleElements,
    sides,
    sinCos,
    smartSnap,
    snap,
    toMM,
    withGroups,
    withMargin,
    type Box,
    type CodeLayout,
    type Guides,
    type Handle,
    type Side,
    type PrintArea,
    type Restack,
    type ShapeKind,
    type TemplateDoc,
    type TemplateElement,
  } from '../../lib/template';
  import GuideLines from './GuideLines.svelte';
  import PolygonPoints from './PolygonPoints.svelte';
  import Ruler from './Ruler.svelte';
  import ShapeTool from './ShapeTool.svelte';

  // The label as the server renders it, with an interactive outline over
  // each element, rulers along it, and its guides. Changes are reported
  // through onchange; onbegin marks the start of one undoable gesture. With
  // a printer chosen, what it does not print is shaded, and elements
  // reaching into it are outlined in amber. Outlines are stacked as the
  // elements are drawn, so a click takes the top one, and with it the rest
  // of its group; Alt+click takes the one beneath the selected one there.
  // Outlines turn with their elements, and the handle above one turns it,
  // snapping to multiples of 45° unless that is switched off. Dragged,
  // elements snap to each other, the label's edges and middle, and the
  // guides, or else to the grid; Alt leaves both. The view zooms, with Ctrl
  // and the wheel, a pinch, or + and -, and pans, by scrolling, with Space
  // or the middle button held, or with two fingers.

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
    tool = null,
    ondraw,
    ontoolcancel,
    onfile,
    onguides,
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
    /** The shape being drawn, if any. */
    tool?: ShapeKind | null;
    ondraw?: (e: TemplateElement) => void;
    ontoolcancel?: () => void;
    /** An SVG file dropped on the label, to import. */
    onfile?: (file: File) => void;
    /** The template's guides changed. */
    onguides?: (guides: Guides) => void;
  } = $props();

  let surface: HTMLDivElement | undefined = $state();
  let viewport: HTMLDivElement | undefined = $state();

  // Settings remembered in this browser: snapping angles, snapping to other
  // elements and guides, and showing where the printer prints.
  function remembered(key: string): boolean {
    try {
      return localStorage.getItem(key) !== 'off';
    } catch {
      return true;
    }
  }
  function remember(key: string, on: boolean) {
    try {
      localStorage.setItem(key, on ? 'on' : 'off');
    } catch {
      // Storage unavailable: it lasts as long as the page.
    }
  }
  let snapAngles = $state(remembered('bam.editor-snap-angles'));
  let smartGuides = $state(remembered('bam.editor-smart-guides'));
  let showLimits = $state(remembered('bam.editor-printer-limits'));

  // The view: how far it is zoomed, 1 fitting the label's width, and how
  // far it has scrolled, for the rulers.
  let zoom = $state(1);
  let scrollX = $state(0);
  let scrollY = $state(0);
  let viewWidth = $state(600);
  const pxPerUnit = $derived((viewWidth * zoom) / doc.width);

  /** Zooms to a level, keeping the point under (cx, cy), in the viewport, where it is. */
  async function zoomTo(level: number, cx?: number, cy?: number) {
    if (!viewport || !surface) return;
    const next = Math.min(Math.max(level, 0.25), 16);
    const px = cx ?? viewport.clientWidth / 2;
    const py = cy ?? viewport.clientHeight / 2;
    const fx = (viewport.scrollLeft + px) / surface.clientWidth;
    const fy = (viewport.scrollTop + py) / surface.clientHeight;
    zoom = next;
    await tick();
    viewport.scrollLeft = fx * surface.clientWidth - px;
    viewport.scrollTop = fy * surface.clientHeight - py;
  }
  /** The zoom that shows the label at its actual size. */
  const actualSize = () => (toMM(doc.width, doc.units) * PX_PER_MM) / (viewport?.clientWidth || 1);

  function wheel(event: WheelEvent) {
    if (!event.ctrlKey || !viewport) return;
    event.preventDefault();
    const r = viewport.getBoundingClientRect();
    void zoomTo(zoom * Math.exp(-event.deltaY * 0.002), event.clientX - r.left, event.clientY - r.top);
  }

  // Panning: Space held, or the middle button, drags the view; two fingers
  // pan and pinch.
  let spaceHeld = $state(false);
  let pan: { x0: number; y0: number; left: number; top: number } | null = null;
  const touches = new Map<number, { x: number; y: number }>();
  let pinch: { distance: number; zoom: number } | null = null;

  function editing(target: EventTarget | null): boolean {
    return target instanceof Element && Boolean(target.closest('input, textarea, select, [contenteditable]'));
  }

  function windowKeydown(event: KeyboardEvent) {
    if (editing(event.target)) return;
    if (event.key === ' ' && !spaceHeld && viewport?.matches(':hover')) {
      event.preventDefault();
      spaceHeld = true;
    }
    if (event.ctrlKey || event.metaKey || event.altKey) return;
    const zooms: Record<string, () => number> = { '+': () => zoom * 1.25, '=': () => zoom * 1.25, '-': () => zoom / 1.25, '0': () => 1, '1': actualSize };
    const z = zooms[event.key];
    if (z && (surface?.contains(document.activeElement) || viewport?.matches(':hover') || document.activeElement === document.body)) {
      event.preventDefault();
      void zoomTo(z());
    }
  }

  function viewportPointerDown(event: PointerEvent) {
    if (event.pointerType === 'touch') {
      touches.set(event.pointerId, { x: event.clientX, y: event.clientY });
      if (touches.size === 2) {
        // A second finger: stop whatever the first began, and pan and pinch.
        gesture = null;
        const [a, b] = [...touches.values()];
        pinch = { distance: Math.hypot(a!.x - b!.x, a!.y - b!.y), zoom };
        pan = { x0: (a!.x + b!.x) / 2, y0: (a!.y + b!.y) / 2, left: viewport!.scrollLeft, top: viewport!.scrollTop };
      }
      return;
    }
    if (!viewport || !(event.button === 1 || (event.button === 0 && spaceHeld))) return;
    event.preventDefault();
    event.stopPropagation();
    pan = { x0: event.clientX, y0: event.clientY, left: viewport.scrollLeft, top: viewport.scrollTop };
    viewport.setPointerCapture(event.pointerId);
  }

  function viewportPointerMove(event: PointerEvent) {
    if (!viewport) return;
    if (event.pointerType === 'touch' && touches.has(event.pointerId)) {
      touches.set(event.pointerId, { x: event.clientX, y: event.clientY });
      if (touches.size === 2 && pinch && pan) {
        const [a, b] = [...touches.values()];
        const mx = (a!.x + b!.x) / 2;
        const my = (a!.y + b!.y) / 2;
        const r = viewport.getBoundingClientRect();
        void zoomTo((pinch.zoom * Math.hypot(a!.x - b!.x, a!.y - b!.y)) / pinch.distance, mx - r.left, my - r.top);
        viewport.scrollLeft -= mx - pan.x0;
        viewport.scrollTop -= my - pan.y0;
        pan.x0 = mx;
        pan.y0 = my;
      }
      return;
    }
    if (!pan) return;
    viewport.scrollLeft = pan.left - (event.clientX - pan.x0);
    viewport.scrollTop = pan.top - (event.clientY - pan.y0);
  }

  function viewportPointerUp(event: PointerEvent) {
    touches.delete(event.pointerId);
    if (touches.size < 2) pinch = null;
    if (pan && viewport?.hasPointerCapture(event.pointerId)) viewport.releasePointerCapture(event.pointerId);
    if (touches.size === 0) pan = null;
  }

  const pct = (v: number, total: number) => `${(v / total) * 100}%`;
  const boxStyle = (e: Box & { rotation?: number }) =>
    `left:${pct(e.x, doc.width)};top:${pct(e.y, doc.height)};width:${pct(e.width, doc.width)};height:${pct(e.height, doc.height)}` +
    (e.rotation ? `;transform:rotate(${e.rotation}deg)` : '');
  // A level or upright line's box has no height or width: its outline is
  // given some, to be seen and taken.
  const outlineStyle = (e: TemplateElement) =>
    boxStyle(e) + (e.type === 'shape' && e.shape === 'line' && (!e.width || !e.height) ? ';padding:4px;margin:-4px;box-sizing:content-box' : '');

  const warned = $derived(new Set([...(area?.outside ?? []), ...(area?.nearEdge ?? [])]));

  /** The strips of the label outside what the printer prints. */
  const unprinted = $derived.by(() => {
    if (!area || !showLimits) return [];
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

  /** Where a pointer is on the label, in its units. */
  function onLabel(event: PointerEvent | MouseEvent): [number, number] {
    const r = surface!.getBoundingClientRect();
    return [((event.clientX - r.left) / r.width) * doc.width, ((event.clientY - r.top) / r.height) * doc.height];
  }

  // The selection's box, when several are selected, with handles of its own.
  const selectionBox = $derived(selected.length > 1 ? boundsOf(selected.map((i) => doc.elements[i]!).filter(Boolean)) : null);

  type Gesture =
    | { kind: 'move'; x0: number; y0: number; start: TemplateElement[]; indices: number[]; moved: boolean }
    | { kind: 'resize'; x0: number; y0: number; start: TemplateElement[]; index: number; handle: Handle; moved: boolean }
    | { kind: 'rotate'; x0: number; y0: number; start: TemplateElement[]; indices: number[]; cx: number; cy: number; moved: boolean }
    | { kind: 'margin'; x0: number; y0: number; start: TemplateElement[]; index: number; side: Side; from: number; moved: boolean }
    | { kind: 'scale'; x0: number; y0: number; start: TemplateElement[]; indices: number[]; handle: Handle; box: Box; moved: boolean }
    | { kind: 'band'; x0: number; y0: number; from: [number, number]; add: boolean; moved: boolean };
  let gesture: Gesture | null = null;
  // Where a drag snapped to, shown while it lasts; and the box being drawn.
  let snapLines = $state<{ x?: number; y?: number }>({});
  let band = $state<Box | null>(null);

  function pointerDownOnElement(event: PointerEvent, i: number) {
    if (event.button !== 0 || spaceHeld) return;
    event.preventDefault();
    let next = selected;
    if (event.altKey && surface) {
      // The next element down under the pointer, round to the top again.
      const [x, y] = onLabel(event);
      const stack = elementsAt(doc, x, y).filter((k) => !doc.elements[k]?.locked);
      const from = selected.length === 1 && stack.includes(selected[0]!) ? selected[0]! : i;
      i = stack[(stack.indexOf(from) + 1) % stack.length] ?? i;
      next = [i];
      onselect(next);
      surface.querySelector<HTMLElement>(`[data-element="${i}"]`)?.focus();
    } else {
      (event.currentTarget as HTMLElement).focus();
    }
    // A click takes the element's whole group.
    const taken = event.altKey ? [i] : withGroups(doc.elements, [i]);
    if (event.shiftKey) {
      next = taken.every((k) => selected.includes(k)) ? selected.filter((s) => !taken.includes(s)) : [...new Set([...selected, ...taken])];
      onselect(next);
      return;
    }
    if (!next.includes(i)) {
      next = taken;
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

  function pointerDownOnSelectionHandle(event: PointerEvent, handle: Handle) {
    if (event.button !== 0 || !selectionBox) return;
    event.preventDefault();
    event.stopPropagation();
    gesture = { kind: 'scale', x0: event.clientX, y0: event.clientY, start: $state.snapshot(doc.elements) as TemplateElement[], indices: [...selected], handle, box: { ...selectionBox }, moved: false };
    surface?.setPointerCapture(event.pointerId);
  }

  /** Turning one element about its center, or the selection about its middle. */
  function pointerDownOnRotate(event: PointerEvent, indices: number[], box: Box) {
    if (event.button !== 0 || !surface) return;
    event.preventDefault();
    event.stopPropagation();
    const r = surface.getBoundingClientRect();
    const cx = r.left + ((box.x + box.width / 2) / doc.width) * r.width;
    const cy = r.top + ((box.y + box.height / 2) / doc.height) * r.height;
    gesture = { kind: 'rotate', x0: event.clientX, y0: event.clientY, start: $state.snapshot(doc.elements) as TemplateElement[], indices, cx, cy, moved: false };
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
    if (!gesture || touches.size > 1) return;
    if (gesture.kind === 'band') {
      const [x, y] = onLabel(event);
      const g = gesture;
      if (!g.moved && Math.abs(event.clientX - g.x0) + Math.abs(event.clientY - g.y0) < 3) return;
      g.moved = true;
      band = { x: Math.min(g.from[0], x), y: Math.min(g.from[1], y), width: Math.abs(x - g.from[0]), height: Math.abs(y - g.from[1]) };
      return;
    }
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
      // The angle the pointer has swept about the center.
      const delta = ((Math.atan2(event.clientY - g.cy, event.clientX - g.cx) - Math.atan2(g.y0 - g.cy, g.x0 - g.cx)) * 180) / Math.PI;
      onchange(rotateElements(doc, g.indices, g.start, delta, snapAngles && !event.altKey));
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
      const g = gesture;
      if (smartGuides && !event.altKey) {
        // Snapped to what is near, within 6 pixels; the grid otherwise.
        const s = smartSnap(doc, g.indices, boundsOf(g.indices.map((i) => g.start[i]!)), dx, dy, 6 * k);
        snapLines = { x: s.x, y: s.y };
        onchange(moveElements(doc, g.indices, g.start, s.dx, s.dy, step, { x: s.x !== undefined, y: s.y !== undefined }));
      } else {
        snapLines = {};
        onchange(moveElements(doc, g.indices, g.start, dx, dy, step));
      }
    } else if (gesture.kind === 'scale') {
      const g = gesture;
      const b = g.box;
      let [x0, y0, x1, y1] = [b.x, b.y, b.x + b.width, b.y + b.height];
      if (g.handle.includes('w')) x0 = Math.min(snap(x0 + dx, step, doc.units), x1 - step);
      if (g.handle.includes('e')) x1 = Math.max(snap(x1 + dx, step, doc.units), x0 + step);
      if (g.handle.includes('n')) y0 = Math.min(snap(y0 + dy, step, doc.units), y1 - step);
      if (g.handle.includes('s')) y1 = Math.max(snap(y1 + dy, step, doc.units), y0 + step);
      onchange(scaleElements(doc, g.indices, g.start, b, { x: x0, y: y0, width: x1 - x0, height: y1 - y0 }));
    } else {
      const g = gesture;
      onchange(g.start.map((e, i) => (i === g.index ? resizeElement(doc, e, g.handle, dx, dy, step) : e)));
    }
  }

  function pointerUp(event: PointerEvent) {
    const g = gesture;
    if (g && surface?.hasPointerCapture(event.pointerId)) surface.releasePointerCapture(event.pointerId);
    gesture = null;
    snapLines = {};
    if (g?.kind === 'band') {
      if (!g.moved) {
        onselect([]);
      } else if (band) {
        // What the box touches, as it lies turned, and not locked.
        const b = band;
        const hit = doc.elements.flatMap((e, i) => {
          const r = bounds(e);
          return !e.locked && r.x < b.x + b.width && b.x < r.x + r.width && r.y < b.y + b.height && b.y < r.y + r.height ? [i] : [];
        });
        const taken = withGroups(doc.elements, hit);
        onselect(g.add ? [...new Set([...selected, ...taken])] : taken);
      }
      band = null;
    }
  }

  function pointerDownOnSurface(event: PointerEvent) {
    if (spaceHeld || event.button !== 0) return;
    if (event.target === event.currentTarget || (event.target as Element).tagName === 'IMG') {
      // On the label itself: draw a box to select what it touches.
      gesture = { kind: 'band', x0: event.clientX, y0: event.clientY, from: onLabel(event), add: event.shiftKey, moved: false };
      surface?.setPointerCapture(event.pointerId);
    }
  }

  // Dragging a guide: out of a ruler, or one already placed; off the label,
  // it comes off.
  let guideDrag: { axis: 'x' | 'y'; index: number; moved: boolean } | null = null;

  function startGuide(event: PointerEvent, axis: 'x' | 'y', index: number) {
    if (event.button !== 0 || !onguides) return;
    event.preventDefault();
    event.stopPropagation();
    guideDrag = { axis, index, moved: false };
    window.addEventListener('pointermove', moveGuide);
    window.addEventListener('pointerup', dropGuide, { once: true });
  }

  function guideAt(event: PointerEvent, axis: 'x' | 'y'): number | null {
    const [x, y] = onLabel(event);
    const at = axis === 'x' ? x : y;
    const limit = axis === 'x' ? doc.width : doc.height;
    if (at < 0 || at > limit || (axis === 'x' ? y < 0 || y > doc.height : x < 0 || x > doc.width)) return null;
    return event.altKey ? Math.round(at * 1000) / 1000 : snap(at, gridStep(doc.units), doc.units);
  }

  function moveGuide(event: PointerEvent) {
    const g = guideDrag;
    if (!g || !onguides) return;
    const at = guideAt(event, g.axis);
    const list = [...(doc.guides?.[g.axis] ?? [])];
    if (!g.moved) {
      g.moved = true;
      onbegin();
      if (g.index < 0) {
        // A new guide, from a ruler.
        if (at === null) {
          g.moved = false;
          return;
        }
        list.push(at);
        g.index = list.length - 1;
        onguides({ ...doc.guides, [g.axis]: list });
        return;
      }
    }
    if (at !== null) {
      list[g.index] = at;
      onguides({ ...doc.guides, [g.axis]: list });
    }
  }

  function dropGuide(event: PointerEvent) {
    window.removeEventListener('pointermove', moveGuide);
    const g = guideDrag;
    guideDrag = null;
    if (!g || !g.moved || !onguides) return;
    if (guideAt(event, g.axis) === null) {
      // Dropped off the label: it comes off.
      onguides({ ...doc.guides, [g.axis]: (doc.guides?.[g.axis] ?? []).filter((_, i) => i !== g.index) });
    }
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

  const zoomLabel = $derived(`${Math.round(zoom * 100)}%`);
</script>

<svelte:window onkeydown={windowKeydown} onkeyup={(ev) => ev.key === ' ' && (spaceHeld = false)} />

<div class="space-y-2">
  <div class="flex flex-wrap items-center gap-2 text-sm" role="toolbar" aria-label="View">
    <button type="button" class="btn-secondary" aria-label="Zoom out" onclick={() => zoomTo(zoom / 1.25)}>−</button>
    <span class="w-12 text-center tabular-nums" aria-live="polite" aria-label="Zoom {zoomLabel}">{zoomLabel}</span>
    <button type="button" class="btn-secondary" aria-label="Zoom in" onclick={() => zoomTo(zoom * 1.25)}>+</button>
    <button type="button" class="btn-secondary" onclick={() => zoomTo(1)}>Fit</button>
    <button type="button" class="btn-secondary" onclick={() => zoomTo(actualSize())}>Actual size</button>
    {#if onguides}
      <button type="button" class="btn-secondary" onclick={() => { onbegin(); onguides({ ...doc.guides, x: [...(doc.guides?.x ?? []), snap(doc.width / 2, gridStep(doc.units), doc.units)] }); }}>Add a vertical guide</button>
      <button type="button" class="btn-secondary" onclick={() => { onbegin(); onguides({ ...doc.guides, y: [...(doc.guides?.y ?? []), snap(doc.height / 2, gridStep(doc.units), doc.units)] }); }}>Add a horizontal guide</button>
    {/if}
  </div>

  <div class="grid" style="grid-template-columns: 20px minmax(0, 1fr); grid-template-rows: 20px auto">
    <div aria-hidden="true" class="bg-zinc-100 dark:bg-zinc-800"></div>
    <Ruler axis="x" length={doc.width} {pxPerUnit} offset={scrollX} onguide={(ev) => startGuide(ev, 'y', -1)} />
    <Ruler axis="y" length={doc.height} {pxPerUnit} offset={scrollY} onguide={(ev) => startGuide(ev, 'x', -1)} />
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      bind:this={viewport}
      bind:clientWidth={viewWidth}
      class="viewport relative max-h-[70vh] overflow-auto bg-zinc-200 dark:bg-zinc-900"
      class:cursor-grab={spaceHeld}
      onscroll={() => {
        scrollX = viewport?.scrollLeft ?? 0;
        scrollY = viewport?.scrollTop ?? 0;
      }}
      onwheel={wheel}
      onpointerdowncapture={viewportPointerDown}
      onpointermove={viewportPointerMove}
      onpointerup={viewportPointerUp}
      onpointercancel={viewportPointerUp}
    >
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        bind:this={surface}
        class="relative touch-none select-none border border-zinc-400 bg-white shadow-sm dark:border-zinc-600"
        style="width: {zoom * 100}%; aspect-ratio: {doc.width} / {doc.height}"
        onpointerdown={pointerDownOnSurface}
        onpointermove={pointerMove}
        onpointerup={pointerUp}
        onpointercancel={pointerUp}
        ondragover={(ev) => {
          if (ev.dataTransfer?.types.includes('Files')) ev.preventDefault();
        }}
        ondrop={(ev) => {
          const file = ev.dataTransfer?.files[0];
          if (!file || !onfile) return;
          ev.preventDefault();
          if (file.type === 'image/svg+xml' || file.name.toLowerCase().endsWith('.svg')) onfile(file);
        }}
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
        {#if area && area.edge > 0 && showLimits}
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
            tabindex={e.locked && !isSelected ? -1 : 0}
            aria-pressed={isSelected}
            aria-label="{describeElement(e, i)}{e.group ? `, in ${e.group}` : ''}{e.locked ? ', locked' : ''}"
            data-element={i}
            class="absolute outline-offset-0 {e.locked ? '' : 'cursor-move'} {isSelected
              ? 'outline-2 outline-blue-600'
              : e.locked
                ? 'outline-1 outline-dotted outline-zinc-300'
                : 'outline-1 outline-dashed outline-zinc-400 hover:outline-blue-400'} {problemElements.has(i)
              ? 'bg-red-500/15 outline-red-600'
              : warned.has(i) && showLimits
                ? 'bg-amber-400/15 outline-amber-500'
                : ''}"
            style={outlineStyle(e)}
            class:pointer-events-none={tool || spaceHeld || (e.locked && !isSelected)}
            onpointerdown={(ev) => !e.locked && pointerDownOnElement(ev, i)}
            onkeydown={(ev) => !e.locked && keydown(ev)}
            onfocus={() => {
              if (!isSelected && !gesture) onselect(withGroups(doc.elements, [i]));
            }}
          >
            {#if isSelected && selected.length === 1 && !e.locked}
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
                onpointerdown={(ev) => pointerDownOnRotate(ev, [i], e)}
              ></span>
            {/if}
          </div>
        {/each}
        {#if selectionBox && !tool}
          <!-- Several selected, or a group: one box, scaled and turned as one. -->
          <div aria-hidden="true" class="selection-box pointer-events-none absolute" style={boxStyle(selectionBox)}>
            {#each handles as h (h)}
              <span
                class="handle pointer-events-auto absolute border border-white bg-blue-600"
                style="cursor:{h}-resize;{h.includes('n') ? 'top:calc(var(--h) / -2)' : h.includes('s') ? 'bottom:calc(var(--h) / -2)' : 'top:calc(50% - var(--h) / 2)'};{h.includes('w')
                  ? 'left:calc(var(--h) / -2)'
                  : h.includes('e')
                    ? 'right:calc(var(--h) / -2)'
                    : 'left:calc(50% - var(--h) / 2)'}"
                onpointerdown={(ev) => pointerDownOnSelectionHandle(ev, h)}
              ></span>
            {/each}
            <span class="rotate-stem absolute bg-blue-600"></span>
            <span
              class="handle rotate-handle pointer-events-auto absolute rounded-full border border-white bg-blue-600"
              title="Drag to turn them together"
              onpointerdown={(ev) => pointerDownOnRotate(ev, [...selected], selectionBox!)}
            ></span>
          </div>
        {/if}
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
        {#if !tool && selected.length === 1 && doc.elements[selected[0]!]?.type === 'shape' && doc.elements[selected[0]!]?.shape === 'polygon'}
          {@const i = selected[0]!}
          <PolygonPoints
            {doc}
            e={doc.elements[i]!}
            style={boxStyle(doc.elements[i]!)}
            {onbegin}
            onupdate={(e) => onchange(doc.elements.map((x, k) => (k === i ? e : x)))}
          />
        {/if}
        {#if onguides}
          <GuideLines {doc} {onbegin} onchange={onguides} dragging={startGuide} />
        {/if}
        {#if snapLines.x !== undefined}<span aria-hidden="true" class="snap-line pointer-events-none absolute top-0 bottom-0" style="left:{pct(snapLines.x, doc.width)}"></span>{/if}
        {#if snapLines.y !== undefined}<span aria-hidden="true" class="snap-line pointer-events-none absolute right-0 left-0" style="top:{pct(snapLines.y, doc.height)}"></span>{/if}
        {#if band}<span aria-hidden="true" class="band pointer-events-none absolute" style={boxStyle(band)}></span>{/if}
        {#if tool && ondraw}
          <ShapeTool {doc} {tool} {ondraw} oncancel={() => ontoolcancel?.()} />
        {/if}
      </div>
    </div>
  </div>
  <div class="flex flex-wrap gap-x-4 gap-y-1 text-sm">
    <label class="flex items-center gap-2">
      <input type="checkbox" checked={snapAngles} onchange={(ev) => { snapAngles = ev.currentTarget.checked; remember('bam.editor-snap-angles', snapAngles); }} />
      Snap angles to 45°
    </label>
    <label class="flex items-center gap-2">
      <input type="checkbox" checked={smartGuides} onchange={(ev) => { smartGuides = ev.currentTarget.checked; remember('bam.editor-smart-guides', smartGuides); }} />
      Snap to elements and guides
    </label>
    {#if area}
      <label class="flex items-center gap-2">
        <input type="checkbox" checked={showLimits} onchange={(ev) => { showLimits = ev.currentTarget.checked; remember('bam.editor-printer-limits', showLimits); }} />
        Show where the printer prints
      </label>
    {/if}
  </div>
  {#if unprinted.length || (area && area.edge > 0 && showLimits)}
    <p class="text-xs text-zinc-600 dark:text-zinc-400">
      {#if unprinted.length}Shaded: where the printer does not print.{/if}
      {#if area && area.edge > 0}Dashed: within {area.edge} {doc.units} of the edge, which may be cut off.{/if}
    </p>
  {/if}
  <p class="hidden text-xs text-zinc-600 pointer-coarse:block dark:text-zinc-400">
    Drag to move, and drag a corner to resize; two fingers pan and pinch to zoom; or set an element's position and size exactly in its settings.
  </p>
  <p class="text-xs text-zinc-600 pointer-coarse:hidden dark:text-zinc-400">
    Drag to move, drag a corner to resize, drag the round handle to turn, Shift+click to select several, drag on the label to select what a box
    touches, Alt+click to select what is beneath. Drag from a ruler for a guide. Press ? for every shortcut.
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
  .selection-box {
    outline: 1px dashed rgb(37 99 235);
  }
  .snap-line {
    border-left: 1px solid rgb(219 39 119);
    border-top: 1px solid rgb(219 39 119);
  }
  .band {
    background: rgb(37 99 235 / 0.08);
    outline: 1px dashed rgb(37 99 235);
  }
  .unprinted {
    background: repeating-linear-gradient(45deg, rgb(113 113 122 / 0.35) 0 4px, rgb(113 113 122 / 0.12) 4px 8px);
  }
</style>
