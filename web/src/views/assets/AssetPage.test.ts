import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import AssetPage from './AssetPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { AssetEvent, AssetWithDetails } from '../../lib/models';

const withStock = (quantity: number, low: boolean): AssetWithDetails => ({
  asset: { id: 'a1', name: 'M3 screws', created_at: '', updated_at: '', stock: { quantity, unit: 'pcs', low_stock_threshold: 50, is_running_low: low } },
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
  session.permissions = ['assets:read', 'assets:write'];
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

  it('hides the stock actions from people who cannot change assets', async () => {
    session.permissions = ['assets:read'];
    fakeApi({
      'GET /assets/a1': () => ({ asset: withStock(5, true) }),
      'GET /generators': () => ({ generators: [] }),
      'GET /assets/a1/history': () => ({ events: [] }),
    });
    render(AssetPage, { params: { id: 'a1' } });
    expect(await screen.findByText('Running low.')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Use' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Mark restocked' })).toBeNull();
  });
});
