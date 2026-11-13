// Custom label stock (docs/printing.md): rolls known by their size alone, which
// printers whose drivers take any size may have loaded.

import type { DriverInfo, DriverMedia, LabelStockSize, PrinterChanges } from './models';

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

/** The built-in rolls a model of a driver takes: those that name it, or name no models. */
export function mediaFor(driver: DriverInfo | undefined, model: string): DriverMedia[] {
  return (driver?.media ?? []).filter((m) => !m.models || m.models.includes(model));
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
  brother_ptouch: 'Brother P-touch',
  brother_td: 'Brother TD',
  brady: 'Brady',
  dymo_labelmanager: 'DYMO LabelManager',
  dymo_labelwriter: 'DYMO LabelWriter',
  epson_labelworks: 'Epson LabelWorks',
  niimbot: 'Niimbot',
  tspl: 'TSPL',
  zpl: 'Zebra ZPL',
  epl2: 'Zebra EPL2',
  escpos: 'ESC/POS',
  phomemo: 'Phomemo',
  dpl: 'Datamax and Honeywell DPL',
  sbpl: 'SATO SBPL',
  ezpl: 'Godex EZPL',
  display: 'E-paper display',
  openepaperlink: 'OpenEPaperLink tag',
  trmnl: 'TRMNL',
  paperlesspaper: 'paperlesspaper OpenPaper',
  solum: 'SoluM shelf label',
  'test-laser': 'Test laser (simulated)',
  xtool: 'xTool',
  grbl: 'GRBL laser',
  ruida: 'Ruida laser controller',
};

/** A printer driver as people know it: "Brother QL" rather than brother_ql. */
export function driverName(driver: string): string {
  return driverNames[driver] ?? driver;
}

const optionLabels: Record<string, string> = {
  allow_unknown_firmware: 'Allow firmware BAM! does not know for this model',
  baud: 'Serial speed (baud)',
  calibrate: 'Calibrate before each job',
  chain: 'Chain labels, saving tape between jobs',
  compress: 'Compress labels',
  cut: 'Cut after each label',
  cut_after: 'Cut the tape off',
  density: 'Darkness',
  flame_hold_minutes: 'Refuse to start after a flame for (minutes)',
  feed_mm: 'Paper fed after each label (mm)',
  fill_light: 'Fill light for photos (%)',
  camera_port: 'Camera port (V1 firmware)',
  file_name: 'Name on the controller',
  frame_power: 'Power while framing (%)',
  frame_speed: 'Framing speed (mm/s)',
  half_cut: 'Half cut between labels',
  head_dots: 'Print head width (dots)',
  heat: 'Heat (darkness)',
  image_module: 'Memory module for label images',
  magic: 'Swizzle key',
  margin_mm: 'Margin before and after each label (mm)',
  max_power_s: 'S value of full power',
  media_sensor: 'Label sensor',
  media_type: 'Media type',
  mirror: 'Mirror labels, for clear tape',
  origin: 'Origin',
  part: 'Cartridge part number',
  print_mode: 'After each label',
  print_task: 'Print task, for its firmware',
  refresh_seconds: 'Ask again every (seconds)',
  reply_port: 'Port for its answers',
  roll: 'Roll (twin-roll models)',
  rotate_180: 'Turn labels upside down',
  rx_buffer: 'Receive buffer (bytes)',
  speed: 'Speed (inches per second)',
  stx_etx: 'Wrap jobs in STX and ETX',
  tape_type: 'Tape colors',
  work_height_mm: 'Work area, back to front (mm)',
  work_width_mm: 'Work area, across (mm)',
};

const optionValueLabels: Record<string, string> = {
  b1: 'B1',
  b21_v1: 'B21, first firmware',
  cutter: 'Cut it',
  d110: 'D110',
  direct: 'Direct thermal',
  each_label: 'After each label',
  full: 'Fully',
  gap: 'By the gaps between labels',
  mark: 'By black marks',
  last_label: 'After the last label',
  never: 'Never',
  none: 'Not at all',
  partial: 'Partly, leaving a tab',
  peel_off: 'Peel it off',
  printer: 'As the printer’s menu says',
  png: 'PNG',
  bmp: 'BMP',
  bottom_left: 'Front left',
  bottom_right: 'Front right',
  tear_off: 'Tear it off',
  top_left: 'Back left',
  top_right: 'Back right',
  transfer: 'Thermal transfer, with a ribbon',
};

/** A driver option's value as a form shows it: "Tear it off" rather than tear_off. */
export function optionValueLabel(value: string): string {
  return optionValueLabels[value] ?? value.replace(/_/g, ' ');
}

/** A driver option as a form labels it: "Cut after each label" rather than cut. */
export function optionLabel(name: string): string {
  if (optionLabels[name]) return optionLabels[name];
  const words = name.replace(/_/g, ' ');
  return words.charAt(0).toUpperCase() + words.slice(1);
}
