import { describe, expect, it } from 'vitest';
import { calibrationText, cornerName, describeMaterial, heightFits, interlockName, jobStateText, lasersOf, materialID, placedOf, scrapText, templateMM, turnOf, underWay } from './lasers';
import type { DriverInfo, LaserCalibration, LaserPlacement, Printer } from './models';

const printer = (id: string, driver: string, model: string) => ({ id, name: id, driver, model, media: '', connection: { type: 'tcp', address: 'x' } }) as Printer;

describe('lasers', () => {
  it('finds the printers that are lasers, with their models', () => {
    const drivers = [
      { driver: 'brother_ql', models: ['QL-810W'], media: [] },
      { driver: 'test-laser', models: ['panel'], media: [], one_shot: [{ id: 'panel', work_width_mm: 200, work_height_mm: 150, materials: [], start_button: false, frames: true, interlocks: [], camera: true }] },
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

  it('places the label on a turned workpiece, and on one recorded before pieces turned', () => {
    const piece = { x: 10, y: 20, width: 60, height: 40 };
    const turned: LaserPlacement = { workpiece: piece, angle_deg: 9, placed: { x: 13, y: 23, width: 50, height: 20 }, label: { x: 12, y: 18, width: 53, height: 28 }, mirror: false, cuts: 0 };
    expect(placedOf(turned)).toEqual({ x: 13, y: 23, width: 50, height: 20 });
    expect(turnOf(turned)).toBe('rotate(9 40 40)');
    const before: LaserPlacement = { workpiece: piece, label: { x: 15, y: 30, width: 50, height: 20 }, mirror: false, cuts: 1 };
    expect(placedOf(before)).toEqual(before.label);
    expect(turnOf(before)).toBe('');
  });

  it('says where the label goes, and how the camera is calibrated', () => {
    expect(cornerName('bottom_right')).toBe('bottom right corner');
    expect(cornerName(undefined)).toBe('center');
    expect(scrapText({ marks: [], diameter_mm: 4, origin_diameter_mm: 6.4, scrap: [32.8, 26.8, 134.4, 96.4] })).toBe(
      'Lay light scrap at least 134.4 × 96.4 mm with its top left corner 32.8 mm from the left of the work area and 26.8 mm from its top',
    );
    expect(calibrationText(null)).toMatch(/^Not calibrated/);
    const c = { printer_id: 'p', model: 'panel', image_width: 800, image_height: 600, z_mm: 3, marks: 9, rms_mm: 0.12, max_mm: 0.2, created_at: '2026-11-11T12:00:00Z' } as LaserCalibration;
    expect(calibrationText(c)).toContain('for a surface 3 mm above the bed, from 9 marks fitting within 0.12 mm (0.2 mm at worst)');
    expect(heightFits(c, 3.9, 1)).toBe(true);
    expect(heightFits(c, 4.5, 1)).toBe(false);
    expect(heightFits(null, 3, 1)).toBe(false);
  });
});
