import { describe, expect, it } from 'vitest';
import { amount, assetPayload, contentsOf, placeLink, placeOf, describeChange, describeEvent, dueDate, endOfDay, errorTarget, inputType, isWebURL, keptValues, linkChoices, stockDraft, stockPayload } from './assets';
import type { AssetChange, AssetEvent, AssetStock, EffectiveField, RelationType } from './models';

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

describe('stock', () => {
  const stock: AssetStock = { quantity: 40, unit: 'g', low_stock_threshold: 50, is_running_low: true };

  it('starts counting with the quantity given, or 0', () => {
    expect(stockPayload({ counted: true, quantity: 200, unit: ' pcs ', threshold: 50 })).toEqual({
      quantity: 200,
      unit: 'pcs',
      low_stock_threshold: 50,
    });
    expect(stockPayload({ counted: true, quantity: null, unit: '', threshold: null })).toEqual({
      quantity: 0,
      unit: '',
      low_stock_threshold: null,
    });
  });

  it('sends a quantity on edit only when it was changed', () => {
    expect(stockPayload({ ...stockDraft(stock), unit: 'kg' }, stock)).toEqual({ unit: 'kg', low_stock_threshold: 50 });
    expect(stockPayload({ ...stockDraft(stock), quantity: null }, stock)).toEqual({ unit: 'g', low_stock_threshold: 50 });
    expect(stockPayload({ ...stockDraft(stock), quantity: 7 }, stock)).toEqual({ quantity: 7, unit: 'g', low_stock_threshold: 50 });
  });

  it('stops counting only an asset that was counted', () => {
    expect(stockPayload({ ...stockDraft(stock), counted: false }, stock)).toBeNull();
    expect(stockPayload(stockDraft(null), null)).toBeUndefined();
    const body = assetPayload({ name: 'Drill', categoryIds: [], codes: {}, values: {}, stock: stockDraft(null) }, []);
    expect('stock' in body).toBe(false);
  });

  it('treats a blank or zero threshold as none', () => {
    expect(stockPayload({ counted: true, quantity: 1, unit: '', threshold: 0 })!.low_stock_threshold).toBeNull();
  });

  it('describes quantities and history', () => {
    expect(amount(3, '')).toBe('3');
    expect(amount(3, 'pcs')).toBe('3 pcs');
    const event = (kind: string, payload: Partial<AssetEvent['payload']>): AssetEvent => ({
      id: 'e', kind, created_at: '', payload: { trigger: 'manual', ...payload },
    });
    expect(describeEvent(event('asset.quantity_changed', { quantity: 200, unit: 'pcs' }))).toBe('Started counting at 200 pcs');
    expect(describeEvent(event('asset.quantity_changed', { quantity: 40, change: -60, unit: 'g' }))).toBe('Used 60 g; 40 g left');
    expect(describeEvent(event('asset.quantity_changed', { quantity: 70, change: 30 }))).toBe('Added 30; 70 now');
    expect(describeEvent(event('asset.running_low', { trigger: 'threshold', quantity: 40, threshold: 50, unit: 'g' }))).toBe(
      'Running low: below the threshold of 50 g',
    );
    expect(describeEvent(event('asset.restocked', {}))).toBe('Marked restocked');
    expect(describeEvent(event('asset.created', { trigger: 'import' }))).toBe('Created by an import');
    expect(describeEvent(event('asset.edited', {}))).toBe('Edited');
    expect(describeEvent(event('asset.restored', {}))).toBe('Restored');
    const condition = (to: string, note?: string) =>
      describeEvent(event('asset.condition_changed', { changes: [{ what: 'condition', from: 'ok', to }], note }));
    expect(condition('out_of_service', 'Chuck slips')).toBe('Reported out of service: “Chuck slips”');
    expect(condition('needs_maintenance')).toBe('Reported as needing maintenance');
    expect(condition('ok')).toBe('Marked as working');
    expect(describeChange({ what: 'condition', from: 'out_of_service', to: 'ok' }, 'asset.condition_changed')).toBe('Was out of service');
    const printed = (label: NonNullable<AssetEvent['payload']['label']>) => describeEvent(event('asset.label_printed', { label }));
    expect(printed({ template: 'Shelf', printer: 'Front desk', copies: 1 })).toBe('Printed its label on Front desk (Shelf)');
    expect(printed({ template: 'Shelf', printer: 'Front desk', copies: 3 })).toBe('Printed 3 labels on Front desk (Shelf)');
    expect(printed({ template: 'Tag', download: true, copies: 2 })).toBe('Downloaded 2 labels (Tag)');
    expect(describeEvent(event('asset.action', { action: { key: 'identify', label: 'Identify' } }))).toBe('Identify pressed');
  });

  it('describes the changes an edit made', () => {
    const lines = (kind: string, ...changes: AssetChange[]) => changes.map((c) => describeChange(c, kind));
    expect(
      lines(
        'asset.edited',
        { what: 'name', from: 'Drill', to: 'Cordless drill' },
        { what: 'category', key: 'c1', to: 'Power tools' },
        { what: 'category', key: 'c2', from: 'Bench' },
        { what: 'field', key: 'serial', label: 'Serial', to: 'SN-1' },
        { what: 'field', key: 'serial', label: 'Serial', from: 'SN-1' },
        { what: 'code', key: 'qr', label: 'QR Code', from: 'D-1', to: 'D-2' },
        { what: 'stock', to: 'counted' },
        { what: 'unit', from: 'pcs', to: 'g' },
        { what: 'threshold', to: '5' },
      ),
    ).toEqual([
      'Name: Drill → Cordless drill',
      'Added to Power tools',
      'Removed from Bench',
      'Serial: SN-1',
      'Serial: SN-1 removed',
      'QR Code: D-1 → D-2',
      'Started counting stock',
      'Unit: pcs → g',
      'Low-stock threshold: 5',
    ]);
    expect(lines('asset.archived', { what: 'code', key: 'qr', label: 'QR Code', from: 'S-1' })).toEqual(['Released QR Code S-1']);
    expect(
      lines(
        'asset.restored',
        { what: 'code', key: 'code128', label: 'Code 128', to: 'C-1' },
        { what: 'code', key: 'qr', label: 'QR Code', from: 'S-1' },
      ),
    ).toEqual(['Got back Code 128 C-1', 'Not given back QR Code S-1: another asset has it']);
  });
});

describe('loans', () => {
  it('describes lending in history', () => {
    const event = (kind: string, payload: Partial<AssetEvent['payload']>): AssetEvent => ({ id: 'e', kind, created_at: '', payload: { trigger: 'manual', ...payload } });
    const bo = { id: 'u', email: 'bo@hackspace.org', name: 'Bo' };
    expect(describeEvent(event('asset.checked_out', { borrower: bo }))).toBe('Lent to Bo');
    // In the reader's own format, whatever their locale.
    expect(describeEvent(event('asset.checked_out', { borrower: bo, due_at: '2026-10-20T12:00:00Z' }))).toBe(`Lent to Bo, due back ${dueDate('2026-10-20T12:00:00Z')}`);
    expect(dueDate('2026-10-20T12:00:00Z')).toMatch(/20.*2026|2026.*20/);
    expect(describeEvent(event('asset.checked_in', { borrower: { id: 'u', email: 'bo@hackspace.org' } }))).toBe('Returned by Unnamed user');
    expect(describeEvent(event('asset.overdue', { borrower: bo, due_at: '2026-10-20T12:00:00Z' }))).toBe(`Overdue: Bo was due to bring it back ${dueDate('2026-10-20T12:00:00Z')}`);
  });

  it('makes a loan due at the end of the chosen day, where the person is', () => {
    const due = new Date(endOfDay('2026-10-20'));
    expect([due.getFullYear(), due.getMonth(), due.getDate(), due.getHours(), due.getMinutes()]).toEqual([2026, 9, 20, 23, 59]);
  });
});

describe('links', () => {
  it('describes link changes from the asset whose history it is', () => {
    expect(describeChange({ what: 'link', label: 'contained in', to: 'Bin A3' }, 'asset.edited')).toBe('Contained in Bin A3');
    expect(describeChange({ what: 'link', label: 'contains', from: 'M3 screws' }, 'asset.edited')).toBe('No longer contains M3 screws');
  });

  it('offers each relation from both ends, suggested ones first', () => {
    const type = (id: string, name: string, inverse: string, symmetric = false): RelationType => ({
      id, name, inverse_name: inverse, is_symmetric: symmetric, is_single: false, is_location: false, created_at: '', updated_at: '', link_count: 0,
    });
    const choices = linkChoices([type('c', 'contains', 'contained in'), type('u', 'used with', 'used with', true)], ['u']);
    expect(choices.map((c) => [c.label, c.outgoing, c.suggested])).toEqual([
      ['used with', true, true],
      ['contains', true, false],
      ['contained in', false, false],
    ]);
  });
});

describe('photos in history', () => {
  it('names the photo added or removed', () => {
    expect(describeChange({ what: 'photo', key: 'p1', to: 'front.jpg' }, 'asset.edited')).toBe('Photo added: front.jpg');
    expect(describeChange({ what: 'photo', key: 'p1', from: 'front.jpg' }, 'asset.edited')).toBe('Photo removed: front.jpg');
  });
});

describe('where things are', () => {
  const contains = { id: 'c', is_location: true } as RelationType;
  const usedWith = { id: 'u', is_location: false } as RelationType;
  const link = (id: string, type_id: string, outgoing: boolean, asset: string) => ({ id, type_id, outgoing, label: '', asset: { id: asset, name: asset } });
  const box = {
    location: [{ id: 'room', name: 'Room' }, { id: 'shelf', name: 'Shelf A' }],
    links: [link('l1', 'c', false, 'shelf'), link('l2', 'c', true, 'drill'), link('l3', 'u', true, 'saw'), link('l4', 'c', true, 'bits')],
  };
  it('finds the place it is directly in, and the link that puts it there', () => {
    expect(placeOf(box)).toEqual({ id: 'shelf', name: 'Shelf A' });
    expect(placeLink(box, [contains, usedWith])?.id).toBe('l1');
    expect(placeOf({ location: [] })).toBeNull();
  });
  it('lists what it holds', () => {
    expect(contentsOf(box, [contains, usedWith]).map((l) => l.asset.id)).toEqual(['drill', 'bits']);
  });
});
