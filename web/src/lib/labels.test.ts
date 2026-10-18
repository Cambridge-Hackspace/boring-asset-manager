import { describe, expect, it } from 'vitest';
import { printersFor, sizeText, type LabelOptions } from './labels';

describe('printersFor', () => {
  const options: LabelOptions = {
    templates: [],
    template_id: 't1',
    printers: [{ id: 'p1', name: 'QL' }, { id: 'p2', name: 'Rollo' }],
    fits: { t1: { p1: { fits: true, warnings: ['Edge'] }, p2: { fits: false, reason: 'Wrong roll.', warnings: [] } } },
  };
  it('pairs each printer with how the template suits it', () => {
    expect(printersFor(options, 't1')).toEqual([
      { id: 'p1', name: 'QL', fit: { fits: true, warnings: ['Edge'] } },
      { id: 'p2', name: 'Rollo', fit: { fits: false, reason: 'Wrong roll.', warnings: [] } },
    ]);
  });
  it('does not offer a printer it knows nothing about', () => {
    expect(printersFor(options, 'gone').every((p) => !p.fit.fits)).toBe(true);
  });
});

describe('sizeText', () => {
  it('gives a fixed template its size, and an adaptive one its range', () => {
    expect(sizeText({ units: 'mm', width: 62, height: 29 })).toBe('62 × 29 mm');
    expect(sizeText({ units: 'in', width: 4, height: 6, sizing: 'adaptive', adapt: { min_width: 1, max_width: 4.5, min_height: 0.5, max_height: 6, continuous_length: 'fixed' } }))
      .toBe('any size from 1 × 0.5 to 4.5 × 6 in');
  });
});
