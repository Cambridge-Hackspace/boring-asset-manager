import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import CatalogPanel from './CatalogPanel.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';
import type { CatalogEntry, Category, Vendor } from '../lib/models';

const acme: Vendor = { id: 'v1', name: 'Acme', website: 'https://acme.example', account_number: '', address: '', notes: '', contacts: [], scope_ids: [], created_at: '', updated_at: '' };
const entry = (over: Partial<CatalogEntry>): CatalogEntry => ({
  id: 'e1', vendor_id: 'v1', vendor_name: 'Acme', category_id: 'screws', linked_name: 'Screws', part_number: 'SC-100', description: 'M3 screws', url: '',
  pack_size: 100, pack_unit: 'screws', price: '4.50', currency: 'GBP', price_checked: '2026-11-01', preferred: false, can_manage: true, created_at: '', updated_at: '', ...over,
});
const cat = (id: string, name: string): Category => ({
  id, name, parent_id: null, color_hex: '#1d4ed8', is_consumable: true, unit: '', is_lendable: false, is_running_low: false, created_at: '', updated_at: '',
});

beforeEach(() => {
  session.permissions = ['assets.read', 'vendors.manage', 'purchases.read'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('CatalogPanel', () => {
  it('lists what a vendor sells, and adds an entry for a category', async () => {
    let entries = [entry({ url: 'https://acme.example/sc-100', preferred: true })];
    const { sent } = fakeApi({
      'GET /catalog': () => ({ entries }),
      'GET /categories': () => ({ categories: [cat('screws', 'Screws'), cat('glue', 'Glue')] }),
      'POST /catalog': (body) => {
        entries = [...entries, entry({ id: 'e2', category_id: 'glue', linked_name: 'Glue', part_number: 'G-1' })];
        return { status: 201, body: { entry: body } };
      },
    });
    const changed = vi.fn();
    render(CatalogPanel, { vendor: acme, canAdd: true, onchange: changed });
    const item = await screen.findByRole('listitem', { name: 'SC-100 for Screws' });
    expect(item.textContent).toContain('4.50 GBP for 100 screws, checked 2026-11-01');
    expect(item.textContent).toContain('Preferred');
    expect(within(item).getByRole('link', { name: 'Screws' }).getAttribute('href')).toBe('/categories/screws');
    expect(within(item).getByRole('link', { name: 'Product page of SC-100 for Screws' }).getAttribute('target')).toBe('_blank');
    expect(sent[0]?.path).toBe('/catalog');

    await fireEvent.click(screen.getByRole('button', { name: 'Add a catalog entry' }));
    const form = await screen.findByRole('form', { name: 'Add a catalog entry' });
    const add = within(form).getByRole('button', { name: 'Add entry' }) as HTMLButtonElement;
    await within(form).findByRole('option', { name: 'Glue' });
    await fireEvent.change(within(form).getByLabelText('A category, for things counted by items'), { target: { value: 'glue' } });
    expect(add.disabled).toBe(true);
    await fireEvent.input(within(form).getByLabelText('Part number'), { target: { value: 'G-1' } });
    await fireEvent.input(within(form).getByLabelText('Pack size (optional)'), { target: { value: '6' } });
    await fireEvent.input(within(form).getByLabelText('Price of a pack (optional)'), { target: { value: '3' } });
    await fireEvent.input(within(form).getByLabelText('Currency'), { target: { value: 'GBP' } });
    expect(add.disabled).toBe(false);
    await fireEvent.click(add);
    await screen.findByRole('listitem', { name: 'G-1 for Glue' });
    expect(sent.find((r) => r.method === 'POST')?.body).toMatchObject({ vendor_id: 'v1', category_id: 'glue', asset_id: '', part_number: 'G-1', pack_size: 6, price: '3', currency: 'GBP', preferred: false });
    expect(changed).toHaveBeenCalledWith('Catalog entry added.');
  });

  it('chooses the preferred vendor of a category, and keeps hidden prices out of the form', async () => {
    const entries = [
      entry({ preferred: true }),
      entry({ id: 'e2', vendor_id: 'v2', vendor_name: 'Corner shop', part_number: 'CS-9', price: undefined, currency: undefined, price_checked: undefined, prices_hidden: true }),
    ];
    const { sent } = fakeApi({
      'GET /catalog': () => ({ entries }),
      'PUT /catalog/e2': () => ({ entry: entries[1] }),
    });
    render(CatalogPanel, { category: { id: 'screws', name: 'Screws' } });
    const shop = await screen.findByRole('listitem', { name: 'CS-9 from Corner shop' });
    expect(shop.textContent).toContain('Its price is hidden');
    expect(within(screen.getByRole('listitem', { name: 'SC-100 from Acme' })).queryByRole('button', { name: /^Prefer/ })).toBeNull();
    await fireEvent.click(within(shop).getByRole('button', { name: 'Prefer CS-9 from Corner shop' }));
    await vi.waitFor(() => expect(sent.find((r) => r.method === 'PUT')?.body).toMatchObject({ vendor_id: 'v2', category_id: 'screws', preferred: true }));

    await fireEvent.click(within(shop).getByRole('button', { name: 'Edit CS-9 from Corner shop' }));
    const form = await screen.findByRole('form', { name: 'Change the catalog entry' });
    expect(within(form).queryByLabelText('Price of a pack (optional)')).toBeNull();
    expect(form.textContent).toContain('Its price is hidden from you, and stays as it is.');
    // No one may add without being offered to.
    expect(screen.queryByRole('button', { name: 'Add a catalog entry' })).toBeNull();
  });
});
