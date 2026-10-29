import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import PrinterForm from './PrinterForm.svelte';
import UsersPage from './UsersPage.svelte';
import AccountPage from '../AccountPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { DriverInfo } from '../../lib/models';

const drivers: DriverInfo[] = [
  {
    driver: 'brother_ql',
    models: ['QL-810W', 'QL-820NWB'],
    model_dpi: { 'QL-810W': 300, 'QL-820NWB': 300 },
    media: [{ id: 'dk-11201', name: 'DK-11201', width_mm: 29, length_mm: 90, die_cut: true }],
    options: [{ name: 'cut', type: 'bool', default: true, description: 'Cut each label.' }],
  },
  {
    driver: 'tspl',
    models: ['Rollo X1038'],
    model_dpi: { 'Rollo X1038': 203 },
    media: [{ id: '4x6', name: '4 × 6 in', width_mm: 101.6, length_mm: 152.4, die_cut: true }],
    custom_stock: { max_width_mm: { 'Rollo X1038': 105.6 }, min_length_mm: 10, max_length_mm: 1000 },
    options: [
      { name: 'density', type: 'int', default: null, description: 'Darkness.' },
      { name: 'rotate_180', type: 'bool', default: false, description: 'Flip.' },
    ],
  },
];

beforeEach(() => {
  session.permissions = ['settings.manage', 'users.manage'];
  session.user = { id: 'me', email: 'me@hackspace.org', name: '', is_active: true, created_at: '', updated_at: '' };
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('PrinterForm', () => {
  it('builds its fields from the chosen driver and sends only changed options', async () => {
    const { sent } = fakeApi({ 'POST /printers': () => ({ status: 201, body: {} }) });
    const onsaved = vi.fn();
    render(PrinterForm, { drivers, onsaved, oncancel: () => {} });

    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Rollo' } });
    // One list of models, grouped by make, chooses the driver too.
    await fireEvent.change(screen.getByLabelText('Printer model'), { target: { value: 'tspl|Rollo X1038' } });
    expect(screen.getByRole('group', { name: 'TSPL' })).toBeTruthy();
    expect(screen.getByRole('option', { name: 'Rollo X1038 (203 DPI)' })).toBeTruthy();
    expect((screen.getByLabelText('Roll loaded') as HTMLSelectElement).value).toBe('4x6');
    expect(screen.queryByLabelText('cut')).toBeNull();

    await fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'device' } });
    expect(screen.getByText(/\[printers\] devices setting/)).toBeTruthy();
    await fireEvent.input(screen.getByLabelText('Address'), { target: { value: '/dev/usb/lp0' } });
    await fireEvent.input(screen.getByLabelText('Darkness'), { target: { value: '10' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Create printer' }));

    await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
    expect(sent[0]!.body).toEqual({
      name: 'Rollo',
      driver: 'tspl',
      model: 'Rollo X1038',
      media: '4x6',
      connection: { type: 'device', address: '/dev/usb/lp0' },
      options: { density: 10 },
    });
  });
});

describe('PrinterForm with custom stock', () => {
  const stock = [
    { id: 's1', name: 'Bin labels', width_mm: 60, length_mm: 40, gap_mm: 3, printers: [], created_at: '', updated_at: '' },
    { id: 's2', name: 'Banner', width_mm: 150, length_mm: 0, gap_mm: 0, printers: [], created_at: '', updated_at: '' },
  ];

  it('offers the custom stock a model takes, and none to a Brother', async () => {
    const { sent } = fakeApi({ 'POST /printers': () => ({ status: 201, body: {} }) });
    const onsaved = vi.fn();
    render(PrinterForm, { drivers, stock, onsaved, oncancel: () => {} });

    expect(screen.queryByRole('option', { name: /Bin labels/ })).toBeNull();
    expect(screen.getByText(/This driver takes only its own rolls\./)).toBeTruthy();

    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Rollo' } });
    await fireEvent.change(screen.getByLabelText('Printer model'), { target: { value: 'tspl|Rollo X1038' } });
    expect(screen.getByRole('option', { name: 'Bin labels (60 × 40 mm, 3 mm gap)' })).toBeTruthy();
    // Too wide for the Rollo's head.
    expect(screen.queryByRole('option', { name: /Banner/ })).toBeNull();
    expect(screen.getByText(/custom stock up to 105.6 mm wide/)).toBeTruthy();

    await fireEvent.change(screen.getByLabelText('Roll loaded'), { target: { value: 's1' } });
    await fireEvent.input(screen.getByLabelText('Address'), { target: { value: 'rollo.lan' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Create printer' }));
    await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
    expect((sent[0]!.body as { media: string }).media).toBe('s1');
  });
});

describe('PrinterForm changing a roll', () => {
  const printer = {
    id: 'p1', name: 'Front desk', driver: 'brother_ql', model: 'QL-810W', media: 'dk-11201',
    connection: { type: 'tcp' as const, address: 'ql.lan' }, created_at: '', updated_at: '',
  };
  const twoRolls: DriverInfo[] = [{
    ...drivers[0]!,
    media: [...drivers[0]!.media, { id: 'dk-22205', name: 'DK-22205', width_mm: 62, die_cut: false }],
  }];

  it('says what saving would affect, and saves only when asked again', async () => {
    const { sent } = fakeApi({
      'POST /printers/p1/changes': () => ({
        changes: { stranded: [{ template_id: 't', template_name: 'Bin tag', destination: 'printer:p1', target: 'Front desk', labels: 3 }], categories: [], pools: [] },
      }),
      'PUT /printers/p1': () => ({ printer }),
    });
    const onsaved = vi.fn();
    render(PrinterForm, { printer, drivers: twoRolls, onsaved, oncancel: () => {} });
    await fireEvent.change(screen.getByLabelText('Roll loaded'), { target: { value: 'dk-22205' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));

    const group = await screen.findByRole('group', { name: 'Saving this change would affect:' });
    expect(group.textContent).toContain('3 labels waiting for Front desk with Bin tag would no longer fit.');
    expect(sent.some((r) => r.method === 'PUT')).toBe(false);

    await fireEvent.submit(screen.getByRole('button', { name: 'Save anyway' }));
    await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
    expect((sent.find((r) => r.method === 'PUT')!.body as { media: string }).media).toBe('dk-22205');
  });

  it('saves at once when nothing is affected', async () => {
    const { sent } = fakeApi({
      'POST /printers/p1/changes': () => ({ changes: { stranded: [], categories: [], pools: [] } }),
      'PUT /printers/p1': () => ({ printer }),
    });
    const onsaved = vi.fn();
    render(PrinterForm, { printer, drivers: twoRolls, onsaved, oncancel: () => {} });
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
    expect(sent.map((r) => r.method)).toEqual(['POST', 'PUT']);
  });
});

describe('UsersPage', () => {
  function api(rolesPut: () => unknown) {
    return fakeApi({
      'GET /users': () => ({
        users: [
          { id: 'me', email: 'me@hackspace.org', name: '', is_active: true, created_at: '', updated_at: '', role_ids: ['admin'], roles: [{ role_id: 'admin', scope_ids: [] }] },
          { id: 'u2', email: 'bo@hackspace.org', name: 'Bo', is_active: true, created_at: '', updated_at: '', role_ids: [], roles: [] },
        ],
      }),
      'GET /roles': () => ({
        roles: [
          { id: 'admin', name: 'admin', description: '', permissions: ['users.manage'] },
          { id: 'viewer', name: 'viewer', description: '', permissions: ['assets.read'] },
        ],
      }),
      'PUT /users/me/roles': rolesPut,
      'PUT /users/u2/roles': () => ({ role_ids: ['viewer'] }),
      'PUT /users/u2/name': (body) => ({ user: { id: 'u2', email: 'bo@hackspace.org', ...(body as object) } }),
    });
  }

  it('sets roles, and explains an anti-lockout refusal', async () => {
    const { sent } = api(() => ({ status: 400, body: { error: "anti-lockout guard: operation rejected because at least one active user must retain 'users.manage'" } }));
    render(UsersPage, { params: {} });

    const bo = (await screen.findByRole('cell', { name: 'bo@hackspace.org' })).closest('tr')!;
    await fireEvent.click(within(bo).getByRole('button', { name: 'Change roles for bo@hackspace.org' }));
    await fireEvent.click(within(bo).getByRole('checkbox', { name: 'viewer' }));
    await fireEvent.click(within(bo).getByRole('button', { name: 'Save roles' }));
    await vi.waitFor(() => expect(sent.some((r) => r.path === '/users/u2/roles')).toBe(true));
    expect(sent.find((r) => r.path === '/users/u2/roles')!.body).toEqual({ roles: [{ role_id: 'viewer', scope_ids: [] }] });
    await screen.findByText('Roles updated for bo@hackspace.org.');

    const me = screen.getByRole('cell', { name: 'me@hackspace.org (you)' }).closest('tr')!;
    await fireEvent.click(within(me).getByRole('button', { name: 'Change roles for me@hackspace.org' }));
    await fireEvent.click(within(me).getByRole('checkbox', { name: 'admin' }));
    await fireEvent.click(within(me).getByRole('button', { name: 'Save roles' }));
    expect((await screen.findByRole('alert')).textContent).toContain('would remove the last one');
  });

  it('gives a role in some scopes only, saying what that leaves out', async () => {
    const { sent } = fakeApi({
      'GET /users': () => ({
        users: [{ id: 'u2', email: 'bo@hackspace.org', name: 'Bo', is_active: true, created_at: '', updated_at: '', role_ids: ['viewer'], roles: [{ role_id: 'viewer', scope_ids: ['s-tools'] }] }],
      }),
      'GET /roles': () => ({
        roles: [
          { id: 'viewer', name: 'viewer', description: '', permissions: ['assets.read'] },
          { id: 'editor', name: 'editor', description: '', permissions: ['assets.write', 'labels.print'] },
        ],
      }),
      'GET /scopes': () => ({ scopes: [{ id: 's-tools', name: 'tools' }, { id: 's-kitchen', name: 'kitchen' }] }),
      'PUT /users/u2/roles': () => ({}),
    });
    render(UsersPage, { params: {} });

    const bo = (await screen.findByRole('cell', { name: 'bo@hackspace.org' })).closest('tr')!;
    expect(bo.textContent).toContain('viewer in tools');
    await fireEvent.click(within(bo).getByRole('button', { name: 'Change roles for bo@hackspace.org' }));
    // What it holds now is ticked, and only a ticked role offers scopes.
    const viewerScopes = within(bo).getAllByRole('group', { name: 'Only in these scopes' });
    expect(viewerScopes).toHaveLength(1);
    expect((within(viewerScopes[0]!).getByRole('checkbox', { name: 'tools' }) as HTMLInputElement).checked).toBe(true);

    await fireEvent.click(within(bo).getByRole('checkbox', { name: 'editor' }));
    const editorScopes = within(bo).getAllByRole('group', { name: 'Only in these scopes' })[1]!;
    expect(editorScopes.textContent).toContain('None ticked: in every scope');
    await fireEvent.click(within(editorScopes).getByRole('checkbox', { name: 'kitchen' }));
    expect(editorScopes.textContent).toContain('Leaves out: Print labels.');

    await fireEvent.click(within(bo).getByRole('button', { name: 'Save roles' }));
    await vi.waitFor(() => expect(sent.some((r) => r.path === '/users/u2/roles')).toBe(true));
    expect(sent.find((r) => r.path === '/users/u2/roles')!.body).toEqual({
      roles: [
        { role_id: 'viewer', scope_ids: ['s-tools'] },
        { role_id: 'editor', scope_ids: ['s-kitchen'] },
      ],
    });
  });

  it('shows names, and renames someone in place', async () => {
    const { sent } = api(() => ({}));
    render(UsersPage, { params: {} });

    const bo = (await screen.findByRole('cell', { name: 'bo@hackspace.org' })).closest('tr')!;
    expect(within(bo).getByRole('cell', { name: 'Bo' })).toBeTruthy();
    await fireEvent.click(within(bo).getByRole('button', { name: 'Rename bo@hackspace.org' }));
    const input = within(bo).getByLabelText('Name for bo@hackspace.org') as HTMLInputElement;
    expect(input.value).toBe('Bo');
    await fireEvent.input(input, { target: { value: 'Bo Diddley' } });
    await fireEvent.click(within(bo).getByRole('button', { name: 'Save name' }));
    await screen.findByText('bo@hackspace.org is now shown as Bo Diddley.');
    expect(sent.find((r) => r.path === '/users/u2/name')!.body).toEqual({ name: 'Bo Diddley' });
  });
});

describe('AccountPage', () => {
  // The Assistants section reads the account's personal tokens.
  const assistants = { 'GET /auth/tokens': () => ({ tokens: [] }), 'GET /info': () => ({ mcp: true, base_url: 'https://bam.example.org' }) };
  const changes = <R extends { method: string }>(sent: R[]) => sent.filter((r) => r.method !== 'GET');

  it('refuses mismatched passwords without asking the server', async () => {
    const { sent } = fakeApi({ ...assistants });
    render(AccountPage, { params: {} });
    await fireEvent.input(screen.getByLabelText('Current password'), { target: { value: 'OldPass123!' } });
    await fireEvent.input(screen.getByLabelText('New password'), { target: { value: 'NewPass123!' } });
    await fireEvent.input(screen.getByLabelText('Repeat new password'), { target: { value: 'NewPass124!' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Change password' }));
    expect(screen.getByRole('alert').textContent).toContain('do not match');
    expect(changes(sent)).toHaveLength(0);
  });

  it('sets your own name, and shows it where you are named', async () => {
    const { sent } = fakeApi({
      ...assistants,
      'PUT /auth/name': (body) => ({ user: { ...session.user, ...(body as object) } }),
    });
    render(AccountPage, { params: {} });
    await fireEvent.input(screen.getByLabelText('Your name'), { target: { value: 'Ada Lovelace' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save name' }));
    await screen.findByText('Name saved.');
    expect(changes(sent)[0]!.body).toEqual({ name: 'Ada Lovelace' });
    expect(session.user!.name).toBe('Ada Lovelace');
  });
});
