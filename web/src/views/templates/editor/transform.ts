// The geometry of selecting and transforming, as Krita's shape tool does
// it: resizing from a handle with Shift keeping proportions and Ctrl working
// from the center, scaling a selection the same way, moving along one axis,
// turning in steps, the anchor numbers are given at, lengths typed with
// units, and box selection.

import { bounds, elementsAt, sinCos, snap, tidy, type Box, type Handle, type TemplateDoc, type TemplateElement, type Units } from '../../../lib/template';

export interface ResizeOptions {
  /** Shift: both sides in proportion. */
  keepRatio?: boolean;
  /** Ctrl: about the center, both sides moving. */
  fromCenter?: boolean;
  /** The grid step sizes snap to; none for no snapping. */
  step?: number;
}

/** Which way a handle pulls: -1, 0 or 1 across and down. */
export function pull(handle: Handle): [number, number] {
  return [handle.includes('e') ? 1 : handle.includes('w') ? -1 : 0, handle.includes('s') ? 1 : handle.includes('n') ? -1 : 0];
}

/** New sizes for a box w × h pulled by (along, down) in its own frame. */
function sized(w: number, h: number, sx: number, sy: number, along: number, down: number, o: ResizeOptions, least: number, units: Units): [number, number] {
  const k = o.fromCenter ? 2 : 1;
  let nw = sx ? w + sx * along * k : w;
  let nh = sy ? h + sy * down * k : h;
  if (o.keepRatio && w > 0 && h > 0) {
    // In proportion, by the side pulled, or the more pulled of two.
    const rw = nw / w;
    const rh = nh / h;
    const r = sx && sy ? (Math.abs(rw - 1) >= Math.abs(rh - 1) ? rw : rh) : sx ? rw : rh;
    nw = w * Math.max(r, least / Math.min(w, h));
    nh = h * Math.max(r, least / Math.min(w, h));
    return [nw, nh];
  }
  if (o.step) {
    if (sx) nw = snap(nw, o.step, units);
    if (sy) nh = snap(nh, o.step, units);
  }
  // A side with no length, as a level line's, keeps none.
  return [sx ? Math.max(nw, least) : nw, sy ? Math.max(nh, least) : nh];
}

/**
 * An element resized by dragging a handle (dx, dy) on the label: along its
 * own sides, as it lies turned. The corner or side opposite the handle stays
 * where it is on the label, or with Ctrl the center.
 */
export function resizeFrom(doc: TemplateDoc, e: TemplateElement, handle: Handle, dx: number, dy: number, o: ResizeOptions = {}): TemplateElement {
  const [sin, cos] = sinCos(e.rotation ?? 0);
  const along = dx * cos + dy * sin;
  const down = -dx * sin + dy * cos;
  const [sx, sy] = pull(handle);
  const least = o.step ?? 0.01;
  const [w, h] = sized(e.width, e.height, sx, sy, along, down, o, least, doc.units);
  let cx = e.x + e.width / 2;
  let cy = e.y + e.height / 2;
  if (!o.fromCenter) {
    // The center moves half the growth toward the handle, in the
    // element's frame, turned, so the opposite side stays.
    const ux = (sx * (w - e.width)) / 2;
    const uy = (sy * (h - e.height)) / 2;
    cx += ux * cos - uy * sin;
    cy += ux * sin + uy * cos;
  }
  const t = (v: number) => tidy(v, doc.units);
  return { ...e, x: t(cx - w / 2), y: t(cy - h / 2), width: t(w), height: t(h) };
}

/** The box round a selection, pulled by a handle (dx, dy), as resizeFrom pulls one element. */
export function scaledBox(box: Box, handle: Handle, dx: number, dy: number, units: Units, o: ResizeOptions = {}): Box {
  const [sx, sy] = pull(handle);
  const [w, h] = sized(box.width, box.height, sx, sy, dx, dy, o, o.step ?? 0.01, units);
  // The side opposite the handle stays; with Ctrl, or along a side not
  // pulled, the middle does.
  const x = o.fromCenter || !sx ? box.x + (box.width - w) / 2 : sx > 0 ? box.x : box.x + box.width - w;
  const y = o.fromCenter || !sy ? box.y + (box.height - h) / 2 : sy > 0 ? box.y : box.y + box.height - h;
  return { x, y, width: w, height: h };
}

/** A move held to the axis it goes along more, as Shift does. */
export function alongOneAxis(dx: number, dy: number): [number, number] {
  return Math.abs(dx) >= Math.abs(dy) ? [dx, 0] : [0, dy];
}

/**
 * An angle from turning: with Ctrl in 15° steps; otherwise snapped to a
 * multiple of 45° when within 5° of one, unless that is switched off.
 */
export function turned(angle: number, o: { steps?: boolean; snap45?: boolean }): number {
  const a = ((angle % 360) + 360) % 360;
  if (o.steps) return (Math.round(a / 15) * 15) % 360;
  if (o.snap45) {
    const nearest = Math.round(a / 45) * 45;
    if (Math.abs(a - nearest) <= 5) return nearest % 360;
  }
  return Math.round(a * 100) / 100;
}

/** The nine points numbers can be given at, as fractions of the box across and down. */
export type Anchor = 'nw' | 'n' | 'ne' | 'w' | 'c' | 'e' | 'sw' | 's' | 'se';
export const anchors: Anchor[] = ['nw', 'n', 'ne', 'w', 'c', 'e', 'sw', 's', 'se'];

export function anchorFraction(a: Anchor): [number, number] {
  return [a.includes('w') ? 0 : a.includes('e') ? 1 : 0.5, a.includes('n') ? 0 : a.includes('s') ? 1 : 0.5];
}

/** Where an element's anchor is on the label, its box as it is before it is turned. */
export function anchorAt(e: TemplateElement, a: Anchor): [number, number] {
  const [fx, fy] = anchorFraction(a);
  return [e.x + fx * e.width, e.y + fy * e.height];
}

/**
 * An element with numbers set at its anchor: x and y place the anchor; a
 * new width or height keeps the anchor where it is, and with the aspect
 * locked, the other side in proportion.
 */
export function setAtAnchor(
  doc: TemplateDoc,
  e: TemplateElement,
  a: Anchor,
  set: { x?: number; y?: number; width?: number; height?: number },
  locked = false,
): TemplateElement {
  const [fx, fy] = anchorFraction(a);
  let [ax, ay] = anchorAt(e, a);
  let { width, height } = e;
  if (set.width !== undefined && set.width > 0) {
    if (locked && e.width > 0) height = (height * set.width) / e.width;
    width = set.width;
  }
  if (set.height !== undefined && set.height > 0) {
    if (locked && e.height > 0) width = (width * set.height) / e.height;
    height = set.height;
  }
  if (set.x !== undefined) ax = set.x;
  if (set.y !== undefined) ay = set.y;
  const t = (v: number) => tidy(v, doc.units);
  return { ...e, x: t(ax - fx * width), y: t(ay - fy * height), width: t(width), height: t(height) };
}

/**
 * A length typed with its unit, "5mm", "0.2 in", "1.5cm" or "36pt", in the
 * template's units; a bare number is in them already. Null if unreadable.
 */
export function parseLength(text: string, units: Units): number | null {
  const m = /^\s*(-?\d*\.?\d+)\s*(mm|cm|in|"|pt)?\s*$/i.exec(text);
  if (!m) return null;
  const n = Number(m[1]);
  const unit = m[2]?.toLowerCase();
  const mm = unit === 'mm' ? n : unit === 'cm' ? n * 10 : unit === 'in' || unit === '"' ? n * 25.4 : unit === 'pt' ? (n * 25.4) / 72 : null;
  if (mm === null) return n;
  return units === 'mm' ? mm : mm / 25.4;
}

/**
 * What a selection box takes, as in Krita: dragged left to right, what it
 * covers wholly; right to left, what it touches. Locked elements are left.
 */
export function inBox(doc: TemplateDoc, box: Box, covering: boolean): number[] {
  return doc.elements.flatMap((e, i) => {
    if (e.locked) return [];
    const r = bounds(e);
    const hit = covering
      ? r.x >= box.x && r.y >= box.y && r.x + r.width <= box.x + box.width && r.y + r.height <= box.y + box.height
      : r.x < box.x + box.width && box.x < r.x + r.width && r.y < box.y + box.height && box.y < r.y + r.height;
    return hit ? [i] : [];
  });
}

/**
 * Ctrl+click, as Krita's: the element beneath the one selected at a point,
 * round to the top again; the top one when none there is selected.
 */
export function beneath(doc: TemplateDoc, x: number, y: number, selected: number[]): number | null {
  const stack = elementsAt(doc, x, y).filter((i) => !doc.elements[i]?.locked);
  if (!stack.length) return null;
  const from = selected.length === 1 && stack.includes(selected[0]!) ? stack.indexOf(selected[0]!) : -1;
  return stack[(from + 1) % stack.length]!;
}
