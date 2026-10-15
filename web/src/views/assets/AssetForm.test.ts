import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import AssetForm from './AssetForm.svelte';
import { fakeApi } from '../../test/fetch';
import { router } from '../../lib/router.svelte';
import { session } from '../../lib/session.svelte';
import type { AssetWithDetails, Category, EffectiveField } from '../../lib/models';

const cat = (id: string, name: string): Category => ({
  id, name, parent_id: null, color_hex: '#336699', is_consumable: false, is_running_low: false, created_at: '', updated_at: '',
});
const field = (key: string, label: string, required = false, type: 'text' | 'number' = 'text'): EffectiveField => ({
  field: { id: key, key, label, validation_type: type, created_at: '', updated_at: '' },
  is_required: required,
});
const generators = [{ type: 'qr', name: 'QR Code', error_correction_levels: ['M'], code_rules: 'Any text.' }];

beforeEach(() => {
  session.permissions = ['assets:read', 'assets:write'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('AssetForm', () => {
  it('re-resolves fields as categories change, and creates the asset', async () => {
    const { sent } = fakeApi({
      'GET /categories': () => ({ categories: [cat('tools', 'Tools'), cat('uncategorized', 'Uncategorized')] }),
      'GET /generators': () => ({ generators }),
      'POST /categories/effective-fields': (b) => ({
        fields: (b as { category_ids: string[] }).category_ids.includes('tools') ? [field('serial', 'Serial', true)] : [],
      }),
      'POST /assets': () => ({ status: 201, body: { asset: { asset: { id: 'new-id' } } } }),
    });
    render(AssetForm, { params: {} });

    await fireEvent.input(await screen.findByLabelText('Name'), { target: { value: 'Drill' } });
    expect(screen.queryByLabelText(/Serial/)).toBeNull();
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Tools' }));
    const serial = await screen.findByLabelText(/Serial/);
    await fireEvent.input(serial, { target: { value: 'SN-1' } });
    await fireEvent.input(screen.getByLabelText('QR Code'), { target: { value: 'D-1' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Create asset' }));

    await vi.waitFor(() => expect(router.path).toBe('/assets/new-id'));
    expect(sent.find((r) => r.method === 'POST' && r.path === '/assets')!.body).toEqual({
      name: 'Drill',
      category_ids: ['tools'],
      generator_keys: [{ generator_type: 'qr', assigned_code: 'D-1' }],
      field_values: { serial: 'SN-1' },
    });
  });

  it('shows a server error beside the input it is about', async () => {
    const existing: AssetWithDetails = {
      asset: { id: 'a1', name: 'Drill', created_at: '', updated_at: '' },
      categories: [cat('tools', 'Tools')],
      generator_keys: [],
      field_values: { weight: '', old_note: 'from before' },
      effective_fields: [field('weight', 'Weight', false, 'number')],
    };
    const { sent } = fakeApi({
      'GET /categories': () => ({ categories: [cat('tools', 'Tools')] }),
      'GET /generators': () => ({ generators }),
      'GET /assets/a1': () => ({ asset: existing }),
      'POST /categories/effective-fields': () => ({ fields: existing.effective_fields }),
      'PUT /assets/a1': () => ({ status: 400, body: { error: 'field "Weight" (weight): invalid field value: "x" is not a valid number' } }),
    });
    render(AssetForm, { params: { id: 'a1' } });

    expect(await screen.findByText(/Kept from former categories, unchanged: old_note/)).toBeTruthy();
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    const weight = await screen.findByLabelText('Weight');
    await vi.waitFor(() => expect(weight.getAttribute('aria-invalid')).toBe('true'));
    expect(weight.getAttribute('aria-describedby')).toBe('field:weight-error');
    expect(document.getElementById('field:weight-error')!.textContent).toContain('not a valid number');
    // Kept values are not sent, so they stay as they are.
    expect(sent.find((r) => r.method === 'PUT')!.body).toMatchObject({ field_values: { weight: '' }, generator_keys: [] });
  });

  it('counts stock when asked, and an edit leaves the quantity alone unless it is changed', async () => {
    const { sent } = fakeApi({
      'GET /categories': () => ({ categories: [] }),
      'GET /generators': () => ({ generators }),
      'POST /categories/effective-fields': () => ({ fields: [] }),
      'POST /assets': () => ({ status: 201, body: { asset: { asset: { id: 'new-id' } } } }),
    });
    render(AssetForm, { params: {} });
    await fireEvent.input(await screen.findByLabelText('Name'), { target: { value: 'M3 screws' } });
    expect(screen.queryByLabelText('Quantity')).toBeNull();
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Count stock by quantity' }));
    await fireEvent.input(screen.getByLabelText('Quantity'), { target: { value: '200' } });
    await fireEvent.input(screen.getByLabelText('Unit (optional)'), { target: { value: 'pcs' } });
    await fireEvent.input(screen.getByLabelText('Low stock threshold (optional)'), { target: { value: '50' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Create asset' }));
    await vi.waitFor(() => expect(router.path).toBe('/assets/new-id'));
    expect(sent.find((r) => r.method === 'POST' && r.path === '/assets')!.body).toMatchObject({
      stock: { quantity: 200, unit: 'pcs', low_stock_threshold: 50 },
    });

    cleanup();
    const existing: AssetWithDetails = {
      asset: { id: 'a1', name: 'M3 screws', created_at: '', updated_at: '', stock: { quantity: 40, unit: 'pcs', low_stock_threshold: 50, is_running_low: true } },
      categories: [],
      generator_keys: [],
      field_values: {},
      effective_fields: [],
    };
    const edit = fakeApi({
      'GET /categories': () => ({ categories: [] }),
      'GET /generators': () => ({ generators }),
      'GET /assets/a1': () => ({ asset: existing }),
      'POST /categories/effective-fields': () => ({ fields: [] }),
      'PUT /assets/a1': () => ({ asset: existing }),
    });
    render(AssetForm, { params: { id: 'a1' } });
    const unit = await screen.findByLabelText('Unit (optional)');
    expect((screen.getByLabelText('Quantity') as HTMLInputElement).value).toBe('40');
    await fireEvent.input(unit, { target: { value: 'screws' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(edit.sent.some((r) => r.method === 'PUT')).toBe(true));
    expect(edit.sent.find((r) => r.method === 'PUT')!.body).toMatchObject({ stock: { unit: 'screws', low_stock_threshold: 50 } });
    expect((edit.sent.find((r) => r.method === 'PUT')!.body as { stock: object }).stock).not.toHaveProperty('quantity');
  });
});
