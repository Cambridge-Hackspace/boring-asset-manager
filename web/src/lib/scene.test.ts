import { describe, expect, it } from 'vitest';
import { describeItem, gridLines, hull, panBy, project, zoomAt, type ViewBox } from './scene';

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
});
