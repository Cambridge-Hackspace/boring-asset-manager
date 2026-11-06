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
  {
    driver: 'zpl',
    models: ['Zebra GK420d'],
    model_dpi: { 'Zebra GK420d': 203 },
    media: [{ id: '4x6', name: '4 × 6 in', width_mm: 101.6, length_mm: 152.4, die_cut: true }],
    custom_stock: { max_width_mm: { 'Zebra GK420d': 105.6 }, min_length_mm: 10, max_length_mm: 1000 },
    options: [
      { name: 'print_mode', type: 'string', default: null, values: ['tear_off', 'peel_off', 'cutter'], description: 'After printing.' },
      { name: 'compress', type: 'bool', default: true, description: 'Compress.' },
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
  it('reaches a printer through a relay', async () => {
    const { sent } = fakeApi({ 'POST /printers': () => ({ status: 201, body: {} }) });
    const onsaved = vi.fn();
    render(PrinterForm, { drivers, relays: ['front-desk'], onsaved, oncancel: () => {} });

    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Desk' } });
    await fireEvent.input(screen.getByLabelText('Address'), { target: { value: '192.168.1.40' } });
    await fireEvent.change(screen.getByLabelText('Reached through'), { target: { value: 'front-desk' } });
    expect(screen.getByText(/front-desk’s network/)).toBeTruthy();
    await fireEvent.submit(screen.getByRole('button', { name: 'Create printer' }));

    await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
    expect((sent[0]!.body as { connection: object }).connection).toEqual({ type: 'tcp', address: '192.168.1.40', relay: 'front-desk' });
  });

  it('reaches a Bluetooth LE printer, with the service and characteristics it gives', async () => {
    const { sent } = fakeApi({ 'POST /printers': () => ({ status: 201, body: {} }) });
    const onsaved = vi.fn();
    render(PrinterForm, { drivers, onsaved, oncancel: () => {} });

    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Pocket' } });
    expect(screen.queryByLabelText('Service')).toBeNull();
    await fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'ble' } });
    expect(screen.getByText(/\[printers\] bluetooth setting/)).toBeTruthy();
    // This driver has no profile of its own, so the fields say they are needed.
    expect((screen.getByLabelText('Service') as HTMLInputElement).placeholder).toBe('Required');
    expect(screen.getByText(/this driver has none of its own/)).toBeTruthy();
    await fireEvent.input(screen.getByLabelText('Address'), { target: { value: '7C:2F:80:A1:B2:C3' } });
    await fireEvent.input(screen.getByLabelText('Service'), { target: { value: 'fff0' } });
    await fireEvent.input(screen.getByLabelText('Write characteristic'), { target: { value: 'fff2' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Create printer' }));

    await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
    expect((sent[0]!.body as { connection: object }).connection).toEqual({ type: 'ble', address: '7C:2F:80:A1:B2:C3?service=fff0&write=fff2' });
  });

  it('shows a Bluetooth LE driver’s defaults, and keeps what the printer overrides', async () => {
    const { sent } = fakeApi({
      'POST /printers/p1/changes': () => ({ changes: { stranded: [], categories: [], pools: [] } }),
      'PUT /printers/p1': () => ({}),
    });
    const onsaved = vi.fn();
    const withBLE: DriverInfo[] = [
      { ...drivers[1]!, ble: { service: '0000fff0-0000-1000-8000-00805f9b34fb', write: '0000fff2-0000-1000-8000-00805f9b34fb', ack: true } },
    ];
    const printer = {
      id: 'p1', name: 'Pocket', driver: 'tspl', model: 'Rollo X1038', media: '4x6',
      connection: { type: 'ble' as const, address: '7C:2F:80:A1:B2:C3?write=0000ae01-0000-1000-8000-00805f9b34fb&ack=false' },
      created_at: '', updated_at: '',
    };
    render(PrinterForm, { printer, drivers: withBLE, onsaved, oncancel: () => {} });

    expect((screen.getByLabelText('Address') as HTMLInputElement).value).toBe('7C:2F:80:A1:B2:C3');
    expect((screen.getByLabelText('Service') as HTMLInputElement).placeholder).toBe('0000fff0-0000-1000-8000-00805f9b34fb');
    expect((screen.getByLabelText('Write characteristic') as HTMLInputElement).value).toBe('0000ae01-0000-1000-8000-00805f9b34fb');
    expect((screen.getByLabelText('Notify characteristic') as HTMLInputElement).placeholder).toBe('None');
    expect(screen.getByText(/blank for the driver’s own/)).toBeTruthy();
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));

    await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
    // What the form does not show, such as ack, is kept as it was.
    expect((sent.find((r) => r.method === 'PUT')!.body as { connection: object }).connection).toEqual({
      type: 'ble',
      address: '7C:2F:80:A1:B2:C3?write=0000ae01-0000-1000-8000-00805f9b34fb&ack=false',
    });
  });

  it('offers no relay where there are none', () => {
    render(PrinterForm, { drivers, onsaved: () => {}, oncancel: () => {} });
    expect(screen.queryByLabelText('Reached through')).toBeNull();
  });

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

describe('PrinterForm for a Zebra', () => {
  it('keeps the printer default for a list option until one is chosen, in words', async () => {
    const { sent } = fakeApi({ 'POST /printers': () => ({ status: 201, body: {} }) });
    const onsaved = vi.fn();
    render(PrinterForm, { drivers, onsaved, oncancel: () => {} });

    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Shipping' } });
    await fireEvent.change(screen.getByLabelText('Printer model'), { target: { value: 'zpl|Zebra GK420d' } });
    expect(screen.getByRole('group', { name: 'Zebra ZPL' })).toBeTruthy();
    const mode = screen.getByLabelText('After each label') as HTMLSelectElement;
    expect(mode.value).toBe('');
    expect(within(mode).getByRole('option', { name: 'Peel it off' })).toBeTruthy();
    await fireEvent.input(screen.getByLabelText('Address'), { target: { value: 'zebra.lan' } });
    await fireEvent.change(mode, { target: { value: 'peel_off' } });
    await fireEvent.change(mode, { target: { value: '' } });
    await fireEvent.change(mode, { target: { value: 'cutter' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Create printer' }));

    await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
    expect((sent[0]!.body as { options: object }).options).toEqual({ print_mode: 'cutter' });
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

  it('invites someone in place of setting a password, and sends an invitation again', async () => {
    const { sent } = fakeApi({
      'GET /users': () => ({ users: [{ id: 'u2', email: 'bo@hackspace.org', name: 'Bo', is_active: true, created_at: '', updated_at: '', role_ids: [], roles: [] }] }),
      'GET /roles': () => ({ roles: [] }),
      'POST /users': () => ({ status: 201, body: { user: { id: 'u3' }, invited: true } }),
      'POST /users/u2/invite': () => ({ message: 'Invitation sent to bo@hackspace.org' }),
    });
    render(UsersPage, { params: {} });
    await fireEvent.click(await screen.findByRole('button', { name: 'New user' }));
    await fireEvent.input(screen.getByLabelText('Email'), { target: { value: 'cy@hackspace.org' } });
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Cy' } });
    await fireEvent.click(screen.getByLabelText(/Email them an invitation/));
    expect(screen.queryByLabelText('Initial password')).toBeNull();
    await fireEvent.click(screen.getByRole('button', { name: 'Create user' }));
    expect(await screen.findByText('User created, and invited by email.')).toBeTruthy();
    expect(sent.find((r) => r.method === 'POST' && r.path === '/users')!.body).toEqual({ email: 'cy@hackspace.org', name: 'Cy', invite: true });

    await fireEvent.click(screen.getByRole('button', { name: 'Send bo@hackspace.org an invitation' }));
    expect(await screen.findByText('Invitation sent to bo@hackspace.org.')).toBeTruthy();
  });

  it('shows who wrong passwords hold back, and lets them sign in again at once', async () => {
    const { sent } = fakeApi({
      'GET /users': () => ({
        users: [
          { id: 'u2', email: 'bo@hackspace.org', name: 'Bo', is_active: true, created_at: '', updated_at: '', role_ids: [], roles: [], sign_in_delayed_until: '2026-11-02T12:00:00Z' },
          { id: 'u3', email: 'cy@hackspace.org', name: 'Cy', is_active: true, created_at: '', updated_at: '', role_ids: [], roles: [] },
        ],
      }),
      'GET /roles': () => ({ roles: [] }),
      'DELETE /users/u2/sign-in-delay': () => ({ message: 'They can sign in again at once' }),
    });
    render(UsersPage, { params: {} });

    const bo = (await screen.findByRole('cell', { name: 'bo@hackspace.org' })).closest('tr')!;
    expect(bo.textContent).toContain('Wrong passwords: signing in waits until');
    const cy = screen.getByRole('cell', { name: 'cy@hackspace.org' }).closest('tr')!;
    expect(within(cy).queryByRole('button', { name: /Clear sign-in delay/ })).toBeNull();

    await fireEvent.click(within(bo).getByRole('button', { name: 'Clear sign-in delay for bo@hackspace.org' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'DELETE' && r.path === '/users/u2/sign-in-delay')).toBe(true));
    await screen.findByText('bo@hackspace.org can sign in again at once.');
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

  it('finds someone by name or email', async () => {
    api(() => ({}));
    render(UsersPage, { params: {} });
    await screen.findByRole('cell', { name: 'bo@hackspace.org' });
    const find = screen.getByRole('searchbox', { name: 'Find someone' });
    await fireEvent.input(find, { target: { value: 'BO' } });
    expect(screen.queryByRole('cell', { name: /me@hackspace.org/ })).toBeNull();
    expect(screen.getByRole('cell', { name: 'bo@hackspace.org' })).toBeTruthy();
    await fireEvent.input(find, { target: { value: 'me@' } });
    expect(screen.queryByRole('cell', { name: 'bo@hackspace.org' })).toBeNull();
    expect(screen.getByRole('cell', { name: /me@hackspace.org/ })).toBeTruthy();
  });
});

describe('AccountPage', () => {
  // The Assistants section reads the account's personal tokens, and the
  // two-factor section its factors.
  const assistants = {
    'GET /auth/tokens': () => ({ tokens: [] }),
    'GET /info': () => ({ mcp: true, base_url: 'https://bam.example.org' }),
    'GET /auth/me/factors': () => ({ factors: [], available: true }),
  };
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

  it('turns off email sent to your roles, and back on', async () => {
    const { sent } = fakeApi({
      ...assistants,
      'PUT /auth/email': (body) => ({ user: { ...session.user, email_notifications: (body as { notifications: boolean }).notifications } }),
    });
    render(AccountPage, { params: {} });
    const box = screen.getByRole('checkbox', { name: 'Send me email that notification channels send to my roles' }) as HTMLInputElement;
    expect(box.checked).toBe(true);
    await fireEvent.click(box);
    await screen.findByText('Email sent to your roles will no longer reach you.');
    expect(changes(sent)[0]!.body).toEqual({ notifications: false });
    expect(session.user!.email_notifications).toBe(false);
    await fireEvent.click(box);
    await screen.findByText('Email sent to your roles will reach you.');
    expect(changes(sent)[1]!.body).toEqual({ notifications: true });
  });

  it('turns their loan reminders off and on', async () => {
    const { sent } = fakeApi({
      ...assistants,
      'PUT /auth/loan-reminders': (body) => ({ user: { ...session.user, loan_reminders: (body as { reminders: boolean }).reminders } }),
    });
    render(AccountPage, { params: {} });
    const box = screen.getByRole('checkbox', { name: 'Remind me of what I borrow, before it is due and once it is overdue' }) as HTMLInputElement;
    expect(box.checked).toBe(true);
    await fireEvent.click(box);
    await screen.findByText('You will no longer be reminded of your loans.');
    expect(changes(sent)[0]!.body).toEqual({ reminders: false });
    expect(session.user!.loan_reminders).toBe(false);
  });
});
