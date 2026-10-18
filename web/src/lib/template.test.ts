import { describe, expect, it } from 'vitest';
import { History } from './history';
import {
  blankTemplate,
  convertUnits,
  elementIndex,
  makeAdaptive,
  makeFixed,
  moveElements,
  newElement,
  opensAPage,
  placeholders,
  resizeElement,
  resizeLabel,
  snap,
  tooSmall,
  withProp,
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

  it('converts an adaptive template range too', () => {
    const d = { ...doc(), sizing: 'adaptive' as const, adapt: { min_width: 25.4, max_width: 101.6, min_height: 12.7, max_height: 50.8, continuous_length: 'fixed' as const } };
    expect(convertUnits(d, 'in').adapt).toEqual({ min_width: 1, max_width: 4, min_height: 0.5, max_height: 2, continuous_length: 'fixed' });
    expect(convertUnits(doc(), 'in').adapt).toBeUndefined();
    const limited = { ...doc(), elements: [{ type: 'generator' as const, x: 0, y: 0, width: 25.4, height: 25.4, min_size: 12.7, max_size: 50.8, min_module_mm: 0.3 }] };
    expect(convertUnits(limited, 'in').elements[0]).toMatchObject({ min_size: 0.5, max_size: 2, min_module_mm: 0.3 });
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
    // On the grid, so nudging moves by exact steps.
    for (const v of Object.values(newElement('text', d)).filter((v) => typeof v === 'number')) expect((v * 2) % 1).toBe(0);
  });

  it('lists placeholders', () => {
    expect(placeholders(['serial'], ['qr'])).toContain('{{field.serial}}');
    expect(placeholders([], ['micro_qr'])).toContain('{{asset.code.micro_qr}}');
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

describe('adaptive templates', () => {
  it('makes a template adaptive, taking half to twice its size, and fixed again', () => {
    const d = { ...doc(), elements: [{ type: 'text' as const, x: 1, y: 1, width: 10, height: 5, anchor_x: 'both' as const, min_size: 3, when_too_small: 'hide' as const }] };
    const adaptive = makeAdaptive(d);
    expect(adaptive.sizing).toBe('adaptive');
    expect(adaptive.adapt).toEqual({ min_width: d.width / 2, max_width: d.width * 2, min_height: d.height / 2, max_height: d.height * 2, continuous_length: 'proportional' });
    const fixed = makeFixed(adaptive);
    expect(fixed.sizing).toBeUndefined();
    expect(fixed.adapt).toBeUndefined();
    expect(fixed.elements[0]).toEqual({ type: 'text', x: 1, y: 1, width: 10, height: 5 });
  });

  it('sets and removes element properties, and knows what gives way by default', () => {
    const e = { type: 'generator' as const, x: 0, y: 0, width: 5, height: 5 };
    expect(withProp(e, 'min_size', 3)).toMatchObject({ min_size: 3 });
    expect('min_size' in withProp({ ...e, min_size: 3 }, 'min_size', undefined)).toBe(false);
    expect(tooSmall(e)).toBe('refuse');
    expect(tooSmall({ ...e, type: 'static_image' })).toBe('hide');
    expect(tooSmall({ ...e, when_too_small: 'alternative' })).toBe('alternative');
  });
});

describe('resizeLabel', () => {
  it('scales elements with the label, keeping codes square and text in proportion', () => {
    const d: TemplateDoc = {
      ...blankTemplate(), units: 'mm', width: 90, height: 29,
      elements: [
        { type: 'generator', generator_type: 'qr', x: 2, y: 2, width: 25, height: 25 },
        { type: 'text', x: 30, y: 4, width: 58, height: 10, font_size: 14, min_font_size: 6 },
      ],
    };
    const r = resizeLabel(d, 62, 29);
    expect([r.width, r.height]).toEqual([62, 29]);
    // Across by 62/90; down unchanged.
    expect(r.elements[0]).toMatchObject({ x: 1.38, y: 2, width: 17.22, height: 17.22 });
    expect(r.elements[1]).toMatchObject({ x: 20.67, y: 4, width: 39.96, height: 10, font_size: 9.5, min_font_size: 4 });
    // Every element stays on the label.
    for (const e of r.elements) expect(e.x + e.width).toBeLessThanOrEqual(62);
  });
});

describe('codes phones open', () => {
  it('fills in the scan link for a new QR code, not for other codes', () => {
    expect(newElement('generator', doc(), 'qr').content_binding).toBe('{{scan_url}}');
    expect(newElement('generator', doc(), 'code128').content_binding).toBeUndefined();
  });
  it('knows a QR code that is not a link', () => {
    const qr = (content_binding?: string) => ({ type: 'generator' as const, generator_type: 'qr', x: 0, y: 0, width: 10, height: 10, content_binding });
    expect(opensAPage(qr('{{scan_url}}'))).toBe(true);
    expect(opensAPage(qr('https://wiki.example.org/{{asset.id}}'))).toBe(true);
    expect(opensAPage(qr())).toBe(false);
    expect(opensAPage(qr('{{asset.code.qr}}'))).toBe(false);
    expect(opensAPage({ ...qr(), generator_type: 'code128' })).toBe(true);
  });
});
