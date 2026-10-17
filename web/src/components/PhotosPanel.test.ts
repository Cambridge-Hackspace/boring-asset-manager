import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import PhotosPanel from './PhotosPanel.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';
import type { AssetPhoto, AssetWithDetails } from '../lib/models';

const photo = (id: string): AssetPhoto => ({ id, filename: `${id}.jpg`, content_type: 'image/jpeg', width_px: 4, height_px: 3, byte_size: 10, created_at: '' });
const drill = (photos: AssetPhoto[]): AssetWithDetails => ({
  asset: { id: 'a1', name: 'Drill', created_at: '', updated_at: '', condition: 'ok', condition_note: '' },
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
  photos,
});

beforeEach(() => {
  session.permissions = ['assets.read', 'assets.write'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('PhotosPanel', () => {
  it('uploads a photo, moves one, and removes one', async () => {
    const { sent } = fakeApi({
      'POST /assets/a1/photos': () => ({ status: 201, body: { asset: drill([photo('p1')]) } }),
      'PUT /assets/a1/photos': () => ({ asset: drill([photo('p2'), photo('p1')]) }),
      'DELETE /assets/a1/photos/p1': () => ({ asset: drill([photo('p2')]) }),
    });
    const done: string[] = [];
    render(PhotosPanel, { asset: drill([photo('p1'), photo('p2')]), onchange: (_a: AssetWithDetails, d: string) => done.push(d) });

    const first = screen.getByRole('img', { name: 'Photo 1 of Drill, shown in lists' });
    expect(first.getAttribute('src')).toBe('/api/v1/assets/a1/photos/p1?size=thumb');
    expect(first.closest('a')!.getAttribute('href')).toBe('/api/v1/assets/a1/photos/p1');

    const file = new File(['jpeg'], 'front.jpg', { type: 'image/jpeg' });
    await fireEvent.change(screen.getByLabelText('Add a photo'), { target: { files: [file] } });
    await vi.waitFor(() => expect(done).toContain('Photo added.'));
    expect(sent.find((r) => r.method === 'POST')!.body).toBeInstanceOf(FormData);

    await fireEvent.click(screen.getByRole('button', { name: 'Move photo 2 earlier' }));
    await vi.waitFor(() => expect(done).toContain('Photo moved earlier.'));
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({ photo_ids: ['p2', 'p1'] });

    const item = first.closest('li')!;
    await fireEvent.click(within(item).getByRole('button', { name: 'Remove' }));
    await fireEvent.click(within(within(item).getByRole('group')).getByRole('button', { name: 'Remove' }));
    await vi.waitFor(() => expect(done).toContain('Photo removed.'));
  });

  it('only shows photos to readers, and stops at ten', () => {
    session.permissions = ['assets.read'];
    fakeApi({});
    render(PhotosPanel, { asset: drill([photo('p1')]), onchange: () => {} });
    expect(screen.getByRole('img')).toBeTruthy();
    expect(screen.queryByLabelText('Add a photo')).toBeNull();
    expect(screen.queryByRole('button')).toBeNull();
    cleanup();
    session.permissions = ['assets.read', 'assets.write'];
    render(PhotosPanel, { asset: drill(Array.from({ length: 10 }, (_, i) => photo(`p${i}`))), onchange: () => {} });
    expect(screen.queryByLabelText('Add a photo')).toBeNull();
    expect(screen.getByText(/remove one to add another/)).toBeTruthy();
  });
});
