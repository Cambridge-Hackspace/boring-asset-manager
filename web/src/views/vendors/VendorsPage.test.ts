import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import VendorsPage from './VendorsPage.svelte';
import { fakeApi } from '../../test/fetch';
import { router } from '../../lib/router.svelte';
import { session } from '../../lib/session.svelte';
import type { Vendor } from '../../lib/models';

const vendor = (id: string, name: string, over: Partial<Vendor> = {}): Vendor => ({
  id, name, website: '', account_number: '', address: '', notes: '', contacts: [], scope_ids: [], created_at: '', updated_at: '', ...over,
});
const acme = vendor('v1', 'Acme', {
  website: 'https://www.acme.example/',
  contacts: [
    { id: 'c1', name: 'Ada', role: 'sales', email: 'ada@acme.example', phone: '+44 1223 555010' },
    { id: 'c2', name: 'Bob', role: 'support', email: '', phone: '' },
  ],
});
const zed = vendor('v2', 'Zed Supplies');

// Opens the page at an address, as a link to it would.
function at(search: string) {
  history.replaceState(null, '', `/vendors${search}`);
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

describe('VendorsPage', () => {
  it('lists vendors, each with its website and main contact', async () => {
    fakeApi({ 'GET /vendors': () => ({ vendors: [acme, zed] }) });
    render(VendorsPage, { params: {} });
    const list = await screen.findByRole('list', { name: 'Vendors' });
    const [first, second] = within(list).getAllByRole('listitem');
    expect(within(first!).getByRole('link', { name: 'Acme' }).getAttribute('href')).toBe('/vendors/v1');
    const site = within(first!).getByRole('link', { name: 'acme.example' });
    expect([site.getAttribute('href'), site.getAttribute('rel'), site.getAttribute('target')]).toEqual(['https://www.acme.example/', 'noopener noreferrer', '_blank']);
    expect(first!.textContent).toContain('Ada (sales)');
    expect(first!.textContent).not.toContain('Bob');
    expect(within(first!).getByRole('link', { name: 'ada@acme.example' }).getAttribute('href')).toBe('mailto:ada@acme.example');
    expect(within(first!).getByRole('link', { name: '+44 1223 555010' }).getAttribute('href')).toBe('tel:+441223555010');
    expect(within(second!).getAllByRole('link').map((a) => a.textContent)).toEqual(['Zed Supplies']);
    // Only those who manage vendors make them.
    expect(screen.queryByRole('link', { name: 'New vendor' })).toBeNull();
  });

  it('searches, keeping the search in the address', async () => {
    const { fetchMock } = fakeApi({ 'GET /vendors': () => ({ vendors: [] }) });
    at('?q=drill+bits');
    render(VendorsPage, { params: {} });
    expect(await screen.findByText('No vendors match.')).toBeTruthy();
    expect((screen.getByRole('searchbox', { name: 'Search' }) as HTMLInputElement).value).toBe('drill bits');
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/vendors?q=drill%20bits');

    await fireEvent.input(screen.getByRole('searchbox', { name: 'Search' }), { target: { value: ' acme ' } });
    await fireEvent.submit(screen.getByRole('search'));
    await vi.waitFor(() => expect(fetchMock.mock.calls.at(-1)![0]).toBe('/api/v1/vendors?q=acme'));
    expect(router.search).toBe('?q=acme');
  });

  it('says when there are none, and offers a new one to those who manage vendors', async () => {
    session.permissions = ['assets.read', 'tools:vendors.manage'];
    fakeApi({ 'GET /vendors': () => ({ vendors: [] }) });
    render(VendorsPage, { params: {} });
    expect(await screen.findByText('No vendors yet.')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'New vendor' }).getAttribute('href')).toBe('/vendors/new');
  });
});
