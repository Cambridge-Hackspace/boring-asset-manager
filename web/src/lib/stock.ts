// Custom label stock (README §4.4): rolls known by their size alone, which
// printers whose drivers take any size may have loaded.

import type { DriverInfo, LabelStockSize } from './models';

/** A stock's size in words: "60 × 40 mm, 3 mm gap" or "50 mm continuous". */
export function describeStock(s: LabelStockSize): string {
  const mm = (n: number) => `${Number(n.toFixed(2))}`;
  if (!s.length_mm) return `${mm(s.width_mm)} mm continuous`;
  return `${mm(s.width_mm)} × ${mm(s.length_mm)} mm${s.gap_mm ? `, ${mm(s.gap_mm)} mm gap` : ''}`;
}

/**
 * The custom stock a model of a driver can take: none when the driver takes
 * only its own rolls, else what is no wider than the model takes.
 */
export function stockFor<T extends LabelStockSize>(driver: DriverInfo | undefined, model: string, stock: T[]): T[] {
  const widest = driver?.custom_stock?.max_width_mm[model];
  if (widest === undefined) return [];
  return stock.filter((s) => s.width_mm <= widest);
}
