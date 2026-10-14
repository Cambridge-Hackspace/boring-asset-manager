import { describe, expect, it } from 'vitest';
import { describeProblem, duplicateTargets, exportUrl, importForm, summarize, targetOptions } from './importing';
import type { Field, ImportPlan } from './models';

const field = (key: string, label: string): Field => ({ id: key, key, label, validation_type: 'text', created_at: '', updated_at: '' });

describe('importing helpers', () => {
  it('lists targets by group, fields by label', () => {
    const opts = targetOptions([field('serial', 'Serial'), field('color', 'Color')], [
      { type: 'qr', name: 'QR Code', error_correction_levels: [], code_rules: '' },
    ]);
    expect(opts.map((o) => o.value)).toEqual(['id', 'name', 'categories', 'code.qr', 'field.color', 'field.serial']);
    expect(opts.find((o) => o.value === 'field.serial')).toMatchObject({ label: 'Serial (serial)', group: 'Fields' });
  });

  it('builds the export address from the applied filters', () => {
    expect(exportUrl('', '')).toBe('/api/v1/assets/export.csv');
    expect(exportUrl(' drill ', 'c 1')).toBe('/api/v1/assets/export.csv?search=drill&category_id=c+1');
  });

  it('sends the mapping only when there is one', () => {
    const file = new File(['name\nSaw\n'], 'a.csv');
    expect(importForm(file).has('mapping')).toBe(false);
    expect(importForm(file, ['name']).get('mapping')).toBe('["name"]');
    expect(importForm(file).get('file')).toBeInstanceOf(File);
  });

  it('finds columns sent to the same place', () => {
    expect([...duplicateTargets(['name', 'ignore', 'ignore', '', '', 'field.x', 'name'])]).toEqual(['name']);
  });

  it('describes problems and plans', () => {
    expect(describeProblem({ row: 1, column: 'Colour', message: 'not mapped' })).toBe('Header, Colour: not mapped');
    expect(describeProblem({ row: 7, message: 'too short' })).toBe('Line 7: too short');
    const plan = { rows: 1, create: 1, update: 0, unchanged: 0 } as ImportPlan;
    expect(summarize(plan)).toBe('1 row: 1 new, 0 updated, 0 unchanged.');
  });
});
