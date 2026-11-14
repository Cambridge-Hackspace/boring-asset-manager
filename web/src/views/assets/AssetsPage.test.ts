import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
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
  hooks: [],
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

  it('says why an asset matched through a related term, and offers related terms to search for', async () => {
    let calls = 0;
    fakeApi({
      'GET /categories': () => ({ categories: [category] }),
      'GET /assets': () => {
        calls++;
        return calls === 1
          ? { assets: [asset('a1', 'Black TPU'), asset('a2', 'Flexible filament sampler')], matched: { a1: 'TPU, narrower than Flexible filament' }, suggestions: ['TPE'] }
          : { assets: [asset('a3', 'TPE spool')] };
      },
    });
    render(AssetsPage, { params: {} });
    const why = await screen.findByTestId('match-why');
    expect(why.textContent).toBe('Found as TPU, narrower than Flexible filament');
    expect(why.closest('tr')!.textContent).toContain('Black TPU');
    expect(screen.getAllByTestId('match-why')).toHaveLength(1);

    await fireEvent.click(within(screen.getByTestId('search-suggestions')).getByRole('button', { name: 'TPE' }));
    expect(await screen.findByRole('link', { name: 'TPE spool' })).toBeTruthy();
    expect((screen.getByLabelText('Search') as HTMLInputElement).value).toBe('TPE');
    expect(screen.queryByTestId('search-suggestions')).toBeNull();
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

  it('adds the selection, or everything the filters match, to the print queue', async () => {
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
    await fireEvent.click(await screen.findByRole('button', { name: 'Add all 2 to the print queue' }));
    expect(await screen.findByText('Added 1 label to the print queue.')).toBeTruthy();
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Select Saw' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Add 1 to the print queue' }));
    const spooled = () => sent.filter((r) => r.method === 'POST' && r.path === '/spool');
    await vi.waitFor(() => expect(spooled().length).toBe(2));
    expect(spooled().map((r) => r.body)).toEqual([{ filter: { condition: 'problem' } }, { asset_ids: ['a2'] }]);
  });

  it('lists only the favorites, kept in the address', async () => {
    session.user = { id: 'ada', email: 'ada@hackspace.org', name: 'Ada', is_active: true, created_at: '', updated_at: '' };
    navigate('/assets', true);
    const { fetchMock } = fakeApi({
      'GET /categories': () => ({ categories: [category] }),
      'GET /assets': () => ({ assets: [asset('a1', 'Drill')] }),
    });
    render(AssetsPage, { params: {} });
    await screen.findByRole('link', { name: 'Drill' });
    // The star sits in a column of its own: the name's cell is named by the name alone.
    expect(screen.getByRole('cell', { name: 'Drill' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Favorite Drill' }).closest('td')).not.toBe(screen.getByRole('cell', { name: 'Drill' }));
    await fireEvent.change(screen.getByLabelText('Favorites'), { target: { value: 'true' } });
    await vi.waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url).includes('favorite=true'))).toBe(true));
    expect(location.search).toContain('favorite=true');
    session.user = null;
  });

  it('filters by review state where a category uses review states, and marks each asset’s', async () => {
    navigate('/assets', true);
    const reviewed = { ...category, effective_review: { enabled: true, months: 12, on_edit: 'draft' as const, self_verify: false } };
    const saw: AssetWithDetails = {
      ...asset('a1', 'Saw'),
      review: { state: 'confirmed', set_by: null, set_at: null, confirmed_by: null, confirmed_at: null, reviewed_at: null, due_at: '2026-11-01T00:00:00Z', due: true, overdue: true, rules: reviewed.effective_review, proposal: null },
    };
    const { fetchMock } = fakeApi({
      'GET /categories': () => ({ categories: [reviewed] }),
      'GET /assets': () => ({ assets: [saw] }),
    });
    render(AssetsPage, { params: {} });
    await screen.findByRole('link', { name: 'Saw' });
    expect(screen.getByText('Confirmed, review overdue').className).toContain('chip-red');
    await fireEvent.change(screen.getByLabelText('Review'), { target: { value: 'due' } });
    await vi.waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url).includes('review=due'))).toBe(true));
    expect(location.search).toContain('review=due');
  });

  it('has no review filter where no category uses review states', async () => {
    navigate('/assets', true);
    fakeApi({ 'GET /categories': () => ({ categories: [category] }), 'GET /assets': () => ({ assets: [asset('a1', 'Drill')] }) });
    render(AssetsPage, { params: {} });
    await screen.findByRole('link', { name: 'Drill' });
    expect(screen.queryByLabelText('Review')).toBeNull();
  });

  it('offers the spool only to those who print labels', async () => {
    fakeApi({ 'GET /categories': () => ({ categories: [category] }), 'GET /assets': () => ({ assets: [asset('a1', 'Drill')] }) });
    render(AssetsPage, { params: {} });
    await screen.findByRole('link', { name: 'Drill' });
    expect(screen.queryByRole('button', { name: /to the print queue/ })).toBeNull();
  });
  it('underlines a query mistake under the search box, and builds queries', async () => {
    navigate('/assets', true);
    const searched: string[] = [];
    fakeApi({
      'GET /categories': () => ({ categories: [category] }),
      'GET /fields': () => ({ fields: [{ id: 'f1', key: 'size', label: 'Size', validation_type: 'number', created_at: '', updated_at: '' }] }),
      'GET /assets': (_body, path) => {
        void path;
        const search = new URLSearchParams(location.search).get('search') ?? '';
        searched.push(search);
        if (search === 'field.colour = red') {
          return { status: 400, body: { error: 'The search can\'t be run: no field has the key "colour"', query_error: { message: 'no field has the key "colour"', start: 0, end: 12 } } };
        }
        return { assets: [asset('a1', 'Drill')] };
      },
    });
    render(AssetsPage, { params: {} });
    await screen.findByRole('link', { name: 'Drill' });
    const box = screen.getByLabelText('Search');
    await fireEvent.input(box, { target: { value: 'field.colour = red' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Search' }));
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('no field has the key "colour"');
    expect(document.querySelector('mark.query-fault')?.textContent).toBe('field.colour');
    expect(box.getAttribute('aria-invalid')).toBe('true');
    expect(box.getAttribute('aria-describedby')).toBe('asset-search-problem');
    expect(screen.queryByRole('link', { name: 'Drill' })).toBeNull();

    // The builder writes a query into the box, and searches with it.
    await fireEvent.click(screen.getByRole('button', { name: 'Build a query' }));
    await fireEvent.change(await screen.findByLabelText('Condition 1: what to search by'), { target: { value: 'field.size' } });
    await fireEvent.change(screen.getByLabelText('Condition 1: how to compare'), { target: { value: '>' } });
    await fireEvent.input(screen.getByLabelText('Condition 1: value'), { target: { value: '2' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Search with this query' }));
    await screen.findByRole('link', { name: 'Drill' });
    expect((box as HTMLInputElement).value).toBe('field.size > 2');
    expect(searched.at(-1)).toBe('field.size > 2');
    expect(screen.queryByRole('alert')).toBeNull();
  });
});
