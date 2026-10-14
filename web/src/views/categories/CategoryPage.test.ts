import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import CategoryPage from './CategoryPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { Category, EffectiveField } from '../../lib/models';

const cat = (id: string, name: string, parent: string | null = null, over: Partial<Category> = {}): Category => ({
  id, name, parent_id: parent, color_hex: '#1d4ed8', is_consumable: false, is_running_low: false, created_at: '', updated_at: '', ...over,
});
const categories = [cat('tools', 'Tools'), cat('power', 'Power tools', 'tools'), cat('drills', 'Drills', 'power'), cat('paint', 'Paint'), cat('uncategorized', 'Uncategorized')];
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
      name: 'Power tools', parent_id: null, color_hex: '#1d4ed8', is_consumable: false, is_running_low: false,
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

  it('is read-only without categories:manage', async () => {
    session.permissions = ['assets:read'];
    api();
    render(CategoryPage, { params: { id: 'power' } });
    await screen.findByRole('table');
    expect(screen.queryByLabelText('Name')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Remove' })).toBeNull();
  });
});
