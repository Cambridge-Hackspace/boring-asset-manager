import { describe, expect, it } from 'vitest';
import type { DriverInfo, LabelStockSize } from './models';
import { describeChanges, describeStock, driverName, mediaFor, optionLabel, optionValueLabel, stockFor } from './stock';

const size = (name: string, width_mm: number, length_mm = 0, gap_mm = 0): LabelStockSize => ({ id: name, name, width_mm, length_mm, gap_mm });

describe('describeStock', () => {
  it('says the size, the gap, or that the roll is continuous', () => {
    expect(describeStock(size('Bins', 60, 40, 3))).toBe('60 × 40 mm, 3 mm gap');
    expect(describeStock(size('Tags', 25.4, 12.7))).toBe('25.4 × 12.7 mm');
    expect(describeStock(size('Tape', 50))).toBe('50 mm continuous');
  });
});

describe('stockFor', () => {
  const tspl: DriverInfo = {
    driver: 'tspl', models: ['Rollo X1038'], media: [],
    custom_stock: { max_width_mm: { 'Rollo X1038': 105.6 }, min_length_mm: 10, max_length_mm: 1000 },
  };
  const brother: DriverInfo = { driver: 'brother_ql', models: ['QL-810W'], media: [] };
  const stock = [size('Bins', 60, 40, 3), size('Wide', 150, 100)];

  it('offers what the model is wide enough for', () => {
    expect(stockFor(tspl, 'Rollo X1038', stock).map((s) => s.name)).toEqual(['Bins']);
  });

  it('offers nothing to drivers that take only their own rolls, or unknown models', () => {
    expect(stockFor(brother, 'QL-810W', stock)).toEqual([]);
    expect(stockFor(tspl, 'Rollo X9999', stock)).toEqual([]);
    expect(stockFor(undefined, 'Rollo X1038', stock)).toEqual([]);
  });
});

describe('mediaFor', () => {
  const brother: DriverInfo = {
    driver: 'brother_ql', models: ['QL-810W', 'QL-1100'],
    media: [
      { id: 'dk-22205', name: 'DK-22205 62 mm continuous', width_mm: 62, die_cut: false },
      { id: 'dk-11241', name: 'DK-11241 102 × 152 mm die-cut', width_mm: 102, length_mm: 152, die_cut: true, models: ['QL-1100'] },
    ],
  };

  it('offers the rolls that name the model, or name none', () => {
    expect(mediaFor(brother, 'QL-1100').map((m) => m.id)).toEqual(['dk-22205', 'dk-11241']);
    expect(mediaFor(brother, 'QL-810W').map((m) => m.id)).toEqual(['dk-22205']);
    expect(mediaFor(undefined, 'QL-810W')).toEqual([]);
  });
});

describe('describeChanges', () => {
  it('says what a change would affect, or nothing', () => {
    expect(describeChanges({ stranded: [], categories: [], pools: [] })).toEqual([]);
    expect(
      describeChanges({
        stranded: [
          { template_id: 't', template_name: 'Bin tag', destination: 'printer:a', target: 'Front desk', labels: 2 },
          { template_id: 't', template_name: 'Bin tag', destination: 'pool:b', target: 'Bench', labels: 1 },
          { template_id: 't', template_name: 'Bin tag', destination: 'printer:a', target: 'Front desk', labels: 4, move_to: 'w', move_to_name: 'Wide tag' },
        ],
        categories: [{ id: 'c1', name: 'Tools' }, { id: 'c2', name: 'Bins' }],
        pools: [{ id: 'b', name: 'Bench' }],
      }),
    ).toEqual([
      '2 labels waiting for Front desk with Bin tag would no longer fit.',
      '1 label waiting for Bench with Bin tag would no longer fit.',
      '4 labels waiting for Front desk with Bin tag would move to Wide tag, their category’s next template that fits.',
      'No printer would suit the label template of the categories Tools, Bins.',
      'The printers in Bench would no longer be loaded alike.',
    ]);
  });
});

describe('printer words', () => {
  it('names drivers and their options as people know them', () => {
    expect(driverName('brother_ql')).toBe('Brother QL');
    expect(driverName('brother_td')).toBe('Brother TD');
    expect(driverName('tspl')).toBe('TSPL');
    expect(driverName('someday_driver')).toBe('someday_driver');
    expect(optionLabel('cut')).toBe('Cut after each label');
    expect(optionLabel('density')).toBe('Darkness');
    expect(optionLabel('print_speed')).toBe('Print speed');
    expect(driverName('zpl')).toBe('Zebra ZPL');
    expect(optionValueLabel('tear_off')).toBe('Tear it off');
    expect(driverName('brother_ptouch')).toBe('Brother P-touch');
    expect(optionLabel('cut_after')).toBe('Cut the tape off');
    expect(optionValueLabel('last_label')).toBe('After the last label');
    expect(optionValueLabel('right_side')).toBe('right side');
    expect(driverName('xtool')).toBe('xTool');
    expect(optionLabel('allow_unknown_firmware')).toContain('firmware');
    expect(driverName('grbl')).toBe('GRBL laser');
    expect(driverName('ruida')).toBe('Ruida laser controller');
    expect(optionLabel('work_width_mm')).toBe('Work area, across (mm)');
    expect(optionValueLabel('bottom_left')).toBe('Front left');
    expect(driverName('escpos')).toBe('ESC/POS');
    expect(driverName('phomemo')).toBe('Phomemo');
    expect(optionLabel('head_dots')).toBe('Print head width (dots)');
    expect(optionValueLabel('partial')).toBe('Partly, leaving a tab');
  });
});
