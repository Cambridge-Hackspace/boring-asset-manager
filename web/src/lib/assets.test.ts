import { describe, expect, it } from 'vitest';
import { assetPayload, errorTarget, inputType, isWebURL, keptValues } from './assets';
import type { EffectiveField } from './models';

const ef = (key: string, required = false): EffectiveField => ({
  field: { id: key, key, label: key, validation_type: 'text', created_at: '', updated_at: '' },
  is_required: required,
});

describe('asset form helpers', () => {
  it('maps field types to inputs', () => {
    expect(inputType('number')).toBe('number');
    expect(inputType('date')).toBe('date');
    expect(inputType('url')).toBe('url');
    expect(inputType('text')).toBe('text');
  });

  it('sends every effective field and only non-blank codes', () => {
    const body = assetPayload(
      { name: ' Drill ', categoryIds: ['tools'], codes: { qr: ' D-1 ', code128: '  ' }, values: { serial: ' 42 ', old: 'x' } },
      [ef('serial'), ef('notes')],
    );
    expect(body).toEqual({
      name: 'Drill',
      category_ids: ['tools'],
      generator_keys: [{ generator_type: 'qr', assigned_code: 'D-1' }],
      field_values: { serial: '42', notes: '' },
    });
  });

  it('sends empty lists so they clear', () => {
    expect(assetPayload({ name: 'x', categoryIds: [], codes: {}, values: {} }, [])).toMatchObject({ category_ids: [], generator_keys: [] });
  });

  it('lists values kept from former categories', () => {
    expect(keptValues({ serial: '42', old: 'x', blank: '' }, [ef('serial')])).toEqual([['old', 'x']]);
  });

  it('finds the input a server error is about', () => {
    expect(errorTarget('required field "Serial" (serial) is missing', [ef('serial')], ['qr'])).toBe('field:serial');
    expect(errorTarget('a generator code of this type is already assigned to another active asset (qr: D-1)', [], ['qr', 'micro_qr'])).toBe('code:qr');
    expect(errorTarget('numeric micro_qr capacity is 1-35 digits (got 40)', [], ['qr', 'micro_qr'])).toBe('code:micro_qr');
    expect(errorTarget('something else', [ef('serial')], ['qr'])).toBeNull();
  });
});

describe('isWebURL', () => {
  it('only accepts http and https', () => {
    expect(isWebURL('https://example.com/manual.pdf')).toBe(true);
    expect(isWebURL('http://10.0.0.1')).toBe(true);
    expect(isWebURL('javascript:alert(1)')).toBe(false);
    expect(isWebURL('data:text/html,hi')).toBe(false);
    expect(isWebURL('not a url')).toBe(false);
  });
});
