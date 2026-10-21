// How wide the template editor's panels are: dragged by their edge, as in
// Figma or Krita, and remembered in this browser.

const storageKey = 'bam.editor-panels-width';

/** The panels' width as they come, and the least and most they can be. */
export const PANELS_DEFAULT = 288;
export const PANELS_MIN = 224;
export const PANELS_MAX = 640;

/** The least the canvas is left, however wide the panels are dragged. */
const CANVAS_MIN = 320;

/** A width for the panels, kept in bounds, given the room they share with the canvas. */
export function clampPanels(width: number, room: number = Infinity): number {
  const most = Math.max(PANELS_MIN, Math.min(PANELS_MAX, room - CANVAS_MIN));
  return Math.round(Math.min(Math.max(width, PANELS_MIN), most));
}

/** The width last left, or the default. */
export function storedPanels(): number {
  try {
    const w = Number(localStorage.getItem(storageKey));
    return Number.isFinite(w) && w > 0 ? clampPanels(w) : PANELS_DEFAULT;
  } catch {
    return PANELS_DEFAULT;
  }
}

/** Remembers the width, or forgets it at the default. */
export function storePanels(width: number): void {
  try {
    if (width === PANELS_DEFAULT) localStorage.removeItem(storageKey);
    else localStorage.setItem(storageKey, String(width));
  } catch {
    // Storage unavailable: it lasts as long as the page.
  }
}
