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
  quiet_zone?: number;
  /** The smallest module drawn, in millimeters. */
  min_module_mm?: number;
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
  /**
   * Elements are drawn in the order they are listed, the first at the back.
   * Absent in documents written before that, which drew images, then text,
   * then codes.
   */
  layering?: 'list';
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
    })),
  };
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

const layerOfType: Record<ElementType, number> = { static_image: 0, text: 1, generator: 2 };

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

/** The elements whose boxes take in a point, in the template's units, the top one first. */
export function elementsAt(doc: TemplateDoc, x: number, y: number): number[] {
  const hits: number[] = [];
  doc.elements.forEach((e, i) => {
    if (x >= e.x && x <= e.x + e.width && y >= e.y && y <= e.y + e.height) hits.unshift(i);
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
    default:
      return `Text: ${e.content_binding || 'empty'}`;
  }
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

/** A fixed copy of a template: its size range and its elements' adaptive settings go. */
export function makeFixed(doc: TemplateDoc): TemplateDoc {
  const { sizing: _s, adapt: _a, ...rest } = doc;
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
  return e.when_too_small ?? (e.type === 'static_image' ? 'hide' : 'refuse');
}
