import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import AssetForm from './AssetForm.svelte';
import { fakeApi } from '../../test/fetch';
import { navigate, router } from '../../lib/router.svelte';
import { session } from '../../lib/session.svelte';
import type { AssetWithDetails, Category, EffectiveField } from '../../lib/models';

const cat = (id: string, name: string): Category => ({
  id, name, parent_id: null, color_hex: '#336699', is_consumable: false, unit: '', is_lendable: false, is_running_low: false, created_at: '', updated_at: '',
});
const field = (key: string, label: string, required = false, type: 'text' | 'number' = 'text'): EffectiveField => ({
  field: { id: key, key, label, validation_type: type, created_at: '', updated_at: '' },
  is_required: required,
});
const generators = [{ type: 'qr', name: 'QR Code', error_correction_levels: ['M'], code_rules: 'Any text.' }];

beforeEach(() => {
  session.permissions = ['assets.read', 'assets.write'];
});
afterEach(() => {
  cleanup();
  navigate('/', true);
  vi.unstubAllGlobals();
});

describe('AssetForm', () => {
  it('re-resolves fields as categories change, and creates the asset', async () => {
    const { sent } = fakeApi({
      'GET /categories': () => ({ categories: [cat('tools', 'Tools'), cat('uncategorized', 'Uncategorized')] }),
      'GET /generators': () => ({ generators }),
      'GET /relation-types': () => ({ relation_types: [] }),
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

  it('fills a new asset in from one it is like, links it as that one is, and leaves out what is unticked', async () => {
    navigate('/assets/new?like=spool');
    const rel = (id: string, name: string, inverse: string, over = {}) => ({
      id, name, inverse_name: inverse, is_symmetric: false, is_single: false, is_location: false, created_at: '', updated_at: '', link_count: 0, ...over,
    });
    const { sent } = fakeApi({
      'GET /categories': () => ({ categories: [cat('filament', 'Filament')] }),
      'GET /generators': () => ({ generators }),
      'GET /relation-types': () => ({
        relation_types: [rel('in', 'contains', 'contained in', { is_single: true, is_location: true }), rel('with', 'used with', 'used with', { is_symmetric: true }), rel('part', 'has part', 'part of')],
      }),
      'GET /assets/spool': () => ({ asset: { asset: { id: 'spool', name: 'Yellow PLA' } } }),
      'GET /assets/spool/copy': () => ({
        copy: {
          name: 'Yellow PLA',
          category_ids: ['filament'],
          field_values: { colour: 'Yellow' },
          stock: { quantity: 1000, unit: 'g', low_stock_threshold: null },
          generator_keys: [{ generator_type: 'qr', assigned_code: 'SP-015' }],
          links: [
            { type_id: 'in', label: 'contained in', asset: { id: 'shelf', name: 'Shelf 2' }, outgoing: false, is_location: true },
            { type_id: 'with', label: 'used with', asset: { id: 'prusa', name: 'Prusa' }, outgoing: true, is_location: false },
            { type_id: 'part', label: 'part of', asset: { id: 'kit', name: 'Starter kit' }, outgoing: false, is_location: false },
          ],
        },
      }),
      'POST /categories/effective-fields': () => ({ fields: [field('colour', 'Color')] }),
      'POST /assets': () => ({ status: 201, body: { asset: { asset: { id: 'new-id' }, location: [], links: [] } } }),
      'POST /assets/new-id/links': () => ({ asset: {} }),
    });
    render(AssetForm, { params: {} });

    expect(await screen.findByRole('heading', { name: 'New asset like this one' })).toBeTruthy();
    expect(((await screen.findByLabelText('Name')) as HTMLInputElement).value).toBe('Yellow PLA');
    expect((await screen.findByLabelText('Color') as HTMLInputElement).value).toBe('Yellow');
    expect((screen.getByLabelText('QR Code') as HTMLInputElement).value).toBe('SP-015');
    expect((screen.getByLabelText('Quantity') as HTMLInputElement).value).toBe('1000');
    expect((screen.getByRole('checkbox', { name: 'Filament' }) as HTMLInputElement).checked).toBe(true);
    // Where it is goes where it is chosen; the rest are listed, each to keep or not.
    expect(screen.getByText('Shelf 2')).toBeTruthy();
    expect((screen.getByRole('checkbox', { name: 'used with Prusa' }) as HTMLInputElement).checked).toBe(true);
    await fireEvent.click(screen.getByRole('checkbox', { name: 'part of Starter kit' }));
    await fireEvent.input(screen.getByLabelText('Color'), { target: { value: 'Blue' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Create asset' }));

    await vi.waitFor(() => expect(router.path).toBe('/assets/new-id'));
    expect(sent.find((r) => r.method === 'POST' && r.path === '/assets')!.body).toEqual({
      name: 'Yellow PLA',
      category_ids: ['filament'],
      generator_keys: [{ generator_type: 'qr', assigned_code: 'SP-015' }],
      field_values: { colour: 'Blue' },
      stock: { quantity: 1000, unit: 'g', low_stock_threshold: null },
    });
    expect(sent.filter((r) => r.path === '/assets/new-id/links').map((r) => r.body)).toEqual([
      { type_id: 'in', asset_id: 'shelf', outgoing: false },
      { type_id: 'with', asset_id: 'prusa', outgoing: true },
    ]);
  });

  it('copies the photos of the one it is like, unless unticked', async () => {
    const photo = (id: string) => ({ id, thumb_id: `${id}t`, filename: `${id}.jpg`, content_type: 'image/jpeg', width_px: 4, height_px: 3, byte_size: 10, created_at: '' });
    const made = async (untick: boolean) => {
      navigate('/assets/new?like=spool');
      const api = fakeApi({
        'GET /categories': () => ({ categories: [] }),
        'GET /generators': () => ({ generators }),
        'GET /relation-types': () => ({ relation_types: [] }),
        'GET /assets/spool': () => ({ asset: { asset: { id: 'spool', name: 'Yellow PLA' }, photos: [photo('front'), photo('side')] } }),
        'GET /assets/spool/copy': () => ({ copy: { name: 'Yellow PLA', category_ids: [], field_values: {}, stock: null, generator_keys: [], links: [] } }),
        'POST /categories/effective-fields': () => ({ fields: [] }),
        'POST /assets': () => ({ status: 201, body: { asset: { asset: { id: 'new-id' }, location: [], links: [] } } }),
        'POST /assets/new-id/photos/copy': () => ({ asset: {} }),
      });
      render(AssetForm, { params: {} });
      const box = (await screen.findByRole('checkbox', { name: 'Copy its 2 photos' })) as HTMLInputElement;
      expect(box.checked).toBe(true);
      expect(document.querySelector('img')!.getAttribute('src')).toBe('/api/v1/assets/spool/photos/front?size=thumb');
      if (untick) await fireEvent.click(box);
      await fireEvent.submit(screen.getByRole('button', { name: 'Create asset' }));
      await vi.waitFor(() => expect(router.path).toBe('/assets/new-id'));
      cleanup();
      return api.sent.filter((r) => r.path === '/assets/new-id/photos/copy').map((r) => r.body);
    };
    expect(await made(false)).toEqual([{ from_asset_id: 'spool' }]);
    expect(await made(true)).toEqual([]);
  });

  it('starts in a place, in a category, or linked to another, from the new asset’s side', async () => {
    const rel = (id: string, name: string, inverse: string, over = {}) => ({
      id, name, inverse_name: inverse, is_symmetric: false, is_single: false, is_location: false, created_at: '', updated_at: '', link_count: 0, ...over,
    });
    const named = (id: string, name: string) => () => ({ asset: { asset: { id, name } } });
    const routes = {
      'GET /categories': () => ({ categories: [cat('tools', 'Tools'), cat('bits', 'Bits')] }),
      'GET /generators': () => ({ generators }),
      'GET /relation-types': () => ({ relation_types: [rel('in', 'contains', 'contained in', { is_single: true, is_location: true }), rel('part', 'has part', 'part of')] }),
      'GET /assets/bin': named('bin', 'Bin A3'),
      'GET /assets/drill': named('drill', 'Drill'),
      'POST /categories/effective-fields': () => ({ fields: [] }),
      'POST /assets': () => ({ status: 201, body: { asset: { asset: { id: 'new-id' }, location: [], links: [] } } }),
      'POST /assets/new-id/links': () => ({ asset: {} }),
    };

    // In a bin, and a category; and a part of a drill.
    navigate('/assets/new?in=bin&category=bits&link=part:in&to=drill');
    const { sent } = fakeApi(routes);
    render(AssetForm, { params: {} });
    await fireEvent.input(await screen.findByLabelText('Name'), { target: { value: 'Chuck key' } });
    expect((screen.getByRole('checkbox', { name: 'Bits' }) as HTMLInputElement).checked).toBe(true);
    expect((screen.getByRole('checkbox', { name: 'Tools' }) as HTMLInputElement).checked).toBe(false);
    expect(screen.getByText('Bin A3')).toBeTruthy();
    expect((screen.getByRole('checkbox', { name: 'part of Drill' }) as HTMLInputElement).checked).toBe(true);
    expect(screen.getByRole('link', { name: 'Cancel' }).getAttribute('href')).toBe('/assets/bin');
    await fireEvent.submit(screen.getByRole('button', { name: 'Create asset' }));
    await vi.waitFor(() => expect(router.path).toBe('/assets/new-id'));
    expect(sent.find((r) => r.method === 'POST' && r.path === '/assets')!.body).toMatchObject({ category_ids: ['bits'] });
    expect(sent.filter((r) => r.path === '/assets/new-id/links').map((r) => r.body)).toEqual([
      { type_id: 'in', asset_id: 'bin', outgoing: false },
      { type_id: 'part', asset_id: 'drill', outgoing: false },
    ]);
    cleanup();

    // A location link read as "contained in" is where it is.
    navigate('/assets/new?link=in:in&to=bin');
    fakeApi(routes);
    render(AssetForm, { params: {} });
    expect(await screen.findByText('Bin A3')).toBeTruthy();
    expect(screen.queryByRole('checkbox', { name: /contained in/ })).toBeNull();
    cleanup();

    // A category alone goes back to it.
    navigate('/assets/new?category=tools');
    fakeApi(routes);
    render(AssetForm, { params: {} });
    expect(((await screen.findByRole('checkbox', { name: 'Tools' })) as HTMLInputElement).checked).toBe(true);
    expect(screen.getByRole('link', { name: 'Cancel' }).getAttribute('href')).toBe('/categories/tools');
  });

  it('creates one and keeps the form for the next, with the next code in the series', async () => {
    let n = 0;
    const { sent } = fakeApi({
      'GET /categories': () => ({ categories: [cat('filament', 'Filament')] }),
      'GET /generators': () => ({ generators }),
      'GET /relation-types': () => ({ relation_types: [] }),
      'POST /categories/effective-fields': () => ({ fields: [field('colour', 'Color')] }),
      'POST /assets': (b) => ({ status: 201, body: { asset: { asset: { id: `s${++n}`, name: (b as { name: string }).name }, location: [], links: [] } } }),
      'GET /assets/s1/copy': () => ({ copy: { generator_keys: [{ generator_type: 'qr', assigned_code: 'SP-016' }] } }),
    });
    render(AssetForm, { params: {} });
    const nameInput = (await screen.findByLabelText('Name')) as HTMLInputElement;
    await fireEvent.input(nameInput, { target: { value: 'Yellow PLA' } });
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Filament' }));
    await fireEvent.input(await screen.findByLabelText('Color'), { target: { value: 'Yellow' } });
    await fireEvent.input(screen.getByLabelText('QR Code'), { target: { value: 'SP-015' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Create and add another' }));

    // Still here, with what it was, but the next code; and it says what it made.
    expect(await screen.findByRole('link', { name: 'Yellow PLA' })).toBeTruthy();
    expect(screen.getByRole('status').textContent).toMatch(/Created Yellow PLA\. The form is ready for the next\./);
    expect(router.path).not.toBe('/assets/s1');
    await vi.waitFor(() => expect((screen.getByLabelText('QR Code') as HTMLInputElement).value).toBe('SP-016'));
    expect(nameInput.value).toBe('Yellow PLA');
    expect(document.activeElement).toBe(nameInput);
    expect((screen.getByLabelText('Color') as HTMLInputElement).value).toBe('Yellow');
    expect((screen.getByRole('checkbox', { name: 'Filament' }) as HTMLInputElement).checked).toBe(true);

    // The next, and then the last as usual.
    await fireEvent.input(nameInput, { target: { value: 'Blue PLA' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Create asset' }));
    await vi.waitFor(() => expect(router.path).toBe('/assets/s2'));
    expect(sent.filter((r) => r.method === 'POST' && r.path === '/assets').map((r) => (r.body as { generator_keys: unknown }).generator_keys)).toEqual([
      [{ generator_type: 'qr', assigned_code: 'SP-015' }],
      [{ generator_type: 'qr', assigned_code: 'SP-016' }],
    ]);
  });

  it('shows a server error beside the input it is about', async () => {
    const existing: AssetWithDetails = {
      asset: { id: 'a1', name: 'Drill', created_at: '', updated_at: '', condition: 'ok', condition_note: '' },
      categories: [cat('tools', 'Tools')],
      generator_keys: [],
      field_values: { weight: '', old_note: 'from before' },
      effective_fields: [field('weight', 'Weight', false, 'number')],
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
    };
    const { sent } = fakeApi({
      'GET /categories': () => ({ categories: [cat('tools', 'Tools')] }),
      'GET /generators': () => ({ generators }),
      'GET /relation-types': () => ({ relation_types: [] }),
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
      'GET /relation-types': () => ({ relation_types: [] }),
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
      asset: { id: 'a1', name: 'M3 screws', created_at: '', updated_at: '', condition: 'ok', condition_note: '', stock: { quantity: 40, unit: 'pcs', low_stock_threshold: 50, is_running_low: true } },
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
    };
    const edit = fakeApi({
      'GET /categories': () => ({ categories: [] }),
      'GET /generators': () => ({ generators }),
      'GET /relation-types': () => ({ relation_types: [] }),
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

  it('adds a new asset’s label to the print queue, remembering whether to next time', async () => {
    session.permissions = ['assets.read', 'assets.write', 'labels.print'];
    session.user = { id: 'ada', email: 'ada@hackspace.org', name: 'Ada', is_active: true, created_at: '', updated_at: '' };
    localStorage.clear();
    const routes = {
      'GET /categories': () => ({ categories: [] }),
      'GET /generators': () => ({ generators: [] }),
      'GET /relation-types': () => ({ relation_types: [] }),
      'POST /categories/effective-fields': () => ({ fields: [] }),
      'POST /assets': () => ({ status: 201, body: { asset: { asset: { id: 'new-id' } } } }),
      'POST /spool': () => ({ status: 201, body: { added: [{ id: 'e' }], already_waiting: [], skipped: [] } }),
    };
    let { sent } = fakeApi(routes);
    render(AssetForm, { params: {} });
    const box = (await screen.findByRole('checkbox', { name: 'Add a label to the print queue' })) as HTMLInputElement;
    expect(box.checked).toBe(true);
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Drill' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Create asset' }));
    await vi.waitFor(() => expect(router.path).toBe('/assets/new-id'));
    expect(sent.find((r) => r.path === '/spool')!.body).toEqual({ asset_ids: ['new-id'] });
    cleanup();

    // Unticked once, it stays unticked, and nothing is queued.
    ({ sent } = fakeApi(routes));
    render(AssetForm, { params: {} });
    await fireEvent.click(await screen.findByRole('checkbox', { name: 'Add a label to the print queue' }));
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Saw' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Create asset' }));
    await vi.waitFor(() => expect(sent.some((r) => r.path === '/assets' && r.method === 'POST')).toBe(true));
    await new Promise((r) => setTimeout(r, 20));
    expect(sent.some((r) => r.path === '/spool')).toBe(false);
    cleanup();
    fakeApi(routes);
    render(AssetForm, { params: {} });
    expect(((await screen.findByRole('checkbox', { name: 'Add a label to the print queue' })) as HTMLInputElement).checked).toBe(false);
  });

  it('offers the spool only to those who print labels, and only for new assets', async () => {
    fakeApi({ 'GET /categories': () => ({ categories: [] }), 'GET /generators': () => ({ generators: [] }), 'POST /categories/effective-fields': () => ({ fields: [] }) });
    render(AssetForm, { params: {} });
    await screen.findByLabelText('Name');
    expect(screen.queryByRole('checkbox', { name: 'Add a label to the print queue' })).toBeNull();
  });
});

describe('AssetForm icons', () => {
  const drawn = (name: string) => ({ name, category: 'storage', viewbox: [0, 0, 10, 10] as [number, number, number, number], describe: [], grants: [], shapes: [] });
  // Bins draws Storage's icon, inherited
  const storage = cat('storage', 'Storage');
  const bins = { ...cat('bins', 'Bins'), parent_id: 'storage', color_hex: '#aa0000' };
  const editing = (iconName: string | null): AssetWithDetails => ({
    asset: { id: 'a1', name: 'Bin', created_at: '', updated_at: '', condition: 'ok', condition_note: '', icon_name: iconName },
    categories: [bins],
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
  let n = 0;
  function serve(existing: AssetWithDetails) {
    session.user = { id: `icon-form-${++n}`, email: 'ada@hackspace.org', name: '', is_active: true, created_at: '', updated_at: '' };
    return fakeApi({
      'GET /categories': () => ({ categories: [storage, bins] }),
      'GET /generators': () => ({ generators }),
      'GET /relation-types': () => ({ relation_types: [] }),
      'GET /assets/a1': () => ({ asset: existing }),
      'POST /categories/effective-fields': () => ({ fields: [] }),
      'PUT /assets/a1': () => ({ asset: existing }),
      'GET /icons': () => ({ category_icons: { storage: 'crate', bins: 'crate' } }),
      'GET /icons/ir': () => ({ ir_version: 1, icons: [drawn('crate'), drawn('crate-down')] }),
      'GET /icons/palette': () => ({ palette: [] }),
    });
  }
  const put = (sent: { method: string; body: unknown }[]) => sent.find((r) => r.method === 'PUT')?.body as Record<string, unknown>;
  afterEach(() => {
    session.user = null;
  });

  it("shows the icon its categories give it, and sends one of its own only once chosen", async () => {
    const { sent } = serve(editing(null));
    render(AssetForm, { params: { id: 'a1' } });
    expect(await screen.findByText('From Storage.')).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Choose another…' }));
    await fireEvent.click(screen.getByRole('button', { name: 'crate-down' }));
    expect(screen.getByText(/Its own,/).textContent).toContain('crate-down');
    expect(screen.queryByRole('group', { name: 'Choose an icon' })).toBeNull();
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(put(sent)).toBeTruthy());
    expect(put(sent).icon_name).toBe('crate-down');
  });

  it("leaves an icon of its own alone unless it changes, and drops it for the category's", async () => {
    const first = serve(editing('gone'));
    render(AssetForm, { params: { id: 'a1' } });
    // no longer drawn: said so, and kept as it is when saved
    expect((await screen.findByText(/Its own,/)).textContent).toContain('no icon is drawn by that name any more');
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(put(first.sent)).toBeTruthy());
    expect(put(first.sent)).not.toHaveProperty('icon_name');

    cleanup();
    vi.unstubAllGlobals();
    const second = serve(editing('crate-down'));
    render(AssetForm, { params: { id: 'a1' } });
    await fireEvent.click(await screen.findByRole('button', { name: "Use the category's" }));
    expect(screen.getByText('From Storage.')).toBeTruthy();
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(put(second.sent)).toBeTruthy());
    expect(put(second.sent).icon_name).toBeNull();
  });
});
