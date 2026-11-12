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
