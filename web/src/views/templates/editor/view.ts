// The view of the label: how many screen pixels a template unit takes, and
// where the label's top left corner is in the viewport, in CSS pixels.

export interface View {
  scale: number;
  x: number;
  y: number;
}

/** Room left round the label when it is fitted. */
export const PAD = 32;

/** The view showing the whole label, centered. */
export function fitView(width: number, height: number, vw: number, vh: number, pad = PAD): View {
  const scale = Math.max(1e-3, Math.min((vw - 2 * pad) / width, (vh - 2 * pad) / height));
  return { scale, x: (vw - width * scale) / 2, y: (vh - height * scale) / 2 };
}

/** The view filling the viewport's width with the label, its top at the top, or centered when it fits. */
export function fitWidthView(width: number, height: number, vw: number, vh: number, pad = PAD): View {
  const scale = Math.max(1e-3, (vw - 2 * pad) / width);
  const tall = height * scale;
  return { scale, x: pad, y: tall <= vh - 2 * pad ? (vh - tall) / 2 : pad };
}

/** The view zoomed to scale, the label's point under (ax, ay) staying there. */
export function zoomAround(view: View, scale: number, ax: number, ay: number): View {
  const ux = (ax - view.x) / view.scale;
  const uy = (ay - view.y) / view.scale;
  return { scale, x: ax - ux * scale, y: ay - uy * scale };
}

/** A scale kept between 5% and 6400% of the one at 100%. */
export function clampScale(scale: number, at100: number): number {
  return Math.min(Math.max(scale, at100 * 0.05), at100 * 64);
}

/**
 * The scale at 100%. In pixel size, as Krita's, one printer dot is one
 * screen pixel; in print size, the label is its real size on the screen.
 */
export function scaleAt100(dotsPerUnit: number, devicePixelRatio: number, printSize: boolean, cssPxPerMm: number, mmPerUnit: number): number {
  return printSize ? cssPxPerMm * mmPerUnit : dotsPerUnit / devicePixelRatio;
}

/** Zoom levels the + and - keys step through, as percentages. */
export const ZOOM_STEPS = [5, 10, 15, 25, 33.3, 50, 66.7, 100, 150, 200, 300, 400, 600, 800, 1200, 1600, 2400, 3200, 4800, 6400];

/** The next zoom step in or out from a percentage. */
export function nextStep(percent: number, inward: boolean): number {
  if (inward) return ZOOM_STEPS.find((s) => s > percent * 1.001) ?? ZOOM_STEPS[ZOOM_STEPS.length - 1]!;
  return [...ZOOM_STEPS].reverse().find((s) => s < percent / 1.001) ?? ZOOM_STEPS[0]!;
}

/** A zoom typed as text, "150", "150%" or "1.5x", as a percentage, or null. */
export function parseZoom(text: string): number | null {
  const t = text.trim().toLowerCase();
  const x = /^(\d+(?:\.\d+)?)\s*x$/.exec(t);
  if (x) return Number(x[1]) * 100;
  const p = /^(\d+(?:\.\d+)?)\s*%?$/.exec(t);
  return p ? Number(p[1]) : null;
}
