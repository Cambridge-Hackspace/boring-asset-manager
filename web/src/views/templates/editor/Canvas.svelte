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
    scaleElements,
    sinCos,
    snap,
    tidy,
    withGroups,
    withProp,
    rotateElements,
    type Box,
    type Handle,
    type PrintArea,
    type ShapeKind,
    type TemplateElement,
  } from '../../../lib/template';
  import { untrack } from 'svelte';
  import { useEditor, type Tool } from './editor.svelte';
  import Ruler from './Ruler.svelte';
  import ContextMenu, { type MenuItem } from './ContextMenu.svelte';
  import { alongOneAxis, beneath, inBox, resizeFrom, scaledBox, turned, type Anchor } from './transform';

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
    | { kind: 'move'; x0: number; y0: number; start: TemplateElement[]; indices: number[]; hit: number; moved: boolean; last?: Pointer }
    | { kind: 'resize'; x0: number; y0: number; start: TemplateElement[]; index: number; handle: Handle; moved: boolean; last?: Pointer }
    | { kind: 'scale'; x0: number; y0: number; start: TemplateElement[]; indices: number[]; handle: Handle; box: Box; moved: boolean; last?: Pointer }
    | { kind: 'rotate'; x0: number; y0: number; start: TemplateElement[]; indices: number[]; cx: number; cy: number; moved: boolean; last?: Pointer }
    | { kind: 'band'; x0: number; y0: number; from: [number, number]; to: [number, number]; add: boolean; moved: boolean; last?: Pointer }
    | { kind: 'draw'; from: [number, number]; to: [number, number] }
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
    else if (g?.kind === 'draw') editor.hint = `Drawing ${toolNames[tool as Exclude<Tool, 'select'>] ?? 'a shape'}. Alt: off the grid. Esc: stop.`;
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
    gesture = { kind: 'move', x0: event.clientX, y0: event.clientY, start: editor.elements(), indices: next, hit: i, moved: false };
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
    if (startPan(event) || event.button !== 0 || !paper) return;
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
    gesture = { kind: 'draw', from: p, to: p };
    capture(event);
  }

  /** Carries a gesture to where the pointer is now, with the modifiers held now. */
  function apply(g: Exclude<Gesture, { kind: 'draw' } | { kind: 'pan' }>, p: Pointer) {
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
    if (g.kind === 'move') {
      if (p.shiftKey) [dx, dy] = alongOneAxis(dx, dy);
      editor.doc.elements = moveElements(doc, g.indices, g.start, dx, dy, step ?? 0.001);
    } else if (g.kind === 'resize') {
      const o = { keepRatio: p.shiftKey, fromCenter: ctrl, step };
      editor.doc.elements = g.start.map((e, i) => (i === g.index ? resizeFrom(doc, e, g.handle, dx, dy, o) : e));
    } else if (g.kind === 'scale') {
      const to = scaledBox(g.box, g.handle, dx, dy, doc.units, { keepRatio: p.shiftKey, fromCenter: ctrl, step });
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

  function pointerMove(event: PointerEvent) {
    if (paper) {
      const [x, y] = onLabel(event);
      editor.pointer = x >= 0 && y >= 0 && x <= doc.width && y <= doc.height ? { x: tidy(x, doc.units), y: tidy(y, doc.units) } : null;
    }
    if (viewport) {
      const r = viewport.getBoundingClientRect();
      readoutAt = [event.clientX - r.left, event.clientY - r.top];
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
      g.to = gridPoint(event);
      return;
    }
    apply(g, event);
  }

  // A modifier pressed or let go mid-drag: the drag again, as it would be with it.
  function modifierChanged(event: KeyboardEvent) {
    const g = gesture;
    if (!g || !('last' in g) || !g.last || !['Shift', 'Control', 'Alt', 'Meta'].includes(event.key)) return;
    apply(g, { ...g.last, shiftKey: event.shiftKey, ctrlKey: event.ctrlKey, metaKey: event.metaKey, altKey: event.altKey });
  }

  function pointerUp(event: PointerEvent) {
    if (viewport?.hasPointerCapture(event.pointerId)) viewport.releasePointerCapture(event.pointerId);
    const g = gesture;
    gesture = null;
    if (g?.kind === 'draw') finishDrawing(g.from, g.to);
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
      if (g.kind !== 'draw' && g.kind !== 'pan' && g.kind !== 'band' && g.moved) editor.cancel();
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
<div class="border-b border-zinc-200 dark:border-zinc-800"><Ruler axis="x" /></div>
<div class="border-r border-zinc-200 dark:border-zinc-800"><Ruler axis="y" /></div>
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  bind:this={viewport}
  bind:clientWidth={viewW}
  bind:clientHeight={viewH}
  class="relative h-full w-full overflow-hidden bg-zinc-300 select-none dark:bg-zinc-800 {gesture?.kind === 'pan' ? 'cursor-grabbing' : spaceHeld ? 'cursor-grab' : editor.tool === 'select' ? '' : 'cursor-crosshair'}"
  onpointerenter={() => (hovering = true)}
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
    {#each editor.selected as i (i)}
      {@const e = doc.elements[i]}
      {#if e}
        {@const h = handlePoints(e)}
        <polygon points={[h.nw, h.ne, h.se, h.sw].map((p) => p.join(',')).join(' ')} class="outline" />
      {/if}
    {/each}
    {#if single && !single.locked}
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
  .draft {
    fill: rgb(37 99 235 / 0.08);
    stroke: rgb(37 99 235);
    stroke-width: 1.5px;
    stroke-dasharray: 4 3;
  }
</style>
