import { describe, expect, it } from 'vitest';
import type { DriverInfo, LabelStockSize } from './models';
import { describeChanges, describeStock, driverName, optionLabel, stockFor } from './stock';

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
    expect(driverName('tspl')).toBe('TSPL');
    expect(driverName('someday_driver')).toBe('someday_driver');
    expect(optionLabel('cut')).toBe('Cut after each label');
    expect(optionLabel('density')).toBe('Darkness');
    expect(optionLabel('print_speed')).toBe('Print speed');
  });
});
