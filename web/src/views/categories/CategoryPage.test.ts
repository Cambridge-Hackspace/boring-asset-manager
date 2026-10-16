import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import CategoryPage from './CategoryPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { Category, EffectiveField } from '../../lib/models';

const cat = (id: string, name: string, parent: string | null = null, over: Partial<Category> = {}): Category => ({
  id, name, parent_id: parent, color_hex: '#1d4ed8', is_consumable: false, is_lendable: false, is_running_low: false, created_at: '', updated_at: '', ...over,
});
const categories = [
  cat('tools', 'Tools'),
  cat('power', 'Power tools', 'tools'),
  cat('drills', 'Drills', 'power'),
  cat('paint', 'Paint', null, { is_consumable: true, is_running_low: true, low_stock_threshold: 3 }),
  cat('uncategorized', 'Uncategorized'),
];
const fieldOf = (id: string, label: string) => ({ id, key: id, label, validation_type: 'text' as const, created_at: '', updated_at: '' });
const effective: EffectiveField[] = [
  { field: fieldOf('serial', 'Serial'), is_required: true, source_category_id: 'tools' },
  { field: fieldOf('voltage', 'Voltage'), is_required: false, source_category_id: 'power' },
];

beforeEach(() => {
  session.permissions = ['assets:read', 'categories:manage'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function api(extra: Record<string, (b: unknown) => unknown> = {}) {
  return fakeApi({
    'GET /categories': () => ({ categories }),
    'GET /categories/power/fields': () => ({ fields: effective }),
    'GET /categories/paint/fields': () => ({ fields: [] }),
    'GET /categories/power': () => ({ category: categories[1], active_count: 4 }),
    'GET /categories/paint': () => ({ category: categories[3], active_count: 2 }),
    'GET /fields': () => ({ fields: [fieldOf('serial', 'Serial'), fieldOf('voltage', 'Voltage'), fieldOf('notes', 'Notes')] }),
    ...extra,
  });
}

describe('CategoryPage', () => {
  it('shows own and inherited fields, and only offers to remove its own', async () => {
    api();
    render(CategoryPage, { params: { id: 'power' } });
    const table = await screen.findByRole('table');
    const rows = within(table).getAllByRole('row').slice(1);
    expect(rows[0]!.textContent).toContain('Inherited from');
    expect(rows[0]!.textContent).toContain('Tools');
    expect(within(rows[0]!).queryByRole('button', { name: 'Remove' })).toBeNull();
    expect(rows[1]!.textContent).toContain('This category');
    expect(within(rows[1]!).getByRole('button', { name: 'Remove' })).toBeTruthy();
    // Only fields not already its own can be added.
    const options = within(screen.getByLabelText('Add a field')).getAllByRole('option').map((o) => o.textContent);
    expect(options).toEqual(['Choose a field…', 'Serial', 'Notes']);
  });

  it('cannot become its own descendant, and always sends its parent', async () => {
    const { sent } = api({ 'PUT /categories/power': () => ({ category: categories[1] }) });
    render(CategoryPage, { params: { id: 'power' } });
    const parent = (await screen.findByLabelText('Parent')) as HTMLSelectElement;
    const choices = [...parent.options].map((o) => o.value);
    expect(choices).toEqual(['', 'paint', 'tools']); // not itself, Drills or Uncategorized

    await fireEvent.change(parent, { target: { value: '' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'PUT')).toBe(true));
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({
      name: 'Power tools', parent_id: null, color_hex: '#1d4ed8', is_consumable: false, is_lendable: false, is_running_low: false, low_stock_threshold: null,
    });
  });

  it('only lets consumable categories run low', async () => {
    api();
    render(CategoryPage, { params: { id: 'power' } });
    const low = (await screen.findByLabelText('Running low')) as HTMLInputElement;
    expect(low.disabled).toBe(true);
    await fireEvent.click(screen.getByLabelText('Consumable'));
    expect(low.disabled).toBe(false);
  });

  it('edits the low stock threshold, and drops it for non-consumables', async () => {
    const { sent } = api({ 'PUT /categories/paint': () => ({ category: categories[3] }) });
    render(CategoryPage, { params: { id: 'paint' } });
    const input = (await screen.findByLabelText('Low stock threshold (optional)')) as HTMLInputElement;
    expect(input.value).toBe('3');
    await fireEvent.input(input, { target: { value: '5' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(sent.filter((r) => r.method === 'PUT')).toHaveLength(1));
    expect(sent.find((r) => r.method === 'PUT')!.body).toMatchObject({ is_consumable: true, low_stock_threshold: 5 });

    await fireEvent.click(screen.getByLabelText('Consumable'));
    expect(input.disabled).toBe(true);
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(sent.filter((r) => r.method === 'PUT')).toHaveLength(2));
    expect(sent.filter((r) => r.method === 'PUT')[1]!.body).toMatchObject({ is_consumable: false, is_running_low: false, low_stock_threshold: null });
  });

  it('shows stock and lets people with stock:record flag it by hand', async () => {
    session.permissions = ['assets:read', 'stock:record'];
    const { sent } = api({ 'PUT /categories/paint/running-low': () => ({ category: categories[3], changed: true }) });
    render(CategoryPage, { params: { id: 'paint' } });
    const stock = await screen.findByRole('region', { name: 'Stock' });
    expect(stock.textContent).toContain('Running low.');
    expect(stock.textContent).toContain('2 active assets here and in its subcategories; flagged automatically below 3.');
    expect(screen.queryByLabelText('Name')).toBeNull();
    await fireEvent.click(within(stock).getByRole('button', { name: 'Mark restocked' }));
    await vi.waitFor(() => expect(sent.some((r) => r.path === '/categories/paint/running-low')).toBe(true));
    expect(sent.find((r) => r.path === '/categories/paint/running-low')!.body).toEqual({ running_low: false });
    expect(await screen.findByText('Marked as restocked.')).toBeTruthy();
  });

  it('offers no stock actions without stock:record, and none for non-consumables', async () => {
    // Editing assets is not enough.
    session.permissions = ['assets:read', 'assets:write'];
    api();
    render(CategoryPage, { params: { id: 'paint' } });
    const stock = await screen.findByRole('region', { name: 'Stock' });
    expect(within(stock).queryByRole('button')).toBeNull();
    cleanup();
    render(CategoryPage, { params: { id: 'power' } });
    await screen.findByRole('table');
    expect(screen.queryByRole('region', { name: 'Stock' })).toBeNull();
  });

  it('is read-only without categories:manage', async () => {
    session.permissions = ['assets:read'];
    api();
    render(CategoryPage, { params: { id: 'power' } });
    await screen.findByRole('table');
    expect(screen.queryByLabelText('Name')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Remove' })).toBeNull();
  });
});
