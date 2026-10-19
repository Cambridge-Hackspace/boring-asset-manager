// Label templates as the editor works on them (README §4.1), and the pure
// geometry the editor applies to them.

export type Units = 'mm' | 'in';
export type ElementType = 'text' | 'generator' | 'static_image' | 'shape';
export type ShapeKind = 'rectangle' | 'ellipse' | 'line' | 'polygon' | 'path';
/** Labels print black and white; white paints over what is beneath it. */
export type Paint = 'black' | 'white' | 'none';
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
  /** Degrees it is turned clockwise about the center of its box, from 0 up to 360; absent is 0. */
  rotation?: number;
  anchor_x?: Anchor;
  anchor_y?: Anchor;
  /** Bounds on each side of its box as an adaptive template is laid out, in the template's units. */
  min_size?: number;
  max_size?: number;
  /** What it does where it does not fit; absent: images are left off, the rest refuse. */
  when_too_small?: 'refuse' | 'hide' | 'alternative';
  /** Laid out in its box in its place, when when_too_small is alternative. */
  alternative?: TemplateElement;
  content_binding?: string;
  // generator
  generator_type?: string;
  error_correction?: string;
  /** The same margin on every side, in modules, from before margins; margin replaces it. */
  quiet_zone?: number;
  /** The smallest module drawn, in millimeters. */
  min_module_mm?: number;
  /** The space kept clear around a code; a side left out takes its standard quiet zone. */
  margin?: Margin;
  /** What margin is counted in: modules (the default) or the template's units. */
  margin_unit?: 'modules' | 'length';
  /** Where a code sits down its box; absent is the middle. */
  vertical_align?: 'top' | 'middle' | 'bottom';
  // text
  font_size?: number;
  font_weight?: 'normal' | 'bold';
  font_family?: 'sans' | 'mono';
  /** How text lines up in its box, or where a code sits across it (absent for a code: the center). */
  align?: 'left' | 'center' | 'right';
  wrap?: boolean;
  min_font_size?: number;
  // static_image
  image_id?: string;
  /** An image flipped across, down or both. */
  mirror?: 'across' | 'down' | 'both';
  /** The group it is in, moved, resized and turned as one in the editor. */
  group?: string;
  /** Kept from being selected on the canvas; still selectable from the layers. */
  locked?: boolean;
  // shape, and fill for text too (black, or white reversed out of black)
  shape?: ShapeKind;
  /** A rectangle's corners, in the template's units. */
  corner_radius?: number;
  /** A line from the box's bottom left to its top right, not top left to bottom right. */
  flip?: boolean;
  /** A polygon's points, as fractions of its box across and down. */
  points?: [number, number][];
  /** Whether a polygon is closed; absent is closed. */
  closed?: boolean;
  /** Imported artwork's SVG path data, its points fractions of its box. */
  d?: string;
  fill_rule?: 'nonzero' | 'evenodd';
  stroke?: Paint;
  /** In the template's units. */
  stroke_width?: number;
  fill?: Paint;
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
  /**
   * Elements are drawn in the order they are listed, the first at the back.
   * Absent in documents written before that, which drew images, then text,
   * then codes.
   */
  layering?: 'list';
  /** Which way labels that fit either way go through the printer; absent is as fits. */
  feed?: Feed;
  /** An adaptive template takes labels turned a quarter turn too, its content turned to match. */
  turnable?: boolean;
  /** Lines to line elements up against, across (at each x) and down (at each y); they never print. */
  guides?: Guides;
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
    elements: doc.elements.map((e) => ({
      ...e, x: conv(e.x), y: conv(e.y), width: conv(e.width), height: conv(e.height),
      ...(e.min_size ? { min_size: conv(e.min_size) } : {}),
      ...(e.max_size ? { max_size: conv(e.max_size) } : {}),
      ...(e.margin && e.margin_unit === 'length' ? { margin: mapMargin(e.margin, conv) } : {}),
      ...(e.stroke_width ? { stroke_width: Math.max(conv(e.stroke_width), to === 'in' ? 0.001 : 0.01) } : {}),
      ...(e.corner_radius ? { corner_radius: conv(e.corner_radius) } : {}),
    })),
  };
}

/** A margin with each side given changed by f. */
function mapMargin(m: Margin, f: (v: number) => number): Margin {
  return Object.fromEntries(Object.entries(m).map(([side, v]) => [side, f(v)]));
}

/** Where a printer prints on a template, in its units, from the fit check (README §4.4). */
export interface PrintArea {
  /** The part of the label it prints. */
  printable: { x: number; y: number; width: number; height: number };
  /** How near a die-cut label's edges content may be cut off; 0 on a continuous roll. */
  edge: number;
  /** Elements reaching outside what it prints, or near the edge. */
  outside: number[];
  nearEdge: number[];
}

/**
 * Gives a template a new size, scaling its elements with it so they stay on
 * the label: positions and boxes in proportion across and down, codes kept
 * square, and text by the smaller of the two.
 */
export function resizeLabel(doc: TemplateDoc, width: number, height: number): TemplateDoc {
  const sx = width / doc.width;
  const sy = height / doc.height;
  const s = Math.min(sx, sy);
  const t = (v: number) => tidy(v, doc.units);
  const pt = (v: number) => Math.max(1, Math.round(v * s * 2) / 2);
  return {
    ...doc,
    width,
    height,
    elements: doc.elements.map((e) => {
      const square = e.type === 'generator';
      const w = square ? e.width * s : e.width * sx;
      const h = square ? e.height * s : e.height * sy;
      return {
        ...e,
        x: t(e.x * sx),
        y: t(e.y * sy),
        width: t(w),
        height: t(h),
        ...(e.font_size ? { font_size: pt(e.font_size) } : {}),
        ...(e.min_font_size ? { min_font_size: pt(e.min_font_size) } : {}),
        ...(e.margin && e.margin_unit === 'length' ? { margin: mapMargin(e.margin, (v) => t(v * s)) } : {}),
        ...(e.corner_radius ? { corner_radius: t(e.corner_radius * s) } : {}),
      };
    }),
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
        // A QR code a phone scans opens the asset's scan page.
        ...(generatorType === 'qr' ? { content_binding: SCAN_URL } : {}),
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

/** The sine and cosine of an angle in degrees, exact at quarter turns. */
export function sinCos(degrees: number): [number, number] {
  const d = ((degrees % 360) + 360) % 360;
  if (d === 0) return [0, 1];
  if (d === 90) return [1, 0];
  if (d === 180) return [0, -1];
  if (d === 270) return [-1, 0];
  const r = (d * Math.PI) / 180;
  return [Math.sin(r), Math.cos(r)];
}

/** Where an element lies once turned: its own box when it is not. */
export function bounds(e: TemplateElement): { x: number; y: number; width: number; height: number } {
  if (!e.rotation) return { x: e.x, y: e.y, width: e.width, height: e.height };
  const [sin, cos] = sinCos(e.rotation);
  const width = Math.abs(e.width * cos) + Math.abs(e.height * sin);
  const height = Math.abs(e.width * sin) + Math.abs(e.height * cos);
  return { x: e.x + (e.width - width) / 2, y: e.y + (e.height - height) / 2, width, height };
}

/**
 * Moves elements together by (dx, dy), snapped to the grid, without letting
 * any of them leave the canvas, as they lie once turned. An axis marked
 * exact, snapped already to a guide or another element, is not put on the
 * grid.
 */
export function moveElements(
  doc: TemplateDoc,
  indices: number[],
  start: TemplateElement[],
  dx: number,
  dy: number,
  step: number,
  exact: { x?: boolean; y?: boolean } = {},
): TemplateElement[] {
  const moving = indices.map((i) => start[i]!);
  if (!moving.length) return start;
  const boxes = moving.map(bounds);
  const minX = Math.min(...boxes.map((b) => b.x));
  const minY = Math.min(...boxes.map((b) => b.y));
  const maxX = Math.max(...boxes.map((b) => b.x + b.width));
  const maxY = Math.max(...boxes.map((b) => b.y + b.height));
  // Snap the group's corner, then keep the whole group inside.
  const sx = clamp(exact.x ? tidy(minX + dx, doc.units) : snap(minX + dx, step, doc.units), 0, Math.max(0, doc.width - (maxX - minX))) - minX;
  const sy = clamp(exact.y ? tidy(minY + dy, doc.units) : snap(minY + dy, step, doc.units), 0, Math.max(0, doc.height - (maxY - minY))) - minY;
  return start.map((e, i) =>
    indices.includes(i) ? { ...e, x: tidy(e.x + sx, doc.units), y: tidy(e.y + sy, doc.units) } : e,
  );
}

/**
 * Scales elements together from the box round them, from to the box to:
 * each keeps its place in it, its box scaled across and down, and its text
 * and corners by the lesser of the two.
 */
export function scaleElements(doc: TemplateDoc, indices: number[], start: TemplateElement[], from: Box, to: Box): TemplateElement[] {
  const sx = to.width / from.width;
  const sy = to.height / from.height;
  const s = Math.min(sx, sy);
  const t = (v: number) => tidy(v, doc.units);
  return start.map((e, i) => {
    if (!indices.includes(i)) return e;
    // Its center keeps its place in the box; its box scales.
    const cx = to.x + (e.x + e.width / 2 - from.x) * sx;
    const cy = to.y + (e.y + e.height / 2 - from.y) * sy;
    const w = e.width * sx;
    const h = e.height * sy;
    return {
      ...e,
      x: t(cx - w / 2),
      y: t(cy - h / 2),
      width: t(w),
      height: t(h),
      ...(e.font_size ? { font_size: Math.max(1, Math.round(e.font_size * s * 2) / 2) } : {}),
      ...(e.min_font_size ? { min_font_size: Math.max(1, Math.round(e.min_font_size * s * 2) / 2) } : {}),
      ...(e.corner_radius ? { corner_radius: t(e.corner_radius * s) } : {}),
    };
  });
}

/** An angle as kept: from 0 up to 360, to a hundredth of a degree. */
export function normalAngle(degrees: number): number {
  const d = Math.round((((degrees % 360) + 360) % 360) * 100) / 100;
  return d === 360 ? 0 : d;
}

/** How near a multiple of 45° an angle snaps to it, in degrees. */
export const ANGLE_SNAP = 5;

/** An angle, snapped to a multiple of 45° when within ANGLE_SNAP of one. */
export function snapAngle(degrees: number): number {
  const nearest = Math.round(degrees / 45) * 45;
  return normalAngle(Math.abs(degrees - nearest) <= ANGLE_SNAP ? nearest : degrees);
}

/**
 * Turns elements clockwise from where they start: one about its own center
 * to an angle delta more, snapped to 45° when snapped is set, and several
 * together by delta about the center of all of them as they lie.
 */
export function rotateElements(doc: TemplateDoc, indices: number[], start: TemplateElement[], delta: number, snapped = false): TemplateElement[] {
  const moving = indices.map((i) => start[i]!);
  if (!moving.length) return start;
  const turned = (e: TemplateElement, angle: number) => withProp(e, 'rotation', angle ? angle : undefined);
  if (moving.length === 1) {
    return start.map((e, i) => {
      if (!indices.includes(i)) return e;
      const angle = normalAngle((e.rotation ?? 0) + delta);
      return turned(e, snapped ? snapAngle(angle) : angle);
    });
  }
  const boxes = moving.map(bounds);
  const cx = (Math.min(...boxes.map((b) => b.x)) + Math.max(...boxes.map((b) => b.x + b.width))) / 2;
  const cy = (Math.min(...boxes.map((b) => b.y)) + Math.max(...boxes.map((b) => b.y + b.height))) / 2;
  const [sin, cos] = sinCos(delta);
  return start.map((e, i) => {
    if (!indices.includes(i)) return e;
    const ex = e.x + e.width / 2 - cx;
    const ey = e.y + e.height / 2 - cy;
    const nx = cx + ex * cos - ey * sin;
    const ny = cy + ex * sin + ey * cos;
    return { ...turned(e, normalAngle((e.rotation ?? 0) + delta)), x: tidy(nx - e.width / 2, doc.units), y: tidy(ny - e.height / 2, doc.units) };
  });
}

export type Handle = 'n' | 'ne' | 'e' | 'se' | 's' | 'sw' | 'w' | 'nw';
export const handles: Handle[] = ['nw', 'n', 'ne', 'e', 'se', 's', 'sw', 'w'];

/**
 * Resizes an element by dragging one handle by (dx, dy), snapping the moved
 * edges to the grid and keeping the element inside the canvas and at least
 * one grid step in size.
 */
export function resizeElement(doc: TemplateDoc, e: TemplateElement, handle: Handle, dx: number, dy: number, step: number): TemplateElement {
  if (e.rotation) return resizeTurned(doc, e, handle, dx, dy, step);
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

/**
 * Resizes a turned element along its own sides: the drag is turned back
 * onto them, its size snapped to the grid, and the corner or side opposite
 * the handle stays where it is on the label.
 */
function resizeTurned(doc: TemplateDoc, e: TemplateElement, handle: Handle, dx: number, dy: number, step: number): TemplateElement {
  const [sin, cos] = sinCos(e.rotation ?? 0);
  // The drag along the element's own sides.
  const along = dx * cos + dy * sin;
  const down = -dx * sin + dy * cos;
  const sx = handle.includes('e') ? 1 : handle.includes('w') ? -1 : 0;
  const sy = handle.includes('s') ? 1 : handle.includes('n') ? -1 : 0;
  const width = sx ? Math.max(step, snap(e.width + sx * along, step, doc.units)) : e.width;
  const height = sy ? Math.max(step, snap(e.height + sy * down, step, doc.units)) : e.height;
  // The point that stays, the opposite corner or side, from the center, turned.
  const stays = (w: number, h: number): [number, number] => {
    const u = (-sx * w) / 2;
    const v = (-sy * h) / 2;
    return [u * cos - v * sin, u * sin + v * cos];
  };
  const [ox, oy] = stays(e.width, e.height);
  const [nx, ny] = stays(width, height);
  const cx = e.x + e.width / 2 + ox - nx;
  const cy = e.y + e.height / 2 + oy - ny;
  return { ...e, x: tidy(cx - width / 2, doc.units), y: tidy(cy - height / 2, doc.units), width: tidy(width, doc.units), height: tidy(height, doc.units) };
}

/** The placeholder for an asset's scan page address (README §6.4). */
export const SCAN_URL = '{{scan_url}}';

/**
 * Whether a phone scanning a code element opens a page: a QR code whose
 * content is a link. Other symbologies are read by scanners, not phones.
 */
export function opensAPage(e: TemplateElement): boolean {
  if (e.type !== 'generator' || e.generator_type !== 'qr') return true;
  const c = (e.content_binding ?? '').trim();
  return c.includes(SCAN_URL) || /^https?:\/\//i.test(c);
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
    case 'shape':
      return `Shape ${i + 1} (${e.shape})`;
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
  return { id: '', name: '', units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list', elements: [] };
}

const layerOfType: Record<ElementType, number> = { static_image: 0, text: 1, generator: 2, shape: 3 };

/**
 * A template in list order: one written before layering has its elements
 * listed as they were drawn then, images, then text, then codes, so it
 * looks the same.
 */
export function inListOrder(doc: TemplateDoc): TemplateDoc {
  if (doc.layering === 'list') return doc;
  const elements = doc.elements.map((e, i) => ({ e, i }));
  elements.sort((a, b) => layerOfType[a.e.type] - layerOfType[b.e.type] || a.i - b.i);
  return { ...doc, layering: 'list', elements: elements.map((x) => x.e) };
}

/** How the selected elements move in the drawing order. */
export type Restack = 'front' | 'forward' | 'backward' | 'back';

/**
 * Moves the selected elements in the drawing order, where later elements
 * are drawn over earlier ones: one step forward or backward past the next
 * element not selected, or all the way to the front or back, keeping their
 * own order. Returns the elements and where the selected ones are now.
 */
export function restack(elements: TemplateElement[], selected: number[], how: Restack): { elements: TemplateElement[]; selected: number[] } {
  const items = elements.map((e, i) => ({ e, picked: selected.includes(i) }));
  switch (how) {
    case 'front':
      items.sort((a, b) => Number(a.picked) - Number(b.picked));
      break;
    case 'back':
      items.sort((a, b) => Number(b.picked) - Number(a.picked));
      break;
    case 'forward':
      for (let k = items.length - 2; k >= 0; k--) {
        if (items[k]!.picked && !items[k + 1]!.picked) [items[k], items[k + 1]] = [items[k + 1]!, items[k]!];
      }
      break;
    case 'backward':
      for (let k = 1; k < items.length; k++) {
        if (items[k]!.picked && !items[k - 1]!.picked) [items[k], items[k - 1]] = [items[k - 1]!, items[k]!];
      }
      break;
  }
  return { elements: items.map((x) => x.e), selected: items.flatMap((x, k) => (x.picked ? [k] : [])) };
}

/** Moves one element to another place in the drawing order; the selection follows the elements. */
export function moveLayer(elements: TemplateElement[], selected: number[], from: number, to: number): { elements: TemplateElement[]; selected: number[] } {
  const items = elements.map((e, i) => ({ e, picked: selected.includes(i) }));
  const [moved] = items.splice(from, 1);
  items.splice(Math.min(Math.max(to, 0), items.length), 0, moved!);
  return { elements: items.map((x) => x.e), selected: items.flatMap((x, k) => (x.picked ? [k] : [])) };
}

/** The elements that take in a point, in the template's units, as they lie turned, the top one first. */
export function elementsAt(doc: TemplateDoc, x: number, y: number): number[] {
  const hits: number[] = [];
  doc.elements.forEach((e, i) => {
    // The point turned back onto the element's box.
    const [sin, cos] = sinCos(e.rotation ?? 0);
    const dx = x - (e.x + e.width / 2);
    const dy = y - (e.y + e.height / 2);
    const u = dx * cos + dy * sin;
    const v = -dx * sin + dy * cos;
    if (Math.abs(u) <= e.width / 2 + 1e-9 && Math.abs(v) <= e.height / 2 + 1e-9) hits.unshift(i);
  });
  return hits;
}

const codeNames: Record<string, string> = { qr: 'QR code', micro_qr: 'Micro QR code', code128: 'Code 128' };

/** An element's name in the layers list: what it is and what it shows. */
export function layerName(e: TemplateElement): string {
  switch (e.type) {
    case 'generator': {
      const kind = codeNames[e.generator_type ?? ''] ?? `${e.generator_type} code`;
      return e.content_binding ? `${kind}: ${e.content_binding}` : kind;
    }
    case 'static_image':
      return 'Image';
    case 'shape':
      return shapeNames[e.shape ?? 'rectangle'];
    default:
      return `Text: ${e.content_binding || 'empty'}`;
  }
}

export const shapeNames: Record<ShapeKind, string> = { rectangle: 'Rectangle', ellipse: 'Ellipse', line: 'Line', polygon: 'Polygon', path: 'Artwork' };

/** A shape's stroke width when it is drawn: 0.25 mm. */
export function defaultStroke(units: Units): number {
  return units === 'mm' ? 0.25 : 0.01;
}

/**
 * A new shape in a box, in the template's units: outlined in black, a
 * polygon a triangle unless its points are given.
 */
export function newShape(kind: ShapeKind, doc: TemplateDoc, box?: { x: number; y: number; width: number; height: number }): TemplateElement {
  const step = gridStep(doc.units);
  const side = Math.max(step, snap(Math.min(doc.width, doc.height) * 0.5, step, doc.units));
  const b = box ?? {
    x: tidy((doc.width - side) / 2, doc.units),
    y: tidy((doc.height - (kind === 'line' ? 0 : side)) / 2, doc.units),
    width: side,
    height: kind === 'line' ? 0 : side,
  };
  return {
    type: 'shape',
    shape: kind,
    ...b,
    stroke: 'black',
    stroke_width: defaultStroke(doc.units),
    fill: 'none',
    ...(kind === 'polygon' ? { points: [[0.5, 0], [1, 1], [0, 1]] as [number, number][] } : {}),
  };
}

/** Whether a shape's stroke is thinner than one printer dot, and so printed one dot wide. */
export function hairline(e: TemplateElement, doc: TemplateDoc): boolean {
  return e.type === 'shape' && e.stroke !== 'none' && toMM(e.stroke_width ?? 0, doc.units) * (doc.dpi / MM_PER_INCH) < 1;
}

/**
 * A polygon through points given in the template's units in its own box's
 * frame, before it is turned: its box fitted round them, moved so that the
 * points stay where they lie on the label, turned, and the points as
 * fractions of it.
 */
export function polygonThrough(doc: TemplateDoc, e: TemplateElement, pts: [number, number][]): TemplateElement {
  const xs = pts.map((p) => p[0]);
  const ys = pts.map((p) => p[1]);
  const minX = Math.min(...xs);
  const minY = Math.min(...ys);
  const w = Math.max(Math.max(...xs) - minX, gridStep(doc.units));
  const h = Math.max(Math.max(...ys) - minY, gridStep(doc.units));
  // Where the new box's center lies, turned about the old one's.
  const [sin, cos] = sinCos(e.rotation ?? 0);
  const ox = minX + w / 2 - e.width / 2;
  const oy = minY + h / 2 - e.height / 2;
  const cx = e.x + e.width / 2 + ox * cos - oy * sin;
  const cy = e.y + e.height / 2 + ox * sin + oy * cos;
  const frac = (v: number) => Math.round(v * 10000) / 10000;
  return {
    ...e,
    x: tidy(cx - w / 2, doc.units),
    y: tidy(cy - h / 2, doc.units),
    width: tidy(w, doc.units),
    height: tidy(h, doc.units),
    points: pts.map(([x, y]) => [frac((x - minX) / w), frac((y - minY) / h)]),
  };
}

/** A polygon's points in the template's units, in its own box's frame. */
export function polygonPoints(e: TemplateElement): [number, number][] {
  return (e.points ?? []).map(([u, v]) => [u * e.width, v * e.height]);
}

/** An adaptive copy of a fixed template, taking half to twice its size. */
export function makeAdaptive(doc: TemplateDoc): TemplateDoc {
  return {
    ...doc,
    sizing: 'adaptive',
    adapt: doc.adapt ?? {
      min_width: tidy(doc.width / 2, doc.units),
      max_width: tidy(doc.width * 2, doc.units),
      min_height: tidy(doc.height / 2, doc.units),
      max_height: tidy(doc.height * 2, doc.units),
      continuous_length: 'proportional',
    },
  };
}

export type Feed = 'auto' | 'across' | 'along';

/** Which way up a template is read, from its sides. */
export function orientation(doc: TemplateDoc): 'portrait' | 'landscape' | 'square' {
  if (doc.height > doc.width) return 'portrait';
  return doc.width > doc.height ? 'landscape' : 'square';
}

/** An anchor seen from the other side: start and end swap. */
function reversed(a: Anchor | undefined): Anchor | undefined {
  return a === 'start' ? 'end' : a === 'end' ? 'start' : a;
}

/**
 * A template turned a quarter turn, clockwise or not, with everything on
 * it: its sides swap, as do an adaptive template's ranges, and each element
 * turns about the label's center, its box keeping its size, so the design
 * reads the same, turned. Anchors follow the sides they were on. As the
 * server turns it (README §4.1).
 */
export function turnTemplate(doc: TemplateDoc, clockwise: boolean): TemplateDoc {
  const fine = (v: number) => Math.round(v * 1e6) / 1e6;
  return {
    ...doc,
    width: doc.height,
    height: doc.width,
    ...(doc.adapt
      ? { adapt: { ...doc.adapt, min_width: doc.adapt.min_height, max_width: doc.adapt.max_height, min_height: doc.adapt.min_width, max_height: doc.adapt.max_width } }
      : {}),
    elements: doc.elements.map((e) => {
      const cx = e.x + e.width / 2;
      const cy = e.y + e.height / 2;
      const [nx, ny] = clockwise ? [doc.height - cy, cx] : [cy, doc.width - cx];
      const [ax, ay] = clockwise ? [reversed(e.anchor_y), e.anchor_x] : [e.anchor_y, reversed(e.anchor_x)];
      let next: TemplateElement = { ...e, x: fine(nx - e.width / 2), y: fine(ny - e.height / 2) };
      next = withProp(next, 'rotation', normalAngle((e.rotation ?? 0) + (clockwise ? 90 : 270)) || undefined);
      next = withProp(next, 'anchor_x', ax);
      return withProp(next, 'anchor_y', ay);
    }),
  };
}

/** A fixed copy of a template: its size range and its elements' adaptive settings go. */
export function makeFixed(doc: TemplateDoc): TemplateDoc {
  const { sizing: _s, adapt: _a, turnable: _t, ...rest } = doc;
  return {
    ...rest,
    elements: doc.elements.map((e) => {
      const { anchor_x: _x, anchor_y: _y, min_size: _min, max_size: _max, when_too_small: _w, alternative: _alt, ...kept } = e;
      return kept;
    }),
  };
}

/** An element with a property set, or removed when the value is undefined. */
export function withProp<K extends keyof TemplateElement>(e: TemplateElement, key: K, value: TemplateElement[K] | undefined): TemplateElement {
  const next = { ...e };
  if (value === undefined) delete next[key];
  else next[key] = value;
  return next;
}

/** What an element does where it does not fit, by default: images are left off, the rest refuse. */
export function tooSmall(e: TemplateElement): 'refuse' | 'hide' | 'alternative' {
  return e.when_too_small ?? (e.type === 'static_image' || e.type === 'shape' ? 'hide' : 'refuse');
}

export type Side = 'top' | 'right' | 'bottom' | 'left';
export const sides: Side[] = ['top', 'right', 'bottom', 'left'];
export type Margin = Partial<Record<Side, number>>;

/** How the server laid a code out in its box, from the JSON preview, in the template's units. */
export interface CodeLayout {
  module: number;
  module_dots: number;
  /** The space between the code and each side of its box, as drawn. */
  margin: Record<Side, number>;
  /** Sides narrower than its standard quiet zone. */
  narrow?: Side[];
}

/**
 * A code with its margin on some sides set, or put back to standard where a
 * value is undefined. A quiet_zone from before margins becomes the margin
 * on every side it gave, first.
 */
export function withMargin(e: TemplateElement, set: Side[], value: number | undefined): TemplateElement {
  const margin: Margin = { ...(e.margin ?? {}) };
  if (e.quiet_zone !== undefined) {
    // Codes 128 have no quiet zone above and below.
    for (const side of sides) if (e.generator_type !== 'code128' || side === 'left' || side === 'right') margin[side] = e.quiet_zone;
  }
  for (const side of set) {
    if (value === undefined) delete margin[side];
    else margin[side] = value;
  }
  const { quiet_zone: _q, margin: _m, ...rest } = e;
  return Object.keys(margin).length ? { ...rest, margin } : rest;
}

/** The margin given for one side, in its unit, or undefined for the standard. */
export function marginOf(e: TemplateElement, side: Side): number | undefined {
  if (e.margin) return e.margin[side];
  if (e.quiet_zone === undefined || (e.generator_type === 'code128' && (side === 'top' || side === 'bottom'))) return undefined;
  return e.quiet_zone;
}

export interface Guides {
  x?: number[];
  y?: number[];
}

/** A box on the label, in the template's units. */
export interface Box {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** The box round some elements, as they lie turned. */
export function boundsOf(elements: TemplateElement[]): Box {
  const boxes = elements.map(bounds);
  const x = Math.min(...boxes.map((b) => b.x));
  const y = Math.min(...boxes.map((b) => b.y));
  return { x, y, width: Math.max(...boxes.map((b) => b.x + b.width)) - x, height: Math.max(...boxes.map((b) => b.y + b.height)) - y };
}

export type Alignment = 'left' | 'center' | 'right' | 'top' | 'middle' | 'bottom';

/**
 * Lines elements up as they lie turned: with each other, or with the label;
 * or, with distribute, spreads three or more out evenly, the outermost
 * staying put. As the server arranges them (README §4.1).
 */
export function arrange(doc: TemplateDoc, indices: number[], how: { align?: Alignment; distribute?: 'across' | 'down'; toLabel?: boolean }): TemplateElement[] {
  const els = doc.elements;
  const boxes = indices.map((i) => bounds(els[i]!));
  const moved = new Map<number, [number, number]>();
  if (how.distribute) {
    if (indices.length < 3) return els;
    const across = how.distribute === 'across';
    const start = (b: Box) => (across ? b.x : b.y);
    const size = (b: Box) => (across ? b.width : b.height);
    const order = indices.map((_, k) => k).sort((a, b) => start(boxes[a]!) + size(boxes[a]!) / 2 - (start(boxes[b]!) + size(boxes[b]!) / 2));
    const first = boxes[order[0]!]!;
    const last = boxes[order[order.length - 1]!]!;
    const total = boxes.reduce((n, b) => n + size(b), 0);
    const gap = (start(last) + size(last) - start(first) - total) / (order.length - 1);
    let at = start(first);
    for (const k of order) {
      const d = at - start(boxes[k]!);
      moved.set(indices[k]!, across ? [d, 0] : [0, d]);
      at += size(boxes[k]!) + gap;
    }
  } else if (how.align) {
    if (indices.length < (how.toLabel ? 1 : 2)) return els;
    const t = how.toLabel ? { x: 0, y: 0, width: doc.width, height: doc.height } : boundsOf(indices.map((i) => els[i]!));
    boxes.forEach((b, k) => {
      const dx = { left: t.x - b.x, center: t.x + t.width / 2 - (b.x + b.width / 2), right: t.x + t.width - (b.x + b.width) }[how.align as string] ?? 0;
      const dy = { top: t.y - b.y, middle: t.y + t.height / 2 - (b.y + b.height / 2), bottom: t.y + t.height - (b.y + b.height) }[how.align as string] ?? 0;
      moved.set(indices[k]!, [dx, dy]);
    });
  }
  return els.map((e, i) => {
    const d = moved.get(i);
    return d ? { ...e, x: tidy(e.x + d[0], doc.units), y: tidy(e.y + d[1], doc.units) } : e;
  });
}

/** Normalized path data, as the server writes it, with each point mirrored. */
function mirrorPathData(d: string, across: boolean): string {
  if (!/^[MLQCZ\d\s.eE+-]*$/.test(d)) return d;
  let k = 0;
  return d.replace(/-?\d*\.?\d+(?:[eE][+-]?\d+)?/g, (n) => {
    const v = Number(n);
    const mirror = across ? k % 2 === 0 : k % 2 === 1;
    k++;
    return mirror ? String(Math.round((1 - v) * 1e5) / 1e5) : n;
  });
}

/**
 * Flips elements across (left for right) or down, about the middle of them
 * all: each moves to its mirrored place and its turn is mirrored. Images,
 * polygons, artwork and lines are mirrored too; text and codes are not, so
 * they stay readable.
 */
export function flipElements(doc: TemplateDoc, indices: number[], across: boolean): TemplateElement[] {
  if (!indices.length) return doc.elements;
  const box = boundsOf(indices.map((i) => doc.elements[i]!));
  const mid = across ? box.x + box.width / 2 : box.y + box.height / 2;
  return doc.elements.map((e, i) => {
    if (!indices.includes(i)) return e;
    let next: TemplateElement = across ? { ...e, x: tidy(2 * mid - (e.x + e.width), doc.units) } : { ...e, y: tidy(2 * mid - (e.y + e.height), doc.units) };
    next = withProp(next, 'rotation', normalAngle(-(e.rotation ?? 0)) || undefined);
    if (e.type === 'static_image') {
      const a = (e.mirror === 'across' || e.mirror === 'both') !== across;
      const d = (e.mirror === 'down' || e.mirror === 'both') !== !across;
      next = withProp(next, 'mirror', a && d ? 'both' : a ? 'across' : d ? 'down' : undefined);
    } else if (e.type === 'shape' && e.shape === 'polygon' && e.points) {
      const frac = (v: number) => Math.round((1 - v) * 1e4) / 1e4;
      next.points = e.points.map(([u, v]) => (across ? [frac(u), v] : [u, frac(v)]));
    } else if (e.type === 'shape' && e.shape === 'path' && e.d) {
      next.d = mirrorPathData(e.d, across);
    } else if (e.type === 'shape' && e.shape === 'line') {
      next = withProp(next, 'flip', e.flip ? undefined : true);
    }
    return next;
  });
}

/** Where a moving box snaps to, and the lines it snapped to, to show. */
export interface Snapped {
  dx: number;
  dy: number;
  /** Lines across (at x) and down (at y) it snapped to. */
  x?: number;
  y?: number;
}

/**
 * Snaps a box being moved by (dx, dy) to what is near: the edges and
 * middles of the other elements, the label's edges and middle, and its
 * guides, within threshold, on each axis by itself. An axis with nothing
 * near keeps its delta, for the grid.
 */
export function smartSnap(doc: TemplateDoc, moving: number[], start: Box, dx: number, dy: number, threshold: number): Snapped {
  const xs = [0, doc.width / 2, doc.width, ...(doc.guides?.x ?? [])];
  const ys = [0, doc.height / 2, doc.height, ...(doc.guides?.y ?? [])];
  doc.elements.forEach((e, i) => {
    if (moving.includes(i)) return;
    const b = bounds(e);
    xs.push(b.x, b.x + b.width / 2, b.x + b.width);
    ys.push(b.y, b.y + b.height / 2, b.y + b.height);
  });
  const best = (edges: number[], targets: number[]): [number, number] | null => {
    let found: [number, number] | null = null;
    for (const edge of edges) {
      for (const t of targets) {
        const d = t - edge;
        if (Math.abs(d) <= threshold && (!found || Math.abs(d) < Math.abs(found[0]))) found = [d, t];
      }
    }
    return found;
  };
  const sx = best([start.x + dx, start.x + dx + start.width / 2, start.x + dx + start.width], xs);
  const sy = best([start.y + dy, start.y + dy + start.height / 2, start.y + dy + start.height], ys);
  return {
    dx: sx ? dx + sx[0] : dx,
    dy: sy ? dy + sy[0] : dy,
    ...(sx ? { x: sx[1] } : {}),
    ...(sy ? { y: sy[1] } : {}),
  };
}

/** The selection, with every member of the groups it touches. */
export function withGroups(elements: TemplateElement[], indices: number[]): number[] {
  const groups = new Set(indices.map((i) => elements[i]?.group).filter((g): g is string => Boolean(g)));
  if (!groups.size) return indices;
  return elements.flatMap((e, i) => (indices.includes(i) || (e.group && groups.has(e.group)) ? [i] : []));
}

/** A new group's name, not one in use. */
export function newGroupName(elements: TemplateElement[]): string {
  const used = new Set(elements.map((e) => e.group));
  for (let n = 1; ; n++) if (!used.has(`group-${n}`)) return `group-${n}`;
}

/** What the editor puts on the clipboard: elements, and the units they are in. */
export interface Clipping {
  bam: 'elements';
  units: Units;
  elements: TemplateElement[];
}

/** Elements copied, in their units, as text for the clipboard. */
export function clip(doc: TemplateDoc, indices: number[]): string {
  const c: Clipping = { bam: 'elements', units: doc.units, elements: indices.map((i) => doc.elements[i]!) };
  return JSON.stringify(c);
}

/**
 * Elements from the clipboard, in a template's units and moved a little,
 * so they are seen; null when it holds none. Groups get names of their own.
 */
export function unclip(doc: TemplateDoc, text: string, offset: number): TemplateElement[] | null {
  let c: Clipping;
  try {
    c = JSON.parse(text) as Clipping;
  } catch {
    return null;
  }
  if (c?.bam !== 'elements' || !Array.isArray(c.elements) || (c.units !== 'mm' && c.units !== 'in')) return null;
  const converted = convertUnits({ ...doc, units: c.units, elements: c.elements }, doc.units).elements;
  const renamed = new Map<string, string>();
  const used = [...doc.elements];
  return converted.map((e) => {
    let next: TemplateElement = { ...e, x: tidy(e.x + offset, doc.units), y: tidy(e.y + offset, doc.units) };
    if (e.group) {
      if (!renamed.has(e.group)) {
        renamed.set(e.group, newGroupName(used));
        used.push({ ...next, group: renamed.get(e.group) });
      }
      next = { ...next, group: renamed.get(e.group) };
    }
    return next;
  });
}

/** CSS pixels in a millimeter, for showing a label at its actual size. */
export const PX_PER_MM = 96 / MM_PER_INCH;

/** A ruler's ticks for a scale: a step of 1, 2, 5 or 10 times a power of ten, at least minPx apart. */
export function rulerStep(pxPerUnit: number, minPx = 8): number {
  for (let p = -3; p < 6; p++) {
    for (const m of [1, 2, 5]) {
      const step = m * 10 ** p;
      if (step * pxPerUnit >= minPx) return step;
    }
  }
  return 10 ** 6;
}
