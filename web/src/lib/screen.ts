// How large a millimeter is on the screen the window is on, for showing a
// label at its real size. Krita reads that from the operating system for the
// screen its window is on; a browser never reveals a screen's physical size,
// and its inch is always 96 CSS pixels. So each screen is calibrated once,
// against a ruler, and remembered in this browser, as the screen of its
// size and pixel ratio; one not yet calibrated uses the nominal inch.

const storageKey = 'bam.screen-calibration';

/** CSS pixels in a millimeter by the browser's own inch. */
export const NOMINAL_PX_PER_MM = 96 / 25.4;

interface ScreenLike {
  width: number;
  height: number;
}

/** The screen the window is on, as calibrations are kept: its size and pixel ratio. */
export function screenKey(screen: ScreenLike = window.screen, dpr: number = window.devicePixelRatio || 1): string {
  return `${screen.width}x${screen.height}@${Math.round(dpr * 100) / 100}`;
}

function read(): Record<string, number> {
  try {
    const raw = JSON.parse(localStorage.getItem(storageKey) ?? '{}') as Record<string, unknown>;
    return Object.fromEntries(Object.entries(raw).filter((e): e is [string, number] => typeof e[1] === 'number' && e[1] > 0));
  } catch {
    return {};
  }
}

/** CSS pixels in a millimeter on a screen, and whether that was measured. */
export function pxPerMm(key: string): { pxPerMm: number; calibrated: boolean } {
  const known = read()[key];
  return known ? { pxPerMm: known, calibrated: true } : { pxPerMm: NOMINAL_PX_PER_MM, calibrated: false };
}

/** Remembers a screen's calibration: a bar barPx CSS pixels long measured mm millimeters. */
export function calibrate(key: string, barPx: number, mm: number): number {
  const value = barPx / mm;
  try {
    localStorage.setItem(storageKey, JSON.stringify({ ...read(), [key]: value }));
  } catch {
    // Storage unavailable: it lasts as long as the page.
  }
  return value;
}

/** Forgets a screen's calibration. */
export function forget(key: string): void {
  try {
    const all = read();
    delete all[key];
    localStorage.setItem(storageKey, JSON.stringify(all));
  } catch {
    // Storage unavailable: nothing was kept.
  }
}
