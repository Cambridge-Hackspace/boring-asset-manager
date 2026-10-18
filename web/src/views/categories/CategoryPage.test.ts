import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import CategoryPage from './CategoryPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { Category, EffectiveField } from '../../lib/models';

const cat = (id: string, name: string, parent: string | null = null, over: Partial<Category> = {}): Category => ({
  id, name, parent_id: parent, color_hex: '#1d4ed8', is_consumable: false, unit: '', is_lendable: false, is_running_low: false, created_at: '', updated_at: '', ...over,
});
const categories = [
  cat('tools', 'Tools'),
  cat('power', 'Power tools', 'tools'),
  cat('drills', 'Drills', 'power'),
  cat('paint', 'Paint', null, { is_consumable: true, unit: '', is_running_low: true, low_stock_threshold: 3 }),
  cat('uncategorized', 'Uncategorized'),
];
const fieldOf = (id: string, label: string) => ({ id, key: id, label, validation_type: 'text' as const, created_at: '', updated_at: '' });
const effective: EffectiveField[] = [
  { field: fieldOf('serial', 'Serial'), is_required: true, source_category_id: 'tools' },
  { field: fieldOf('voltage', 'Voltage'), is_required: false, source_category_id: 'power' },
];

beforeEach(() => {
  session.permissions = ['assets.read', 'categories.manage'];
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
    'GET /reports': () => ({ reports: [] }),
    'GET /templates': () => ({ templates: [] }),
    'GET /actions': () => ({ actions: [] }),
    'GET /categories/power/actions': () => ({ actions: [] }),
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
      name: 'Power tools', parent_id: null, color_hex: '#1d4ed8', is_consumable: false, unit: '', is_lendable: false, is_running_low: false, low_stock_threshold: null,
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
    expect(sent.find((r) => r.method === 'PUT')!.body).toMatchObject({ is_consumable: true, unit: '', low_stock_threshold: 5 });

    await fireEvent.click(screen.getByLabelText('Consumable'));
    expect(input.disabled).toBe(true);
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(sent.filter((r) => r.method === 'PUT')).toHaveLength(2));
    expect(sent.filter((r) => r.method === 'PUT')[1]!.body).toMatchObject({ is_consumable: false, unit: '', is_running_low: false, low_stock_threshold: null });
  });

  it('shows stock and lets people with stock.record flag it by hand', async () => {
    session.permissions = ['assets.read', 'stock.record'];
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

  it('offers no stock actions without stock.record, and none for non-consumables', async () => {
    // Editing assets is not enough.
    session.permissions = ['assets.read', 'assets.write'];
    api();
    render(CategoryPage, { params: { id: 'paint' } });
    const stock = await screen.findByRole('region', { name: 'Stock' });
    expect(within(stock).queryByRole('button')).toBeNull();
    cleanup();
    render(CategoryPage, { params: { id: 'power' } });
    await screen.findByRole('table');
    expect(screen.queryByRole('region', { name: 'Stock' })).toBeNull();
  });

  it('is read-only without categories.manage', async () => {
    session.permissions = ['assets.read'];
    api();
    render(CategoryPage, { params: { id: 'power' } });
    await screen.findByRole('table');
    expect(screen.queryByLabelText('Name')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Remove' })).toBeNull();
  });

  it('puts it in scopes, for those who manage users, and shows them to category managers', async () => {
    const scopes = [
      { id: 'd', name: 'default', created_at: '', reserved: true, category_count: 0, role_count: 0 },
      { id: 'w', name: 'workshop', created_at: '', reserved: false, category_count: 1, role_count: 0 },
      { id: 'k', name: 'kitchen', created_at: '', reserved: false, category_count: 0, role_count: 0 },
    ];
    const scopeApi = () =>
      api({
        'GET /relation-types': () => ({ relation_types: [] }),
        'GET /categories/power/relation-types': () => ({ relation_type_ids: [] }),
        'GET /scopes': () => ({ scopes }),
        'GET /categories/power/scopes': () => ({ scope_ids: ['w'] }),
        'PUT /categories/power/scopes': (body) => body,
      });

    // A category manager sees them, but cannot change them.
    scopeApi();
    render(CategoryPage, { params: { id: 'power' } });
    const section = (await screen.findByRole('heading', { name: 'Scopes' })).closest('section')!;
    const boxes = within(section).getAllByRole('checkbox') as HTMLInputElement[];
    // The default scope is not offered: it is where assets in no other are.
    expect(boxes.map((b) => b.parentElement!.textContent!.trim())).toEqual(['workshop', 'kitchen']);
    expect(boxes.map((b) => [b.checked, b.disabled])).toEqual([[true, true], [false, true]]);
    expect(within(section).queryByRole('button', { name: 'Save scopes' })).toBeNull();
    cleanup();

    session.permissions = ['assets.read', 'categories.manage', 'users.manage'];
    const { sent } = scopeApi();
    render(CategoryPage, { params: { id: 'power' } });
    await fireEvent.click(await screen.findByRole('checkbox', { name: 'kitchen' }));
    await fireEvent.submit(screen.getByRole('button', { name: 'Save scopes' }));
    await screen.findByText('Scopes saved.');
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({ scope_ids: ['w', 'k'] });
  });

  // Everything the page loads for a category manager.
  const loaded = (extra: Record<string, (b: unknown) => unknown> = {}) =>
    api({
      'GET /relation-types': () => ({ relation_types: [] }),
      'GET /categories/power/relation-types': () => ({ relation_type_ids: [] }),
      'GET /scopes': () => ({ scopes: [] }),
      'GET /categories/power/scopes': () => ({ scope_ids: [] }),
      ...extra,
    });

  it('chooses a scan template, warning first when it would tie with another category', async () => {
    const scan = (id: string, name: string) => ({ id, name, kind: 'scan', default: id === 'd', spec: { filter: {}, sections: [] }, created_at: '', updated_at: '' });
    const { sent } = loaded({
      'GET /reports': () => ({ reports: [scan('d', 'Scanned'), scan('bins', 'Bins')] }),
      'GET /categories/scan-conflicts': (_b: unknown) => ({ categories: { tools: 0 } }),
      'PUT /categories/power/scan-template': () => ({ category: categories[1] }),
    });
    // The answer about the change: power would tie with paint over 2 assets.
    let asked = 0;
    const realFetch = globalThis.fetch;
    vi.stubGlobal('fetch', (url: string, init?: RequestInit) => {
      if (url.includes('scan-conflicts?')) {
        asked++;
        return Promise.resolve(new Response(JSON.stringify({ categories: { power: 2, paint: 2 } }), { headers: { 'Content-Type': 'application/json' } }));
      }
      return realFetch(url, init);
    });
    render(CategoryPage, { params: { id: 'power' } });
    const select = (await screen.findByLabelText('Shows')) as HTMLSelectElement;
    expect([...select.options].map((o) => o.textContent)).toEqual(['Its parent’s, or the default', 'Scanned (default)', 'Bins']);
    await fireEvent.change(select, { target: { value: 'bins' } });
    expect(await screen.findByText(/Saving this ties Paint with this category/)).toBeTruthy();
    expect(asked).toBe(1);
    await fireEvent.submit(screen.getByRole('button', { name: 'Save scan template' }));
    await screen.findByText('Scan template saved.');
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({ scan_template_id: 'bins' });
  });

  it('says when its assets show a template chosen by a tie', async () => {
    loaded({ 'GET /reports': () => ({ reports: [] }), 'GET /categories/scan-conflicts': () => ({ categories: { power: 3 } }) });
    render(CategoryPage, { params: { id: 'power' } });
    const note = await screen.findByRole('note');
    expect(note.textContent).toContain('3 assets here show a scan template chosen by a tie');
    expect(within(note).getByRole('link', { name: 'See which' }).getAttribute('href')).toBe('/assets?conflict=scan_template');
  });

  it('lists label templates in order of preference, warning first when they would tie with another category', async () => {
    const tmpl = (id: string, name: string) => ({ id, name, units: 'mm', width: 62, height: 29, dpi: 300 });
    const { sent } = loaded({
      'GET /templates': () => ({ templates: [tmpl('shelf', 'Shelf'), tmpl('tag', 'Tag'), tmpl('wide', 'Wide')] }),
      'GET /categories/label-conflicts': () => ({ categories: {} }),
      'PUT /categories/power/label-template': () => ({ category: categories[1] }),
    });
    const asked: string[] = [];
    const realFetch = globalThis.fetch;
    vi.stubGlobal('fetch', (url: string, init?: RequestInit) => {
      if (url.includes('label-conflicts?')) {
        asked.push(decodeURIComponent(url.split('?')[1]!));
        return Promise.resolve(new Response(JSON.stringify({ categories: { power: 1, paint: 1 } }), { headers: { 'Content-Type': 'application/json' } }));
      }
      return realFetch(url, init);
    });
    render(CategoryPage, { params: { id: 'power' } });
    const add = (await screen.findByLabelText('Add a template')) as HTMLSelectElement;
    expect([...add.options].map((o) => o.textContent)).toEqual(['Choose one', 'Shelf', 'Tag', 'Wide']);
    expect(screen.getByText('None: its parent’s, if it names any.')).toBeTruthy();

    for (const id of ['tag', 'wide']) {
      await fireEvent.change(add, { target: { value: id } });
      await fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    }
    // Added ones are no longer offered; the second moves up to first.
    expect([...add.options].map((o) => o.textContent)).toEqual(['Choose one', 'Shelf']);
    await fireEvent.click(screen.getByRole('button', { name: 'Move Wide up' }));
    const list = within(screen.getByRole('form', { name: 'Label templates' })).getByRole('list');
    expect(within(list).getAllByRole('listitem').map((li) => li.textContent?.split('↑')[0]?.trim())).toEqual(['1. Wide', '2. Tag']);
    expect(asked.at(-1)).toBe('category_id=power&label_template_ids=wide,tag');
    expect(await screen.findByText(/Saving this ties Paint with this category/)).toBeTruthy();

    await fireEvent.submit(screen.getByRole('button', { name: 'Save label templates' }));
    await screen.findByText('Label templates saved.');
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({ label_template_ids: ['wide', 'tag'] });
  });

  it('says when its assets take a label template chosen by a tie', async () => {
    loaded({ 'GET /categories/label-conflicts': () => ({ categories: { power: 2 } }) });
    render(CategoryPage, { params: { id: 'power' } });
    const note = await screen.findByRole('note');
    expect(note.textContent).toContain('2 assets here take a label template chosen by a tie');
    expect(within(note).getByRole('link', { name: 'See which' }).getAttribute('href')).toBe('/assets?conflict=label_template');
  });

  it('adds its assets to the spool, with or without its subcategories’', async () => {
    session.permissions = ['assets.read', 'labels.print'];
    const { sent } = api({
      'GET /categories/label-conflicts': () => ({ categories: {} }),
      'POST /spool': () => ({ status: 201, body: { added: [{ id: 'e1' }, { id: 'e2' }], already_waiting: ['a3'], skipped: [] } }),
    });
    render(CategoryPage, { params: { id: 'power' } });
    // Without categories.manage there is no template to choose, only the spool.
    const add = await screen.findByRole('button', { name: 'Add its assets to the spool' });
    expect(screen.queryByLabelText('Add a template')).toBeNull();
    await fireEvent.click(add);
    expect(await screen.findByText('Added 2 labels to the spool; 1 label already waiting.')).toBeTruthy();
    await fireEvent.click(screen.getByRole('checkbox', { name: /With its subcategories/ }));
    await fireEvent.click(add);
    await vi.waitFor(() => expect(sent.filter((r) => r.method === 'POST').length).toBe(2));
    expect(sent.filter((r) => r.method === 'POST').map((r) => r.body)).toEqual([
      { category_id: 'power', subcategories: true },
      { category_id: 'power', subcategories: false },
    ]);
  });

  it('chooses the actions it offers, and whether its subcategories’ assets get them', async () => {
    const act = (id: string, label: string) => ({ id, key: id, label, confirm: '', created_at: '', updated_at: '' });
    const { sent } = loaded({
      'GET /actions': () => ({ actions: [act('identify', 'Identify'), act('open', 'Open drawer')] }),
      'GET /categories/power/actions': () => ({ actions: [{ action_id: 'open', cascades: false }] }),
      'PUT /categories/power/actions': (body) => body,
    });
    render(CategoryPage, { params: { id: 'power' } });
    const section = (await screen.findByRole('heading', { name: 'Actions' })).closest('section')!;
    expect((within(section).getByRole('checkbox', { name: 'Open drawer' }) as HTMLInputElement).checked).toBe(true);
    expect((within(section).getByRole('checkbox', { name: 'On its subcategories’ assets too' }) as HTMLInputElement).checked).toBe(false);
    await fireEvent.click(within(section).getByRole('checkbox', { name: 'Identify' }));
    await fireEvent.submit(within(section).getByRole('button', { name: 'Save actions' }));
    await screen.findByText('Actions saved.');
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({
      actions: [
        { action_id: 'identify', cascades: true },
        { action_id: 'open', cascades: false },
      ],
    });
  });
});
