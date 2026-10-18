import { describe, expect, it } from 'vitest';
import type { DriverInfo, LabelStockSize } from './models';
import { describeStock, stockFor } from './stock';

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
