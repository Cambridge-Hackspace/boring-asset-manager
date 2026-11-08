import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/svelte';
import Home from './Home.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';
import type { AssetWithDetails, CatalogEntry, Category, PurchaseOrder, Vendor } from '../lib/models';

const cat = (id: string, name: string, over: Partial<Category> = {}): Category => ({
  id, name, parent_id: null, color_hex: '#1d4ed8', is_consumable: true, unit: '', is_lendable: false, is_running_low: true, created_at: '', updated_at: '', ...over,
});
const vendor = (id: string, name: string, website = ''): Vendor => ({
  id, name, website, account_number: '', address: '', notes: '', contacts: [], scope_ids: [], created_at: '', updated_at: '',
});

beforeEach(() => {
  session.permissions = ['assets.read'];
  session.user = { id: 'u1', email: 'ada@example.org', name: 'Ada' } as typeof session.user;
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  session.user = null;
});

function api(categories: Category[], vendors: Vendor[] = [], assets: AssetWithDetails[] = [], entries: CatalogEntry[] = [], orders: PurchaseOrder[] = []) {
  return fakeApi({
    'GET /auth/me/favorites': () => ({ favorites: [] }),
    'GET /loans': () => ({ loans: [] }),
    'GET /categories': () => ({ categories }),
    'GET /assets': () => ({ assets }),
    'GET /vendors': () => ({ vendors }),
    'GET /catalog': () => ({ entries }),
    'GET /purchase-orders': () => ({ orders }),
  });
}

const entry = (over: Partial<CatalogEntry>): CatalogEntry => ({
  id: 'e1', vendor_id: 'v1', vendor_name: 'Acme', linked_name: '', part_number: '', description: '', url: '', pack_size: null, pack_unit: '',
  preferred: true, can_manage: false, created_at: '', updated_at: '', ...over,
});

describe('Home', () => {
  it('offers to reorder a running-low category from its vendors, in order of preference', async () => {
    api(
      [cat('paint', 'Paint', { vendor_ids: ['v2', 'v1'] }), cat('glue', 'Glue'), cat('tape', 'Tape', { is_running_low: false, vendor_ids: ['v1'] })],
      [vendor('v1', 'Acme', 'https://acme.example/order'), vendor('v2', 'Corner shop')],
    );
    render(Home, { params: {} });
    const paint = await screen.findByRole('list', { name: 'Vendors of Paint' });
    expect(within(paint).getAllByRole('listitem').map((li) => li.querySelector('a')?.textContent)).toEqual(['Corner shop', 'Acme']);

    const fromAcme = within(paint).getByRole('link', { name: 'Reorder Paint from Acme' });
    expect([fromAcme.getAttribute('href'), fromAcme.getAttribute('rel'), fromAcme.getAttribute('target')]).toEqual(['https://acme.example/order', 'noopener noreferrer', '_blank']);
    // With no website, reordering starts at the vendor's page.
    const fromShop = within(paint).getByRole('link', { name: 'Reorder Paint from Corner shop' });
    expect([fromShop.getAttribute('href'), fromShop.getAttribute('target')]).toEqual(['/vendors/v2', null]);

    // A category naming no vendor shows none, and one not running low is not listed.
    expect(screen.queryByRole('list', { name: 'Vendors of Glue' })).toBeNull();
    expect(screen.queryByText('Tape')).toBeNull();
  });

  it('shows the preferred catalog entry of each running-low category and asset, with its part number, price and link', async () => {
    const solder = { asset: { id: 'a1', name: 'Solder', condition: 'ok', stock: { quantity: 1, unit: 'reels', is_running_low: true } } } as unknown as AssetWithDetails;
    const { sent } = api([cat('screws', 'Screws')], [vendor('v2', 'Corner shop')], [solder], [
      entry({ category_id: 'screws', linked_name: 'Screws', part_number: 'SC-100', price: '4.50', currency: 'GBP', pack_size: 100, pack_unit: 'screws', url: 'https://acme.example/sc-100' }),
      entry({ id: 'e2', vendor_id: 'v2', vendor_name: 'Corner shop', asset_id: 'a1', linked_name: 'Solder', description: 'Solder, 500 g' }),
    ]);
    render(Home, { params: {} });
    const screws = await screen.findByRole('link', { name: 'Reorder part SC-100 of Screws from Acme' });
    expect([screws.getAttribute('href'), screws.getAttribute('target')]).toEqual(['https://acme.example/sc-100', '_blank']);
    expect(screws.closest('p')?.textContent).toContain('4.50 GBP for 100 screws');
    // Without a product page or a website, reordering starts at the vendor's page.
    const solderLink = await screen.findByRole('link', { name: 'Reorder part Solder, 500 g of Solder from Corner shop' });
    expect(solderLink.getAttribute('href')).toBe('/vendors/v2');
    expect(sent.find((r) => r.path === '/catalog')).toBeTruthy();
  });

  it('says what is on order, and offers to start orders to those who manage vendors', async () => {
    session.permissions = ['assets.read', 'vendors.manage'];
    const solder = { asset: { id: 'a1', name: 'Solder', condition: 'ok', stock: { quantity: 1, unit: 'reels', is_running_low: true } } } as unknown as AssetWithDetails;
    const order = (id: string, number: number, state: PurchaseOrder['state'], thing: Partial<PurchaseOrder['lines'][number]>): PurchaseOrder => ({
      id, number, reference: `PO-${String(number).padStart(4, '0')}`, vendor_id: 'v1', vendor_name: 'Acme', state, vendor_reference: '', ordered_on: '',
      expected_on: '', received_on: '', deliver_to: '', notes: '', can_manage: true, created_at: '', updated_at: '',
      lines: [{ id: `l${id}`, part_number: '', description: 'x', pack_size: null, pack_unit: '', quantity: 1, quantity_received: 0, ...thing }],
    });
    const { sent } = api([cat('screws', 'Screws')], [], [solder], [], [order('o2', 2, 'draft', { asset_id: 'a1' }), order('o1', 1, 'ordered', { category_id: 'screws' })]);
    render(Home, { params: {} });
    const screws = await screen.findByRole('link', { name: 'On order: PO-0001' });
    expect(screws.getAttribute('href')).toBe('/orders/o1');
    expect(screen.getByRole('link', { name: 'In draft PO-0002' }).getAttribute('href')).toBe('/orders/o2');
    expect(screen.getByRole('link', { name: 'Start orders…' }).getAttribute('href')).toBe('/orders/new?from=running-low');
    expect(sent.find((r) => r.path === '/purchase-orders')).toBeTruthy();
  });

  it('reads no vendors when no running-low category names one', async () => {
    const { sent } = api([cat('glue', 'Glue'), cat('tape', 'Tape', { is_running_low: false, vendor_ids: ['v1'] })]);
    render(Home, { params: {} });
    await screen.findByRole('link', { name: 'Glue' });
    expect(sent.some((r) => r.path === '/vendors')).toBe(false);
  });
});
