import { describe, expect, it } from 'vitest';
import { History } from './history';
import {
  blankTemplate,
  convertUnits,
  elementIndex,
  isTemplateID,
  moveElements,
  newElement,
  placeholders,
  resizeElement,
  snap,
  type TemplateDoc,
} from './template';

const doc = (): TemplateDoc => ({
  ...blankTemplate(),
  width: 90,
  height: 29,
  elements: [
    { type: 'text', x: 10, y: 5, width: 20, height: 10 },
    { type: 'text', x: 60, y: 15, width: 20, height: 10 },
  ],
});

describe('snapping and units', () => {
  it('snaps without floating-point noise', () => {
    expect(snap(10.26, 0.5, 'mm')).toBe(10.5);
    expect(snap(0.3, 0.02, 'in')).toBe(0.3);
    expect(snap(0.031, 0.02, 'in')).toBe(0.04);
  });

  it('converts every length but not font sizes', () => {
    const d = { ...doc(), elements: [{ type: 'text' as const, x: 25.4, y: 12.7, width: 50.8, height: 6.35, font_size: 12 }] };
    const inches = convertUnits(d, 'in');
    expect(inches).toMatchObject({ units: 'in', width: 3.543, height: 1.142 });
    expect(inches.elements[0]).toMatchObject({ x: 1, y: 0.5, width: 2, height: 0.25, font_size: 12 });
    expect(convertUnits(inches, 'mm').elements[0]).toMatchObject({ x: 25.4, y: 12.7, width: 50.8 });
  });
});

describe('moving', () => {
  it('moves the selection by whole grid steps', () => {
    const d = doc();
    const moved = moveElements(d, [0], d.elements, 3.2, 1.1, 0.5);
    expect(moved[0]).toMatchObject({ x: 13, y: 6 });
    expect(moved[1]).toBe(d.elements[1]);
  });

  it('keeps the whole group inside the canvas', () => {
    const d = doc();
    const moved = moveElements(d, [0, 1], d.elements, 50, 50, 0.5);
    // The right element hits the right and bottom edges; the other keeps its offset.
    expect(moved[1]).toMatchObject({ x: 70, y: 19 });
    expect(moved[0]).toMatchObject({ x: 20, y: 9 });
    expect(moveElements(d, [0], d.elements, -100, -100, 0.5)[0]).toMatchObject({ x: 0, y: 0 });
  });
});

describe('resizing', () => {
  it('moves only the dragged edges', () => {
    const d = doc();
    const e = d.elements[0]!;
    expect(resizeElement(d, e, 'se', 5, 2, 0.5)).toMatchObject({ x: 10, y: 5, width: 25, height: 12 });
    expect(resizeElement(d, e, 'nw', -4, -2, 0.5)).toMatchObject({ x: 6, y: 3, width: 24, height: 12 });
    expect(resizeElement(d, e, 'e', 0, 99, 0.5)).toMatchObject({ width: 20, height: 10 });
  });

  it('stops at the canvas edge and at one grid step', () => {
    const d = doc();
    const e = d.elements[0]!;
    expect(resizeElement(d, e, 'e', 500, 0, 0.5)).toMatchObject({ x: 10, width: 80 });
    expect(resizeElement(d, e, 'n', 0, -50, 0.5)).toMatchObject({ y: 0, height: 15 });
    expect(resizeElement(d, e, 'w', 50, 0, 0.5)).toMatchObject({ x: 29.5, width: 0.5 });
  });
});

describe('other helpers', () => {
  it('reads element indices from problem paths', () => {
    expect(elementIndex('elements[3].font_size')).toBe(3);
    expect(elementIndex('elements[12]')).toBe(12);
    expect(elementIndex('width')).toBeNull();
  });

  it('creates elements that fit the canvas', () => {
    const d = doc();
    for (const type of ['text', 'generator', 'static_image'] as const) {
      const e = newElement(type, d);
      expect(e.x + e.width).toBeLessThanOrEqual(d.width);
      expect(e.y + e.height).toBeLessThanOrEqual(d.height);
    }
    expect(newElement('generator', d, 'code128').width).toBeGreaterThan(newElement('generator', d).width);
  });

  it('lists placeholders and checks IDs', () => {
    expect(placeholders(['serial'], ['qr'])).toContain('{{field.serial}}');
    expect(placeholders([], ['micro_qr'])).toContain('{{asset.code.micro_qr}}');
    expect(isTemplateID('ql-29x90')).toBe(true);
    expect(isTemplateID('QL 29')).toBe(false);
    expect(isTemplateID('')).toBe(false);
  });
});

describe('History', () => {
  it('undoes and redoes, and a new change drops the redo trail', () => {
    const h = new History<number>();
    h.record(1);
    h.record(2);
    expect(h.undo(3)).toBe(2);
    expect(h.undo(2)).toBe(1);
    expect(h.undo(1)).toBeNull();
    expect(h.redo(1)).toBe(2);
    h.record(2);
    expect(h.canRedo).toBe(false);
  });

  it('skips repeated snapshots and keeps a limit', () => {
    const h = new History<number>(2);
    h.record(1);
    h.record(1);
    h.record(2);
    h.record(3);
    expect(h.undo(4)).toBe(3);
    expect(h.undo(3)).toBe(2);
    expect(h.undo(2)).toBeNull();
  });
});
