import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import AssetsPage from './AssetsPage.svelte';
import { fakeApi } from '../../test/fetch';
import { navigate } from '../../lib/router.svelte';
import { session } from '../../lib/session.svelte';
import type { AssetWithDetails, Category } from '../../lib/models';

const category: Category = {
  id: 'tools',
  name: 'Tools',
  parent_id: null,
  color_hex: '#336699',
  is_consumable: false,
  unit: '',
  is_lendable: false,
  is_running_low: false,
  created_at: '',
  updated_at: '',
};
const asset = (id: string, name: string): AssetWithDetails => ({
  asset: { id, name, created_at: '', updated_at: '', condition: 'ok', condition_note: '' },
  categories: [],
  generator_keys: [],
  field_values: {},
  effective_fields: [],
  lendable: false,
  weighed: false,
  loan: null,
  links: [],
  location: [],
  suggested_relation_type_ids: [],
  scopes: ['default'],
  scan_template: { template_id: 'default' },
  label_template: { template_id: '' },
  actions: [],
  photos: [],
});

beforeEach(() => {
  session.permissions = ['assets.read'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('AssetsPage', () => {
  it('keeps the filtered list when an older, slower answer arrives after it', async () => {
    let answerFirst: (v: unknown) => void = () => {};
    let calls = 0;
    fakeApi({
      'GET /categories': () => ({ categories: [category] }),
      'GET /assets': () => {
        calls++;
        // The first, unfiltered list is slow; the filtered one is quick.
        if (calls === 1) return new Promise((resolve) => (answerFirst = resolve));
        return { assets: [asset('a1', 'Drill')] };
      },
    });
    render(AssetsPage, { params: {} });
    await vi.waitFor(() => expect(calls).toBe(1));
    await fireEvent.change(screen.getByLabelText('Category'), { target: { value: 'tools' } });
    expect(await screen.findByRole('link', { name: 'Drill' })).toBeTruthy();

    answerFirst({ assets: [asset('a1', 'Drill'), asset('a2', 'Saw')] });
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByRole('link', { name: 'Saw' })).toBeNull();
    expect(screen.getAllByRole('row')).toHaveLength(2);
  });

  it('marks assets whose scan template was chosen by a tie', async () => {
    fakeApi({
      'GET /categories': () => ({ categories: [category] }),
      'GET /assets': () => ({
        assets: [
          { ...asset('a1', 'Kettle'), scan_template: { template_id: 't', source_category_id: 'tools', conflict: true, contenders: ['tools', 'kitchen'] } },
          asset('a2', 'Drill'),
        ],
      }),
    });
    render(AssetsPage, { params: {} });
    const marks = await screen.findAllByRole('img', { name: 'Scan template chosen by a tie between categories' });
    expect(marks).toHaveLength(1);
    expect(marks[0]!.closest('tr')!.textContent).toContain('Kettle');
  });

  it('adds the selection, or everything the filters match, to the spool', async () => {
    navigate('/assets', true);
    session.permissions = ['assets.read', 'labels.print'];
    const { sent } = fakeApi({
      'GET /categories': () => ({ categories: [category] }),
      'GET /assets': () => ({ assets: [asset('a1', 'Drill'), { ...asset('a2', 'Saw'), label_template: { template_id: 't', conflict: true, contenders: ['x', 'y'] } }] }),
      'POST /spool': () => ({ status: 201, body: { added: [{ id: 'e1' }], already_waiting: [], skipped: [] } }),
    });
    render(AssetsPage, { params: {} });
    expect(await screen.findByRole('img', { name: 'Label template chosen by a tie between categories' })).toBeTruthy();
    await fireEvent.change(screen.getByLabelText('Condition'), { target: { value: 'problem' } });
    await fireEvent.click(await screen.findByRole('button', { name: 'Add all 2 to the spool' }));
    expect(await screen.findByText('Added 1 label to the spool.')).toBeTruthy();
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Select Saw' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Add 1 to the spool' }));
    const spooled = () => sent.filter((r) => r.method === 'POST' && r.path === '/spool');
    await vi.waitFor(() => expect(spooled().length).toBe(2));
    expect(spooled().map((r) => r.body)).toEqual([{ filter: { condition: 'problem' } }, { asset_ids: ['a2'] }]);
  });

  it('offers the spool only to those who print labels', async () => {
    fakeApi({ 'GET /categories': () => ({ categories: [category] }), 'GET /assets': () => ({ assets: [asset('a1', 'Drill')] }) });
    render(AssetsPage, { params: {} });
    await screen.findByRole('link', { name: 'Drill' });
    expect(screen.queryByRole('button', { name: /to the spool/ })).toBeNull();
  });
});
