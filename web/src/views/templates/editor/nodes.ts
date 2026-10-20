// Points and their handles, as Krita's path tool edits them: a polygon's
// points, or a path's (drawn with the pen, or imported artwork), in the
// element's own box before it is turned, in the template's units. A point
// is sharp, its handles moving alone or absent, or smooth, its handles in
// line. Read from an element and written back, the box is fitted round
// them again, the points staying where they lie on the label.

import { sinCos, tidy, type TemplateDoc, type TemplateElement } from '../../../lib/template';

export type Pt = [number, number];

export interface Node {
  p: Pt;
  /** The handle the curve arrives along, and the one it leaves along. */
  in?: Pt;
  out?: Pt;
  smooth?: boolean;
}

export interface Contour {
  nodes: Node[];
  closed: boolean;
}

/** Path data as the server writes it: absolute M, L, Q, C and Z, numbers to five places. */
export function formatPath(contours: Contour[], frac: (p: Pt) => Pt): string {
  const n = (v: number) => {
    const s = String(Math.round(v * 1e5) / 1e5);
    return s === '-0' ? '0' : s;
  };
  const pt = (p: Pt) => {
    const [x, y] = frac(p);
    return `${n(x)} ${n(y)}`;
  };
  const out: string[] = [];
  for (const c of contours) {
    if (!c.nodes.length) continue;
    out.push(`M${pt(c.nodes[0]!.p)}`);
    const seg = (a: Node, b: Node) => (a.out || b.in ? `C${pt(a.out ?? a.p)} ${pt(b.in ?? b.p)} ${pt(b.p)}` : `L${pt(b.p)}`);
    for (let k = 1; k < c.nodes.length; k++) out.push(seg(c.nodes[k - 1]!, c.nodes[k]!));
    if (c.closed) {
      const last = c.nodes[c.nodes.length - 1]!;
      const first = c.nodes[0]!;
      if (c.nodes.length > 1 && (last.out || first.in)) out.push(seg(last, first));
      out.push('Z');
    }
  }
  return out.join(' ');
}

/** Path data as the server writes it, back into contours, its points through toUnits. */
export function parsePath(d: string, toUnits: (p: Pt) => Pt): Contour[] {
  const tokens = d.match(/[MLQCZ]|-?\d*\.?\d+(?:[eE][+-]?\d+)?/g) ?? [];
  const contours: Contour[] = [];
  let current: Contour | null = null;
  let i = 0;
  const num = () => Number(tokens[i++]);
  const point = (): Pt => toUnits([num(), num()]);
  const near = (a: Pt, b: Pt) => Math.abs(a[0] - b[0]) < 1e-6 && Math.abs(a[1] - b[1]) < 1e-6;
  while (i < tokens.length) {
    const op = tokens[i++];
    const last = current?.nodes[current.nodes.length - 1];
    switch (op) {
      case 'M':
        current = { nodes: [{ p: point() }], closed: false };
        contours.push(current);
        break;
      case 'L':
        current?.nodes.push({ p: point() });
        break;
      case 'Q': {
        // A quadratic, as the cubic it is.
        const c = point();
        const p = point();
        if (last && current) {
          last.out = [last.p[0] + (2 / 3) * (c[0] - last.p[0]), last.p[1] + (2 / 3) * (c[1] - last.p[1])];
          current.nodes.push({ p, in: [p[0] + (2 / 3) * (c[0] - p[0]), p[1] + (2 / 3) * (c[1] - p[1])] });
        }
        break;
      }
      case 'C': {
        const c1 = point();
        const c2 = point();
        const p = point();
        if (last && current) {
          last.out = c1;
          current.nodes.push({ p, in: c2 });
        }
        break;
      }
      case 'Z':
        if (current) {
          current.closed = true;
          // A closing curve ends on the first point: its handle is the first point's.
          const nodes = current.nodes;
          if (nodes.length > 1 && near(nodes[nodes.length - 1]!.p, nodes[0]!.p)) {
            const end = nodes.pop()!;
            if (end.in) nodes[0]!.in = end.in;
          }
        }
        break;
      default:
        return contours;
    }
  }
  // Smooth where the handles are in line.
  for (const c of contours) for (const n of c.nodes) n.smooth = inLine(n);
  return contours;
}

function inLine(n: Node): boolean {
  if (!n.in || !n.out) return false;
  const a = [n.in[0] - n.p[0], n.in[1] - n.p[1]];
  const b = [n.out[0] - n.p[0], n.out[1] - n.p[1]];
  const cross = a[0]! * b[1]! - a[1]! * b[0]!;
  const dot = a[0]! * b[0]! + a[1]! * b[1]!;
  return dot < 0 && Math.abs(cross) <= 1e-6 * (Math.hypot(a[0]!, a[1]!) * Math.hypot(b[0]!, b[1]!) + 1e-12);
}

/** A polygon's or path's points, in its own box before it is turned. */
export function contoursOf(e: TemplateElement): Contour[] {
  if (e.shape === 'polygon') {
    return [{ nodes: (e.points ?? []).map(([u, v]) => ({ p: [u * e.width, v * e.height] as Pt })), closed: e.closed !== false }];
  }
  if (e.shape === 'path' && e.d) return parsePath(e.d, ([u, v]) => [u * e.width, v * e.height]);
  return [];
}

/** Whether contours are only straight sides, as a polygon's are. */
function straight(contours: Contour[]): boolean {
  return contours.length === 1 && contours[0]!.nodes.every((n) => !n.in && !n.out);
}

/**
 * The element with these points, in its own box's frame: the box fitted
 * round them and moved so they stay where they lie on the label, turned.
 * A polygon stays one while its sides are straight; given a curve, it
 * becomes a path.
 */
export function withContours(doc: TemplateDoc, e: TemplateElement, contours: Contour[]): TemplateElement {
  const all: Pt[] = contours.flatMap((c) => c.nodes.flatMap((n) => [n.p, ...(n.in ? [n.in] : []), ...(n.out ? [n.out] : [])]));
  if (!all.length) return e;
  const xs = all.map((p) => p[0]);
  const ys = all.map((p) => p[1]);
  const minX = Math.min(...xs);
  const minY = Math.min(...ys);
  // A box at least a hundredth across, so a level or upright path keeps a size.
  const w = Math.max(Math.max(...xs) - minX, 0.01);
  const h = Math.max(Math.max(...ys) - minY, 0.01);
  const [sin, cos] = sinCos(e.rotation ?? 0);
  const ox = minX + w / 2 - e.width / 2;
  const oy = minY + h / 2 - e.height / 2;
  const cx = e.x + e.width / 2 + ox * cos - oy * sin;
  const cy = e.y + e.height / 2 + ox * sin + oy * cos;
  const frac = (p: Pt): Pt => [Math.round(((p[0] - minX) / w) * 1e5) / 1e5, Math.round(((p[1] - minY) / h) * 1e5) / 1e5];
  const box = { x: tidy(cx - w / 2, doc.units), y: tidy(cy - h / 2, doc.units), width: tidy(w, doc.units), height: tidy(h, doc.units) };
  if (e.shape === 'polygon' && straight(contours)) {
    return { ...e, ...box, points: contours[0]!.nodes.map((n) => frac(n.p)) };
  }
  const { points: _p, closed: _c, ...rest } = e;
  const open = contours.some((c) => !c.closed);
  return { ...rest, shape: 'path', ...box, d: formatPath(contours, frac), ...(open ? { fill: 'none' as const } : {}) };
}

/** A point's handles in line, as smooth, or taken away, as sharp. */
export function toggled(n: Node, next: Node | undefined, prev: Node | undefined): Node {
  if (n.smooth) return { p: n.p };
  // Smooth: handles along the line from the point before to the one after, a third of the way.
  const a = prev?.p ?? n.p;
  const b = next?.p ?? n.p;
  const dx = (b[0] - a[0]) / 6;
  const dy = (b[1] - a[1]) / 6;
  if (!dx && !dy) return { ...n, smooth: true };
  return { p: n.p, in: [n.p[0] - dx, n.p[1] - dy], out: [n.p[0] + dx, n.p[1] + dy], smooth: true };
}

/** A point on a segment, at t from a to b, and the segment split there, as de Casteljau splits a cubic. */
export function splitAt(a: Node, b: Node, t: number): { a: Node; mid: Node; b: Node } {
  if (!a.out && !b.in) {
    const p: Pt = [a.p[0] + (b.p[0] - a.p[0]) * t, a.p[1] + (b.p[1] - a.p[1]) * t];
    return { a, mid: { p }, b };
  }
  const p0 = a.p;
  const p1 = a.out ?? a.p;
  const p2 = b.in ?? b.p;
  const p3 = b.p;
  const lerp = (u: Pt, v: Pt): Pt => [u[0] + (v[0] - u[0]) * t, u[1] + (v[1] - u[1]) * t];
  const q0 = lerp(p0, p1);
  const q1 = lerp(p1, p2);
  const q2 = lerp(p2, p3);
  const r0 = lerp(q0, q1);
  const r1 = lerp(q1, q2);
  const s = lerp(r0, r1);
  return { a: { ...a, out: q0 }, mid: { p: s, in: r0, out: r1, smooth: true }, b: { ...b, in: q2 } };
}

/** Where on a segment a point is nearest, as t from a to b, and how far it is. */
export function nearestOn(a: Node, b: Node, q: Pt): { t: number; distance: number } {
  let best = { t: 0, distance: Infinity };
  for (let k = 0; k <= 64; k++) {
    const t = k / 64;
    const p = splitAt(a, b, t).mid.p;
    const d = Math.hypot(p[0] - q[0], p[1] - q[1]);
    if (d < best.distance) best = { t, distance: d };
  }
  return best;
}
