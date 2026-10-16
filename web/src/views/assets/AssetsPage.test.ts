import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import AssetsPage from './AssetsPage.svelte';
import { fakeApi } from '../../test/fetch';
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
});
