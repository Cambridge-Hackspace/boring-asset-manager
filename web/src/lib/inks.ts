// Inks (docs/labels.md#inks): the named colors a label's text, shapes and
// codes are drawn in, and the palettes of them printers and displays have.
// The server's internal/labels/inks.go is the authority; this mirrors it.

import type { TemplateDoc, TemplateElement } from './template';

export type Ink = 'black' | 'white' | 'red' | 'yellow' | 'green' | 'blue' | 'orange';
export type Palette = 'bw' | 'bwr' | 'bwy' | 'bwry' | 'bwrygb' | 'bwrygbo';

/** Every ink, in the order palettes list them, with its name and the color it is shown in. */
export const INKS: { ink: Ink; name: string; color: string }[] = [
  { ink: 'black', name: 'Black', color: '#000000' },
  { ink: 'white', name: 'White', color: '#ffffff' },
  { ink: 'red', name: 'Red', color: '#cc0000' },
  { ink: 'yellow', name: 'Yellow', color: '#ffd200' },
  { ink: 'green', name: 'Green', color: '#008c3c' },
  { ink: 'blue', name: 'Blue', color: '#0046c8' },
  { ink: 'orange', name: 'Orange', color: '#ff8000' },
];

export const PALETTES: Record<Palette, Ink[]> = {
  bw: ['black', 'white'],
  bwr: ['black', 'white', 'red'],
  bwy: ['black', 'white', 'yellow'],
  bwry: ['black', 'white', 'red', 'yellow'],
  bwrygb: ['black', 'white', 'red', 'yellow', 'green', 'blue'],
  bwrygbo: ['black', 'white', 'red', 'yellow', 'green', 'blue', 'orange'],
};

export const isInk = (v: unknown): v is Ink => INKS.some((i) => i.ink === v);

/** The color an ink, or none, is shown in. */
export const inkColor = (ink: string): string => INKS.find((i) => i.ink === ink)?.color ?? '#000000';

/** An ink's name, for people. */
export const inkName = (ink: string): string => INKS.find((i) => i.ink === ink)?.name ?? ink;

/** The inks a palette has; black and white for none, or one it does not know. */
export function paletteInks(palette: string | null | undefined): Ink[] {
  return PALETTES[(palette ?? 'bw') as Palette] ?? PALETTES.bw;
}

/** The inks an element draws in: black for text or a code without a fill, and a shape's stroke and fill. */
function elementInks(e: TemplateElement): string[] {
  if (e.type === 'text' || e.type === 'generator') return [e.fill ?? 'black'];
  if (e.type === 'shape') return [e.stroke ?? 'black', e.fill ?? 'none'];
  return [];
}

/** The inks a template draws in, alternatives included, in the order of INKS. */
export function templateInks(doc: TemplateDoc): Ink[] {
  const used = new Set(doc.elements.flatMap((e) => [...elementInks(e), ...(e.alternative ? elementInks(e.alternative) : [])]));
  return INKS.map((i) => i.ink).filter((ink) => used.has(ink));
}

/**
 * The inks to offer for an element: the device's, and the one it has if the
 * device lacks it, so what is chosen still shows. With no device, every ink.
 */
export function offeredInks(palette: string | null, current: string | undefined, white = true): Ink[] {
  const inks = palette ? paletteInks(palette) : INKS.map((i) => i.ink);
  const offered = inks.filter((i) => white || i !== 'white');
  if (current && isInk(current) && !offered.includes(current)) offered.push(current);
  return offered;
}
