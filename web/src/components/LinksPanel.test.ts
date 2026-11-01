import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import LinksPanel from './LinksPanel.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';
import type { AssetWithDetails, RelationType } from '../lib/models';

const contains: RelationType = {
  id: 'c', name: 'contains', inverse_name: 'contained in', is_symmetric: false, is_single: true, is_location: true, created_at: '', updated_at: '', link_count: 0,
};
const asset = (id: string, name: string, over: Partial<AssetWithDetails> = {}): AssetWithDetails => ({
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
  ...over,
});

beforeEach(() => {
  session.permissions = ['assets.read', 'assets.write'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('LinksPanel', () => {
  it('starts a new asset linked as chosen, read from the new one', async () => {
    const usedWith: RelationType = { ...contains, id: 'w', name: 'used with', inverse_name: 'used with', is_symmetric: true, is_single: false, is_location: false };
    fakeApi({ 'GET /relation-types': () => ({ relation_types: [contains, usedWith] }) });
    render(LinksPanel, { asset: asset('bin', 'Bin'), onchange: () => {} });
    const select = await screen.findByLabelText('This asset');
    const link = () => screen.getByRole('link', { name: 'New asset…' }).getAttribute('href');
    // The bin contains the new one: the new one is contained in the bin.
    await fireEvent.change(select, { target: { value: 'c:out' } });
    expect(link()).toBe('/assets/new?link=c:in&to=bin');
    await fireEvent.change(select, { target: { value: 'c:in' } });
    expect(link()).toBe('/assets/new?link=c:out&to=bin');
    // Read the same both ways.
    await fireEvent.change(select, { target: { value: 'w:out' } });
    expect(link()).toBe('/assets/new?link=w:out&to=bin');
  });

  it('finds an asset and links to it, reading from this one', async () => {
    const { sent } = fakeApi({
      'GET /relation-types': () => ({ relation_types: [contains] }),
      'GET /assets': () => ({ assets: [asset('screws', 'M3 screws'), asset('bin', 'Bin A3')] }),
      'POST /assets/screws/links': () => ({ asset: asset('screws', 'M3 screws') }),
    });
    const done: string[] = [];
    render(LinksPanel, { asset: asset('screws', 'M3 screws'), onchange: (_a: AssetWithDetails, d: string) => done.push(d) });
    await screen.findByRole('option', { name: 'contained in' });
    await fireEvent.change(screen.getByLabelText('This asset'), { target: { value: 'c:in' } });
    await fireEvent.input(screen.getByLabelText('Find an asset'), { target: { value: 'bin' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Find' }));
    // Not itself.
    expect(await screen.findByRole('option', { name: 'Bin A3' })).toBeTruthy();
    expect(screen.queryByRole('option', { name: 'M3 screws' })).toBeNull();
    await fireEvent.click(screen.getByRole('button', { name: 'Add link' }));
    await vi.waitFor(() => expect(done).toEqual(['Now contained in Bin A3.']));
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ type_id: 'c', asset_id: 'bin', outgoing: false });
  });

  it('lists links and unlinks one; readers only see them', async () => {
    const linked = asset('bin', 'Bin A3', {
      links: [{ id: 'l1', type_id: 'c', label: 'contains', asset: { id: 'screws', name: 'M3 screws' }, outgoing: true }],
    });
    const { sent } = fakeApi({
      'GET /relation-types': () => ({ relation_types: [contains] }),
      'DELETE /assets/bin/links/l1': () => ({ asset: asset('bin', 'Bin A3') }),
    });
    const done: string[] = [];
    render(LinksPanel, { asset: linked, onchange: (_a: AssetWithDetails, d: string) => done.push(d) });
    expect(screen.getByText('Contains')).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Unlink M3 screws' }));
    await vi.waitFor(() => expect(done).toEqual(['No longer contains M3 screws.']));
    expect(sent.some((r) => r.method === 'DELETE')).toBe(true);

    cleanup();
    session.permissions = ['assets.read'];
    render(LinksPanel, { asset: linked, onchange: () => {} });
    expect(screen.getByRole('link', { name: 'M3 screws' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Unlink/ })).toBeNull();
    expect(screen.queryByLabelText('Find an asset')).toBeNull();
  });
});
