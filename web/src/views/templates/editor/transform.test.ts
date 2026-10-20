import { describe, expect, it } from 'vitest';
import type { TemplateDoc, TemplateElement } from '../../../lib/template';
import { alongOneAxis, anchorAt, beneath, inBox, parseLength, resizeFrom, scaledBox, setAtAnchor, turned } from './transform';

const doc = (elements: TemplateElement[] = []): TemplateDoc => ({ id: 't', name: 'T', units: 'mm', width: 90, height: 29, dpi: 300, layering: 'list', elements });
const box = (x: number, y: number, width: number, height: number, extra: Partial<TemplateElement> = {}): TemplateElement => ({ type: 'text', x, y, width, height, ...extra });

describe('resizing from a handle', () => {
  it('keeps the opposite corner, on the grid', () => {
    expect(resizeFrom(doc(), box(10, 5, 20, 10), 'se', 4.2, 2.2, { step: 0.5 })).toMatchObject({ x: 10, y: 5, width: 24, height: 12 });
    expect(resizeFrom(doc(), box(10, 5, 20, 10), 'nw', -4, -2, { step: 0.5 })).toMatchObject({ x: 6, y: 3, width: 24, height: 12 });
    // A side handle moves one side only.
    expect(resizeFrom(doc(), box(10, 5, 20, 10), 'e', 5, 9, { step: 0.5 })).toMatchObject({ x: 10, y: 5, width: 25, height: 10 });
  });

  it('keeps proportions with Shift, and works from the center with Ctrl', () => {
    expect(resizeFrom(doc(), box(10, 5, 20, 10), 'se', 10, 1, { keepRatio: true })).toMatchObject({ x: 10, y: 5, width: 30, height: 15 });
    // From a side, the other side follows, about the middle.
    expect(resizeFrom(doc(), box(10, 5, 20, 10), 'e', 20, 0, { keepRatio: true })).toMatchObject({ x: 10, y: 0, width: 40, height: 20 });
    expect(resizeFrom(doc(), box(10, 5, 20, 10), 'e', 5, 0, { fromCenter: true })).toMatchObject({ x: 5, y: 5, width: 30, height: 10 });
    expect(resizeFrom(doc(), box(10, 5, 20, 10), 'se', 5, 5, { fromCenter: true, keepRatio: true })).toMatchObject({ x: 0, y: 0, width: 40, height: 20 });
  });

  it('never goes below the least size, and keeps a level line level', () => {
    expect(resizeFrom(doc(), box(10, 5, 20, 10), 'e', -50, 0, { step: 0.5 }).width).toBe(0.5);
    const line = box(10, 5, 20, 0, { type: 'shape', shape: 'line' });
    expect(resizeFrom(doc(), line, 'e', 5, 0, { keepRatio: true })).toMatchObject({ width: 25, height: 0 });
  });

  it('resizes a turned element along its own sides, the opposite side staying put', () => {
    // Turned a quarter turn, its "east" handle points down the label.
    const e = box(10, 5, 20, 10, { rotation: 90 });
    // Its west side, at the top as it lies, stays at (20, 0): the new center is 12 below it.
    expect(resizeFrom(doc(), e, 'e', 0, 4, { step: 0.5 })).toMatchObject({ x: 8, y: 7, width: 24, height: 10, rotation: 90 });
  });

  it('scales the box round a selection the same way', () => {
    const b = { x: 10, y: 10, width: 20, height: 10 };
    expect(scaledBox(b, 'se', 10, 0, 'mm', { keepRatio: true })).toEqual({ x: 10, y: 10, width: 30, height: 15 });
    expect(scaledBox(b, 'w', -5, 0, 'mm')).toEqual({ x: 5, y: 10, width: 25, height: 10 });
    expect(scaledBox(b, 'e', 5, 0, 'mm', { fromCenter: true })).toEqual({ x: 5, y: 10, width: 30, height: 10 });
  });
});

describe('moving and turning', () => {
  it('moves along one axis with Shift', () => {
    expect(alongOneAxis(5, 2)).toEqual([5, 0]);
    expect(alongOneAxis(-1, 3)).toEqual([0, 3]);
  });

  it('turns in 15° steps with Ctrl, and snaps near 45° unless switched off', () => {
    expect(turned(37, { steps: true })).toBe(30);
    expect(turned(-8, { steps: true })).toBe(345);
    expect(turned(87, { snap45: true })).toBe(90);
    expect(turned(87, { snap45: false })).toBe(87);
    expect(turned(60, { snap45: true })).toBe(60);
    expect(turned(359, { snap45: true })).toBe(0);
  });
});

describe('numbers at an anchor', () => {
  it('places and sizes about the anchor, with the aspect locked or not', () => {
    const e = box(10, 5, 20, 10);
    expect(anchorAt(e, 'c')).toEqual([20, 10]);
    expect(setAtAnchor(doc(), e, 'c', { width: 30 })).toMatchObject({ x: 5, width: 30, y: 5, height: 10 });
    expect(setAtAnchor(doc(), e, 'se', { width: 30 }, true)).toMatchObject({ x: 0, y: 0, width: 30, height: 15 });
    expect(setAtAnchor(doc(), e, 'ne', { x: 50, y: 2 })).toMatchObject({ x: 30, y: 2 });
  });

  it('reads lengths typed with their units', () => {
    expect(parseLength('5mm', 'mm')).toBe(5);
    expect(parseLength('1.5 cm', 'mm')).toBe(15);
    expect(parseLength('0.5in', 'mm')).toBeCloseTo(12.7);
    expect(parseLength('25.4mm', 'in')).toBeCloseTo(1);
    expect(parseLength('72pt', 'in')).toBeCloseTo(1);
    expect(parseLength(' 7 ', 'mm')).toBe(7);
    expect(parseLength('wide', 'mm')).toBeNull();
  });
});

describe('selecting', () => {
  const d = doc([box(0, 0, 10, 10), box(20, 0, 10, 10), box(5, 5, 10, 10, { locked: true })]);

  it('takes what a box covers, left to right, or touches, right to left; never what is locked', () => {
    const b = { x: -1, y: -1, width: 25, height: 12 };
    expect(inBox(d, b, true)).toEqual([0]);
    expect(inBox(d, b, false)).toEqual([0, 1]);
  });

  it('takes the element beneath the selected one, round to the top', () => {
    const stacked = doc([box(0, 0, 10, 10), box(2, 2, 10, 10), box(4, 4, 10, 10)]);
    expect(beneath(stacked, 5, 5, [])).toBe(2);
    expect(beneath(stacked, 5, 5, [2])).toBe(1);
    expect(beneath(stacked, 5, 5, [1])).toBe(0);
    expect(beneath(stacked, 5, 5, [0])).toBe(2);
    expect(beneath(stacked, 50, 5, [0])).toBeNull();
  });
});
