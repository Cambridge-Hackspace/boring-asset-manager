import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import VendorPage from './VendorPage.svelte';
import { fakeApi } from '../../test/fetch';
import { router } from '../../lib/router.svelte';
import { session } from '../../lib/session.svelte';
import type { Category, Scope, Vendor } from '../../lib/models';
import type { VendorEvent } from '../../lib/vendors';

const acme: Vendor = {
  id: 'v1',
  name: 'Acme',
  website: 'https://acme.example',
  account_number: 'ACC-42',
  address: '1 High Street\nCambridge',
  notes: 'Ask for **trade** prices. [Price list](https://acme.example/prices) <script>alert(1)</script> [Bad](javascript:alert`1`)',
  contacts: [
    { id: 'c1', name: 'Ada', role: 'sales', email: 'ada@acme.example', phone: '+44 1223 555010' },
    { id: 'c2', name: 'Bob', role: 'support', email: 'bob@acme.example', phone: '' },
  ],
  scope_ids: ['w'],
  created_at: '',
  updated_at: '',
};
const cat = (id: string, name: string, vendorIds: string[] = []): Category => ({
  id, name, parent_id: null, color_hex: '#1d4ed8', is_consumable: true, unit: '', is_lendable: false, is_running_low: false, vendor_ids: vendorIds, created_at: '', updated_at: '',
});
const scopes: Scope[] = [
  { id: 'd', name: 'default', created_at: '', reserved: true, category_count: 0, role_count: 0, assignment_count: 0 },
  { id: 'w', name: 'workshop', created_at: '', reserved: false, category_count: 1, role_count: 0, assignment_count: 0 },
  { id: 'k', name: 'kitchen', created_at: '', reserved: false, category_count: 0, role_count: 0, assignment_count: 0 },
];
const events: VendorEvent[] = [
  { id: 'e2', kind: 'vendor.edited', created_at: '2026-11-05T10:00:00Z', payload: { actor: { id: 'u1', email: 'ada@example.org', name: 'Ada' }, changes: [{ what: 'account_number', from: 'ACC-1', to: 'ACC-42' }, { what: 'notes' }] } },
  { id: 'e1', kind: 'vendor.created', created_at: '2026-11-04T10:00:00Z', payload: { changes: [{ what: 'name', to: 'Acme' }] } },
];

// Opens the page at a tab, as a link to it would.
function at(tab: string) {
  history.replaceState(null, '', tab ? `/?tab=${tab}` : '/');
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

function api(canManage: boolean, extra: Record<string, (b: unknown) => unknown> = {}) {
  return fakeApi({
    'GET /vendors/v1': () => ({ vendor: acme, can_manage: canManage }),
    'GET /vendors/v1/history': () => ({ events, more: false }),
    'GET /vendors/v1/attachments': () => ({ attachments: [], limits: { max_file_bytes: 1, max_image_bytes: 1, max_per_object: 3, allowed_types: [] } }),
    'GET /categories': () => ({ categories: [cat('paint', 'Paint', ['v1']), cat('glue', 'Glue', ['v2']), cat('tape', 'Tape', ['v2', 'v1'])] }),
    'GET /scopes': () => ({ scopes }),
    ...extra,
  });
}

describe('VendorPage', () => {
  it('shows its details, contacts, safe notes and the categories it is preferred for', async () => {
    api(false);
    render(VendorPage, { params: { id: 'v1' } });
    const tabs = await screen.findByRole('navigation', { name: 'Parts of this vendor' });
    // Those who do not manage vendors, categories or users see no scopes.
    expect(within(tabs).getAllByRole('link').map((a) => a.textContent?.trim())).toEqual(['Details', 'Attachments', 'History']);

    const site = screen.getByRole('link', { name: 'https://acme.example' });
    expect([site.getAttribute('href'), site.getAttribute('rel'), site.getAttribute('target')]).toEqual(['https://acme.example', 'noopener noreferrer', '_blank']);
    expect(screen.getByText('ACC-42')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'ada@acme.example' }).getAttribute('href')).toBe('mailto:ada@acme.example');
    expect(screen.getByRole('link', { name: '+44 1223 555010' }).getAttribute('href')).toBe('tel:+441223555010');

    // Notes are Markdown, with nothing written becoming markup or a script link.
    const notes = screen.getByRole('heading', { name: 'Notes' }).closest('section')!;
    expect(within(notes).getByText('trade').tagName).toBe('STRONG');
    expect(within(notes).getByRole('link', { name: 'Price list' }).getAttribute('href')).toBe('https://acme.example/prices');
    expect(within(notes).queryByRole('link', { name: 'Bad' })).toBeNull();
    expect(notes.querySelector('script')).toBeNull();
    expect(notes.textContent).toContain('<script>alert(1)</script>');

    const preferred = screen.getByRole('heading', { name: 'Preferred for' }).closest('section')!;
    expect(within(preferred).getAllByRole('link').map((a) => a.textContent?.trim())).toEqual(['Paint', 'Tape']);
    // Only those who may change it are offered to.
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull();
  });

  it('shows its history, as an asset’s', async () => {
    api(false);
    at('history');
    render(VendorPage, { params: { id: 'v1' } });
    const section = (await screen.findByRole('heading', { name: 'History' })).closest('section')!;
    const items = within(section).getAllByRole('listitem');
    expect(items[0]!.textContent).toContain('Edited');
    expect(items[0]!.textContent).toContain('Account number: ACC-1 → ACC-42');
    expect(items[0]!.textContent).toContain('Notes changed');
    expect(items[0]!.textContent).toContain('Ada');
    expect(section.textContent).toContain('Name: Acme');
  });

  it('edits its details and contacts: adding, reordering and removing them, keeping their IDs', async () => {
    session.permissions = ['assets.read', 'vendors.manage'];
    const { sent } = api(true, { 'PUT /vendors/v1': (body) => ({ vendor: { ...acme, ...(body as object) } }) });
    render(VendorPage, { params: { id: 'v1' } });
    await fireEvent.click(await screen.findByRole('button', { name: 'Edit' }));
    await fireEvent.input(screen.getByLabelText('Account number (optional)'), { target: { value: 'ACC-43' } });
    expect(await screen.findByText(/Not saved yet: its details\./)).toBeTruthy();

    await fireEvent.click(screen.getByRole('button', { name: 'Add a contact' }));
    const form = screen.getByRole('form', { name: 'Its details' });
    const names = () => within(form).getAllByLabelText('Name').slice(1) as HTMLInputElement[];
    await fireEvent.input(names()[2]!, { target: { value: 'Cy' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Move Cy up' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Remove Ada' }));
    expect(names().map((n) => n.value)).toEqual(['Cy', 'Bob']);

    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    await screen.findByText('Saved.');
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({
      name: 'Acme',
      website: 'https://acme.example',
      account_number: 'ACC-43',
      address: '1 High Street\nCambridge',
      notes: acme.notes,
      contacts: [
        { name: 'Cy', role: '', email: '', phone: '' },
        { id: 'c2', name: 'Bob', role: 'support', email: 'bob@acme.example', phone: '' },
      ],
      scope_ids: ['w'],
    });
    // Saved, it shows as it is again.
    expect(screen.queryByRole('form', { name: 'Its details' })).toBeNull();
    expect(screen.queryByText(/Not saved yet/)).toBeNull();
  });

  it('puts it in scopes, leaving the default out, and saves them alone', async () => {
    session.permissions = ['assets.read', 'vendors.manage'];
    const { sent } = api(true, { 'PUT /vendors/v1': (body) => ({ vendor: { ...acme, ...(body as object) } }) });
    at('scopes');
    render(VendorPage, { params: { id: 'v1' } });
    const section = (await screen.findByRole('heading', { name: 'Who can see it' })).closest('section')!;
    const boxes = within(section).getAllByRole('checkbox') as HTMLInputElement[];
    expect(boxes.map((b) => [b.parentElement!.textContent!.trim(), b.checked])).toEqual([['workshop', true], ['kitchen', false]]);
    await fireEvent.click(within(section).getByRole('checkbox', { name: 'kitchen' }));
    await fireEvent.submit(within(section).getByRole('button', { name: 'Save scopes' }));
    await screen.findByText('Scopes saved.');
    expect(sent.find((r) => r.method === 'PUT')!.body).toMatchObject({ name: 'Acme', contacts: acme.contacts, scope_ids: ['w', 'k'] });
  });

  it('shows its scopes, but not to change, to a category manager who cannot change it', async () => {
    session.permissions = ['assets.read', 'categories.manage'];
    api(false);
    at('scopes');
    render(VendorPage, { params: { id: 'v1' } });
    const section = (await screen.findByRole('heading', { name: 'Who can see it' })).closest('section')!;
    expect((within(section).getAllByRole('checkbox') as HTMLInputElement[]).every((b) => b.disabled)).toBe(true);
    expect(within(section).queryByRole('button')).toBeNull();
  });

  it('deletes it after asking', async () => {
    session.permissions = ['assets.read', 'vendors.manage'];
    const { sent } = api(true, { 'DELETE /vendors/v1': () => ({ message: 'Vendor deleted' }) });
    render(VendorPage, { params: { id: 'v1' } });
    await fireEvent.click(await screen.findByRole('button', { name: 'Delete' }));
    await fireEvent.click(within(screen.getByRole('group')).getByRole('button', { name: 'Delete' }));
    await vi.waitFor(() => expect(router.path).toBe('/vendors'));
    expect(sent.some((r) => r.method === 'DELETE' && r.path === '/vendors/v1')).toBe(true);
  });

  it('creates one with a contact and scopes, then opens it', async () => {
    session.permissions = ['assets.read', 'workshop:vendors.manage'];
    const { sent } = api(true, { 'POST /vendors': (body) => ({ status: 201, body: { vendor: { ...acme, ...(body as object), id: 'v9' } } }) });
    render(VendorPage, { params: {} });
    expect(await screen.findByRole('heading', { level: 1, name: 'New vendor' })).toBeTruthy();
    await fireEvent.input(await screen.findByLabelText('Name'), { target: { value: ' Zed Supplies ' } });
    await fireEvent.input(screen.getByLabelText('Website (optional)'), { target: { value: 'https://zed.example' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Add a contact' }));
    await fireEvent.input(screen.getAllByLabelText('Name')[1]!, { target: { value: 'Zoe' } });
    await fireEvent.input(screen.getByLabelText('Email (optional)'), { target: { value: 'zoe@zed.example' } });
    await fireEvent.click(screen.getByRole('checkbox', { name: 'workshop' }));
    await fireEvent.submit(screen.getByRole('button', { name: 'Create vendor' }));
    await vi.waitFor(() => expect(router.path).toBe('/vendors/v9'));
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({
      name: 'Zed Supplies',
      website: 'https://zed.example',
      account_number: '',
      address: '',
      notes: '',
      contacts: [{ name: 'Zoe', role: '', email: 'zoe@zed.example', phone: '' }],
      scope_ids: ['w'],
    });
  });

  it('says when it is not found', async () => {
    fakeApi({ 'GET /vendors/v1': () => ({ status: 404, body: { error: 'Vendor not found' } }) });
    render(VendorPage, { params: { id: 'v1' } });
    expect((await screen.findByRole('alert')).textContent).toContain('Vendor not found');
  });
});
