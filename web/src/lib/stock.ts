// Custom label stock (README §4.4): rolls known by their size alone, which
// printers whose drivers take any size may have loaded.

import type { DriverInfo, LabelStockSize, PrinterChanges } from './models';

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

/** What changing a printer would affect, a sentence each; none when nothing. */
export function describeChanges(c: PrinterChanges): string[] {
  const out = c.stranded.map((s) => {
    const labels = `${s.labels} ${s.labels === 1 ? 'label' : 'labels'} waiting for ${s.target} with ${s.template_name}`;
    return s.move_to_name ? `${labels} would move to ${s.move_to_name}, their category’s next template that fits.` : `${labels} would no longer fit.`;
  });
  if (c.categories.length) {
    const names = c.categories.map((x) => x.name).join(', ');
    out.push(`No printer would suit the label template of ${c.categories.length === 1 ? 'the category' : 'the categories'} ${names}.`);
  }
  for (const p of c.pools) out.push(`The printers in ${p.name} would no longer be loaded alike.`);
  return out;
}

const driverNames: Record<string, string> = {
  brother_ql: 'Brother QL',
  dymo_labelwriter: 'DYMO LabelWriter',
  tspl: 'TSPL',
};

/** A printer driver as people know it: "Brother QL" rather than brother_ql. */
export function driverName(driver: string): string {
  return driverNames[driver] ?? driver;
}

const optionLabels: Record<string, string> = {
  cut: 'Cut after each label',
  density: 'Darkness',
  roll: 'Roll (twin-roll models)',
  rotate_180: 'Turn labels upside down',
};

/** A driver option as a form labels it: "Cut after each label" rather than cut. */
export function optionLabel(name: string): string {
  if (optionLabels[name]) return optionLabels[name];
  const words = name.replace(/_/g, ' ');
  return words.charAt(0).toUpperCase() + words.slice(1);
}
