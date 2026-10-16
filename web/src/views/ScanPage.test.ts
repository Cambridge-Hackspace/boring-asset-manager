import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import ScanPage from './ScanPage.svelte';
import { fakeApi } from '../test/fetch';
import type { ScanAction } from '../lib/models';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
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
      'PUT /assets/b1/condition': () => ({ asset: {}, changed: true }),
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

    await fireEvent.click(screen.getByRole('button', { name: 'Report a problem' }));
    await fireEvent.change(screen.getByLabelText('What is wrong'), { target: { value: 'out_of_service' } });
    await fireEvent.input(screen.getByLabelText('Note (optional)'), { target: { value: 'Lid cracked' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Report it' }));
    expect(await screen.findByText('Thank you; the problem is reported.')).toBeTruthy();
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({ condition: 'out_of_service', note: 'Lid cracked' });
  });

  it('offers only what the server allows: nothing, signed out with no actions', async () => {
    fakeApi({ 'GET /scan/BIN-A3': () => scanOf(10, []) });
    render(ScanPage, { params: { code: 'BIN-A3' } });
    await screen.findByRole('heading', { name: 'Bin A3', level: 1 });
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('says when nothing has the code', async () => {
    fakeApi({ 'GET /scan/NOPE': () => ({ status: 404, body: { error: 'No asset has that code' } }) });
    render(ScanPage, { params: { code: 'NOPE' } });
    expect(await screen.findByRole('heading', { name: 'Not found', level: 1 })).toBeTruthy();
    expect(screen.getByRole('alert').textContent).toContain('No asset has that code');
  });
});
