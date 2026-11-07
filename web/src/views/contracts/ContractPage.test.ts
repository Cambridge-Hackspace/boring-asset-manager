import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import ContractPage from './ContractPage.svelte';
import { fakeApi } from '../../test/fetch';
import { router } from '../../lib/router.svelte';
import { session } from '../../lib/session.svelte';
import type { Category, Contract, Vendor } from '../../lib/models';
import type { ContractEvent } from '../../lib/contracts';

const vendor = (id: string, name: string): Vendor => ({
  id, name, website: '', account_number: '', address: '', notes: '', contacts: [], scope_ids: [], created_at: '', updated_at: '',
});
const cat = (id: string, name: string): Category => ({
  id, name, parent_id: null, color_hex: '#1d4ed8', is_consumable: false, unit: '', is_lendable: false, is_running_low: false, created_at: '', updated_at: '',
});
const upkeep: Contract = {
  id: 'k1', vendor_id: 'v1', vendor_name: 'Acme', kind: 'maintenance', name: 'Laser upkeep', reference: 'MA-9', start_date: '2026-07-01', end_date: '2099-06-30',
  auto_renews: true, notice_days: 30, notice_deadline: '2099-05-31', cost: '1200.00', currency: 'GBP', billing: 'yearly',
  notes: 'Call **first**. <script>alert(1)</script>', asset_ids: ['a1'], category_ids: ['c1'], assets: [{ id: 'a1', name: 'Laser' }],
  categories: [{ id: 'c1', name: 'Lasers' }], created_at: '', updated_at: '',
};
const events: ContractEvent[] = [
  { id: 'e2', kind: 'contract.expiring', created_at: '2026-11-05T10:00:00Z', payload: { due_at: '2099-05-31T00:00:00Z' } },
  { id: 'e1', kind: 'contract.created', created_at: '2026-11-04T10:00:00Z', payload: { actor: { id: 'u1', email: 'ada@example.org', name: 'Ada' }, changes: [{ what: 'name', to: 'Laser upkeep' }] } },
];

// Opens the page at an address, as a link to it would.
function at(search: string) {
  history.replaceState(null, '', `/contracts/k1${search}`);
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

function api(canManage: boolean, contract: Contract = upkeep, extra: Record<string, (b: unknown) => unknown> = {}) {
  return fakeApi({
    'GET /contracts/k1': () => ({ contract, can_manage: canManage }),
    'GET /contracts/k1/history': () => ({ events, more: false }),
    'GET /contracts/k1/attachments': () => ({ attachments: [], limits: { max_file_bytes: 1, max_image_bytes: 1, max_per_object: 3, allowed_types: [] } }),
    'GET /vendors': () => ({ vendors: [vendor('v1', 'Acme'), vendor('v2', 'Zed')] }),
    'GET /categories': () => ({ categories: [cat('c1', 'Lasers'), cat('c2', 'Printers')] }),
    ...extra,
  });
}

describe('ContractPage', () => {
  it('shows its details, its renewal and cost, and safe notes', async () => {
    api(false);
    render(ContractPage, { params: { id: 'k1' } });
    const tabs = await screen.findByRole('navigation', { name: 'Parts of this contract' });
    expect(within(tabs).getAllByRole('link').map((a) => a.textContent?.trim())).toEqual(['Details', 'Covers', 'Attachments', 'History']);
    const details = document.getElementById('tab-details')!;
    expect(details.textContent).toContain('Maintenance');
    expect(details.textContent).toContain('MA-9');
    expect(details.textContent).toContain('Renews automatically, unless notice is given 30 days before it ends, by 2099-05-31');
    expect(details.textContent).toContain('1200.00 GBP, yearly');
    expect(within(details).getByRole('link', { name: 'Acme' }).getAttribute('href')).toBe('/vendors/v1');
    const notes = screen.getByRole('heading', { name: 'Notes' }).closest('section')!;
    expect(within(notes).getByText('first').tagName).toBe('STRONG');
    expect(notes.querySelector('script')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
  });

  it('says its cost is hidden from those who may not see it', async () => {
    api(false, { ...upkeep, cost: undefined, currency: undefined, billing: undefined, costs_hidden: true });
    render(ContractPage, { params: { id: 'k1' } });
    await screen.findByRole('navigation', { name: 'Parts of this contract' });
    expect(document.getElementById('tab-details')!.textContent).toMatch(/Cost\s*Hidden/);
  });

  it('shows what it covers, and its history', async () => {
    api(false);
    at('?tab=covers');
    render(ContractPage, { params: { id: 'k1' } });
    const covers = (await screen.findByRole('heading', { name: 'What it covers' })).closest('section')!;
    expect(within(covers).getByRole('link', { name: 'Lasers' }).getAttribute('href')).toBe('/categories/c1');
    expect(within(covers).getByRole('link', { name: 'Laser' }).getAttribute('href')).toBe('/assets/a1');
    const history = document.getElementById('tab-history')!;
    await vi.waitFor(() => expect(history.textContent).toContain('Reminder: ends or needs notice by 2099-05-31'));
    expect(history.textContent).toContain('Name: Laser upkeep');
    expect(history.textContent).toContain('Ada');
  });

  it('edits its details, keeping what it covers', async () => {
    session.permissions = ['assets.read', 'vendors.manage'];
    const { sent } = api(true, upkeep, { 'PUT /contracts/k1': (body) => ({ contract: { ...upkeep, ...(body as object) } }) });
    render(ContractPage, { params: { id: 'k1' } });
    await fireEvent.click(await screen.findByRole('button', { name: 'Edit' }));
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: ' Laser care ' } });
    await fireEvent.change(screen.getByLabelText('Paid'), { target: { value: 'quarterly' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'PUT')).toBe(true));
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({
      vendor_id: 'v1', kind: 'maintenance', name: 'Laser care', reference: 'MA-9', start_date: '2026-07-01', end_date: '2099-06-30', auto_renews: true,
      notice_days: 30, cost: '1200.00', currency: 'GBP', billing: 'quarterly', notes: upkeep.notes,
    });
    expect(await screen.findByText('Saved.')).toBeTruthy();
  });

  it('covers assets found by a search, and saves what it covers alone', async () => {
    session.permissions = ['assets.read', 'vendors.manage'];
    const { sent } = api(true, upkeep, {
      'GET /assets': () => ({ assets: [{ asset: { id: 'a1', name: 'Laser' } }, { asset: { id: 'a3', name: 'Laser cutter' } }] }),
      'PUT /contracts/k1': (body) => ({ contract: { ...upkeep, asset_ids: (body as Contract).asset_ids, assets: [{ id: 'a1', name: 'Laser' }, { id: 'a3', name: 'Laser cutter' }] } }),
    });
    at('?tab=covers');
    render(ContractPage, { params: { id: 'k1' } });
    await fireEvent.input(await screen.findByLabelText('Find an asset'), { target: { value: 'laser' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Find' }));
    const found = await screen.findByRole('list', { name: 'Assets found' });
    // What it covers already is not offered again.
    expect(within(found).getAllByRole('listitem')).toHaveLength(1);
    await fireEvent.click(within(found).getByRole('button', { name: 'Cover Laser cutter' }));
    expect(await screen.findByText(/Not saved yet: what it covers/)).toBeTruthy();
    await fireEvent.submit(screen.getByRole('button', { name: 'Save what it covers' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'PUT')).toBe(true));
    expect(sent.find((r) => r.method === 'PUT')!.body).toMatchObject({ name: 'Laser upkeep', asset_ids: ['a1', 'a3'], category_ids: ['c1'] });
    expect(await screen.findByText('What it covers is saved.')).toBeTruthy();
  });

  it('creates one with the vendor it was opened for, then opens it', async () => {
    session.permissions = ['assets.read', 'vendors.manage'];
    history.replaceState(null, '', '/contracts/new?vendor_id=v2');
    router.path = location.pathname;
    router.search = location.search;
    const { sent } = api(true, upkeep, { 'POST /contracts': (body) => ({ status: 201, body: { contract: { ...upkeep, ...(body as object), id: 'k9' } } }) });
    render(ContractPage, { params: {} });
    expect(await screen.findByRole('heading', { level: 1, name: 'New contract' })).toBeTruthy();
    await vi.waitFor(() => expect((screen.getByLabelText('Vendor') as HTMLSelectElement).value).toBe('v2'));
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Lease' } });
    await fireEvent.change(screen.getByLabelText('Kind'), { target: { value: 'lease' } });
    await fireEvent.input(screen.getByLabelText('Ends (optional)'), { target: { value: '2028-01-31' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Create contract' }));
    await vi.waitFor(() => expect(router.path).toBe('/contracts/k9'));
    expect(sent.find((r) => r.method === 'POST')!.body).toMatchObject({ vendor_id: 'v2', kind: 'lease', name: 'Lease', end_date: '2028-01-31', asset_ids: [], category_ids: [] });
  });

  it('refuses a contract that renews automatically without an end', async () => {
    session.permissions = ['assets.read', 'vendors.manage'];
    const { sent } = api(true);
    render(ContractPage, { params: {} });
    await fireEvent.input(await screen.findByLabelText('Name'), { target: { value: 'Rolling' } });
    await fireEvent.change(screen.getByLabelText('Vendor'), { target: { value: 'v1' } });
    await fireEvent.click(screen.getByRole('checkbox', { name: /Renews automatically/ }));
    await fireEvent.submit(screen.getByRole('button', { name: 'Create contract' }));
    expect((await screen.findByRole('alert')).textContent).toContain('needs an end date');
    expect(sent.some((r) => r.method === 'POST')).toBe(false);
  });

  it('deletes it after asking', async () => {
    session.permissions = ['assets.read', 'vendors.manage'];
    const { sent } = api(true, upkeep, { 'DELETE /contracts/k1': () => ({ message: 'Contract deleted' }) });
    render(ContractPage, { params: { id: 'k1' } });
    await fireEvent.click(await screen.findByRole('button', { name: 'Delete' }));
    await fireEvent.click(within(screen.getByRole('group')).getByRole('button', { name: 'Delete' }));
    await vi.waitFor(() => expect(router.path).toBe('/contracts'));
    expect(sent.some((r) => r.method === 'DELETE' && r.path === '/contracts/k1')).toBe(true);
  });
});
