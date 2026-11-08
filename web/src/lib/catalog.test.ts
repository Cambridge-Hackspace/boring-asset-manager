import { describe, expect, it } from 'vitest';
import { entryHref, entryName, entryPrice, entryProblem, entryRequest, packText, preferredByThing } from './catalog';
import type { CatalogEntry, Vendor } from './models';

const entry = (over: Partial<CatalogEntry> = {}): CatalogEntry => ({
  id: 'e1',
  vendor_id: 'v1',
  vendor_name: 'Acme',
  category_id: 'c1',
  linked_name: 'Screws',
  part_number: 'SC-100',
  description: 'M3 screws',
  url: '',
  pack_size: 100,
  pack_unit: 'screws',
  price: '4.50',
  currency: 'GBP',
  preferred: false,
  can_manage: false,
  created_at: '',
  updated_at: '',
  ...over,
});

const vendor: Vendor = { id: 'v1', name: 'Acme', website: 'https://acme.example', account_number: '', address: '', notes: '', contacts: [], scope_ids: [], created_at: '', updated_at: '' };

describe('catalog entries', () => {
  it('say their pack and price', () => {
    expect(packText(entry())).toBe('100 screws');
    expect(packText(entry({ pack_size: null, pack_unit: '' }))).toBe('');
    expect(entryPrice(entry())).toBe('4.50 GBP for 100 screws');
    expect(entryPrice(entry({ pack_size: null, pack_unit: '' }))).toBe('4.50 GBP');
    expect(entryPrice(entry({ price: undefined, prices_hidden: true }))).toBe('');
    expect(entryName(entry({ part_number: '' }))).toBe('M3 screws');
  });

  it('reorder from the product page, else the vendor', () => {
    expect(entryHref(entry({ url: 'https://acme.example/sc-100' }), vendor)).toEqual({ href: 'https://acme.example/sc-100', external: true });
    expect(entryHref(entry({ url: 'javascript:alert(1)' }), vendor)).toEqual({ href: 'https://acme.example', external: true });
    expect(entryHref(entry())).toEqual({ href: '/vendors/v1', external: false });
  });

  it('find the preferred entry of each thing', () => {
    const map = preferredByThing([entry(), entry({ id: 'e2', preferred: true }), entry({ id: 'e3', category_id: undefined, asset_id: 'a1', preferred: true })]);
    expect(map.get('c1')?.id).toBe('e2');
    expect(map.get('a1')?.id).toBe('e3');
  });

  it('check an entry as typed', () => {
    const ok = entryRequest({ vendor_id: 'v1', category_id: 'c1', part_number: 'X' });
    expect(entryProblem(ok)).toBe('');
    expect(entryProblem({ ...ok, vendor_id: '' })).toContain('vendor');
    expect(entryProblem({ ...ok, category_id: '' })).toContain('category or asset');
    expect(entryProblem({ ...ok, part_number: '' })).toContain('part number');
    expect(entryProblem({ ...ok, url: 'ftp://x' })).toContain('https://');
    expect(entryProblem({ ...ok, pack_size: 0 })).toContain('at least 1');
    expect(entryProblem({ ...ok, price: '£4' })).toContain('number');
    expect(entryProblem({ ...ok, price: '4' })).toContain('currency');
    expect(entryProblem({ ...ok, price: '4', currency: 'GBP' })).toBe('');
  });
});
