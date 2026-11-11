// Lasers (docs/printing.md#lasers): one-shot printers, which engrave a label
// onto a workpiece. What the Lasers page works out from what the API gives.

import type { DriverInfo, LaserJobState, Material, OneShotModel, Printer, TemplateSummary } from './models';

/** The printers that are lasers, each with its model as its driver describes it. */
export function lasersOf(printers: Printer[], drivers: DriverInfo[]): { printer: Printer; model: OneShotModel }[] {
  const out: { printer: Printer; model: OneShotModel }[] = [];
  for (const p of printers) {
    const model = drivers.find((d) => d.driver === p.driver)?.one_shot?.find((m) => m.id === p.model);
    if (model) out.push({ printer: p, model });
  }
  return out;
}

/** A template's size in millimeters. */
export function templateMM(t: Pick<TemplateSummary, 'units' | 'width' | 'height'>): { width: number; height: number } {
  const k = t.units === 'in' ? 25.4 : 1;
  return { width: round2(t.width * k), height: round2(t.height * k) };
}

function round2(v: number): number {
  return Math.round(v * 100) / 100;
}

/** A job's state as people say it. */
export function jobStateText(state: LaserJobState | string): string {
  switch (state) {
    case 'prepared':
      return 'Prepared, not started';
    case 'ready':
      return 'Sent ready: press the button on the machine to start';
    case 'running':
      return 'Running';
    case 'paused':
      return 'Paused';
    case 'done':
      return 'Done';
    case 'cancelled':
      return 'Cancelled';
    case 'failed':
      return 'Failed';
    case 'discarded':
      return 'Discarded: another job took its place';
  }
  return state;
}

/** Whether a job is under way on its machine. */
export function underWay(state: LaserJobState | string | undefined): boolean {
  return state === 'ready' || state === 'running' || state === 'paused';
}

/** An interlock as people say it. */
export function interlockName(name: string): string {
  switch (name) {
    case 'lid':
      return 'Lid';
    case 'safety_key':
      return 'Safety key';
    case 'flame':
      return 'Flame detection';
    case 'exhaust':
      return 'Exhaust';
    case 'drawer':
      return 'Drawer';
    case 'machine':
      return 'Machine';
    case 'firmware':
      return 'Firmware';
  }
  return name;
}

/** A material's settings in a line: "30% at 150 mm/s, 1 pass, 254 lines an inch; cuts at 100% at 8 mm/s, 2 passes". */
export function describeMaterial(m: Material): string {
  const passes = (n: number) => (n === 1 ? '1 pass' : `${n} passes`);
  let s = `Engraves at ${m.engrave.power}% and ${m.engrave.speed} mm/s, ${passes(m.engrave.passes)}, ${m.engrave.lpi} lines an inch`;
  s += m.cut ? `; cuts at ${m.cut.power}% and ${m.cut.speed} mm/s, ${passes(m.cut.passes)}` : '; not cut';
  if (m.from_back) s += '; engraved from the back, mirrored';
  return s;
}

/** An ID for a material of a laser's own, from its name: "Oak, 6 mm" is oak-6-mm. */
export function materialID(name: string): string {
  return (
    name
      .toLowerCase()
      .normalize('NFKD')
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 64)
      .replace(/-+$/, '') || 'material'
  );
}
