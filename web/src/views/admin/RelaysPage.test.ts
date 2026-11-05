import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import RelaysPage from './RelaysPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { Relay, RelayList } from '../../lib/models';

const soon = new Date(Date.now() + 3_600_000).toISOString();
const base: Relay = {
  id: 'r1',
  name: 'front-desk',
  paired: true,
  cert_expires_at: '2030-01-08T00:00:00Z',
  online: true,
  status_at: '2026-11-05T09:00:00Z',
  version: 'v1.4.0',
  address: '203.0.113.7',
  allowlist: ['192.168.1.0/24:9100'],
  transports: ['bonemesh', 'websocket'],
  printers: [
    { id: 'p1', name: 'Desk Zebra', last_job: { state: 'done', at: '2026-11-05T09:30:00Z' } },
    { id: 'p2', name: 'Shelf Rollo', last_job: { state: 'failed', error: 'relay front-desk is offline', at: '2026-11-05T08:00:00Z' } },
    { id: 'p3', name: 'New Dymo' },
  ],
  created_at: '2026-11-01T00:00:00Z',
};

function listing(relays: Relay[], more: Partial<RelayList> = {}): RelayList {
  return { relays, mesh: { name: 'bam-1a2b', created_at: '2026-11-01T00:00:00Z' }, bonemesh: true, websocket: true, secret_key: true, url: 'https://bam.example.org', ...more };
}

beforeEach(() => {
  session.permissions = ['settings.manage'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('RelaysPage', () => {
  it('lists each relay with its health', async () => {
    const offline: Relay = { ...base, id: 'r2', name: 'workshop', online: false, transports: [], allowlist: [], version: '', address: '' };
    const waiting: Relay = { ...base, id: 'r3', name: 'cellar', paired: false, online: false, status_at: null, code_expires_at: soon, cert_expires_at: null, transports: [] };
    const revoked: Relay = { ...base, id: 'r4', name: 'old-pi', online: false, revoked_at: '2026-11-04T00:00:00Z', transports: [] };
    fakeApi({ 'GET /relays': () => listing([base, offline, waiting, revoked]) });
    render(RelaysPage, { params: {} });

    const row = (name: string) => screen.getByRole('cell', { name }).closest('tr')!;
    await screen.findByRole('cell', { name: 'front-desk' });
    expect(row('front-desk').textContent).toContain('Online since');
    expect(row('front-desk').textContent).toContain('over BoneMesh and WebSocket');
    expect(row('front-desk').textContent).toContain('v1.4.0');
    expect(row('front-desk').textContent).toContain('192.168.1.0/24:9100');
    expect(row('front-desk').textContent).toContain('Desk Zebra: printed');
    expect(row('front-desk').textContent).toContain('Shelf Rollo: failed');
    expect(row('front-desk').textContent).toContain('(relay front-desk is offline)');
    expect(row('front-desk').textContent).toContain('New Dymo: no jobs yet');
    expect(row('workshop').textContent).toContain('Offline since');
    expect(row('cellar').textContent).toContain('Waiting to pair, with a code that works until');
    expect(within(row('cellar')).getByRole('button', { name: 'Remove' })).toBeTruthy();
    expect(row('old-pi').textContent).toContain('its name is free after');
    expect(within(row('old-pi')).queryByRole('button')).toBeNull();
    expect(screen.getByText(/The mesh root, bam-1a2b/)).toBeTruthy();
  });

  it('adds a relay and shows the command that pairs it, once', async () => {
    let relays: Relay[] = [];
    const { sent } = fakeApi({
      'GET /relays': () => listing(relays, { mesh: null }),
      'POST /relays': (body) => {
        const r: Relay = { ...base, name: (body as { name: string }).name, paired: false, online: false, code_expires_at: soon, transports: [] };
        relays = [r];
        return { status: 201, body: { relay: r, code: 'ABCD-EFGH-JKMN-PQRS', expires_at: soon, command: 'bam relay join https://bam.example.org ABCD-EFGH-JKMN-PQRS' } };
      },
    });
    render(RelaysPage, { params: {} });
    await screen.findByText('No relays yet.');
    await fireEvent.click(screen.getByRole('button', { name: 'Add relay' }));
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: ' front-desk ' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Add and make a code' }));

    const command = (await screen.findByLabelText('The command that pairs the relay')) as HTMLInputElement;
    expect(command.value).toBe('bam relay join https://bam.example.org ABCD-EFGH-JKMN-PQRS');
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ name: 'front-desk' });
    await screen.findByRole('cell', { name: 'front-desk' });
    await fireEvent.click(screen.getByRole('button', { name: 'Done' }));
    expect(screen.queryByLabelText('The command that pairs the relay')).toBeNull();
    // No mesh root yet: nothing to rotate.
    expect(screen.queryByRole('button', { name: 'Rotate the mesh root' })).toBeNull();
  });

  it('gives a new code, revokes, and rotates the mesh root', async () => {
    let relays: Relay[] = [base];
    const { sent } = fakeApi({
      'GET /relays': () => listing(relays),
      'POST /relays/r1/code': () => ({ relay: base, code: 'WXYZ-0000-1111-2222', expires_at: soon, command: 'bam relay join https://bam.example.org WXYZ-0000-1111-2222' }),
      'DELETE /relays/r1': () => {
        relays = [{ ...base, online: false, revoked_at: '2026-11-05T10:00:00Z', transports: [] }];
        return { relay: relays[0] };
      },
      'POST /relays/mesh/rotate': () => ({ mesh: { name: 'bam-ffff' }, relays: 1 }),
    });
    render(RelaysPage, { params: {} });
    const row = (await screen.findByRole('cell', { name: 'front-desk' })).closest('tr')!;

    await fireEvent.click(within(row).getByRole('button', { name: 'New code' }));
    expect(((await screen.findByLabelText('The command that pairs the relay')) as HTMLInputElement).value).toContain('WXYZ-0000-1111-2222');

    await fireEvent.click(within(row).getByRole('button', { name: 'Revoke' }));
    await fireEvent.click(within(within(row).getByRole('group')).getByRole('button', { name: 'Revoke' }));
    await screen.findByText('Revoked front-desk.');
    expect(screen.queryByLabelText('The command that pairs the relay')).toBeNull();

    await fireEvent.click(screen.getByRole('button', { name: 'Rotate the mesh root' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Rotate' }));
    await screen.findByText(/Rotated the mesh root/);
    expect(sent.some((r) => r.method === 'POST' && r.path.endsWith('/relays/mesh/rotate'))).toBe(true);
  });

  it('says what relays need before they can be added', async () => {
    fakeApi({ 'GET /relays': () => listing([], { secret_key: false, mesh: null }) });
    render(RelaysPage, { params: {} });
    await screen.findByText(/Relays need/);
    expect(screen.queryByRole('button', { name: 'Add relay' })).toBeNull();
    cleanup();

    fakeApi({ 'GET /relays': () => listing([], { bonemesh: false, websocket: false, mesh: null }) });
    render(RelaysPage, { params: {} });
    await screen.findByText(/Relays are off on this server/);
  });
});
