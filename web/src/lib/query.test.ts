import { describe, expect, it } from 'vitest';
import { buildQuery, opsFor, queryKeys, queryProblemOf, quote, writeCondition } from './query';
import type { Field } from './models';

const field = (key: string, label: string, validation_type: Field['validation_type'], options: Field['options'] = null): Field => ({
  id: key,
  key,
  label,
  validation_type,
  options,
  created_at: '',
  updated_at: '',
});

describe('query', () => {
  it('quotes only values that need it', () => {
    expect(quote('drill')).toBe('drill');
    expect(quote('HS-105')).toBe('HS-105');
    expect(quote('-30d')).toBe('-30d');
    expect(quote('shelf a')).toBe('"shelf a"');
    expect(quote('or')).toBe('"or"');
    expect(quote('a=b')).toBe('"a=b"');
    expect(quote('6" ruler')).toBe('"6\\" ruler"');
    expect(quote('C:\\tools')).toBe('C:\\tools');
    expect(quote('back\\slash "x"')).toBe('"back\\\\slash \\"x\\""');
  });

  it('writes conditions, leaving out unfinished ones', () => {
    expect(writeCondition({ key: 'name', op: '~', value: 'spool' })).toBe('name ~ spool');
    expect(writeCondition({ key: 'in', op: '=', value: 'Shelf A' })).toBe('in = "Shelf A"');
    expect(writeCondition({ key: 'assigned', op: '=', value: 'Me' })).toBe('assigned = me');
    expect(writeCondition({ key: 'field.serial', op: 'IS EMPTY', value: 'ignored' })).toBe('field.serial IS EMPTY');
    expect(writeCondition({ key: 'name', op: '~', value: '  ' })).toBe('');
    expect(
      buildQuery(
        [
          { key: 'field.diameter', op: '>=', value: '1.75' },
          { key: 'name', op: '~', value: '' },
          { key: 'condition', op: '!=', value: 'ok' },
        ],
        true,
        'field.diameter',
        true,
      ),
    ).toBe('field.diameter >= 1.75 AND condition != ok ORDER BY field.diameter DESC');
    expect(buildQuery([{ key: 'loan', op: '=', value: 'open' }, { key: 'quantity', op: '<', value: '5' }], false)).toBe('loan = open OR quantity < 5');
    expect(buildQuery([], true, 'name')).toBe('ORDER BY name');
  });

  it('offers each field by its type, with the comparisons that suit it', () => {
    const keys = queryKeys(
      [
        field('diameter', 'Diameter', 'number'),
        field('material', 'Material', 'select', { choices: [{ value: 'tpu', label: 'TPU' }] }),
        field('bought', 'Bought', 'date'),
        field('keeper', 'Keeper', 'person'),
      ],
      [],
      false,
    );
    const byKey = Object.fromEntries(keys.map((k) => [k.key, k]));
    expect(byKey['field.diameter']!.kind).toBe('number');
    expect(byKey['field.material']!.choices).toEqual([['tpu', 'TPU']]);
    expect(byKey['field.bought']!.kind).toBe('date');
    expect(byKey['field.keeper']!.kind).toBe('person');
    expect(byKey.favorite).toBeUndefined();
    expect(opsFor(byKey['field.diameter']!).map((o) => o.op)).toEqual(['=', '!=', '<', '<=', '>', '>=', 'IS EMPTY', 'IS NOT EMPTY']);
    expect(opsFor(byKey.name!).map((o) => o.op)).toEqual(['~', '=', '!=', '!~']);
    expect(queryKeys([], [], true).some((k) => k.key === 'favorite')).toBe(true);
  });

  it('reads where a query went wrong from an error', () => {
    expect(queryProblemOf({ error: 'x', query_error: { message: 'm', start: 1, end: 3 } })).toEqual({ message: 'm', start: 1, end: 3 });
    expect(queryProblemOf({ error: 'x' })).toBeNull();
    expect(queryProblemOf(undefined)).toBeNull();
  });
});
