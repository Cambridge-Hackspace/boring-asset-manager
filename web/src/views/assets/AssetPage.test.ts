import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import AssetPage from './AssetPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { AssetEvent, AssetWithDetails, Condition } from '../../lib/models';

const withStock = (quantity: number, low: boolean): AssetWithDetails => ({
  asset: {
    id: 'a1',
    name: 'M3 screws',
    created_at: '',
    updated_at: '',
    condition: 'ok',
    condition_note: '',
    stock: { quantity, unit: 'pcs', low_stock_threshold: 50, is_running_low: low },
  },
  categories: [],
  generator_keys: [],
  field_values: {},
  effective_fields: [],
});

const used: AssetEvent = {
  id: 'e1',
  kind: 'asset.quantity_changed',
  created_at: '2026-10-14T10:00:00Z',
  payload: { trigger: 'manual', quantity: 40, change: -60, unit: 'pcs', actor: { id: 'u', email: 'ada@hackspace.org' } },
};

beforeEach(() => {
  session.permissions = ['assets:read', 'stock:record'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('AssetPage stock', () => {
  it('uses stock, shows the result and history, and reports running out', async () => {
    let quantity = 100;
    const { sent } = fakeApi({
      'GET /assets/a1': () => ({ asset: withStock(quantity, false) }),
      'GET /generators': () => ({ generators: [] }),
      'GET /assets/a1/history': () => ({ events: quantity === 100 ? [] : [used] }),
      'POST /assets/a1/quantity': (body) => {
        const adjust = (body as { adjust: number }).adjust;
        if (quantity + adjust < 0) return { status: 409, body: { error: `not enough stock: ${quantity} left` } };
        quantity += adjust;
        return { asset: withStock(quantity, quantity < 50) };
      },
    });
    render(AssetPage, { params: { id: 'a1' } });

    expect(await screen.findByText(/100 pcs left; flagged automatically below 50 pcs/)).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'History' })).toBeNull();
    await fireEvent.input(screen.getByLabelText('How many (pcs)'), { target: { value: '60' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Use' }));

    expect(await screen.findByText('Used 60 pcs.')).toBeTruthy();
    expect(screen.getByText('Running low.')).toBeTruthy();
    expect(screen.getByText(/40 pcs left/)).toBeTruthy();
    expect(await screen.findByText('Used 60 pcs; 40 pcs left')).toBeTruthy();
    expect(screen.getByText(/ada@hackspace.org/)).toBeTruthy();
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ adjust: -60 });

    await fireEvent.click(screen.getByRole('button', { name: 'Use' }));
    expect(await screen.findByText(/not enough stock: 40 left/)).toBeTruthy();

    await fireEvent.input(screen.getByLabelText('How many (pcs)'), { target: { value: '0' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    expect(await screen.findByText(/whole number of at least 1/)).toBeTruthy();
    expect(sent.filter((r) => r.method === 'POST')).toHaveLength(2);
  });

  it('hides the stock actions from people who cannot record stock, even if they edit assets', async () => {
    session.permissions = ['assets:read', 'assets:write'];
    fakeApi({
      'GET /assets/a1': () => ({ asset: withStock(5, true) }),
      'GET /generators': () => ({ generators: [] }),
      'GET /assets/a1/history': () => ({ events: [] }),
    });
    render(AssetPage, { params: { id: 'a1' } });
    expect(await screen.findByText('Running low.')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Use' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Mark restocked' })).toBeNull();
    expect(screen.getByRole('link', { name: 'Edit' })).toBeTruthy();
  });
});

describe('AssetPage history', () => {
  it('lists what each edit changed, and pages back through older events', async () => {
    const edited: AssetEvent = {
      id: 'e2',
      kind: 'asset.edited',
      created_at: '2026-10-15T10:00:00Z',
      payload: {
        trigger: 'manual',
        actor: { id: 'u', email: 'ada@hackspace.org', name: 'Ada Lovelace' },
        changes: [{ what: 'name', from: 'Screws', to: 'M3 screws' }, { what: 'category', key: 'c1', to: 'Fixings' }],
      },
    };
    const created: AssetEvent = { id: 'e0', kind: 'asset.created', created_at: '2026-10-13T10:00:00Z', payload: { trigger: 'import' } };
    const { fetchMock } = fakeApi({
      'GET /assets/a1': () => ({ asset: withStock(40, true) }),
      'GET /generators': () => ({ generators: [] }),
      'GET /assets/a1/history': () => {
        const older = String(fetchMock.mock.calls.at(-1)![0]).endsWith('?before=e1');
        return older ? { events: [created], more: false } : { events: [edited, used], more: true };
      },
    });
    render(AssetPage, { params: { id: 'a1' } });

    expect(await screen.findByText('Edited')).toBeTruthy();
    expect(screen.getByText('Name: Screws → M3 screws')).toBeTruthy();
    expect(screen.getByText('Added to Fixings')).toBeTruthy();
    // By name when the person has one, by email otherwise.
    expect(screen.getByText(/· Ada Lovelace$/)).toBeTruthy();
    expect(screen.getByText(/· ada@hackspace.org$/)).toBeTruthy();
    expect(screen.queryByText('Created by an import')).toBeNull();

    await fireEvent.click(screen.getByRole('button', { name: 'Show older' }));
    expect(await screen.findByText('Created by an import')).toBeTruthy();
    expect(screen.getByText('Used 60 pcs; 40 pcs left')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Show older' })).toBeNull();
  });
});

describe('AssetPage archived', () => {
  it('offers only a restore, and says which codes another asset took', async () => {
    session.permissions = ['assets:read', 'assets:write', 'stock:record', 'assets:delete', 'labels:print'];
    const archived: AssetWithDetails = { ...withStock(40, false), asset: { ...withStock(40, false).asset, archived_at: '2026-10-15T10:00:00Z' } };
    const restored: AssetEvent = {
      id: 'e3',
      kind: 'asset.restored',
      created_at: '2026-10-15T11:00:00Z',
      payload: { trigger: 'manual', changes: [{ what: 'code', key: 'qr', label: 'QR Code', from: 'S-1' }] },
    };
    let restoredYet = false;
    const { sent } = fakeApi({
      'GET /assets/a1': () => ({ asset: archived }),
      'GET /generators': () => ({ generators: [{ type: 'qr', name: 'QR Code', error_correction_levels: [], code_rules: '' }] }),
      'GET /assets/a1/history': () => ({ events: restoredYet ? [restored] : [] }),
      'GET /templates': () => ({ templates: [] }),
      'GET /printers': () => ({ printers: [] }),
      'POST /assets/a1/restore': () => {
        restoredYet = true;
        return { asset: withStock(40, false), skipped_codes: [{ asset_id: 'a1', generator_type: 'qr', assigned_code: 'S-1' }] };
      },
    });
    render(AssetPage, { params: { id: 'a1' } });

    expect(await screen.findByText(/^Archived .*Its codes were released/)).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Edit' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Archive' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Use' })).toBeNull();
    expect(screen.queryByRole('region', { name: 'Labels' })).toBeNull();

    await fireEvent.click(screen.getByRole('button', { name: 'Restore' }));
    expect(await screen.findByText('Restored. Other assets have taken QR Code S-1, so it did not get them back.')).toBeTruthy();
    expect(await screen.findByText('Not given back QR Code S-1: another asset has it')).toBeTruthy();
    expect(screen.queryByText(/Its codes were released/)).toBeNull();
    expect(screen.getByRole('link', { name: 'Edit' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Use' })).toBeTruthy();
    expect(sent.filter((r) => r.method === 'POST').map((r) => r.path)).toEqual(['/assets/a1/restore']);
  });
});

describe('AssetPage condition', () => {
  const broken = (asset: AssetWithDetails, condition: Condition, note: string): AssetWithDetails => ({
    ...asset,
    asset: { ...asset.asset, condition, condition_note: note },
  });

  it('lets a member report a problem but not clear it', async () => {
    session.permissions = ['assets:read', 'assets:report'];
    let current = withStock(5, false);
    const { sent } = fakeApi({
      'GET /assets/a1': () => ({ asset: current }),
      'GET /generators': () => ({ generators: [] }),
      'GET /assets/a1/history': () => ({ events: [] }),
      'PUT /assets/a1/condition': (body) => {
        const b = body as { condition: Condition; note: string };
        current = broken(current, b.condition, b.note);
        return { asset: current, changed: true };
      },
    });
    render(AssetPage, { params: { id: 'a1' } });
    await fireEvent.click(await screen.findByRole('button', { name: 'Report a problem' }));
    await fireEvent.click(screen.getByRole('radio', { name: /Out of service/ }));
    await fireEvent.input(screen.getByLabelText('What is wrong?'), { target: { value: 'Chuck slips' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Report' }));

    expect(await screen.findByText('Reported: out of service.')).toBeTruthy();
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({ condition: 'out_of_service', note: 'Chuck slips' });
    expect(screen.getByText('Out of service')).toBeTruthy();
    expect(screen.getByText('Chuck slips')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Mark as working' })).toBeNull();
  });

  it('lets someone who edits assets mark it working again', async () => {
    session.permissions = ['assets:read', 'assets:write'];
    let current = broken(withStock(5, false), 'needs_maintenance', 'Blunt');
    const { sent } = fakeApi({
      'GET /assets/a1': () => ({ asset: current }),
      'GET /generators': () => ({ generators: [] }),
      'GET /assets/a1/history': () => ({ events: [] }),
      'PUT /assets/a1/condition': () => {
        current = broken(current, 'ok', '');
        return { asset: current, changed: true };
      },
    });
    render(AssetPage, { params: { id: 'a1' } });
    expect(await screen.findByText('Needs maintenance')).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Mark as working' }));
    expect(await screen.findByText('Marked as working.')).toBeTruthy();
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({ condition: 'ok' });
    expect(screen.queryByText('Needs maintenance')).toBeNull();
  });
});
