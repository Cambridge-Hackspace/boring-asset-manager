import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/svelte';
import Home from './Home.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';
import type { Category, Vendor } from '../lib/models';

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

function api(categories: Category[], vendors: Vendor[] = []) {
  return fakeApi({
    'GET /auth/me/favorites': () => ({ favorites: [] }),
    'GET /loans': () => ({ loans: [] }),
    'GET /categories': () => ({ categories }),
    'GET /assets': () => ({ assets: [] }),
    'GET /vendors': () => ({ vendors }),
  });
}

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

  it('reads no vendors when no running-low category names one', async () => {
    const { sent } = api([cat('glue', 'Glue'), cat('tape', 'Tape', { is_running_low: false, vendor_ids: ['v1'] })]);
    render(Home, { params: {} });
    await screen.findByRole('link', { name: 'Glue' });
    expect(sent.some((r) => r.path === '/vendors')).toBe(false);
  });
});
