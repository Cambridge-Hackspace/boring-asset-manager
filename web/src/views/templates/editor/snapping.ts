// Snapping, as in Krita: what a drag snaps to is chosen in one "Snap to"
// menu (the grid, guides, other elements' edges and centers, the label's
// edges and center), Shift turns it off while held, and what it snapped to
// is drawn while dragging.

import { bounds, gridStep, tidy, type Box, type Grid, type TemplateDoc } from '../../../lib/template';

export interface SnapTo {
  grid: boolean;
  guides: boolean;
  elements: boolean;
  label: boolean;
}

export const snapNothing: SnapTo = { grid: false, guides: false, elements: false, label: false };

export type SnapKind = 'guide' | 'element' | 'label';

/** A line snapped to: where it is across (x) or down (y), and what it is. */
export interface SnapLine {
  at: number;
  kind: SnapKind;
}

/** The template's grid, or the editor's own: half a millimeter, or a fiftieth of an inch. */
export function gridOf(doc: TemplateDoc): Grid {
  const step = gridStep(doc.units);
  return doc.grid ?? { x: step, y: step };
}

/** A value on the grid's lines one way. */
export function onGrid(value: number, step: number, offset: number, units: TemplateDoc['units']): number {
  return tidy(offset + Math.round((value - offset) / step) * step, units);
}

/** The lines across (xs) and down (ys) a drag can snap to, leaving out the elements moving. */
export function targets(doc: TemplateDoc, moving: number[], to: SnapTo): { xs: SnapLine[]; ys: SnapLine[] } {
  const xs: SnapLine[] = [];
  const ys: SnapLine[] = [];
  if (to.label) {
    for (const at of [0, doc.width / 2, doc.width]) xs.push({ at, kind: 'label' });
    for (const at of [0, doc.height / 2, doc.height]) ys.push({ at, kind: 'label' });
  }
  if (to.guides) {
    for (const at of doc.guides?.x ?? []) xs.push({ at, kind: 'guide' });
    for (const at of doc.guides?.y ?? []) ys.push({ at, kind: 'guide' });
  }
  if (to.elements) {
    doc.elements.forEach((e, i) => {
      if (moving.includes(i)) return;
      const b = bounds(e);
      for (const at of [b.x, b.x + b.width / 2, b.x + b.width]) xs.push({ at, kind: 'element' });
      for (const at of [b.y, b.y + b.height / 2, b.y + b.height]) ys.push({ at, kind: 'element' });
    });
  }
  return { xs, ys };
}

/** The nearest line to any of the edges, within threshold: how far to move, and the line. */
function nearest(edges: number[], lines: SnapLine[], threshold: number): [number, SnapLine] | null {
  let found: [number, SnapLine] | null = null;
  for (const edge of edges) {
    for (const line of lines) {
      const d = line.at - edge;
      if (Math.abs(d) <= threshold && (!found || Math.abs(d) < Math.abs(found[0]))) found = [d, line];
    }
  }
  return found;
}

export interface Snapped {
  dx: number;
  dy: number;
  /** The lines snapped to, to draw; none where it kept to the grid or nothing. */
  x?: SnapLine;
  y?: SnapLine;
}

/**
 * A box moved by (dx, dy), its edges or middle snapped to the nearest line
 * within threshold on each axis by itself; an axis with none near keeps
 * its top left corner on the grid, when snapping to the grid.
 */
export function snapBox(doc: TemplateDoc, moving: number[], start: Box, dx: number, dy: number, threshold: number, to: SnapTo): Snapped {
  const { xs, ys } = targets(doc, moving, to);
  const g = gridOf(doc);
  const axis = (from: number, size: number, d: number, lines: SnapLine[], step: number, offset: number): [number, SnapLine | undefined] => {
    const hit = nearest([from + d, from + d + size / 2, from + d + size], lines, threshold);
    if (hit) return [d + hit[0], hit[1]];
    if (to.grid) return [onGrid(from + d, step, offset, doc.units) - from, undefined];
    return [d, undefined];
  };
  const [sx, lx] = axis(start.x, start.width, dx, xs, g.x, g.offset_x ?? 0);
  const [sy, ly] = axis(start.y, start.height, dy, ys, g.y, g.offset_y ?? 0);
  const fine = (v: number) => Math.round(v * 1e6) / 1e6 || 0;
  return { dx: fine(sx), dy: fine(sy), ...(lx ? { x: lx } : {}), ...(ly ? { y: ly } : {}) };
}

/** A point, as a handle or a drawing tool places it, snapped the same way, each axis by itself. */
export function snapPoint(
  doc: TemplateDoc,
  x: number,
  y: number,
  threshold: number,
  to: SnapTo,
  moving: number[] = [],
): { x: number; y: number; lineX?: SnapLine; lineY?: SnapLine } {
  const { xs, ys } = targets(doc, moving, to);
  const g = gridOf(doc);
  const axis = (v: number, lines: SnapLine[], step: number, offset: number): [number, SnapLine | undefined] => {
    const hit = nearest([v], lines, threshold);
    if (hit) return [hit[1].at, hit[1]];
    return [to.grid ? onGrid(v, step, offset, doc.units) : tidy(v, doc.units), undefined];
  };
  const [sx, lx] = axis(x, xs, g.x, g.offset_x ?? 0);
  const [sy, ly] = axis(y, ys, g.y, g.offset_y ?? 0);
  return { x: sx, y: sy, ...(lx ? { lineX: lx } : {}), ...(ly ? { lineY: ly } : {}) };
}
