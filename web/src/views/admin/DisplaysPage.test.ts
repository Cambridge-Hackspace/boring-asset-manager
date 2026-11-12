import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import DisplaysPage from './DisplaysPage.svelte';
import PrinterForm from './PrinterForm.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { DisplayToken, DisplayView, DriverInfo } from '../../lib/models';

const shelf: DisplayView = {
  id: 'd1',
  name: 'Shelf A',
  driver: 'display',
  model: '2.9in-296x128-bwr',
  connection: { type: 'pull', address: '' },
  shown: {
    printer_id: 'd1', template_id: 't1', template_name: 'Bin', asset_id: 'a1', asset_name: 'M3 screws',
    shown_at: '2026-11-09T10:00:00Z', fetched_at: null, refresh_at: '2026-11-09T10:01:00Z',
  },
  pull_url: 'https://bam.example/api/v1/displays/d1/image',
  refresh_seconds: 900,
};
const tag: DisplayView = {
  id: 'd2', name: 'Bin B1', driver: 'openepaperlink', model: '2.9in',
  connection: { type: 'openepaperlink', address: 'http://192.168.1.50?tag=0000021E3D6F1234' },
  shown: null,
};
const frame: DisplayToken = { id: 'k1', printer_id: 'd1', name: 'Frame', created_at: '2026-11-08T10:00:00Z', last_used_at: null };

beforeEach(() => {
  session.permissions = ['settings.manage', 'labels.print'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('DisplaysPage', () => {
  it('says what each display shows, makes and revokes tokens, refreshes, and keeps the settings', async () => {
    let tokens: DisplayToken[] = [frame];
    const { sent } = fakeApi({
      'GET /displays': () => ({ displays: [shelf, tag] }),
      'GET /settings/displays': () => ({ refresh_window_seconds: 30, pull_refresh_seconds: 900 }),
      'GET /displays/d1/tokens': () => ({ tokens }),
      'POST /displays/d1/tokens': (body) => {
        const t = { ...frame, id: 'k2', name: (body as { name: string }).name };
        tokens = [t, ...tokens];
        return { status: 201, body: { token: t, secret: 'bamd_abc', url: 'https://bam.example/api/v1/displays/d1/image?token=bamd_abc' } };
      },
      'DELETE /displays/d1/tokens/k1': () => {
        tokens = tokens.filter((t) => t.id !== 'k1');
        return { message: 'Display token revoked' };
      },
      'POST /displays/d1/refresh': () => ({ status: 202, body: { message: 'ok' } }),
      'PUT /settings/displays': (body) => body as object,
    });
    render(DisplaysPage, { params: {} });

    const card = within(await screen.findByRole('article', { name: 'Shelf A' }));
    expect(card.getByText(/M3 screws, with Bin, since/)).toBeTruthy();
    expect(card.getByText(/A refresh is due at/)).toBeTruthy();
    expect(card.getByText(/Has not fetched its image yet; asks again every 900 seconds/)).toBeTruthy();
    expect(card.getByText(/Frame, made .*, not used yet/)).toBeTruthy();
    const pushed = within(screen.getByRole('article', { name: 'Bin B1' }));
    expect(pushed.getByText(/Pushed to the access point at http:\/\/192.168.1.50/)).toBeTruthy();
    expect(pushed.getByText('Shown nothing yet.')).toBeTruthy();
    // A display that is pushed has no tokens, and one shown nothing has nothing to refresh.
    expect(pushed.queryByRole('button', { name: 'Make a token' })).toBeNull();
    expect(pushed.queryByRole('button', { name: /Refresh/ })).toBeNull();

    // A token is shown once, in the address to give the display.
    await fireEvent.input(card.getByLabelText('Token name'), { target: { value: 'Kitchen' } });
    await fireEvent.submit(card.getByRole('button', { name: 'Make a token' }));
    const url = (await screen.findByLabelText('The address the display fetches its image from')) as HTMLInputElement;
    expect(url.value).toBe('https://bam.example/api/v1/displays/d1/image?token=bamd_abc');
    expect(sent.find((s) => s.method === 'POST' && s.path === '/displays/d1/tokens')!.body).toEqual({ name: 'Kitchen' });

    await fireEvent.click(card.getByRole('button', { name: 'Revoke' }));
    await fireEvent.click(card.getByRole('button', { name: 'Revoke Frame' }));
    expect((await screen.findByRole('status')).textContent).toContain('Revoked Frame');

    await fireEvent.click(card.getByRole('button', { name: 'Refresh Shelf A now' }));
    await vi.waitFor(() => expect(sent.some((s) => s.method === 'POST' && s.path === '/displays/d1/refresh')).toBe(true));

    await fireEvent.input(screen.getByLabelText('Wait for the rest of a burst (seconds)'), { target: { value: '60' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(sent.find((s) => s.method === 'PUT')?.body).toEqual({ refresh_window_seconds: 60, pull_refresh_seconds: 900 }));
  });

  it('gives an OpenPaper frame its token as a password, and keeps paperlesspaper’s API key sealed', async () => {
    const openPaper: DisplayView = { ...shelf, id: 'd3', name: 'Front desk', driver: 'paperlesspaper', model: 'epd7', shown: null };
    const pushed: DisplayView = { ...tag, id: 'd4', name: 'Hall', driver: 'paperlesspaper', model: 'openpaper13', connection: { type: 'paperlesspaper', address: '65a1f0c2e4b3d5a6b7c8d9e0' } };
    let keySet = true;
    const { sent } = fakeApi({
      'GET /displays': () => ({ displays: [openPaper, pushed] }),
      'GET /settings/displays': () => ({ refresh_window_seconds: 30, pull_refresh_seconds: 900, paperlesspaper_base_url: 'https://api.paperlesspaper.de/v1', paperlesspaper_api_key_set: keySet }),
      'GET /displays/d3/tokens': () => ({ tokens: [] }),
      'POST /displays/d3/tokens': () => ({
        status: 201,
        body: { token: { ...frame, id: 'k3', name: 'Front desk' }, secret: 'bamd_xyz', url: 'https://bam.example/api/v1/displays/d3/image', username: 'bam' },
      }),
      'PUT /settings/displays': (body) => {
        const b = body as { paperlesspaper_api_key?: string };
        if (b.paperlesspaper_api_key !== undefined) keySet = b.paperlesspaper_api_key !== '';
        return { ...(body as object), paperlesspaper_api_key: undefined, paperlesspaper_api_key_set: keySet };
      },
    });
    render(DisplaysPage, { params: {} });

    const hall = within(await screen.findByRole('article', { name: 'Hall' }));
    expect(hall.getByText(/Pushed through paperlesspaper’s cloud, to paper 65a1f0c2e4b3d5a6b7c8d9e0/)).toBeTruthy();

    const desk = within(screen.getByRole('article', { name: 'Front desk' }));
    await fireEvent.submit(desk.getByRole('button', { name: 'Make a token' }));
    expect(((await screen.findByLabelText('Address')) as HTMLInputElement).value).toBe('https://bam.example/api/v1/displays/d3/image');
    expect((screen.getByLabelText('Password') as HTMLInputElement).value).toBe('bamd_xyz');
    expect(screen.getByRole('button', { name: 'Copy the password' })).toBeTruthy();

    // The key is sent only when typed, and shown only as set.
    const key = screen.getByLabelText('API key') as HTMLInputElement;
    expect(key.placeholder).toMatch(/^Set/);
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(sent.filter((s) => s.method === 'PUT')).toHaveLength(1));
    expect(sent.find((s) => s.method === 'PUT')!.body).not.toHaveProperty('paperlesspaper_api_key');
    await fireEvent.input(key, { target: { value: ' pk_live ' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(sent.filter((s) => s.method === 'PUT')).toHaveLength(2));
    expect(sent.filter((s) => s.method === 'PUT')[1]!.body).toMatchObject({ paperlesspaper_api_key: 'pk_live', paperlesspaper_base_url: 'https://api.paperlesspaper.de/v1' });
    expect(key.value).toBe('');

    await fireEvent.click(screen.getByRole('button', { name: 'Clear paperlesspaper’s API key' }));
    await vi.waitFor(() => expect(sent.filter((s) => s.method === 'PUT')).toHaveLength(3));
    expect(sent.filter((s) => s.method === 'PUT')[2]!.body).toMatchObject({ paperlesspaper_api_key: '' });
    expect(await screen.findByText('Cleared paperlesspaper’s API key.')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Clear paperlesspaper’s API key' })).toBeNull();
  });

  it('says which SoluM label a display is, and keeps the AIMS password sealed', async () => {
    const bin: DisplayView = { ...tag, id: 'd5', name: 'Bin C2', driver: 'solum', model: '2.9in-296x128-bwr', connection: { type: 'solum', address: '04507B0AC391' } };
    let passwordSet = true;
    const account = { solum_base_url: 'https://eu.common.solumesl.com', solum_company: 'TST', solum_store: 'S001', solum_username: 'ops@example.org' };
    const { sent } = fakeApi({
      'GET /displays': () => ({ displays: [bin] }),
      'GET /settings/displays': () => ({ refresh_window_seconds: 30, pull_refresh_seconds: 900, ...account, solum_password_set: passwordSet }),
      'PUT /settings/displays': (body) => {
        const b = body as { solum_password?: string };
        if (b.solum_password !== undefined) passwordSet = b.solum_password !== '';
        return { ...(body as object), solum_password: undefined, solum_password_set: passwordSet };
      },
    });
    render(DisplaysPage, { params: {} });

    const card = within(await screen.findByRole('article', { name: 'Bin C2' }));
    expect(card.getByText(/Pushed through SoluM’s AIMS, to label 04507B0AC391/)).toBeTruthy();
    expect(card.queryByRole('button', { name: 'Make a token' })).toBeNull();

    // The password is sent only when typed, and shown only as set.
    const password = screen.getByLabelText('AIMS password') as HTMLInputElement;
    expect(password.placeholder).toMatch(/^Set/);
    await fireEvent.input(screen.getByLabelText('Store code'), { target: { value: 'S002' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(sent.filter((s) => s.method === 'PUT')).toHaveLength(1));
    expect(sent.find((s) => s.method === 'PUT')!.body).toEqual({ refresh_window_seconds: 30, pull_refresh_seconds: 900, ...account, solum_store: 'S002' });
    await fireEvent.input(password, { target: { value: 's3cret' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(sent.filter((s) => s.method === 'PUT')).toHaveLength(2));
    expect(sent.filter((s) => s.method === 'PUT')[1]!.body).toMatchObject({ solum_password: 's3cret' });
    expect(password.value).toBe('');

    await fireEvent.click(screen.getByRole('button', { name: 'Clear SoluM’s password' }));
    await vi.waitFor(() => expect(sent.filter((s) => s.method === 'PUT')).toHaveLength(3));
    expect(sent.filter((s) => s.method === 'PUT')[2]!.body).toMatchObject({ solum_password: '' });
    expect(await screen.findByText('Cleared SoluM’s password.')).toBeTruthy();
  });

  it('shows those who only print what displays show, without tokens or settings', async () => {
    session.permissions = ['labels.print'];
    fakeApi({
      'GET /displays': () => ({ displays: [shelf] }),
      'GET /settings/displays': () => ({ refresh_window_seconds: 30, pull_refresh_seconds: 900 }),
    });
    render(DisplaysPage, { params: {} });
    const card = within(await screen.findByRole('article', { name: 'Shelf A' }));
    expect(card.getByRole('button', { name: 'Refresh Shelf A now' })).toBeTruthy();
    expect(card.queryByText('Tokens')).toBeNull();
    expect(screen.queryByRole('form', { name: 'Keeping displays current' })).toBeNull();
  });
});

const displayDrivers: DriverInfo[] = [
  {
    driver: 'tspl',
    models: ['Rollo X1038'],
    model_dpi: { 'Rollo X1038': 203 },
    media: [{ id: '4x6', name: '4 × 6 in', width_mm: 101.6, length_mm: 152.4, die_cut: true }],
  },
  {
    driver: 'display',
    models: ['2.9in-296x128'],
    model_dpi: { '2.9in-296x128': 112 },
    media: [{ id: '2.9in-296x128', name: 'Screen, 296 × 128', width_mm: 67.13, length_mm: 29.03, die_cut: true, models: ['2.9in-296x128'] }],
    display: { connections: ['pull', 'webhook'], formats: ['png', 'bmp'] },
  },
  {
    driver: 'trmnl',
    models: ['og'],
    model_dpi: { og: 125 },
    media: [{ id: 'og', name: 'Screen, 800 × 480', width_mm: 162.56, length_mm: 97.54, die_cut: true, models: ['og'] }],
    display: { connections: ['pull'], formats: ['png', 'bmp'] },
  },
];

describe('PrinterForm for displays', () => {
  it('offers a display only its own connections, with no address when it fetches its image', async () => {
    const { sent } = fakeApi({ 'POST /printers': () => ({ status: 201, body: {} }) });
    const onsaved = vi.fn();
    render(PrinterForm, { drivers: displayDrivers, onsaved, oncancel: () => {} });

    const type = screen.getByLabelText('Type') as HTMLSelectElement;
    expect([...type.options].map((o) => o.value)).toEqual(['tcp', 'device', 'ipp', 'windows', 'ble', 'browser']);
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Shelf' } });
    await fireEvent.change(screen.getByLabelText('Printer model'), { target: { value: 'display|2.9in-296x128' } });
    expect([...(screen.getByLabelText('Type') as HTMLSelectElement).options].map((o) => o.value)).toEqual(['pull', 'webhook']);
    expect((screen.getByLabelText('Screen') as HTMLSelectElement).value).toBe('2.9in-296x128');
    expect(screen.queryByLabelText('Address')).toBeNull();
    await fireEvent.submit(screen.getByRole('button', { name: 'Create printer' }));

    await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
    expect(sent[0]!.body).toMatchObject({ driver: 'display', model: '2.9in-296x128', media: '2.9in-296x128', connection: { type: 'pull', address: '' } });
  });

  it('asks a TRMNL its MAC address, and a webhook display its channel', async () => {
    const { sent } = fakeApi({
      'POST /printers': () => ({ status: 201, body: {} }),
      'GET /notifiers': () => ({
        channels: [
          { id: 'c1', name: 'Frame hook', driver: 'webhook', options: {}, secrets_set: {}, enabled: true, events: [] },
          { id: 'c2', name: 'Discord', driver: 'discord', options: {}, secrets_set: {}, enabled: true, events: [] },
        ],
      }),
    });
    const onsaved = vi.fn();
    render(PrinterForm, { drivers: displayDrivers, onsaved, oncancel: () => {} });
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Workshop' } });
    await fireEvent.change(screen.getByLabelText('Printer model'), { target: { value: 'trmnl|og' } });
    await fireEvent.input(screen.getByLabelText('MAC address'), { target: { value: 'AA:BB:CC:DD:EE:FF' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Create printer' }));
    await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
    expect((sent[0]!.body as { connection: object }).connection).toEqual({ type: 'pull', address: 'AA:BB:CC:DD:EE:FF' });

    cleanup();
    render(PrinterForm, { drivers: displayDrivers, onsaved, oncancel: () => {} });
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Frame' } });
    await fireEvent.change(screen.getByLabelText('Printer model'), { target: { value: 'display|2.9in-296x128' } });
    await fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'webhook' } });
    const channel = (await screen.findByLabelText('Webhook channel')) as HTMLSelectElement;
    await vi.waitFor(() => expect([...channel.options].map((o) => o.textContent)).toEqual(['Choose a channel', 'Frame hook']));
    await fireEvent.change(channel, { target: { value: 'c1' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Create printer' }));
    await vi.waitFor(() => expect(sent.filter((s) => s.method === 'POST' && s.path === '/printers')).toHaveLength(2));
    expect((sent.filter((s) => s.path === '/printers')[1]!.body as { connection: object }).connection).toEqual({ type: 'webhook', address: 'c1' });
  });
});
