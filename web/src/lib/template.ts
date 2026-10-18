// Label templates as the editor works on them (README §4.1), and the pure
// geometry the editor applies to them.

export type Units = 'mm' | 'in';
export type ElementType = 'text' | 'generator' | 'static_image';
/** How an element of an adaptive template follows the label; absent is scale. */
export type Anchor = 'scale' | 'start' | 'end' | 'both' | 'center';

/** The label sizes an adaptive template takes, in its units. */
export interface Adapt {
  min_width: number;
  max_width: number;
  min_height: number;
  max_height: number;
  continuous_length: 'proportional' | 'fixed';
}

export interface TemplateElement {
  type: ElementType;
  x: number;
  y: number;
  width: number;
  height: number;
  anchor_x?: Anchor;
  anchor_y?: Anchor;
  content_binding?: string;
  // generator
  generator_type?: string;
  error_correction?: string;
  quiet_zone?: number;
  // text
  font_size?: number;
  font_weight?: 'normal' | 'bold';
  font_family?: 'sans' | 'mono';
  align?: 'left' | 'center' | 'right';
  wrap?: boolean;
  min_font_size?: number;
  // static_image
  image_id?: string;
}

export interface TemplateDoc {
  id: string;
  name: string;
  units: Units;
  width: number;
  height: number;
  dpi: number;
  /** Absent for a fixed template. */
  sizing?: 'adaptive';
  adapt?: Adapt;
  elements: TemplateElement[];
}

export const MM_PER_INCH = 25.4;

/** Rounds away floating-point noise: 0.01 mm or 0.001 in. */
export function tidy(value: number, units: Units): number {
  const places = units === 'mm' ? 100 : 1000;
  return Math.round(value * places) / places;
}

/** The grid step for dragging and arrow keys. */
export function gridStep(units: Units): number {
  return units === 'mm' ? 0.5 : 0.02;
}

export function snap(value: number, step: number, units: Units): number {
  return tidy(Math.round(value / step) * step, units);
}

/** Converts a length in the template's units to millimeters. */
export function toMM(value: number, units: Units): number {
  return units === 'mm' ? value : value * MM_PER_INCH;
}

/** Converts millimeters to the template's units. */
export function fromMM(mm: number, units: Units): number {
  return tidy(units === 'mm' ? mm : mm / MM_PER_INCH, units);
}

/** Re-expresses every length of a template in other units. Font sizes are in points and stay. */
export function convertUnits(doc: TemplateDoc, to: Units): TemplateDoc {
  if (doc.units === to) return doc;
  const conv = (v: number) => fromMM(toMM(v, doc.units), to);
  return {
    ...doc,
    units: to,
    width: conv(doc.width),
    height: conv(doc.height),
    ...(doc.adapt
      ? { adapt: { ...doc.adapt, min_width: conv(doc.adapt.min_width), max_width: conv(doc.adapt.max_width), min_height: conv(doc.adapt.min_height), max_height: conv(doc.adapt.max_height) } }
      : {}),
    elements: doc.elements.map((e) => ({ ...e, x: conv(e.x), y: conv(e.y), width: conv(e.width), height: conv(e.height) })),
  };
}

/** A new element of a type, sized to suit the canvas, near its top-left, on the grid. */
export function newElement(type: ElementType, doc: TemplateDoc, generatorType = 'qr'): TemplateElement {
  const step = gridStep(doc.units);
  const side = Math.max(step, snap(Math.min(doc.width, doc.height) * 0.6, step, doc.units));
  const at = snap(Math.min(doc.width, doc.height) * 0.1, step, doc.units);
  switch (type) {
    case 'generator':
      return {
        type,
        x: at,
        y: at,
        width: generatorType === 'code128' ? snap(doc.width * 0.6, step, doc.units) : side,
        height: side,
        generator_type: generatorType,
      };
    case 'static_image':
      return { type, x: at, y: at, width: side, height: side, image_id: '' };
    default:
      return {
        type: 'text',
        x: at,
        y: at,
        width: snap(doc.width * 0.5, step, doc.units),
        height: snap(doc.height * 0.25, step, doc.units),
        content_binding: '{{asset.name}}',
        font_size: 12,
        font_weight: 'normal',
        font_family: 'sans',
        align: 'left',
        wrap: false,
      };
  }
}

/** Keeps a value within [lo, hi]. */
function clamp(v: number, lo: number, hi: number): number {
  return Math.min(Math.max(v, lo), hi);
}

/**
 * Moves elements together by (dx, dy), snapped to the grid, without letting
 * any of them leave the canvas.
 */
export function moveElements(
  doc: TemplateDoc,
  indices: number[],
  start: TemplateElement[],
  dx: number,
  dy: number,
  step: number,
): TemplateElement[] {
  const moving = indices.map((i) => start[i]!);
  if (!moving.length) return start;
  const minX = Math.min(...moving.map((e) => e.x));
  const minY = Math.min(...moving.map((e) => e.y));
  const maxX = Math.max(...moving.map((e) => e.x + e.width));
  const maxY = Math.max(...moving.map((e) => e.y + e.height));
  // Snap the group's corner, then keep the whole group inside.
  const sx = clamp(snap(minX + dx, step, doc.units), 0, doc.width - (maxX - minX)) - minX;
  const sy = clamp(snap(minY + dy, step, doc.units), 0, doc.height - (maxY - minY)) - minY;
  return start.map((e, i) =>
    indices.includes(i) ? { ...e, x: tidy(e.x + sx, doc.units), y: tidy(e.y + sy, doc.units) } : e,
  );
}

export type Handle = 'n' | 'ne' | 'e' | 'se' | 's' | 'sw' | 'w' | 'nw';
export const handles: Handle[] = ['nw', 'n', 'ne', 'e', 'se', 's', 'sw', 'w'];

/**
 * Resizes an element by dragging one handle by (dx, dy), snapping the moved
 * edges to the grid and keeping the element inside the canvas and at least
 * one grid step in size.
 */
export function resizeElement(doc: TemplateDoc, e: TemplateElement, handle: Handle, dx: number, dy: number, step: number): TemplateElement {
  let left = e.x;
  let top = e.y;
  let right = e.x + e.width;
  let bottom = e.y + e.height;
  if (handle.includes('w')) left = clamp(snap(left + dx, step, doc.units), 0, right - step);
  if (handle.includes('e')) right = clamp(snap(right + dx, step, doc.units), left + step, doc.width);
  if (handle.includes('n')) top = clamp(snap(top + dy, step, doc.units), 0, bottom - step);
  if (handle.includes('s')) bottom = clamp(snap(bottom + dy, step, doc.units), top + step, doc.height);
  return { ...e, x: tidy(left, doc.units), y: tidy(top, doc.units), width: tidy(right - left, doc.units), height: tidy(bottom - top, doc.units) };
}

/** The element index a server problem path such as "elements[2].width" refers to. */
export function elementIndex(path: string): number | null {
  const m = /^elements\[(\d+)\]/.exec(path);
  return m ? Number(m[1]) : null;
}

/** A short description of an element for lists and screen readers. */
export function describeElement(e: TemplateElement, i: number): string {
  switch (e.type) {
    case 'generator':
      return `Barcode ${i + 1} (${e.generator_type})`;
    case 'static_image':
      return `Image ${i + 1}`;
    default:
      return `Text ${i + 1}: ${e.content_binding || 'empty'}`;
  }
}

/** The placeholders a content binding may use. */
export function placeholders(fieldKeys: string[], generatorTypes: string[]): string[] {
  return [
    '{{asset.name}}',
    '{{asset.id}}',
    '{{base_url}}',
    '{{base_url}}/a/{{asset.id}}',
    '{{scan_url}}',
    '{{asset.categories}}',
    '{{asset.location}}',
    '{{asset.condition}}',
    '{{stock.quantity}}',
    '{{stock.unit}}',
    '{{loan.borrower}}',
    '{{loan.due}}',
    ...generatorTypes.map((t) => `{{asset.code.${t}}}`),
    ...fieldKeys.map((k) => `{{field.${k}}}`),
  ];
}

/** A starting template. */
export function blankTemplate(): TemplateDoc {
  return { id: '', name: '', units: 'mm', width: 62, height: 29, dpi: 300, elements: [] };
}
