import { describe, expect, it } from 'vitest';
import type { TemplateDoc, TemplateElement } from '../../../lib/template';
import { contoursOf, formatPath, nearestOn, parsePath, splitAt, toggled, withContours, type Contour } from './nodes';

const doc: TemplateDoc = { id: 't', name: 'T', units: 'mm', width: 60, height: 30, dpi: 300, layering: 'list', elements: [] };
const id = (p: [number, number]): [number, number] => p;

describe('points and handles', () => {
  it('reads and writes path data as the server does, a circle included', () => {
    const circle = 'M1 0.5 C1 0.77614 0.77614 1 0.5 1 C0.22386 1 0 0.77614 0 0.5 C0 0.22386 0.22386 0 0.5 0 C0.77614 0 1 0.22386 1 0.5 Z';
    const contours = parsePath(circle, id);
    expect(contours).toHaveLength(1);
    expect(contours[0]!.closed).toBe(true);
    // Four points, the closing curve's end taken as the first.
    expect(contours[0]!.nodes.map((n) => n.p)).toEqual([[1, 0.5], [0.5, 1], [0, 0.5], [0.5, 0]]);
    expect(contours[0]!.nodes.every((n) => n.smooth)).toBe(true);
    expect(formatPath(contours, id)).toBe(circle);
    const square = 'M0.5 0 L1 1 L0 1 Z M0.45 0.35 L0.55 0.35 Z';
    expect(formatPath(parsePath(square, id), id)).toBe(square);
    // Quadratics read as the cubics they are.
    expect(formatPath(parsePath('M0 0 Q0.5 1 1 0', id), id)).toBe('M0 0 C0.33333 0.66667 0.66667 0.66667 1 0');
  });

  it('keeps a polygon a polygon while its sides are straight, and a path once curved', () => {
    const tri: TemplateElement = { type: 'shape', shape: 'polygon', x: 10, y: 5, width: 20, height: 10, points: [[0.5, 0], [1, 1], [0, 1]] };
    const c = contoursOf(tri);
    expect(c[0]!.nodes.map((n) => n.p)).toEqual([[10, 0], [20, 10], [0, 10]]);
    // The top point moved up 5: the box grows up, the others stay where they lie.
    c[0]!.nodes[0]!.p = [10, -5];
    expect(withContours(doc, tri, c)).toMatchObject({ shape: 'polygon', x: 10, y: 0, width: 20, height: 15, points: [[0.5, 0], [1, 1], [0, 1]] });
    // Curved, it becomes a path, its fill kept.
    c[0]!.nodes[0] = toggled(c[0]!.nodes[0]!, c[0]!.nodes[1], c[0]!.nodes[2]);
    const curved = withContours(doc, { ...tri, fill: 'black' }, c);
    expect(curved.shape).toBe('path');
    expect(curved.points).toBeUndefined();
    expect(curved.fill).toBe('black');
    expect(curved.d).toMatch(/^M.* C.* Z$/);
  });

  it('keeps points where they lie on the label when it is turned', () => {
    const e: TemplateElement = { type: 'shape', shape: 'polygon', x: 10, y: 10, width: 10, height: 10, rotation: 90, points: [[0, 0], [1, 0], [1, 1], [0, 1]] };
    const c = contoursOf(e);
    // Unchanged, it is as it was.
    expect(withContours(doc, e, c)).toMatchObject({ x: 10, y: 10, width: 10, height: 10, rotation: 90 });
  });

  it('turns a sharp point smooth along its neighbors, and back', () => {
    const smooth = toggled({ p: [5, 5] }, { p: [11, 5] }, { p: [-1, 5] });
    expect(smooth).toEqual({ p: [5, 5], in: [3, 5], out: [7, 5], smooth: true });
    expect(toggled(smooth, undefined, undefined)).toEqual({ p: [5, 5] });
  });

  it('splits a segment where a point is added, the curve keeping its shape', () => {
    expect(splitAt({ p: [0, 0] }, { p: [10, 0] }, 0.25).mid).toEqual({ p: [2.5, 0] });
    const a = { p: [0, 0] as [number, number], out: [0, 10] as [number, number] };
    const b = { p: [10, 0] as [number, number], in: [10, 10] as [number, number] };
    const half = splitAt(a, b, 0.5);
    expect(half.mid.p).toEqual([5, 7.5]);
    expect(half.a.out).toEqual([0, 5]);
    expect(half.b.in).toEqual([10, 5]);
    expect(nearestOn(a, b, [5, 8]).t).toBeCloseTo(0.5, 1);
  });

  it('writes an open path with no fill', () => {
    const open: Contour[] = [{ nodes: [{ p: [0, 0] }, { p: [10, 5] }], closed: false }];
    const e = withContours(doc, { type: 'shape', shape: 'path', x: 0, y: 0, width: 1, height: 1, d: 'M0 0 L1 1', fill: 'black' }, open);
    expect(e).toMatchObject({ d: 'M0 0 L1 1', fill: 'none', width: 10, height: 5 });
  });
});
