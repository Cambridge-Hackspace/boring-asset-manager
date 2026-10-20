import { describe, expect, it } from 'vitest';
import type { TemplateDoc, TemplateElement } from '../../../lib/template';
import { gridOf, onGrid, snapBox, snapNothing, snapPoint, targets, type SnapTo } from './snapping';

const all: SnapTo = { grid: true, guides: true, elements: true, label: true };
const box = (x: number, y: number, w: number, h: number): TemplateElement => ({ type: 'text', x, y, width: w, height: h });
const doc = (extra: Partial<TemplateDoc> = {}): TemplateDoc => ({
  id: 't', name: 'T', units: 'mm', width: 60, height: 30, dpi: 300, layering: 'list',
  elements: [box(10, 5, 10, 10), box(30, 20, 10, 5)],
  guides: { x: [33], y: [12] },
  ...extra,
});

describe('snapping', () => {
  it('takes the template grid, or the editor own', () => {
    expect(gridOf(doc())).toEqual({ x: 0.5, y: 0.5 });
    expect(gridOf(doc({ grid: { x: 2, y: 3 } }))).toEqual({ x: 2, y: 3 });
    expect(onGrid(7.1, 2, 0.5, 'mm')).toBe(6.5);
  });

  it('gathers only the lines chosen', () => {
    const t = targets(doc(), [0], { ...snapNothing, guides: true });
    expect(t).toEqual({ xs: [{ at: 33, kind: 'guide' }], ys: [{ at: 12, kind: 'guide' }] });
    // The elements moving are left out.
    expect(targets(doc(), [1], { ...snapNothing, elements: true }).xs.map((l) => l.at)).toEqual([10, 15, 20]);
  });

  it('snaps a moving box to the nearest line, each axis by itself, and to the grid otherwise', () => {
    const start = { x: 10, y: 5, width: 10, height: 10 };
    // Its right edge comes within 1 of the guide at 33; its bottom is at the label's middle.
    expect(snapBox(doc(), [0], start, 13.6, 0.3, 1, all)).toEqual({ dx: 13, dy: 0, x: { at: 33, kind: 'guide' }, y: { at: 15, kind: 'label' } });
    // Nothing near down: to the grid of 0.5, as its corner lies.
    expect(snapBox(doc(), [0], start, 0, 3.3, 1, { ...all, elements: false })).toMatchObject({ dy: 3.5 });
    // With a grid of its own, offset.
    expect(snapBox(doc({ grid: { x: 4, y: 4, offset_y: 1 } }), [0], start, 0, 3.3, 0.1, all)).toMatchObject({ dy: 4 });
    // Snapping nothing: as dragged.
    expect(snapBox(doc(), [0], start, 13.6, 0.3, 1, snapNothing)).toEqual({ dx: 13.6, dy: 0.3 });
  });

  it('snaps a point, as handles and drawing tools place them', () => {
    expect(snapPoint(doc(), 29.6, 11.7, 1, all)).toMatchObject({ x: 30, y: 12, lineX: { kind: 'label' }, lineY: { kind: 'guide' } });
    expect(snapPoint(doc(), 39.6, 2, 1, all)).toMatchObject({ x: 40, lineX: { kind: 'element' } });
    expect(snapPoint(doc(), 47.3, 2.6, 1, all)).toEqual({ x: 47.5, y: 2.5 });
    expect(snapPoint(doc(), 47.3, 2.6, 1, snapNothing)).toEqual({ x: 47.3, y: 2.6 });
  });
});
