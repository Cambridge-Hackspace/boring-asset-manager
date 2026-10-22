import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import ScanPage from './ScanPage.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';
import type { ScanAction } from '../lib/models';

let failReport = false;

beforeEach(() => {
  session.user = { id: 'u1', email: 'ada@hackspace.org', name: 'Ada' } as typeof session.user;
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  session.user = null;
});

const scanOf = (left: number, actions: ScanAction[]) => ({
  asset: { id: 'b1', name: 'Bin A3', counted: true, unit: 'pcs' },
  template: { id: 't', name: 'Scanned' },
  result: {
    assets: 1,
    sections: [
      { type: 'details', title: '', columns: ['Where', 'In stock'], groups: [{ title: '', rows: [{ asset_id: 'b1', cells: ['Shelf 2', `${left} pcs`] }] }] },
      { type: 'table', title: 'In it', columns: ['What'], groups: [{ title: '', rows: [{ asset_id: 's1', cells: ['Screws'] }] }] },
      { type: 'actions', title: '', actions },
    ],
  },
});

describe('ScanPage', () => {
  it('shows the template, and takes the quick actions it offers', async () => {
    let left = 10;
    const { sent } = fakeApi({
      'GET /scan/BIN-A3': () => scanOf(left, ['open', 'use', 'report']),
      'POST /assets/b1/quantity': (body) => {
        left += (body as { adjust: number }).adjust;
        return { asset: {} };
      },
      'PUT /assets/b1/condition': () => (failReport ? { status: 503, body: { error: 'Try again later' } } : { asset: {}, changed: true }),
    });
    render(ScanPage, { params: { code: 'BIN-A3' } });
    expect(await screen.findByRole('heading', { name: 'Bin A3', level: 1 })).toBeTruthy();
    expect(screen.getByText('Shelf 2')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Screws' }).getAttribute('href')).toBe('/assets/s1');
    expect(screen.getByRole('link', { name: 'Open its page' }).getAttribute('href')).toBe('/assets/b1');

    await fireEvent.input(screen.getByLabelText('How many (pcs)'), { target: { value: '3' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Use' }));
    expect(await screen.findByText('Used 3 pcs.')).toBeTruthy();
    expect(await screen.findByText('7 pcs')).toBeTruthy();
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ adjust: -3 });

    // The same problem form as the asset page; a failed report keeps what was typed.
    await fireEvent.click(screen.getByRole('button', { name: 'Report a problem' }));
    await fireEvent.click(screen.getByLabelText(/Out of service/));
    await fireEvent.input(screen.getByLabelText('What is wrong?'), { target: { value: 'Lid cracked' } });
    failReport = true;
    await fireEvent.click(screen.getByRole('button', { name: 'Report' }));
    expect(await screen.findByText('Try again later')).toBeTruthy();
    expect((screen.getByLabelText('What is wrong?') as HTMLInputElement).value).toBe('Lid cracked');
    failReport = false;
    await fireEvent.click(screen.getByRole('button', { name: 'Report' }));
    expect(await screen.findByText('Thank you; the problem is reported.')).toBeTruthy();
    expect(screen.queryByLabelText('What is wrong?')).toBeNull();
    expect(sent.filter((r) => r.method === 'PUT').at(-1)!.body).toEqual({ condition: 'out_of_service', note: 'Lid cracked' });
  });

  it('starts a new asset like the one scanned, or in it', async () => {
    fakeApi({ 'GET /scan/BIN-A3': () => scanOf(10, ['add_similar', 'add_inside']) });
    render(ScanPage, { params: { code: 'BIN-A3' } });
    expect((await screen.findByRole('link', { name: 'Add another like it' })).getAttribute('href')).toBe('/assets/new?like=b1');
    expect(screen.getByRole('link', { name: 'Put something in it' }).getAttribute('href')).toBe('/assets/new?in=b1');
  });

  it('borrows with a due date, and leaves off what is empty', async () => {
    const { sent } = fakeApi({
      'GET /scan/BIN-A3': () => ({
        ...scanOf(10, ['borrow']),
        result: {
          name: 'Scanned', columns: [], sections: [
            { type: 'details', title: '', columns: ['Name', 'Where'], groups: [{ title: '', rows: [{ asset_id: 'b1', cells: ['Bin A3', ''] }] }] },
            { type: 'table', title: 'In it', columns: ['What'], groups: [{ title: '', rows: [] }] },
            { type: 'actions', title: '', actions: ['borrow'] },
          ],
        },
      }),
      'POST /assets/b1/checkout': () => ({ asset: {} }),
    });
    render(ScanPage, { params: { code: 'BIN-A3' } });
    await screen.findByRole('heading', { name: 'Bin A3', level: 1 });
    expect(screen.queryByText('Where')).toBeNull();
    expect(screen.queryByText('In it')).toBeNull();
    await fireEvent.input(screen.getByLabelText('Due back (optional)'), { target: { value: '2030-01-31' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Borrow' }));
    expect(await screen.findByText(/^Borrowed, due back/)).toBeTruthy();
    expect((sent.find((r) => r.method === 'POST')!.body as { due_at: string }).due_at).toMatch(/^2030-01-31|^2030-02-01/);
  });

  it('offers only what the server allows: nothing, signed out with no actions', async () => {
    fakeApi({ 'GET /scan/BIN-A3': () => scanOf(10, []) });
    render(ScanPage, { params: { code: 'BIN-A3' } });
    await screen.findByRole('heading', { name: 'Bin A3', level: 1 });
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('asks a visitor who is not signed in to sign in, rather than saying nothing has the code', async () => {
    session.user = null;
    fakeApi({ 'GET /scan/HS-1': () => ({ status: 404, body: { error: 'No asset has that code' } }) });
    render(ScanPage, { params: { code: 'HS-1' } });
    expect(await screen.findByRole('heading', { name: 'Sign in to see this asset', level: 1 })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Sign in' })).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('says when nothing has the code', async () => {
    fakeApi({ 'GET /scan/NOPE': () => ({ status: 404, body: { error: 'No asset has that code' } }) });
    render(ScanPage, { params: { code: 'NOPE' } });
    expect(await screen.findByRole('heading', { name: 'Not found', level: 1 })).toBeTruthy();
    expect(screen.getByRole('alert').textContent).toContain('No asset has that code');
  });

  it('adds the label to the print queue, for those who print labels', async () => {
    session.permissions = ['assets.scan', 'labels.print'];
    const { sent } = fakeApi({
      'GET /scan/BIN-A3': () => scanOf(10, []),
      'POST /spool': () => ({ status: 201, body: { added: [{ id: 'e1' }], already_waiting: [], skipped: [] } }),
    });
    render(ScanPage, { params: { code: 'BIN-A3' } });
    await fireEvent.click(await screen.findByRole('button', { name: 'Add its label to the print queue' }));
    expect(await screen.findByText('Added 1 label to the print queue.')).toBeTruthy();
    expect(sent.find((r) => r.path === '/spool')!.body).toEqual({ asset_ids: ['b1'] });
    session.permissions = [];
  });

  it('offers the actions its categories offer, where the template says', async () => {
    const scan = scanOf(10, ['actions']);
    const { sent } = fakeApi({
      'GET /scan/BIN-A3': () => ({ ...scan, asset: { ...scan.asset, actions: [{ id: '1', key: 'identify', label: 'Identify' }] } }),
      'POST /assets/b1/actions/identify': () => ({ status: 201, body: { event_id: 'e' } }),
    });
    render(ScanPage, { params: { code: 'BIN-A3' } });
    await fireEvent.click(await screen.findByRole('button', { name: 'Identify' }));
    expect(await screen.findByText('Identify: sent.')).toBeTruthy();
    expect(sent.at(-1)!.path).toBe('/assets/b1/actions/identify');
  });
});
