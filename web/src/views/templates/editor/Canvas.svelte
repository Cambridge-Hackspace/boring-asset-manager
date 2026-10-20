<script lang="ts">
  import type { Frame } from '../../../lib/live/client';
  import {
    boundsOf,
    describeElement,
    elementsAt,
    gridStep,
    handles,
    moveElements,
    appliedTurn,
    newElement,
    newShape,
    polygonThrough,
    scaleElements,
    sinCos,
    snap,
    tidy,
    withGroups,
    withProp,
    rotateElements,
    type Box,
    type CodeLayout,
    type Handle,
    type PrintArea,
    type ShapeKind,
    type TemplateElement,
  } from '../../../lib/template';
  import { untrack } from 'svelte';
  import { useEditor, type Tool } from './editor.svelte';
  import Ruler from './Ruler.svelte';
  import ContextMenu, { type MenuItem } from './ContextMenu.svelte';
  import PointEditor from './PointEditor.svelte';
  import TextEditor from './TextEditor.svelte';
  import { withContours, type Node } from './nodes';
  import { alongOneAxis, beneath, inBox, pull, resizeFrom, scaledBox, turned, type Anchor } from './transform';
  import { gridOf, snapBox, snapNothing, snapPoint, type SnapLine } from './snapping';
  import { Touches } from './touch';

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
    codeLayouts = {},
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
    /** How each code was laid out, by index: its padding, and what was left over. */
    codeLayouts?: Record<number, CodeLayout>;
  } = $props();

  const editor = useEditor();
  const doc = $derived(editor.doc);

  let viewport = $state<HTMLDivElement | null>(null);
  let paper = $state<HTMLDivElement | null>(null);

  // The view (editor.view): screen pixels per template unit, and where the
  // label's top left corner is in the viewport. It fits the label until
  // zoomed or moved.
  const scale = $derived(editor.view.scale);
  const ox = $derived(editor.view.x);
  const oy = $derived(editor.view.y);
  let viewW = $state(0);
  let viewH = $state(0);

  $effect(() => {
    editor.viewport = { w: viewW, h: viewH };
  });
  $effect(() => {
    void doc.width;
    void doc.height;
    void viewW;
    void viewH;
    if (untrack(() => editor.fitted)) untrack(() => editor.fit());
  });

  // Hiding everything but the canvas moves it on the screen; the label is
  // moved back, so it stays where it was under the eye, as in Krita.
  let lastOrigin: { left: number; top: number } | null = null;
  $effect(() => {
    void editor.canvasOnly;
    if (!viewport) return;
    const was = lastOrigin;
    queueMicrotask(() => {
      if (!viewport) return;
      const r = viewport.getBoundingClientRect();
      if (was && (r.left !== was.left || r.top !== was.top)) editor.panBy(was.left - r.left, was.top - r.top);
      lastOrigin = { left: r.left, top: r.top };
    });
  });

  // Panning, as in Krita: Space held, or the middle button, drags the view
  // from any tool; the wheel scrolls it, and with Ctrl zooms toward the
  // pointer.
  let spaceHeld = $state(false);
  let hovering = false;
  function typing(target: EventTarget | null): boolean {
    return target instanceof Element && Boolean(target.closest('input, textarea, select, [contenteditable]'));
  }
  function windowKeydown(event: KeyboardEvent) {
    if (event.key === ' ' && hovering && !typing(event.target)) {
      event.preventDefault();
      spaceHeld = true;
    }
  }
  function windowKeyup(event: KeyboardEvent) {
    if (event.key === ' ') spaceHeld = false;
  }
  function wheel(event: WheelEvent) {
    event.preventDefault();
    const r = viewport!.getBoundingClientRect();
    if (event.ctrlKey || event.metaKey) {
      editor.zoomBy(Math.exp(-event.deltaY * 0.002), event.clientX - r.left, event.clientY - r.top);
    } else if (event.shiftKey && !event.deltaX) {
      editor.panBy(-event.deltaY, 0);
    } else {
      editor.panBy(-event.deltaX, -event.deltaY);
    }
  }
  // Not passive, so the page does not scroll or zoom instead.
  $effect(() => {
    const v = viewport;
    if (!v) return;
    v.addEventListener('wheel', wheel, { passive: false });
    return () => v.removeEventListener('wheel', wheel);
  });

  /** Where a point of the label is in the viewport, for zooming toward it. */
  export function viewportPoint(x: number, y: number): [number, number] {
    return [ox + x * scale, oy + y * scale];
  }

  /** Where a pointer is on the label, in its units. */
  function onLabel(event: { clientX: number; clientY: number }): [number, number] {
    const r = paper!.getBoundingClientRect();
    return [((event.clientX - r.left) / r.width) * doc.width, ((event.clientY - r.top) / r.height) * doc.height];
  }

  /** A point on the label, in its units, on the grid unless Alt is held, kept on the label. */
  function gridPoint(event: PointerEvent | MouseEvent): [number, number] {
    const [x, y] = onLabel(event);
    const cx = Math.min(Math.max(x, 0), doc.width);
    const cy = Math.min(Math.max(y, 0), doc.height);
    const s = snapPoint(doc, cx, cy, threshold(), editor.snapTo);
    snapLines = { x: s.lineX, y: s.lineY };
    return [s.x, s.y];
  }

  /** How near a line snaps, in template units: six screen pixels. */
  const threshold = () => 6 / scale;

  // The lines a drag snapped to, drawn while it lasts.
  let snapLines = $state<{ x?: SnapLine; y?: SnapLine }>({});

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

  // What a drag is doing. The last pointer event is kept, so a modifier
  // pressed or let go mid-drag takes effect at once, as in Krita.
  interface Pointer {
    clientX: number;
    clientY: number;
    shiftKey: boolean;
    ctrlKey: boolean;
    metaKey: boolean;
    altKey: boolean;
  }
  type Gesture =
    | { kind: 'move'; x0: number; y0: number; start: TemplateElement[]; indices: number[]; box: Box; hit: number; moved: boolean; last?: Pointer }
    | { kind: 'resize'; x0: number; y0: number; start: TemplateElement[]; index: number; handle: Handle; moved: boolean; last?: Pointer }
    | { kind: 'scale'; x0: number; y0: number; start: TemplateElement[]; indices: number[]; handle: Handle; box: Box; moved: boolean; last?: Pointer }
    | { kind: 'rotate'; x0: number; y0: number; start: TemplateElement[]; indices: number[]; cx: number; cy: number; moved: boolean; last?: Pointer }
    | { kind: 'band'; x0: number; y0: number; from: [number, number]; to: [number, number]; add: boolean; moved: boolean; last?: Pointer }
    | { kind: 'draw'; from: [number, number]; to: [number, number]; prev: [number, number]; shift: boolean; ctrl: boolean; last?: Pointer }
    | { kind: 'pen'; index: number; x0: number; y0: number }
    | { kind: 'measure'; from: [number, number]; to: [number, number] }
    | { kind: 'pan'; x: number; y: number };
  let gesture = $state<Gesture | null>(null);
  // A polygon's points so far, in the template's units.
  let points = $state<[number, number][]>([]);
  // Where the pointer is in the viewport, for the readout beside it.
  let readoutAt = $state<[number, number] | null>(null);

  const toolNames: Record<Exclude<Tool, 'select'>, string> = {
    text: 'a text box',
    code: 'a code',
    image: 'an image',
    rectangle: 'a rectangle',
    ellipse: 'an ellipse',
    line: 'a line',
    polygon: 'a polygon',
    path: 'a path',
    pen: 'a path',
    measure: 'a measurement',
  };

  // What the status bar says can be done now.
  let over = $state<'handle' | 'rotate' | null>(null);
  $effect(() => {
    const g = gesture;
    const tool = editor.tool;
    if (g?.kind === 'pan' || spaceHeld) editor.hint = 'Moving the view.';
    else if (g?.kind === 'move') editor.hint = 'Moving. Shift: along one axis, off the grid. Esc: put it back.';
    else if (g?.kind === 'resize' || g?.kind === 'scale') editor.hint = 'Resizing. Shift: keep proportions, off the grid. Ctrl: from the center. Esc: put it back.';
    else if (g?.kind === 'rotate') editor.hint = `Turning. Ctrl: 15° steps.${editor.snapAngles ? ' Snaps to 45° near one; Shift: freely.' : ''} Esc: put it back.`;
    else if (g?.kind === 'band') editor.hint = 'Selecting. Left to right: what the box covers. Right to left: what it touches. Shift: add to the selection.';
    else if (g?.kind === 'draw')
      editor.hint = `Drawing ${toolNames[tool as Exclude<Tool, 'select'>] ?? 'a shape'}. Shift: ${tool === 'line' ? 'in 15° steps' : 'square'}. Ctrl: from the center. Alt: move it. Esc: stop.`;
    else if (tool === 'pen')
      editor.hint = pen.length
        ? 'Click: the next point. Drag: pull out its handles (Alt: one only). Right-click: take back the last. Double-click or Enter: finish. Click the first point: close it. Esc: stop.'
        : 'Click to place the first point; drag to pull out its handles.';
    else if (tool === 'measure') editor.hint = measured ? `Measured: ${measurement(measured)}.` : 'Drag between two points to measure. Nothing is added.';
    else if (editor.editingText !== null)
      editor.hint = 'Typing. Fields go in braces, such as {{asset.name}}. Esc or a click elsewhere: done; left empty, it comes off.';
    else if (editor.editingPoints !== null)
      editor.hint = 'Drag a point or a handle (Alt: the one handle only). Double-click the outline: add a point. Shift+click a point: sharp or smooth. Delete: remove it. Esc: done.';
    else if (tool === 'polygon')
      editor.hint = points.length ? 'Click to place the next point. Double-click or Enter: finish. Click the first point: close it. Esc: stop.' : 'Click to place the first point.';
    else if (tool !== 'select') editor.hint = `Drag to draw ${toolNames[tool]}, or click to place one. Esc: back to selecting.`;
    else if (over === 'rotate') editor.hint = 'Drag to turn. Ctrl: 15° steps.';
    else if (over === 'handle') editor.hint = 'Drag to resize. Shift: keep proportions. Ctrl: from the center. Ctrl+click: numbers given at this point.';
    else if (editor.selected.length)
      editor.hint = 'Drag to move. Shift+click: add or remove. Ctrl+click: the one beneath. Right-click: more. Arrow keys: move (Shift: ten steps, Alt: a tenth).';
    else editor.hint = 'Click an element to select it. Drag on the label to select with a box. Shift+click: select several.';
  });

  function capture(event: PointerEvent) {
    viewport?.setPointerCapture(event.pointerId);
  }

  let byPointer = false;

  /** Space, or the middle button: the view moves instead. */
  function startPan(event: PointerEvent): boolean {
    if (!(event.button === 1 || (event.button === 0 && spaceHeld))) return false;
    event.preventDefault();
    event.stopPropagation();
    gesture = { kind: 'pan', x: event.clientX, y: event.clientY };
    capture(event);
    return true;
  }

  /** A box to select with, from where the pointer went down on the label. */
  function startBand(event: PointerEvent) {
    gesture = { kind: 'band', x0: event.clientX, y0: event.clientY, from: onLabel(event), to: onLabel(event), add: event.shiftKey, moved: false };
    capture(event);
  }

  function downOnElement(event: PointerEvent, i: number) {
    if (startPan(event)) return;
    if (event.button !== 0 || editor.tool !== 'select') return;
    // Its points being edited, it stays put; a double-click adds a point.
    if (editor.editingPoints === i) {
      event.preventDefault();
      event.stopPropagation();
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    // Alt: a box to select with, even over an element.
    if (event.altKey) {
      startBand(event);
      return;
    }
    // Focused by the pointer, it is selected below, not by its focus.
    byPointer = true;
    (event.currentTarget as HTMLElement).focus();
    byPointer = false;
    // Ctrl: the element beneath the one selected there.
    if (event.ctrlKey || event.metaKey) {
      const [x, y] = onLabel(event);
      const next = beneath(doc, x, y, editor.selected);
      if (next !== null) {
        editor.selected = [next];
        paper?.querySelector<HTMLElement>(`[data-element="${next}"]`)?.focus();
      }
      return;
    }
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
    const start = editor.elements();
    gesture = { kind: 'move', x0: event.clientX, y0: event.clientY, start, indices: next, box: boundsOf(next.map((k) => start[k]!)), hit: i, moved: false };
    capture(event);
  }

  function downOnHandle(event: PointerEvent, handle: Handle) {
    if (startPan(event) || event.button !== 0) return;
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

  /** Just outside a corner: turning, about the element's center or the selection's. */
  function downOnRotate(event: PointerEvent) {
    if (startPan(event) || event.button !== 0 || !viewport) return;
    event.preventDefault();
    event.stopPropagation();
    const b = single ?? selectionBox;
    if (!b) return;
    const r = viewport.getBoundingClientRect();
    const cx = r.left + sx(b.x + b.width / 2);
    const cy = r.top + sy(b.y + b.height / 2);
    gesture = { kind: 'rotate', x0: event.clientX, y0: event.clientY, start: editor.elements(), indices: [...editor.selected], cx, cy, moved: false };
    capture(event);
  }

  function downOnDesk(event: PointerEvent) {
    // In the text being typed, the caret is placed, as in any text box.
    if (event.target instanceof Element && event.target.closest('[data-text-editor]')) return;
    if (startPan(event) || event.button !== 0 || !paper) return;
    // A click elsewhere finishes typing: the desk takes no focus, so the
    // text box is not left by it.
    if (editor.editingText !== null) finishTyping();
    if (editor.tool === 'select') {
      // Off every element: a box to select with; a click alone selects nothing.
      event.preventDefault();
      startBand(event);
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
    if (editor.tool === 'pen') {
      downWithPen(event, p);
      return;
    }
    if (editor.tool === 'measure') {
      measured = null;
      gesture = { kind: 'measure', from: p, to: p };
      capture(event);
      return;
    }
    gesture = { kind: 'draw', from: p, to: p, prev: p, shift: event.shiftKey, ctrl: event.ctrlKey || event.metaKey };
    capture(event);
  }

  /** Carries a gesture to where the pointer is now, with the modifiers held now. */
  function apply(g: Exclude<Gesture, { kind: 'draw' } | { kind: 'pan' } | { kind: 'pen' } | { kind: 'measure' }>, p: Pointer) {
    // A copy: an event's coordinates are not its own properties, and do not spread.
    g.last = { clientX: p.clientX, clientY: p.clientY, shiftKey: p.shiftKey, ctrlKey: p.ctrlKey, metaKey: p.metaKey, altKey: p.altKey };
    if (g.kind === 'band') {
      if (!g.moved && Math.abs(p.clientX - g.x0) + Math.abs(p.clientY - g.y0) < 3) return;
      g.moved = true;
      g.to = onLabel(p);
      return;
    }
    let dx = (p.clientX - g.x0) / scale;
    let dy = (p.clientY - g.y0) / scale;
    if (!g.moved) {
      if (Math.abs(p.clientX - g.x0) + Math.abs(p.clientY - g.y0) < 3) return;
      g.moved = true;
      editor.begin({ move: 'Move', resize: 'Resize', scale: 'Resize', rotate: 'Turn' }[g.kind]);
    }
    const ctrl = p.ctrlKey || p.metaKey;
    // Shift frees it from the grid, as from any snapping.
    const step = p.shiftKey ? undefined : gridStep(doc.units);
    snapLines = {};
    if (g.kind === 'move') {
      if (p.shiftKey) {
        // Along one axis, snapping to nothing.
        [dx, dy] = alongOneAxis(dx, dy);
        editor.doc.elements = moveElements(doc, g.indices, g.start, dx, dy, 0.001);
      } else {
        const s = snapBox(doc, g.indices, g.box, dx, dy, threshold(), editor.snapTo);
        snapLines = { x: s.x, y: s.y };
        editor.doc.elements = moveElements(doc, g.indices, g.start, s.dx, s.dy, 0.001, { x: true, y: true });
      }
    } else if (g.kind === 'resize') {
      const e = g.start[g.index]!;
      // The handle itself snaps, where the element is not turned.
      const snapped = !p.shiftKey && !e.rotation ? snapHandle(e, g.handle, dx, dy, [g.index]) : null;
      const o = { keepRatio: p.shiftKey, fromCenter: ctrl, step: snapped || p.shiftKey ? undefined : step };
      const [hx, hy] = snapped ?? [dx, dy];
      editor.doc.elements = g.start.map((el, i) => (i === g.index ? resizeFrom(doc, el, g.handle, hx, hy, o) : el));
    } else if (g.kind === 'scale') {
      const snapped = !p.shiftKey ? snapHandle(g.box, g.handle, dx, dy, g.indices) : null;
      const [hx, hy] = snapped ?? [dx, dy];
      const to = scaledBox(g.box, g.handle, hx, hy, doc.units, { keepRatio: p.shiftKey, fromCenter: ctrl, step: snapped || p.shiftKey ? undefined : step });
      editor.doc.elements = scaleElements(doc, g.indices, g.start, g.box, to);
    } else {
      // The angle swept about the center.
      const delta = ((Math.atan2(p.clientY - g.cy, p.clientX - g.cx) - Math.atan2(g.y0 - g.cy, g.x0 - g.cx)) * 180) / Math.PI;
      const snapping = { steps: ctrl, snap45: editor.snapAngles && !p.shiftKey };
      if (g.indices.length === 1) {
        const i = g.indices[0]!;
        const angle = turned((g.start[i]!.rotation ?? 0) + delta, snapping);
        editor.doc.elements = g.start.map((e, k) => (k === i ? withProp(e, 'rotation', angle || undefined) : e));
      } else {
        editor.doc.elements = rotateElements(doc, g.indices, g.start, snapping.steps ? Math.round(delta / 15) * 15 : delta);
      }
    }
  }

  /**
   * A handle's drag snapped: where the handle would be, snapped as a point,
   * each axis it pulls; the drag that gets it there.
   */
  function snapHandle(b: Box, handle: Handle, dx: number, dy: number, moving: number[]): [number, number] {
    const [px, py] = pull(handle);
    const hx = b.x + (px + 1) * (b.width / 2);
    const hy = b.y + (py + 1) * (b.height / 2);
    const s = snapPoint(doc, hx + dx, hy + dy, threshold(), editor.snapTo, moving);
    snapLines = { x: px ? s.lineX : undefined, y: py ? s.lineY : undefined };
    return [px ? s.x - hx : dx, py ? s.y - hy : dy];
  }

  // Guides, as in Krita and Inkscape: dragged out of a ruler, moved, and
  // dragged off the label to delete; locked, they stay put.
  type GuideDrag = { axis: 'x' | 'y'; index: number; at: number | null; moved: boolean };
  let guideDrag = $state<GuideDrag | null>(null);

  /** From a ruler: a new guide, across from the top one, down from the left. */
  export function guideFromRuler(event: PointerEvent, axis: 'x' | 'y') {
    if (event.button !== 0) return;
    event.preventDefault();
    if (!editor.guidesShown) editor.setGuidesShown(true);
    guideDrag = { axis, index: -1, at: null, moved: false };
    capture(event);
  }

  function downOnGuide(event: PointerEvent, axis: 'x' | 'y', index: number) {
    if (startPan(event) || event.button !== 0 || editor.guidesLocked) return;
    event.preventDefault();
    event.stopPropagation();
    guideDrag = { axis, index, at: doc.guides?.[axis]?.[index] ?? null, moved: false };
    capture(event);
  }

  /** Where a guide being dragged is, on the grid unless Shift is held; null off the label. */
  function guideAt(event: PointerEvent, axis: 'x' | 'y'): number | null {
    const [x, y] = onLabel(event);
    const at = axis === 'x' ? x : y;
    const across = axis === 'x' ? y : x;
    if (at < 0 || at > (axis === 'x' ? doc.width : doc.height) || across < -threshold() * 4 || across > (axis === 'x' ? doc.height : doc.width) + threshold() * 4) return null;
    if (event.shiftKey || !editor.snapTo.grid) return tidy(at, doc.units);
    return axis === 'x' ? snapPoint(doc, at, 0, 0, { ...snapNothing, grid: true }).x : snapPoint(doc, 0, at, 0, { ...snapNothing, grid: true }).y;
  }

  function moveGuide(event: PointerEvent) {
    const d = guideDrag!;
    d.at = guideAt(event, d.axis);
    d.moved = true;
  }

  function dropGuide() {
    const d = guideDrag!;
    guideDrag = null;
    if (!d.moved) return;
    const list = [...(doc.guides?.[d.axis] ?? [])];
    if (d.index < 0) {
      if (d.at === null) return;
      list.push(d.at);
      editor.setGuides({ ...doc.guides, [d.axis]: list }, 'Add a guide');
    } else if (d.at === null) {
      list.splice(d.index, 1);
      editor.setGuides({ ...doc.guides, [d.axis]: list }, 'Remove a guide');
    } else {
      list[d.index] = d.at;
      editor.setGuides({ ...doc.guides, [d.axis]: list }, 'Move a guide');
    }
  }

  /** The grid's lines in view, in viewport pixels, major and minor, when it is shown. */
  const gridLines = $derived.by(() => {
    const grid = doc.grid;
    if (!grid?.visible) return null;
    const sub = Math.max(1, grid.subdivisions ?? 1);
    const lines = (step: number, offset: number, size: number, origin: number) => {
      const out: { at: number; major: boolean }[] = [];
      const fine = step / sub;
      // Too close together to see: the major lines only, or none.
      const every = fine * scale >= 4 ? 1 : step * scale >= 4 ? sub : 0;
      if (!every) return out;
      for (let k = Math.ceil(-offset / fine); offset + k * fine <= size + 1e-9 && out.length < 4000; k += every) {
        const v = offset + k * fine;
        if (v < -1e-9) continue;
        out.push({ at: Math.round(origin + v * scale) + 0.5, major: k % sub === 0 });
      }
      return out;
    };
    return { xs: lines(grid.x, grid.offset_x ?? 0, doc.width, ox), ys: lines(grid.y, grid.offset_y ?? 0, doc.height, oy) };
  });

  function pointerMove(event: PointerEvent) {
    if (paper) {
      const [x, y] = onLabel(event);
      editor.pointer = x >= 0 && y >= 0 && x <= doc.width && y <= doc.height ? { x: tidy(x, doc.units), y: tidy(y, doc.units) } : null;
    }
    if (viewport) {
      const r = viewport.getBoundingClientRect();
      readoutAt = [event.clientX - r.left, event.clientY - r.top];
    }
    if (editor.tool === 'pen' && pen.length && paper) penAt = onLabel(event);
    if (guideDrag) {
      moveGuide(event);
      return;
    }
    const g = gesture;
    if (!g) return;
    if (g.kind === 'pan') {
      editor.panBy(event.clientX - g.x, event.clientY - g.y);
      g.x = event.clientX;
      g.y = event.clientY;
      return;
    }
    if (g.kind === 'draw') {
      const at = gridPoint(event);
      // Alt: the shape moves while it is drawn, as in Krita.
      if (event.altKey) g.from = [g.from[0] + at[0] - g.prev[0], g.from[1] + at[1] - g.prev[1]];
      g.prev = at;
      g.to = at;
      g.shift = event.shiftKey;
      g.ctrl = event.ctrlKey || event.metaKey;
      return;
    }
    if (g.kind === 'pen') {
      pullHandles(g, event);
      return;
    }
    if (g.kind === 'measure') {
      g.to = gridPoint(event);
      return;
    }
    apply(g, event);
  }

  // A modifier pressed or let go mid-drag: the drag again, as it would be with it.
  function modifierChanged(event: KeyboardEvent) {
    const g = gesture;
    if (!g || !['Shift', 'Control', 'Alt', 'Meta'].includes(event.key)) return;
    if (g.kind === 'draw') {
      g.shift = event.shiftKey;
      g.ctrl = event.ctrlKey || event.metaKey;
      return;
    }
    if (!('last' in g) || !g.last) return;
    apply(g, { ...g.last, shiftKey: event.shiftKey, ctrlKey: event.ctrlKey, metaKey: event.metaKey, altKey: event.altKey });
  }

  function pointerUp(event: PointerEvent) {
    if (viewport?.hasPointerCapture(event.pointerId)) viewport.releasePointerCapture(event.pointerId);
    snapLines = {};
    if (guideDrag) {
      dropGuide();
      return;
    }
    const g = gesture;
    gesture = null;
    if (g?.kind === 'draw') finishDrawing(...corners(g));
    if (g?.kind === 'measure') measured = { from: g.from, to: g.to };
    // A click, not a drag, on one of several selected: that one alone, with its group.
    if (g?.kind === 'move' && !g.moved && g.indices.length > 1) editor.selected = withGroups(doc.elements, [g.hit]);
    if (g?.kind === 'band') {
      if (!g.moved) {
        if (!g.add) editor.deselect();
        return;
      }
      const b = { x: Math.min(g.from[0], g.to[0]), y: Math.min(g.from[1], g.to[1]), width: Math.abs(g.to[0] - g.from[0]), height: Math.abs(g.to[1] - g.from[1]) };
      const taken = withGroups(doc.elements, inBox(doc, b, g.to[0] >= g.from[0]));
      editor.selected = g.add ? [...new Set([...editor.selected, ...taken])] : taken;
    }
    // Ctrl+click on a handle, not dragged: the numbers are given at that point.
    if (g?.kind === 'resize' && !g.moved && (event.ctrlKey || event.metaKey)) editor.anchor = handleAnchor[g.handle];
  }

  const handleAnchor: Record<Handle, Anchor> = { nw: 'nw', n: 'n', ne: 'ne', e: 'e', se: 'se', s: 's', sw: 'sw', w: 'w' };

  /** What floats beside the pointer while dragging: the size, angle or place. */
  const readout = $derived.by(() => {
    const g = gesture;
    const f2 = (v: number) => (doc.units === 'in' ? v.toFixed(3) : v.toFixed(2));
    if (g?.kind === 'draw' && draft) {
      if (editor.tool === 'line') {
        const angle = Math.round(((((-Math.atan2(draft.b[1] - draft.a[1], draft.b[0] - draft.a[0]) * 180) / Math.PI) % 360) + 360) % 360);
        return `${f2(Math.hypot(draft.w, draft.h))} ${doc.units} at ${angle}°`;
      }
      return `${f2(draft.w)} × ${f2(draft.h)} ${doc.units}`;
    }
    if (g?.kind === 'measure') return measurement(g);
    if (!g || !('moved' in g) || !g.moved || g.kind === 'band') return null;
    const u = doc.units;
    const f = (v: number) => (u === 'in' ? v.toFixed(3) : v.toFixed(2));
    const els = editor.selected.map((i) => doc.elements[i]).filter((e) => e !== undefined);
    if (!els.length) return null;
    if (g.kind === 'rotate') return els.length === 1 ? `${els[0]!.rotation ?? 0}°` : null;
    const b = boundsOf(els);
    if (g.kind === 'move') return `${f(b.x)}, ${f(b.y)} ${u}`;
    const e = els.length === 1 ? els[0]! : b;
    return `${f(e.width)} × ${f(e.height)} ${u}`;
  });

  // The menu a right-click opens: on an element not selected, it is selected first.
  let menu = $state<{ x: number; y: number } | null>(null);
  function openMenu(event: MouseEvent) {
    event.preventDefault();
    if (editor.tool === 'pen') {
      pen = pen.slice(0, -1);
      return;
    }
    const hit = (event.target as Element).closest('[data-element]');
    const i = hit ? Number(hit.getAttribute('data-element')) : -1;
    if (i >= 0 && !editor.selected.includes(i) && !doc.elements[i]?.locked) editor.selected = withGroups(doc.elements, [i]);
    menu = { x: event.clientX, y: event.clientY };
  }
  /** Shift+F10 or the menu key, on an element: the same menu, at it. */
  function menuKey(event: KeyboardEvent, i: number) {
    if (!(event.key === 'ContextMenu' || (event.key === 'F10' && event.shiftKey))) return;
    event.preventDefault();
    if (!editor.selected.includes(i)) editor.selected = withGroups(doc.elements, [i]);
    const r = (event.currentTarget as HTMLElement).getBoundingClientRect();
    menu = { x: r.left + r.width / 2, y: r.top + r.height / 2 };
  }

  /** Pastes what was copied here, or what the system clipboard holds, as the menu does. */
  export function paste() {
    void pasteFromMenu();
  }

  async function pasteFromMenu() {
    if (editor.clipboard && editor.paste(editor.clipboard)) return;
    try {
      const text = await navigator.clipboard.readText();
      if (/<svg[\s>]/.test(text)) onfile(new File([text], 'the clipboard', { type: 'image/svg+xml' }));
      else editor.paste(text);
    } catch {
      // The system clipboard cannot be read here: Ctrl+V can.
    }
  }

  const menuItems = $derived.by((): MenuItem[] => {
    const none = !editor.selected.length;
    const grouped =
      editor.selected.length > 1 && editor.selected.every((i) => doc.elements[i]?.group && doc.elements[i]?.group === doc.elements[editor.selected[0]!]?.group);
    const locked = !none && editor.selected.every((i) => doc.elements[i]?.locked);
    return [
      ...(editor.selected.length === 1 && editable(doc.elements[editor.selected[0]!])
        ? [{ label: 'Edit points', run: () => (editor.editingPoints = editor.selected[0]!) }, 'separator' as const]
        : []),
      { label: 'Bring to front', run: () => editor.restack('front'), disabled: none },
      { label: 'Bring forward', run: () => editor.restack('forward'), disabled: none },
      { label: 'Send backward', run: () => editor.restack('backward'), disabled: none },
      { label: 'Send to back', run: () => editor.restack('back'), disabled: none },
      'separator',
      grouped ? { label: 'Ungroup', run: () => editor.ungroup() } : { label: 'Group', run: () => editor.group(), disabled: editor.selected.length < 2 },
      'separator',
      { label: 'Turn 90° left', run: () => editor.turn(-90), disabled: none },
      { label: 'Turn 90° right', run: () => editor.turn(90), disabled: none },
      { label: 'Flip across', run: () => editor.flip(true), disabled: none },
      { label: 'Flip down', run: () => editor.flip(false), disabled: none },
      'separator',
      { label: locked ? 'Unlock' : 'Lock', run: () => editor.lockSelected(), disabled: none },
      'separator',
      {
        label: 'Copy',
        run: () => {
          const text = editor.copy();
          void navigator.clipboard?.writeText(text).catch(() => {});
        },
        disabled: none,
      },
      { label: 'Paste', run: () => void pasteFromMenu() },
      { label: 'Delete', run: () => editor.remove(), disabled: none },
    ];
  });

  /**
   * Where what is being drawn runs from and to, as in Krita: Shift keeps a
   * box square, or a line at a multiple of 15°; Ctrl draws it from its
   * center out.
   */
  function corners(g: Extract<Gesture, { kind: 'draw' }>): [[number, number], [number, number]] {
    let dx = g.to[0] - g.from[0];
    let dy = g.to[1] - g.from[1];
    if (g.shift) {
      if (editor.tool === 'line') {
        const length = Math.hypot(dx, dy);
        const angle = (Math.round(Math.atan2(dy, dx) / (Math.PI / 12)) * Math.PI) / 12;
        dx = tidy(Math.cos(angle) * length, doc.units);
        dy = tidy(Math.sin(angle) * length, doc.units);
      } else {
        const side = Math.max(Math.abs(dx), Math.abs(dy));
        dx = Math.sign(dx || 1) * side;
        dy = Math.sign(dy || 1) * side;
      }
    }
    return g.ctrl ? [[g.from[0] - dx, g.from[1] - dy], [g.from[0] + dx, g.from[1] + dy]] : [g.from, [g.from[0] + dx, g.from[1] + dy]];
  }

  // The pen, as Krita's path tool: a click places a point; a drag pulls out
  // its handles, in line, or with Alt the one only; right-click takes back
  // the last point; a double-click or Enter finishes; a click on the first
  // point closes the path.
  let pen = $state<Node[]>([]);
  let penAt = $state<[number, number] | null>(null);

  function downWithPen(event: PointerEvent, p: [number, number]) {
    const first = pen[0];
    if (first && pen.length >= 2 && Math.hypot(first.p[0] - p[0], first.p[1] - p[1]) * scale < 8) {
      finishPen(true);
      return;
    }
    pen = [...pen, { p }];
    gesture = { kind: 'pen', index: pen.length - 1, x0: event.clientX, y0: event.clientY };
    capture(event);
  }

  function pullHandles(g: Extract<Gesture, { kind: 'pen' }>, event: PointerEvent) {
    if (Math.abs(event.clientX - g.x0) + Math.abs(event.clientY - g.y0) < 4) return;
    const n = pen[g.index];
    if (!n) return;
    const h = gridPoint(event);
    // Handles stay on the label, as the path's box must.
    const on = (q: [number, number]): [number, number] => [Math.min(Math.max(q[0], 0), doc.width), Math.min(Math.max(q[1], 0), doc.height)];
    const mirrored = on([2 * n.p[0] - h[0], 2 * n.p[1] - h[1]]);
    pen[g.index] = event.altKey ? { ...n, out: h, smooth: false } : { p: n.p, in: mirrored, out: h, smooth: true };
  }

  function finishPen(closed: boolean) {
    // A double click's clicks each placed a point; one is enough.
    const nodes = pen.filter((n, k) => k === 0 || n.p[0] !== pen[k - 1]!.p[0] || n.p[1] !== pen[k - 1]!.p[1]);
    pen = [];
    penAt = null;
    if (nodes.length < 2) return;
    const start: TemplateElement = { ...newShape('path', doc), x: 0, y: 0, width: 1, height: 1, fill: 'none', d: 'M0 0' };
    editor.add(withContours(doc, start, [{ nodes: $state.snapshot(nodes) as Node[], closed }]));
    editor.tool = 'select';
  }

  /** A measurement, kept on the label until the next or another tool. */
  let measured = $state<{ from: [number, number]; to: [number, number] } | null>(null);
  const measurement = (m: { from: [number, number]; to: [number, number] }) => {
    const dx = m.to[0] - m.from[0];
    const dy = m.to[1] - m.from[1];
    const f = (v: number) => (doc.units === 'in' ? v.toFixed(3) : v.toFixed(2));
    const angle = Math.round(((((-Math.atan2(dy, dx) * 180) / Math.PI) % 360) + 360) % 360 * 10) / 10;
    return `${f(Math.hypot(dx, dy))} ${doc.units} at ${angle}°, ${f(Math.abs(dx))} across, ${f(Math.abs(dy))} down`;
  };

  // Editing points: entered by a double-click on a selected polygon or path,
  // left with Escape, another tool or another selection.
  let pointEditor = $state<ReturnType<typeof PointEditor> | null>(null);
  const editable = (e: TemplateElement | undefined) => e?.type === 'shape' && (e.shape === 'polygon' || e.shape === 'path');
  $effect(() => {
    const i = editor.editingPoints;
    if (i !== null && (editor.tool !== 'select' || editor.selected.length !== 1 || editor.selected[0] !== i || !editable(doc.elements[i]))) editor.editingPoints = null;
  });
  $effect(() => {
    const i = editor.editingText;
    if (i !== null && (editor.tool !== 'select' || doc.elements[i]?.type !== 'text')) untrack(() => finishTyping());
  });
  $effect(() => {
    if (editor.tool !== 'measure') measured = null;
    if (editor.tool !== 'pen') {
      pen = [];
      penAt = null;
    }
  });

  function doubleClickElement(event: MouseEvent, i: number) {
    if (editor.tool !== 'select') return;
    if (editor.editingPoints === i) {
      pointEditor?.insertAt(onLabel(event));
      return;
    }
    if (editable(doc.elements[i]) && editor.selected.length === 1 && editor.selected[0] === i) editor.editingPoints = i;
  }

  /** Delete, while editing points: the chosen point; reports whether it was taken. */
  export function deletePoint(): boolean {
    return editor.editingPoints !== null && Boolean(pointEditor?.removeChosen());
  }

  /** Adds what the tool draws, in the box dragged out, or at its usual size where clicked. */
  function finishDrawing(a: [number, number], b: [number, number]) {
    const tool = editor.tool;
    if (tool === 'select' || tool === 'polygon' || tool === 'pen' || tool === 'measure') return;
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
    if (tool === 'text') {
      // Typed at once, as Krita's text tool: a click a single line, a box dragged out wraps.
      e = { ...e, content_binding: '', wrap: !clicked };
    }
    editor.add(e);
    editor.tool = 'select';
    if (tool === 'text') editor.editingText = doc.elements.length - 1;
  }

  /** Done typing: a text left empty comes off. */
  function finishTyping() {
    const i = editor.editingText;
    editor.editingText = null;
    if (i === null) return;
    const e = doc.elements[i];
    if (e && !(e.content_binding ?? '').trim()) {
      editor.selected = [i];
      editor.remove();
    }
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
  /** Puts back the drag in progress, if any: a guide, or what was being moved or drawn. */
  function cancelDrag(): boolean {
    snapLines = {};
    if (guideDrag) {
      guideDrag = null;
      return true;
    }
    const g = gesture;
    if (!g) return false;
    gesture = null;
    if ('moved' in g && g.kind !== 'band' && g.moved) editor.cancel();
    return true;
  }

  // Touch, as Krita's gestures (touch.ts): one finger is the tool's, as the
  // mouse is; a second puts back what the first began, and pinches to zoom
  // and pans; a tap of two undoes, of three redoes. A gesture's fingers are
  // kept from the tool, taken before anything on the label sees them.
  const touches = new Touches({
    cancel: () => void cancelDrag(),
    pan: (dx, dy) => editor.panBy(dx, dy),
    zoom: (factor, x, y) => {
      const r = viewport?.getBoundingClientRect();
      if (r) editor.zoomBy(factor, x - r.left, y - r.top);
    },
    undo: () => editor.undo(),
    redo: () => editor.redo(),
  });
  function touchDown(event: PointerEvent) {
    if (event.pointerType !== 'touch' || !touches.down(event.pointerId, event.clientX, event.clientY, event.timeStamp)) return;
    event.stopImmediatePropagation();
    event.preventDefault();
    try {
      viewport?.setPointerCapture(event.pointerId);
    } catch {
      // Gone already.
    }
  }
  function touchMove(event: PointerEvent) {
    if (event.pointerType !== 'touch' || !touches.move(event.pointerId, event.clientX, event.clientY)) return;
    event.stopImmediatePropagation();
  }
  function touchUp(event: PointerEvent) {
    if (event.pointerType !== 'touch' || !touches.up(event.pointerId, event.timeStamp)) return;
    event.stopImmediatePropagation();
    if (viewport?.hasPointerCapture(event.pointerId)) viewport.releasePointerCapture(event.pointerId);
  }

  export function cancel(): boolean {
    snapLines = {};
    if (guideDrag) {
      guideDrag = null;
      return true;
    }
    const g = gesture;
    if (g) {
      gesture = null;
      if ('moved' in g && g.kind !== 'band' && g.moved) editor.cancel();
      return true;
    }
    if (points.length) {
      points = [];
      return true;
    }
    if (pen.length) {
      pen = [];
      penAt = null;
      return true;
    }
    if (measured) {
      measured = null;
      return true;
    }
    if (editor.editingPoints !== null) {
      editor.editingPoints = null;
      return true;
    }
    if (editor.editingText !== null) {
      finishTyping();
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
    if (editor.tool === 'pen' && pen.length >= 2) {
      finishPen(false);
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
    const [a, b] = corners(g);
    return { x: Math.min(a[0], b[0]), y: Math.min(a[1], b[1]), w: Math.abs(b[0] - a[0]), h: Math.abs(b[1] - a[1]), a, b };
  });
</script>

<svelte:window
  onkeydown={(e) => {
    windowKeydown(e);
    modifierChanged(e);
  }}
  onkeyup={(e) => {
    windowKeyup(e);
    modifierChanged(e);
  }}
/>

<div class="grid h-full w-full" style="grid-template-columns: 20px minmax(0, 1fr); grid-template-rows: 20px minmax(0, 1fr)">
<div aria-hidden="true" class="border-r border-b border-zinc-200 bg-zinc-100 dark:border-zinc-800 dark:bg-zinc-900"></div>
<div class="border-b border-zinc-200 dark:border-zinc-800"><Ruler axis="x" onguide={(e) => guideFromRuler(e, 'y')} /></div>
<div class="border-r border-zinc-200 dark:border-zinc-800"><Ruler axis="y" onguide={(e) => guideFromRuler(e, 'x')} /></div>
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  bind:this={viewport}
  bind:clientWidth={viewW}
  bind:clientHeight={viewH}
  class="relative h-full w-full touch-none overflow-hidden bg-zinc-300 select-none dark:bg-zinc-800 {gesture?.kind === 'pan' ? 'cursor-grabbing' : spaceHeld ? 'cursor-grab' : editor.tool === 'select' ? '' : 'cursor-crosshair'}"
  onpointerenter={() => (hovering = true)}
  onpointerdowncapture={touchDown}
  onpointermovecapture={touchMove}
  onpointerupcapture={touchUp}
  onpointercancelcapture={touchUp}
  onpointerdown={downOnDesk}
  onpointermove={pointerMove}
  onpointerup={pointerUp}
  onpointercancel={pointerUp}
  onpointerleave={() => {
    hovering = false;
    editor.pointer = null;
  }}
  oncontextmenu={openMenu}
  ondblclick={(ev) => {
    if (editor.tool === 'polygon') {
      ev.preventDefault();
      finishPolygon();
    } else if (editor.tool === 'pen') {
      ev.preventDefault();
      finishPen(false);
    } else {
      // The pointer is captured mid-click, so the double-click lands here:
      // the element is the one under the pointer.
      const [x, y] = onLabel(ev);
      const one = editor.selected.length === 1 ? editor.selected[0]! : null;
      if (one !== null && doc.elements[one]?.type === 'text' && elementsAt(doc, x, y).includes(one)) {
        editor.editingText = one;
        return;
      }
      if (one !== null && (editor.editingPoints === one || elementsAt(doc, x, y).includes(one))) doubleClickElement(ev, one);
    }
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
    <!-- Where the label lies once it is on, for one that prints turned: what is off it is cut off. -->
    {#if doc.print_rotation}
      <div aria-hidden="true" class="pointer-events-none absolute inset-0 border-2 border-dashed border-violet-600/70" style="transform: rotate({appliedTurn(doc)}deg)" data-applied-outline></div>
    {/if}
    <!-- The selected code's padding, shaded inside its box, and what was left over beyond it. -->
    {#each editor.selected as i (i)}
      {@const e = doc.elements[i]}
      {@const l = codeLayouts[i]}
      {#if e?.type === 'generator' && l?.padding && e.width > 0 && e.height > 0}
        {@const inset = (m: Record<'top' | 'right' | 'bottom' | 'left', number>) => `M${m.left} ${m.top}H${e.width - m.right}V${e.height - m.bottom}H${m.left}Z`}
        <svg
          class="pointer-events-none absolute overflow-visible"
          style={boxStyle(e)}
          viewBox="0 0 {e.width} {e.height}"
          preserveAspectRatio="none"
          aria-hidden="true"
          data-padding={i}
        >
          <path d="M0 0H{e.width}V{e.height}H0Z {inset(l.padding)}" fill-rule="evenodd" class="padding" />
          <path d="{inset(l.padding)} {inset(l.margin)}" fill-rule="evenodd" class="leftover" />
        </svg>
      {/if}
    {/each}
    {#each doc.elements as e, i (i)}
      <button
        type="button"
        data-element={i}
        class="element absolute"
        class:problem={problemElements.has(i)}
        class:warned={warned.has(i)}
        class:locked={e.locked || editor.hidden.includes(i)}
        class:passive={editor.tool !== 'select'}
        style={hitStyle(e)}
        aria-label={describeElement(e, i) + (e.locked ? ', locked' : '') + (editor.hidden.includes(i) ? ', hidden' : '')}
        aria-pressed={editor.selected.includes(i)}
        tabindex={e.locked || editor.hidden.includes(i) ? -1 : 0}
        onpointerdown={(ev) => downOnElement(ev, i)}
        onkeydown={(ev) => menuKey(ev, i)}
        onfocus={() => {
          // Tabbed to, it is selected, as clicked.
          if (!byPointer && !editor.selected.includes(i) && !e.locked) editor.selected = withGroups(doc.elements, [i]);
        }}
      ></button>
    {/each}
  </div>

  <!-- The selection, the same size at every zoom. -->
  <svg class="pointer-events-none absolute inset-0 h-full w-full overflow-visible" aria-hidden="true">
    {#if gridLines}
      <g data-grid>
        {#each gridLines.xs as l, k (k)}
          <line x1={l.at} x2={l.at} y1={oy} y2={oy + doc.height * scale} class={l.major ? 'grid-major' : 'grid-minor'} />
        {/each}
        {#each gridLines.ys as l, k (k)}
          <line y1={l.at} y2={l.at} x1={ox} x2={ox + doc.width * scale} class={l.major ? 'grid-major' : 'grid-minor'} />
        {/each}
      </g>
    {/if}
    {#if editor.guidesShown}
      {#each doc.guides?.x ?? [] as at, k (k)}
        {#if !(guideDrag?.axis === 'x' && guideDrag.index === k && guideDrag.moved)}
          <line x1={sx(at)} x2={sx(at)} y1="0" y2="100%" class="guide" />
          <line x1={sx(at)} x2={sx(at)} y1="0" y2="100%" class="guide-grip" class:locked-guide={editor.guidesLocked} data-guide="x{k}" onpointerdown={(ev) => downOnGuide(ev, 'x', k)} />
        {/if}
      {/each}
      {#each doc.guides?.y ?? [] as at, k (k)}
        {#if !(guideDrag?.axis === 'y' && guideDrag.index === k && guideDrag.moved)}
          <line y1={sy(at)} y2={sy(at)} x1="0" x2="100%" class="guide" />
          <line y1={sy(at)} y2={sy(at)} x1="0" x2="100%" class="guide-grip row" class:locked-guide={editor.guidesLocked} data-guide="y{k}" onpointerdown={(ev) => downOnGuide(ev, 'y', k)} />
        {/if}
      {/each}
    {/if}
    {#if guideDrag?.moved}
      {#if guideDrag.at === null}
        <!-- Off the label: let go here, and it comes off. -->
      {:else if guideDrag.axis === 'x'}
        <line x1={sx(guideDrag.at)} x2={sx(guideDrag.at)} y1="0" y2="100%" class="guide dragging" data-guide-dragging />
      {:else}
        <line y1={sy(guideDrag.at)} y2={sy(guideDrag.at)} x1="0" x2="100%" class="guide dragging" data-guide-dragging />
      {/if}
    {/if}
    {#if snapLines.x}
      <line x1={sx(snapLines.x.at)} x2={sx(snapLines.x.at)} y1="0" y2="100%" class="snapped" data-snapped="x" />
    {/if}
    {#if snapLines.y}
      <line y1={sy(snapLines.y.at)} y2={sy(snapLines.y.at)} x1="0" x2="100%" class="snapped" data-snapped="y" />
    {/if}
    {#each editor.selected as i (i)}
      {@const e = doc.elements[i]}
      {#if e}
        {@const h = handlePoints(e)}
        <polygon points={[h.nw, h.ne, h.se, h.sw].map((p) => p.join(',')).join(' ')} class="outline" />
      {/if}
    {/each}
    {#if editor.editingPoints !== null}
      <PointEditor bind:this={pointEditor} index={editor.editingPoints} {scale} {ox} {oy} />
    {:else if single && !single.locked}
      {@const h = handlePoints(single)}
      {@const c = handlePoints({ ...single, width: 0, height: 0 }).n}
      {#each ['nw', 'ne', 'se', 'sw'] as const as k (k)}
        {@const len = Math.hypot(h[k][0] - c[0], h[k][1] - c[1]) || 1}
        <circle
          cx={h[k][0] + ((h[k][0] - c[0]) / len) * 12}
          cy={h[k][1] + ((h[k][1] - c[1]) / len) * 12}
          r="12"
          class="rotate-zone"
          data-rotate={k}
          onpointerdown={downOnRotate}
          onpointerenter={() => (over = 'rotate')}
          onpointerleave={() => (over = null)}
        />
      {/each}
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
          onpointerenter={() => (over = 'handle')}
          onpointerleave={() => (over = null)}
        />
      {/each}
    {:else if selectionBox}
      {@const b = selectionBox}
      <rect x={sx(b.x)} y={sy(b.y)} width={b.width * scale} height={b.height * scale} class="outline group-box" />
      {#each [[0, 0, -1, -1], [1, 0, 1, -1], [1, 1, 1, 1], [0, 1, -1, 1]] as const as [fx, fy, ux, uy], k (k)}
        <circle
          cx={sx(b.x + fx * b.width) + ux * 8.5}
          cy={sy(b.y + fy * b.height) + uy * 8.5}
          r="12"
          class="rotate-zone"
          data-rotate={k}
          onpointerdown={downOnRotate}
          onpointerenter={() => (over = 'rotate')}
          onpointerleave={() => (over = null)}
        />
      {/each}
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
          onpointerenter={() => (over = 'handle')}
          onpointerleave={() => (over = null)}
        />
      {/each}
    {/if}
    {#if gesture?.kind === 'band' && gesture.moved}
      {@const g = gesture}
      {@const covering = g.to[0] >= g.from[0]}
      <rect
        x={sx(Math.min(g.from[0], g.to[0]))}
        y={sy(Math.min(g.from[1], g.to[1]))}
        width={Math.abs(g.to[0] - g.from[0]) * scale}
        height={Math.abs(g.to[1] - g.from[1]) * scale}
        class={covering ? 'band covering' : 'band touching'}
        data-band={covering ? 'covering' : 'touching'}
      />
    {/if}
    {#if draft}
      {#if editor.tool === 'line'}
        <line x1={sx(draft.a[0])} y1={sy(draft.a[1])} x2={sx(draft.b[0])} y2={sy(draft.b[1])} class="draft" />
      {:else if editor.tool === 'ellipse'}
        <ellipse cx={sx(draft.x + draft.w / 2)} cy={sy(draft.y + draft.h / 2)} rx={(draft.w * scale) / 2} ry={(draft.h * scale) / 2} class="draft" />
      {:else}
        <rect x={sx(draft.x)} y={sy(draft.y)} width={draft.w * scale} height={draft.h * scale} class="draft" />
      {/if}
    {/if}
    {#if pen.length}
      {@const d = pen
        .map((n, k) => {
          const prev = pen[k - 1];
          if (!prev) return `M${sx(n.p[0])} ${sy(n.p[1])}`;
          const c1 = prev.out ?? prev.p;
          const c2 = n.in ?? n.p;
          return `C${sx(c1[0])} ${sy(c1[1])} ${sx(c2[0])} ${sy(c2[1])} ${sx(n.p[0])} ${sy(n.p[1])}`;
        })
        .join(' ')}
      <path {d} class="draft" fill="none" data-pen />
      {#if penAt}
        {@const last = pen[pen.length - 1]!}
        <line x1={sx(last.p[0])} y1={sy(last.p[1])} x2={sx(penAt[0])} y2={sy(penAt[1])} class="draft" />
      {/if}
      {#each pen as n, k (k)}
        {#if n.out}<circle cx={sx(n.out[0])} cy={sy(n.out[1])} r="3" class="handle" />{/if}
        {#if n.in}<circle cx={sx(n.in[0])} cy={sy(n.in[1])} r="3" class="handle" />{/if}
        <rect x={sx(n.p[0]) - 3.5} y={sy(n.p[1]) - 3.5} width="7" height="7" class="handle" />
      {/each}
    {/if}
    {#if gesture?.kind === 'measure' || measured}
      {@const m = gesture?.kind === 'measure' ? gesture : measured!}
      <line x1={sx(m.from[0])} y1={sy(m.from[1])} x2={sx(m.to[0])} y2={sy(m.to[1])} class="measure" data-measure />
      <circle cx={sx(m.from[0])} cy={sy(m.from[1])} r="3" class="measure-end" />
      <circle cx={sx(m.to[0])} cy={sy(m.to[1])} r="3" class="measure-end" />
      {#if measured && !gesture}
        <text x={sx(m.to[0]) + 8} y={sy(m.to[1]) - 8} class="measure-text" data-measured>{measurement(m)}</text>
      {/if}
    {/if}
    {#if points.length}
      <polyline points={points.map(([x, y]) => `${sx(x)},${sy(y)}`).join(' ')} class="draft" />
      {#each points as [x, y], k (k)}
        <circle cx={sx(x)} cy={sy(y)} r="3.5" class="handle" />
      {/each}
    {/if}
  </svg>
  {#if editor.editingText !== null}
    {#key editor.editingText}
      <TextEditor index={editor.editingText} {scale} {ox} {oy} onfinish={finishTyping} />
    {/key}
  {/if}
  {#if readout && readoutAt}
    <div class="pointer-events-none absolute rounded bg-zinc-900/85 px-1.5 py-0.5 text-[0.7rem] text-white tabular-nums" style="left:{readoutAt[0] + 14}px;top:{readoutAt[1] + 14}px" data-readout>
      {readout}
    </div>
  {/if}
</div>
</div>

{#if menu}
  <ContextMenu x={menu.x} y={menu.y} label="Selection" items={menuItems} onclose={() => (menu = null)} />
{/if}

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
  .padding {
    fill: rgb(147 51 234 / 0.18);
  }
  .leftover {
    fill: rgb(113 113 122 / 0.22);
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
  .grid-major {
    stroke: rgb(113 113 122 / 0.35);
    stroke-width: 1px;
  }
  .grid-minor {
    stroke: rgb(113 113 122 / 0.15);
    stroke-width: 1px;
  }
  .guide {
    stroke: rgb(14 165 233);
    stroke-width: 1px;
  }
  .guide.dragging {
    stroke-dasharray: 4 3;
  }
  .guide-grip {
    stroke: transparent;
    stroke-width: 7px;
    pointer-events: auto;
    cursor: col-resize;
  }
  .guide-grip.row {
    cursor: row-resize;
  }
  .guide-grip.locked-guide {
    pointer-events: none;
  }
  .snapped {
    stroke: rgb(219 39 119);
    stroke-width: 1px;
  }
  .rotate-zone {
    fill: transparent;
    pointer-events: auto;
    cursor: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='24' height='24' viewBox='0 0 24 24'%3E%3Cpath d='M18 8a8 8 0 1 0 1 7' fill='none' stroke='white' stroke-width='4'/%3E%3Cpath d='M18 8a8 8 0 1 0 1 7' fill='none' stroke='black' stroke-width='2'/%3E%3Cpath d='M20 3v6h-6' fill='none' stroke='black' stroke-width='2'/%3E%3C/svg%3E") 12 12, alias;
  }
  .band {
    stroke-width: 1px;
  }
  .band.covering {
    fill: rgb(37 99 235 / 0.08);
    stroke: rgb(37 99 235);
  }
  .band.touching {
    fill: rgb(22 163 74 / 0.08);
    stroke: rgb(22 163 74);
    stroke-dasharray: 4 3;
  }
  .measure {
    stroke: rgb(219 39 119);
    stroke-width: 1.5px;
  }
  .measure-end {
    fill: rgb(219 39 119);
  }
  .measure-text {
    fill: rgb(219 39 119);
    font-size: 11px;
    paint-order: stroke;
    stroke: white;
    stroke-width: 3px;
  }
  .draft {
    fill: rgb(37 99 235 / 0.08);
    stroke: rgb(37 99 235);
    stroke-width: 1.5px;
    stroke-dasharray: 4 3;
  }
</style>
