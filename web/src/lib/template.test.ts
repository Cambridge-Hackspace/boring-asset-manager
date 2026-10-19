import { describe, expect, it } from 'vitest';
import { History } from './history';
import {
  blankTemplate,
  bounds,
  convertUnits,
  elementIndex,
  elementsAt,
  inListOrder,
  layerName,
  marginOf,
  hairline,
  newShape,
  polygonPoints,
  polygonThrough,
  makeAdaptive,
  makeFixed,
  moveElements,
  moveLayer,
  newElement,
  opensAPage,
  placeholders,
  resizeElement,
  resizeLabel,
  restack,
  rotateElements,
  snapAngle,
  snap,
  tooSmall,
  withProp,
  type TemplateDoc,
  withMargin,
  type TemplateElement,
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

describe('layers', () => {
  const els = (...names: string[]): TemplateElement[] => names.map((n) => ({ type: 'text', x: 0, y: 0, width: 1, height: 1, content_binding: n }));
  const names = (r: { elements: TemplateElement[] }) => r.elements.map((e) => e.content_binding).join('');

  it('lists a template from before layering as it was drawn: images, then text, then codes', () => {
    const old: TemplateDoc = {
      ...blankTemplate(),
      layering: undefined,
      elements: [
        { type: 'generator', generator_type: 'qr', x: 0, y: 0, width: 5, height: 5 },
        { type: 'text', x: 0, y: 0, width: 5, height: 5, content_binding: 'a' },
        { type: 'static_image', x: 0, y: 0, width: 5, height: 5, image_id: 'i' },
        { type: 'text', x: 0, y: 0, width: 5, height: 5, content_binding: 'b' },
      ],
    };
    const listed = inListOrder(old);
    expect(listed.layering).toBe('list');
    expect(listed.elements.map((e) => e.content_binding ?? e.type)).toEqual(['static_image', 'a', 'b', 'generator']);
    expect(inListOrder(listed)).toBe(listed);
  });

  it('brings forward and sends backward one step, past what is not selected', () => {
    expect(restack(els('a', 'b', 'c', 'd'), [0, 2], 'forward')).toEqual({ elements: els('b', 'a', 'd', 'c'), selected: [1, 3] });
    expect(names(restack(els('a', 'b', 'c', 'd'), [2, 3], 'forward'))).toBe('abcd');
    expect(restack(els('a', 'b', 'c', 'd'), [1, 3], 'backward')).toEqual({ elements: els('b', 'a', 'd', 'c'), selected: [0, 2] });
  });

  it('brings to the front and sends to the back, keeping the selection in its order', () => {
    expect(restack(els('a', 'b', 'c', 'd'), [0, 2], 'front')).toEqual({ elements: els('b', 'd', 'a', 'c'), selected: [2, 3] });
    expect(restack(els('a', 'b', 'c', 'd'), [3, 1], 'back')).toEqual({ elements: els('b', 'd', 'a', 'c'), selected: [0, 1] });
  });

  it('moves one layer, and the selection follows', () => {
    expect(moveLayer(els('a', 'b', 'c'), [0], 0, 2)).toEqual({ elements: els('b', 'c', 'a'), selected: [2] });
    expect(moveLayer(els('a', 'b', 'c'), [1], 2, 0)).toEqual({ elements: els('c', 'a', 'b'), selected: [2] });
  });

  it('finds the elements under a point, the top one first', () => {
    const d = doc();
    d.elements.push({ type: 'text', x: 15, y: 8, width: 50, height: 10 });
    expect(elementsAt(d, 18, 10)).toEqual([2, 0]);
    expect(elementsAt(d, 85, 1)).toEqual([]);
  });

  it('names layers by what they show', () => {
    expect(layerName({ type: 'text', x: 0, y: 0, width: 1, height: 1, content_binding: '{{asset.name}}' })).toBe('Text: {{asset.name}}');
    expect(layerName({ type: 'generator', generator_type: 'qr', x: 0, y: 0, width: 1, height: 1 })).toBe('QR code');
    expect(layerName({ type: 'generator', generator_type: 'code128', content_binding: '{{asset.id}}', x: 0, y: 0, width: 1, height: 1 })).toBe('Code 128: {{asset.id}}');
    expect(layerName({ type: 'static_image', x: 0, y: 0, width: 1, height: 1, image_id: 'x' })).toBe('Image');
  });
});

describe('turning', () => {
  const box = (rotation?: number): TemplateElement => ({ type: 'text', x: 10, y: 10, width: 20, height: 10, ...(rotation ? { rotation } : {}) });

  it('knows where a turned element lies', () => {
    expect(bounds(box())).toEqual({ x: 10, y: 10, width: 20, height: 10 });
    expect(bounds(box(90))).toEqual({ x: 15, y: 5, width: 10, height: 20 });
    const b = bounds(box(45));
    expect(b.width).toBeCloseTo(30 / Math.SQRT2, 9);
    expect(b.x + b.width / 2).toBeCloseTo(20, 9);
  });

  it('snaps to multiples of 45° within 5°, and keeps angles from 0 up to 360', () => {
    expect(snapAngle(43)).toBe(45);
    expect(snapAngle(39)).toBe(39);
    expect(snapAngle(-2)).toBe(0);
    expect(snapAngle(358)).toBe(0);
    expect(snapAngle(272.5)).toBe(270);
  });

  it('turns one element about its center, and several about the middle of them all', () => {
    const d = doc();
    const one = rotateElements(d, [0], d.elements, 30);
    expect(one[0]).toMatchObject({ x: 10, y: 5, rotation: 30 });
    expect(rotateElements(d, [0], [{ ...d.elements[0]!, rotation: 300 }], 60)[0]!.rotation).toBeUndefined();
    expect(rotateElements(d, [0], d.elements, 43, true)[0]!.rotation).toBe(45);

    // Two boxes, 10-30 and 60-80 across, 5-15 and 15-25 down, turned a quarter
    // turn about (45, 15): their centers move round it.
    const two = rotateElements(d, [0, 1], d.elements, 90);
    expect(two.map((e) => [e.x + e.width / 2, e.y + e.height / 2, e.rotation])).toEqual([
      [50, -10, 90],
      [40, 40, 90],
    ]);
  });

  it('resizes a turned element along its own sides, keeping the opposite side where it is', () => {
    const d = doc();
    // Turned upright, its east side faces down: dragging down 4 widens it by 4.
    const e = { ...d.elements[0]!, rotation: 90 };
    const r = resizeElement(d, e, 'e', 0, 4, 0.5);
    expect(r).toMatchObject({ width: 24, height: 10, rotation: 90 });
    // Its west side, the top once turned, stays at y = 0.
    expect(bounds(r).y).toBeCloseTo(bounds(e).y, 9);
  });

  it('finds a turned element under a point only where it lies turned', () => {
    const d = doc();
    d.elements[0] = { ...d.elements[0]!, rotation: 90 };
    expect(elementsAt(d, 20, 1)).toEqual([0]); // above its box, inside it turned
    expect(elementsAt(d, 11, 10)).toEqual([]); // inside its box, outside it turned
  });
});

describe('code margins', () => {
  const qr: TemplateElement = { type: 'generator', generator_type: 'qr', x: 0, y: 0, width: 20, height: 20 };

  it('sets sides, and puts them back to standard', () => {
    const one = withMargin(qr, ['left'], 0);
    expect(one.margin).toEqual({ left: 0 });
    expect(marginOf(one, 'left')).toBe(0);
    expect(marginOf(one, 'top')).toBeUndefined();
    expect(withMargin(one, ['left'], undefined)).toEqual(qr);
  });

  it('turns a quiet zone from before margins into the margin on every side it gave', () => {
    const old = { ...qr, quiet_zone: 2 };
    expect(marginOf(old, 'top')).toBe(2);
    expect(withMargin(old, ['left'], 0)).toEqual({ ...qr, margin: { top: 2, right: 2, bottom: 2, left: 0 } });
    const bars = { ...qr, generator_type: 'code128', quiet_zone: 5 };
    expect(marginOf(bars, 'top')).toBeUndefined();
    expect(withMargin(bars, ['top'], 1).margin).toEqual({ top: 1, right: 5, left: 5 });
  });

  it('converts and scales margins in length, but not in modules', () => {
    const inMM = { ...withMargin(qr, ['left'], 2.54), margin_unit: 'length' as const };
    const d = { ...blankTemplate(), elements: [inMM, withMargin(qr, ['left'], 3)] };
    const inches = convertUnits(d, 'in');
    expect(inches.elements[0]!.margin).toEqual({ left: 0.1 });
    expect(inches.elements[1]!.margin).toEqual({ left: 3 });
  });
});

describe('shapes', () => {
  it('makes shapes outlined in black, in the middle of the label by default', () => {
    const d = { ...blankTemplate(), width: 60, height: 30 };
    expect(newShape('rectangle', d)).toMatchObject({ type: 'shape', shape: 'rectangle', x: 22.5, y: 7.5, width: 15, height: 15, stroke: 'black', stroke_width: 0.25, fill: 'none' });
    expect(newShape('line', d)).toMatchObject({ height: 0, y: 15 });
    expect(newShape('polygon', d, { x: 1, y: 1, width: 4, height: 4 }).points).toEqual([[0.5, 0], [1, 1], [0, 1]]);
    expect(layerName(newShape('ellipse', d))).toBe('Ellipse');
  });

  it('knows a stroke thinner than one dot', () => {
    const d = { ...blankTemplate(), dpi: 300 };
    expect(hairline({ ...newShape('line', d), stroke_width: 0.05 }, d)).toBe(true);
    expect(hairline({ ...newShape('line', d), stroke_width: 0.1 }, d)).toBe(false);
    expect(hairline({ ...newShape('line', d), stroke: 'none' }, d)).toBe(false);
  });

  it('fits the box of a polygon round its points, leaving them where they lie, turned', () => {
    const d = { ...blankTemplate(), width: 100, height: 100 };
    const tri: TemplateElement = { ...newShape('polygon', d, { x: 10, y: 10, width: 20, height: 10 }), rotation: 90 };
    const before = polygonPoints(tri);
    // Point 2 (the bottom left) moves 10 further left: the box widens.
    const moved = polygonThrough(d, tri, [before[0]!, before[1]!, [before[2]![0] - 10, before[2]![1]]]);
    expect(moved).toMatchObject({ width: 30, height: 10, rotation: 90 });
    expect(moved.points).toEqual([[0.6667, 0], [1, 1], [0, 1]]);
    // The points that did not move are where they were on the label, turned.
    const onLabel = (e: TemplateElement, k: number) => {
      const [u, v] = polygonPoints(e)[k]!;
      const cx = e.x + e.width / 2;
      const cy = e.y + e.height / 2;
      // A quarter turn clockwise: (u, v) from the center goes to (-v, u).
      return [cx - (v - e.height / 2), cy + (u - e.width / 2)];
    };
    for (const k of [0, 1]) {
      expect(onLabel(moved, k)[0]).toBeCloseTo(onLabel(tri, k)[0]!, 2);
      expect(onLabel(moved, k)[1]).toBeCloseTo(onLabel(tri, k)[1]!, 2);
    }
  });
});
