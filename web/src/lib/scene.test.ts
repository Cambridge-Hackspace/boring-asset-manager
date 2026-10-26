import { describe, expect, it } from 'vitest';
import { describeItem, floorDelta, gridLines, hull, liftTo, moveTo, overlapNote, position, panBy, project, zoomAt, type ViewBox } from './scene';

describe('scenes', () => {
  it('projects as isolex does', () => {
    expect(project(0, 0, 0)).toEqual([0, 0]);
    expect(project(1, 0, 0)).toEqual([34.64101615137755, 20]);
    expect(project(0, 1, 1)).toEqual([-34.64101615137755, -20]);
  });

  it('draws the floor a line along each tile edge', () => {
    const lines = gridLines(2, 1);
    expect(lines).toHaveLength(5);
    expect(lines[0]).toEqual([0, 0, ...project(0, 1, 0)]);
  });

  it('outlines a box by its silhouette', () => {
    const h = hull({ x: 1, y: 0, z: 0, size: [1, 1, 1] });
    expect(h).toHaveLength(6);
    expect(h[0]).toEqual(project(1, 0, 1));
    expect(h[3]).toEqual(project(2, 1, 0));
  });

  it('zooms about a point and pans, within limits', () => {
    const whole: ViewBox = [0, 0, 100, 50];
    const near = zoomAt(whole, whole, 50, 25, 2);
    expect(near).toEqual([25, 12.5, 50, 25]);
    // the point stays where it was
    const at = zoomAt(whole, whole, 10, 10, 2);
    expect((10 - at[0]) / at[2]).toBeCloseTo(10 / 100);
    expect(zoomAt(whole, whole, 0, 0, 1000)[2]).toBe(10);
    expect(zoomAt(whole, whole, 0, 0, 0.001)[2]).toBe(400);
    expect(panBy(whole, 5, -5)).toEqual([5, -5, 100, 50]);
  });

  it('says what an item is', () => {
    expect(describeItem({ name: 'Web', code: 'SRV-1', condition: 'out_of_service', holds: 0 })).toBe('Web, SRV-1, out of service');
    expect(describeItem({ name: 'Rack', condition: 'ok', holds: 3 })).toBe('Rack, holds 3');
  });

  it('turns a move across the page into tiles across the floor', () => {
    const [dx, dy] = project(2, 1, 0);
    const [x, y] = floorDelta(dx, dy);
    expect(x).toBeCloseTo(2);
    expect(y).toBeCloseTo(1);
  });

  it('moves onto whole tiles, on the floor', () => {
    const item = { size: [2, 1, 1] as [number, number, number] };
    expect(moveTo(item, [1, 1], 0.6, -0.4, [4, 3])).toEqual([2, 1]);
    expect(moveTo(item, [1, 1], 5, 5, [4, 3])).toEqual([2, 2]);
    expect(moveTo(item, [1, 1], -5, -5, [4, 3])).toEqual([0, 0]);
  });

  it('lifts onto what is under it, or onto a step', () => {
    const bench = { id: 'b', x: 0, y: 0, z: 0, size: [2, 1, 0.8] as [number, number, number] };
    const far = { id: 'f', x: 3, y: 0, z: 0, size: [1, 1, 3] as [number, number, number] };
    const box = { id: 'x', x: 1, y: 0, size: [1, 1, 1] as [number, number, number] };
    expect(liftTo([bench, far], box, 0.85)).toBe(0.8);
    expect(liftTo([bench, far], box, 1.4)).toBe(1.5);
    expect(liftTo([bench, far], box, 2.95)).toBe(3);
    expect(liftTo([bench, far], box, -1)).toBe(0);
  });

  it('says where an item stands', () => {
    expect(position({ x: 1, y: 2, z: 0.25 })).toBe('1, 2, 0.25');
    expect(position({ x: 0, y: 0, z: 1 / 3 })).toBe('0, 0, 0.333');
  });

  it('names what an item overlaps', () => {
    const items = [{ id: 'a', name: 'Bench' }, { id: 'b', name: 'Crate' }];
    expect(overlapNote({ overlaps: ['a', 'b'] }, items)).toBe('Overlaps Bench, Crate.');
    expect(overlapNote({}, items)).toBe('');
  });
});
