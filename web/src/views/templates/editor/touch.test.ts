import { describe, expect, it } from 'vitest';
import { Touches, type TouchActions } from './touch';

function record() {
  const done: string[] = [];
  const act: TouchActions = {
    cancel: () => done.push('cancel'),
    pan: (dx, dy) => done.push(`pan ${dx} ${dy}`),
    zoom: (f, x, y) => done.push(`zoom ${f} at ${x} ${y}`),
    undo: () => done.push('undo'),
    redo: () => done.push('redo'),
  };
  return { done, touches: new Touches(act) };
}

describe('touch', () => {
  it('leaves one finger to the tool', () => {
    const { done, touches } = record();
    expect(touches.down(1, 10, 10, 0)).toBe(false);
    expect(touches.move(1, 50, 50)).toBe(false);
    expect(touches.up(1, 500)).toBe(false);
    expect(done).toEqual([]);
  });

  it('pinches toward the fingers and pans with them, putting back what one finger began', () => {
    const { done, touches } = record();
    touches.down(1, 100, 100, 0);
    touches.move(1, 104, 100);
    expect(touches.down(2, 204, 100, 50)).toBe(true);
    expect(done).toEqual(['cancel']);
    // Apart, twice as far: zoom 2× about the point between them.
    expect(touches.move(2, 304, 100)).toBe(true);
    expect(done.slice(1)).toEqual(['pan 50 0', 'zoom 2 at 204 100']);
    // Both down 10, one after the other: panned 10 down, zoomed back as it was.
    touches.move(1, 104, 110);
    touches.move(2, 304, 110);
    const after = done.slice(3);
    const pans = after.filter((d) => d.startsWith('pan')).map((d) => d.split(' ').map(Number));
    expect(pans.reduce((s, p) => s + p[2]!, 0)).toBe(10);
    expect(pans.reduce((s, p) => s + p[1]!, 0)).toBe(0);
    const zoomed = after.filter((d) => d.startsWith('zoom')).reduce((z, d) => z * Number(d.split(' ')[1]), 1);
    expect(zoomed).toBeCloseTo(1, 10);
    // The last finger left keeps to the gesture, not the tool, until it lifts.
    expect(touches.up(2, 900)).toBe(true);
    expect(touches.move(1, 0, 0)).toBe(true);
    expect(touches.up(1, 950)).toBe(true);
    expect(done.filter((d) => d === 'undo' || d === 'redo')).toEqual([]);
  });

  it('undoes on a tap of two fingers and redoes on three, but not when they moved or lingered', () => {
    const { done, touches } = record();
    touches.down(1, 100, 100, 0);
    touches.down(2, 150, 100, 20);
    touches.up(1, 120);
    touches.up(2, 140);
    touches.down(1, 100, 100, 1000);
    touches.down(2, 150, 100, 1010);
    touches.down(3, 200, 100, 1020);
    touches.up(3, 1100);
    touches.up(1, 1110);
    touches.up(2, 1120);
    expect(done.filter((d) => d !== 'cancel')).toEqual(['undo', 'redo']);

    done.length = 0;
    touches.down(1, 100, 100, 2000);
    touches.down(2, 150, 100, 2010);
    touches.up(1, 2500);
    touches.up(2, 2510);
    touches.down(1, 100, 100, 3000);
    touches.down(2, 150, 100, 3010);
    touches.move(2, 190, 100);
    touches.up(1, 3050);
    touches.up(2, 3060);
    expect(done.filter((d) => d === 'undo' || d === 'redo')).toEqual([]);
  });
});
