import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import PoolsSection from './PoolsSection.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { Printer, PrinterPool } from '../../lib/models';

const printer = (id: string, name: string, type: Printer['connection']['type'] = 'tcp'): Printer => ({
  id,
  name,
  driver: 'brother_ql',
  model: 'QL-810W',
  connection: { type, address: type === 'ipp' ? 'ipp://cups/printers/x' : '10.0.0.9' },
  media: 'dk-11201',
  created_at: '',
  updated_at: '',
});
const printers = [printer('p1', 'Front desk'), printer('p2', 'Back room'), printer('p3', 'Office', 'ipp')];

beforeEach(() => {
  session.permissions = ['settings.manage', 'labels.print'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('PoolsSection', () => {
  it('makes a pool of printers, with their shares', async () => {
    let pools: PrinterPool[] = [];
    const { sent } = fakeApi({
      'GET /printer-pools': () => ({ pools }),
      'POST /printer-pools': (body) => {
        const b = body as { name: string; batch_size: number; fixed_shares: boolean; members: PrinterPool['members'] };
        pools = [{ id: 'bench', ...b, created_at: '', updated_at: '' }];
        return { status: 201, body: { pool: pools[0] } };
      },
    });
    render(PoolsSection, { printers });
    await fireEvent.click(await screen.findByRole('button', { name: 'New pool' }));
    const form = screen.getByRole('form', { name: 'New pool' });
    await fireEvent.input(within(form).getByLabelText('Name'), { target: { value: 'Bench' } });
    await fireEvent.input(within(form).getByLabelText('Labels in a batch'), { target: { value: '5' } });
    expect((within(form).getByRole('button', { name: 'Save pool' }) as HTMLButtonElement).disabled).toBe(true);
    await fireEvent.click(within(form).getByRole('checkbox', { name: 'Front desk' }));
    await fireEvent.click(within(form).getByRole('checkbox', { name: 'Office' }));
    // The office printer is behind a spooler: it takes its share.
    expect(within(form).getByText('Behind a spooler: takes its share')).toBeTruthy();
    await fireEvent.input(within(form).getByLabelText('Office\'s share'), { target: { value: '2' } });
    await fireEvent.submit(within(form).getByRole('button', { name: 'Save pool' }));
    expect(await screen.findByText('Saved Bench.')).toBeTruthy();
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({
      name: 'Bench',
      batch_size: 5,
      fixed_shares: false,
      members: [
        { printer_id: 'p1', share: 1 },
        { printer_id: 'p3', share: 2 },
      ],
    });
    expect((await screen.findByRole('heading', { name: 'Bench' })).closest('article')!.textContent).toContain('Batches of 5: Front desk, Office (share 2)');
  });

  it('edits and deletes a pool; others only see them', async () => {
    let pools: PrinterPool[] = [{ id: 'bench', name: 'Bench', batch_size: 10, fixed_shares: true, members: [{ printer_id: 'p1', share: 1 }], created_at: '', updated_at: '' }];
    const { sent } = fakeApi({
      'GET /printer-pools': () => ({ pools }),
      'PUT /printer-pools/bench': (body) => ({ pool: body }),
      'DELETE /printer-pools/bench': () => {
        pools = [];
        return { message: 'Printer pool deleted' };
      },
    });
    render(PoolsSection, { printers });
    const card = (await screen.findByRole('heading', { name: 'Bench' })).closest('article')!;
    expect(card.textContent).toContain('Batches of 10, fixed shares: Front desk (share 1)');
    await fireEvent.click(within(card).getByRole('button', { name: 'Edit' }));
    const form = screen.getByRole('form', { name: 'Edit Bench' });
    expect((within(form).getByRole('checkbox', { name: 'Every printer takes a fixed share' }) as HTMLInputElement).checked).toBe(true);
    await fireEvent.click(within(form).getByRole('checkbox', { name: 'Back room' }));
    await fireEvent.submit(within(form).getByRole('button', { name: 'Save pool' }));
    expect(await screen.findByText('Saved Bench.')).toBeTruthy();
    expect((sent.find((r) => r.method === 'PUT')!.body as { members: unknown[] }).members).toEqual([
      { printer_id: 'p1', share: 1 },
      { printer_id: 'p2', share: 1 },
    ]);
    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Delete Bench' }));
    expect(await screen.findByText('No pools yet.')).toBeTruthy();
    cleanup();

    session.permissions = ['labels.print'];
    pools = [{ id: 'bench', name: 'Bench', batch_size: 10, fixed_shares: false, members: [{ printer_id: 'p1', share: 1 }], created_at: '', updated_at: '' }];
    fakeApi({ 'GET /printer-pools': () => ({ pools }) });
    render(PoolsSection, { printers });
    await screen.findByRole('heading', { name: 'Bench' });
    expect(screen.queryByRole('button', { name: /New pool|Edit|Delete/ })).toBeNull();
  });
});
