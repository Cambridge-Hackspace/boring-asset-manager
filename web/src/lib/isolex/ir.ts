// The shape IR isolex compiles icons to (README §3.7; isolex SPEC §6):
// what pages draw icons from. Mirrors internal/isolex/ir.

export interface IrShape {
  kind: 'polygon' | 'path' | 'ellipse' | 'circle' | 'rect' | 'group';
  points?: [number, number][];
  d?: string;
  cx?: number;
  cy?: number;
  rx?: number;
  ry?: number;
  r?: number;
  x?: number;
  y?: number;
  w?: number;
  h?: number;
  fill?: string;
  fill_opacity?: number;
  stroke?: string;
  stroke_width?: number;
  dash?: string;
  tx?: number;
  ty?: number;
  scale?: number;
  shapes?: IrShape[];
}

export interface IrIcon {
  name: string;
  category: string;
  footprint?: [number, number, number];
  canvas?: [number, number];
  viewbox: [number, number, number, number];
  describe: string[];
  grants: [string, string][];
  shapes: IrShape[];
}

export interface IrDocument {
  ir_version: number;
  icons: IrIcon[];
}

/** An icon of a document by name. */
export function iconNamed(doc: IrDocument | null | undefined, name: string): IrIcon | undefined {
  return doc?.icons.find((i) => i.name === name);
}

/**
 * isolex's rule for numbers in drawings (SPEC §7): two decimals, ties to
 * even on the number's exact binary value, never -0.00. JavaScript's
 * toFixed takes a tie away from zero, so ties are told apart here: a
 * tie is a value whose exact expansion is …5 at the third decimal and
 * nothing after it, which only values such as 0.125 have.
 */
export function formatNumber(v: number): string {
  const exact = Math.abs(v).toFixed(30);
  const dot = exact.indexOf('.');
  const rest = exact.slice(dot + 3);
  let s: string;
  if (/^50*$/u.test(rest)) {
    const down = exact.slice(0, dot + 3);
    const last = Number(down[down.length - 1]);
    s = last % 2 === 0 ? down : (Math.abs(v) + 0.005).toFixed(2);
    if (v < 0) s = '-' + s;
  } else {
    s = v.toFixed(2);
  }
  return s === '-0.00' ? '0.00' : s;
}
