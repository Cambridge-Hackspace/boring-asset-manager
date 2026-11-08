import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import OrdersPage from './OrdersPage.svelte';
import OrderPage from './OrderPage.svelte';
import ReceivePage from './ReceivePage.svelte';
import { fakeApi } from '../../test/fetch';
import { router } from '../../lib/router.svelte';
import { session } from '../../lib/session.svelte';
import type { CatalogEntry, PurchaseOrder, PurchaseOrderLine } from '../../lib/models';

const line = (over: Partial<PurchaseOrderLine>): PurchaseOrderLine => ({
  id: 'l1', part_number: 'M5-100', description: 'M5 bolts', pack_size: 100, pack_unit: 'pcs', quantity: 3, quantity_received: 0, ...over,
});
const order = (over: Partial<PurchaseOrder> = {}): PurchaseOrder => ({
  id: 'o1', number: 12, reference: 'PO-0012', vendor_id: 'v1', vendor_name: 'Acme', state: 'draft', vendor_reference: '', ordered_on: '',
  expected_on: '', received_on: '', deliver_to: 'The Hackspace', notes: '', currency: 'GBP', total: '13.50',
  lines: [line({ asset_id: 'a1', linked_name: 'Bin 3', unit_price: '4.50', total: '13.50' })], can_manage: true, created_at: '', updated_at: '', ...over,
});
const entry: CatalogEntry = {
  id: 'e2', vendor_id: 'v1', vendor_name: 'Acme', category_id: 'c1', linked_name: 'Filament', part_number: 'PLA-1K', description: 'PLA spool', url: '',
  pack_size: 1000, pack_unit: 'g', price: '20.00', currency: 'GBP', preferred: true, can_manage: true, created_at: '', updated_at: '',
};

// Opens a page at an address, as a link to it would.
function at(path: string) {
  history.replaceState(null, '', path);
  router.path = location.pathname;
  router.search = location.search;
}

beforeEach(() => {
  session.permissions = ['assets.read', 'vendors.manage', 'stock.record', 'assets.write'];
  session.user = { id: 'u1', email: 'ada@example.org', name: 'Ada' } as typeof session.user;
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  session.user = null;
});

describe('OrdersPage', () => {
  it('lists orders with their state and total, marking those overdue, and filters by state', async () => {
    const late = order({ id: 'o2', reference: 'PO-0013', state: 'ordered', expected_on: '2000-01-01' });
    const { fetchMock } = fakeApi({ 'GET /purchase-orders': () => ({ orders: [late, order()] }) });
    at('/orders');
    render(OrdersPage, { params: {} });
    const table = await screen.findByRole('table', { name: 'Orders' });
    const rows = within(table).getAllByRole('row').slice(1);
    expect(within(rows[0]!).getAllByRole('cell').map((c) => c.textContent?.trim())).toEqual(['PO-0013', 'Acme', 'Ordered', '', '2000-01-01(overdue)', '13.50 GBP']);
    expect(within(rows[1]!).getByRole('link', { name: 'PO-0012' }).getAttribute('href')).toBe('/orders/o1');
    await fireEvent.change(screen.getByLabelText('State'), { target: { value: 'open' } });
    await vi.waitFor(() => expect(fetchMock.mock.calls.at(-1)![0]).toBe('/api/v1/purchase-orders?state=open'));
    expect(location.search).toBe('?state=open');
    expect(screen.getByRole('link', { name: 'New order' }).getAttribute('href')).toBe('/orders/new');
  });
});

describe('OrderPage', () => {
  it('shows a draft, places it, and offers the PDF', async () => {
    const { sent } = fakeApi({
      'GET /vendors': () => ({ vendors: [{ id: 'v1', name: 'Acme' }] }),
      'GET /catalog': () => ({ entries: [entry] }),
      'GET /purchase-orders/o1': () => ({ order: order() }),
      'GET /purchase-orders/o1/history': () => ({ events: [{ id: 'h1', kind: 'purchase_order.created', created_at: '2026-11-08T00:00:00Z', payload: {} }], more: false }),
      'POST /purchase-orders/o1/place': () => ({ order: order({ state: 'ordered', ordered_on: '2026-11-08' }) }),
    });
    at('/orders/o1');
    render(OrderPage, { params: { id: 'o1' } });
    const table = await screen.findByRole('table', { name: 'Lines' });
    expect(within(within(table).getAllByRole('row')[1]!).getAllByRole('cell').map((c) => c.textContent?.trim())).toEqual([
      'M5-100 M5 bolts', 'Bin 3', '100 pcs', '3', '0', '4.50 GBP', '13.50 GBP',
    ]);
    expect(screen.getByRole('link', { name: 'PDF' }).getAttribute('href')).toBe('/api/v1/purchase-orders/o1/export.pdf');
    expect(screen.queryByRole('link', { name: 'Receive…' })).toBeNull();
    await fireEvent.click(screen.getByRole('button', { name: 'Place order' }));
    expect(await screen.findByText('Placed with the vendor.')).toBeTruthy();
    expect(sent.some((r) => r.method === 'POST' && r.path === '/purchase-orders/o1/place')).toBe(true);
    expect(screen.getByRole('link', { name: 'Receive…' }).getAttribute('href')).toBe('/orders/o1/receive');
  });

  it('adds a line from the vendor’s catalog, with its price, and saves the lines', async () => {
    const { sent } = fakeApi({
      'GET /vendors': () => ({ vendors: [{ id: 'v1', name: 'Acme' }] }),
      'GET /catalog': () => ({ entries: [entry] }),
      'GET /purchase-orders/o1': () => ({ order: order() }),
      'GET /purchase-orders/o1/history': () => ({ events: [], more: false }),
      'PUT /purchase-orders/o1': () => ({ order: order() }),
    });
    at('/orders/o1');
    render(OrderPage, { params: { id: 'o1' } });
    await fireEvent.click(await screen.findByRole('button', { name: 'Edit' }));
    await fireEvent.change(screen.getByLabelText('From the vendor’s catalog'), { target: { value: 'e2' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Add from catalog' }));
    expect((screen.getByLabelText('Price a pack', { selector: '#line-1-price' }) as HTMLInputElement).value).toBe('20.00');
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'PUT')).toBe(true));
    const put = sent.find((r) => r.method === 'PUT')!.body as { lines: unknown[] };
    expect(put.lines).toEqual([
      { id: 'l1', part_number: 'M5-100', description: 'M5 bolts', pack_size: 100, pack_unit: 'pcs', quantity: 3, asset_id: 'a1', unit_price: '4.50' },
      { catalog_entry_id: 'e2', part_number: 'PLA-1K', description: 'PLA spool', pack_size: 1000, pack_unit: 'g', quantity: 1, unit_price: '20.00' },
    ]);
  });

  it('starts orders from what is running low, a draft for each vendor', async () => {
    const { sent } = fakeApi({
      'GET /purchase-orders/suggestions': () => ({
        vendors: [
          { vendor_id: 'v1', vendor_name: 'Acme', lines: [{ asset_id: 'a1', name: 'Bin 3', level: 50, threshold: 200, unit: 'pcs', catalog_entry_id: 'e1', part_number: 'M5-100', quantity: 2 }] },
          { vendor_id: 'v2', vendor_name: 'Corner shop', lines: [{ category_id: 'c2', name: 'Glue', level: 0, threshold: 2, catalog_entry_id: 'e3', quantity: 2 }] },
        ],
        on_order: [{ category_id: 'c1', name: 'Filament', level: 1000, threshold: 3000, quantity: 0, order: { id: 'o9', reference: 'PO-0009', state: 'ordered' } }],
        without_entry: [{ asset_id: 'a5', name: 'Solder', level: 0, threshold: null, quantity: 0 }],
      }),
      'POST /purchase-orders': () => ({ status: 201, body: { order: order() } }),
    });
    at('/orders/new?from=running-low');
    render(OrderPage, { params: {} });
    expect(await screen.findByRole('link', { name: 'On order: PO-0009' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Solder' }).getAttribute('href')).toBe('/assets/a5');
    // The glue is left out.
    await fireEvent.click(screen.getByRole('checkbox', { name: /Glue/ }));
    await fireEvent.input(screen.getByLabelText('Packs', { selector: '#start-v1-0' }), { target: { value: '3' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Create drafts' }));
    await vi.waitFor(() => expect(location.pathname).toBe('/orders'));
    expect(sent.filter((r) => r.method === 'POST').map((r) => r.body)).toEqual([{ vendor_id: 'v1', lines: [{ catalog_entry_id: 'e1', quantity: 3 }] }]);
  });
});

describe('ReceivePage', () => {
  it('receives what came: restocking the bin, and making spools like the newest', async () => {
    const placed = order({
      state: 'partly_received',
      lines: [
        line({ asset_id: 'a1', linked_name: 'Bin 3', quantity_received: 1 }),
        line({ id: 'l2', category_id: 'c1', linked_name: 'Filament', part_number: 'PLA-1K', description: 'PLA spool', pack_size: 1000, pack_unit: 'g', quantity: 2 }),
        line({ id: 'l3', part_number: '', description: 'Delivery', pack_size: null, pack_unit: '', quantity: 1 }),
      ],
    });
    const { sent } = fakeApi({
      'GET /purchase-orders/o1': () => ({ order: placed }),
      'GET /assets/a1': () => ({ asset: { asset: { id: 'a1', name: 'Bin 3', stock: { quantity: 5, unit: 'pcs' } } } }),
      'GET /categories/c1': () => ({ category: { id: 'c1', name: 'Filament', is_consumable: true, unit: 'g' } }),
      'GET /assets': () => ({
        assets: [
          { asset: { id: 's1', name: 'Old spool', created_at: '2026-01-01T00:00:00Z' } },
          { asset: { id: 's2', name: 'New spool', created_at: '2026-06-01T00:00:00Z' } },
        ],
      }),
      'POST /purchase-orders/o1/receive': () => ({ order: placed, created_asset_ids: ['n1', 'n2'], restocked: [] }),
    });
    at('/orders/o1/receive');
    render(ReceivePage, { params: { id: 'o1' } });
    expect(await screen.findByText('Adds 200 pcs to Bin 3.')).toBeTruthy();
    expect((screen.getByLabelText('Like', { selector: '#receive-1-like' }) as HTMLSelectElement).value).toBe('s2');
    expect((screen.getByLabelText('Each holds, in g') as HTMLInputElement).value).toBe('1000');
    // Only one pack of bolts came; the delivery did not.
    await fireEvent.input(screen.getByLabelText('Packs', { selector: '#receive-0-packs' }), { target: { value: '1' } });
    expect(screen.getByText('Adds 100 pcs to Bin 3.')).toBeTruthy();
    await fireEvent.click(within(screen.getByRole('group', { name: /Delivery/ })).getByRole('checkbox'));
    await fireEvent.click(screen.getByRole('button', { name: 'Receive' }));
    await vi.waitFor(() => expect(location.pathname).toBe('/orders/o1'));
    const post = sent.find((r) => r.method === 'POST')!.body as { lines: unknown[] };
    expect(post.lines).toEqual([
      { line_id: 'l1', quantity: 1, restock: 100 },
      { line_id: 'l2', quantity: 2, assets: 2, like: 's2', each: 1000 },
    ]);
  });
});
