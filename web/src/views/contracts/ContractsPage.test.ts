import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import ContractsPage from './ContractsPage.svelte';
import { fakeApi } from '../../test/fetch';
import { router } from '../../lib/router.svelte';
import { session } from '../../lib/session.svelte';
import type { Contract } from '../../lib/models';

const contract = (id: string, name: string, over: Partial<Contract> = {}): Contract => ({
  id, vendor_id: 'v1', vendor_name: 'Acme', kind: 'service', name, reference: '', start_date: '', end_date: '', auto_renews: false, notice_days: 0,
  notes: '', asset_ids: [], category_ids: [], assets: [], categories: [], created_at: '', updated_at: '', ...over,
});
const soon = contract('k1', 'Laser upkeep', { kind: 'maintenance', end_date: '2099-03-31', asset_ids: ['a1', 'a2'] });
const later = contract('k2', 'CAD seats', { kind: 'license', end_date: '2099-09-30', auto_renews: true, notice_days: 30, notice_deadline: '2099-08-31', category_ids: ['c1'] });
const open = contract('k3', 'Cleaning');

// Opens the page at an address, as a link to it would.
function at(search: string) {
  history.replaceState(null, '', `/contracts${search}`);
  router.path = location.pathname;
  router.search = location.search;
}

beforeEach(() => {
  session.permissions = ['assets.read'];
  at('');
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const names = (table: HTMLElement) => within(table).getAllByRole('row').slice(1).map((r) => within(r).getAllByRole('cell')[0]!.textContent?.trim());

describe('ContractsPage', () => {
  it('lists contracts by when they end, and turns the order round', async () => {
    fakeApi({ 'GET /contracts': () => ({ contracts: [open, later, soon] }) });
    render(ContractsPage, { params: {} });
    const table = await screen.findByRole('table', { name: 'Contracts' });
    expect(names(table)).toEqual(['Laser upkeep', 'CAD seats', 'Cleaning']);
    const [, first, second, third] = within(table).getAllByRole('row');
    expect(within(first!).getAllByRole('cell').map((c) => c.textContent?.trim())).toEqual(['Laser upkeep', 'Maintenance', 'Acme', '2099-03-31', '2 assets']);
    expect(within(first!).getByRole('link', { name: 'Laser upkeep' }).getAttribute('href')).toBe('/contracts/k1');
    expect(second!.textContent).toContain('notice by 2099-08-31');
    expect(third!.textContent).toContain('No end');

    const ends = within(table).getByRole('columnheader', { name: /Ends/ });
    expect(ends.getAttribute('aria-sort')).toBe('ascending');
    await fireEvent.click(within(ends).getByRole('button'));
    expect(ends.getAttribute('aria-sort')).toBe('descending');
    expect(names(table)).toEqual(['CAD seats', 'Laser upkeep', 'Cleaning']);
    expect(location.search).toBe('?sort=desc');
    // Only those who manage vendors make contracts.
    expect(screen.queryByRole('link', { name: 'New contract' })).toBeNull();
  });

  it('searches, keeping the search in the address', async () => {
    session.permissions = ['assets.read', 'vendors.manage'];
    const { fetchMock } = fakeApi({ 'GET /contracts': () => ({ contracts: [] }) });
    at('?q=laser');
    render(ContractsPage, { params: {} });
    expect(await screen.findByText('No contracts match.')).toBeTruthy();
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/contracts?q=laser');
    expect(screen.getByRole('link', { name: 'New contract' }).getAttribute('href')).toBe('/contracts/new');
  });
});
