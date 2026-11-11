import { describe, expect, it } from 'vitest';
import { describeMaterial, interlockName, jobStateText, lasersOf, materialID, templateMM, underWay } from './lasers';
import type { DriverInfo, Printer } from './models';

const printer = (id: string, driver: string, model: string) => ({ id, name: id, driver, model, media: '', connection: { type: 'tcp', address: 'x' } }) as Printer;

describe('lasers', () => {
  it('finds the printers that are lasers, with their models', () => {
    const drivers = [
      { driver: 'brother_ql', models: ['QL-810W'], media: [] },
      { driver: 'test-laser', models: ['panel'], media: [], one_shot: [{ id: 'panel', work_width_mm: 200, work_height_mm: 150, materials: [], start_button: false, frames: true, interlocks: [] }] },
    ] as DriverInfo[];
    const found = lasersOf([printer('ql', 'brother_ql', 'QL-810W'), printer('laser', 'test-laser', 'panel')], drivers);
    expect(found.map((l) => [l.printer.id, l.model.work_width_mm])).toEqual([['laser', 200]]);
  });

  it('says what a material does, in words', () => {
    expect(describeMaterial({ id: 'b', name: 'Birch', engrave: { power: 30, speed: 150, passes: 1, lpi: 254 }, cut: { power: 100, speed: 8, passes: 2 } })).toBe(
      'Engraves at 30% and 150 mm/s, 1 pass, 254 lines an inch; cuts at 100% and 8 mm/s, 2 passes',
    );
    expect(describeMaterial({ id: 'a', name: 'Acrylic', engrave: { power: 20, speed: 200, passes: 2, lpi: 254 }, from_back: true })).toBe(
      'Engraves at 20% and 200 mm/s, 2 passes, 254 lines an inch; not cut; engraved from the back, mirrored',
    );
  });

  it('makes an ID for a material from its name', () => {
    expect(materialID('Oak, 6 mm')).toBe('oak-6-mm');
    expect(materialID('  ')).toBe('material');
    expect(materialID('x'.repeat(80)).length).toBe(64);
  });

  it('reads sizes, states and interlocks', () => {
    expect(templateMM({ units: 'in', width: 2, height: 1 })).toEqual({ width: 50.8, height: 25.4 });
    expect(jobStateText('ready')).toContain('press the button');
    expect(underWay('paused')).toBe(true);
    expect(underWay('prepared')).toBe(false);
    expect(interlockName('safety_key')).toBe('Safety key');
    expect(interlockName('firmware')).toBe('Firmware');
    expect(interlockName('drawer')).toBe('Drawer');
  });
});
