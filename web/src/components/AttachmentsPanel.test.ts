import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import AttachmentsPanel from './AttachmentsPanel.svelte';
import { fakeApi } from '../test/fetch';
import type { Attachment, AttachmentLimits } from '../lib/models';

const limits: AttachmentLimits = { max_file_bytes: 25 << 20, max_image_bytes: 15 << 20, max_per_object: 3, allowed_types: [] };
const image = (id: string): Attachment => ({ id, kind: 'image', name: `${id}.jpg`, description: '', filename: `${id}.jpg`, content_type: 'image/jpeg', byte_size: 10, width_px: 4, height_px: 3, created_at: '2026-11-05T10:00:00Z' });
const pdf = (id: string): Attachment => ({ id, kind: 'document', name: 'Manual', description: 'The maker’s manual', filename: 'manual.pdf', content_type: 'application/pdf', byte_size: 2_000, added_by_name: 'Ada', created_at: '2026-11-05T10:00:00Z' });

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('AttachmentsPanel', () => {
  it('shows images as photos and documents as downloads, and adds, moves, renames and removes them', async () => {
    const { sent } = fakeApi({
      'GET /assets/a1/attachments': () => ({ attachments: [image('p1'), pdf('d1')], limits }),
      'POST /assets/a1/attachments': () => ({ status: 201, body: { attachments: [image('p1'), pdf('d1'), image('p2')], limits } }),
      'PUT /assets/a1/attachments': () => ({ attachments: [pdf('d1'), image('p1'), image('p2')], limits }),
      'PUT /assets/a1/attachments/d1': () => ({ attachment: { ...pdf('d1'), name: 'Drill manual' } }),
      'DELETE /assets/a1/attachments/p1': () => ({ attachments: [pdf('d1'), image('p2')], limits }),
    });
    const done: string[] = [];
    render(AttachmentsPanel, { owner: 'asset', ownerId: 'a1', ownerName: 'Drill', canEdit: true, onchange: (d: string) => done.push(d) });

    const photo = await screen.findByRole('img', { name: 'Photo 1 of Drill, shown in lists' });
    expect(photo.getAttribute('src')).toBe('/api/v1/assets/a1/attachments/p1?size=thumb');
    expect(photo.closest('a')!.getAttribute('href')).toBe('/api/v1/assets/a1/attachments/p1');
    const download = screen.getByRole('link', { name: 'Download Manual' });
    expect(download.getAttribute('href')).toBe('/api/v1/assets/a1/attachments/d1');
    expect(download.getAttribute('download')).toBe('manual.pdf');
    expect(download.textContent).toContain('PDF');
    expect(screen.getByText('The maker’s manual')).toBeTruthy();
    expect(screen.getByText(/added by Ada/)).toBeTruthy();

    const file = new File(['jpeg'], 'side.jpg', { type: 'image/jpeg' });
    await fireEvent.change(screen.getByLabelText('Add a file'), { target: { files: [file] } });
    await vi.waitFor(() => expect(done).toContain('Attachment added.'));
    expect(sent.find((r) => r.method === 'POST')!.body).toBeInstanceOf(FormData);
    expect(await screen.findByText('This can have 3 attachments; remove one to add another.')).toBeTruthy();
    expect(screen.queryByLabelText('Add a file')).toBeNull();

    await fireEvent.click(screen.getByRole('button', { name: 'Move Manual earlier' }));
    await vi.waitFor(() => expect(done).toContain('Attachment moved earlier.'));
    expect(sent.find((r) => r.method === 'PUT' && r.path === '/assets/a1/attachments')!.body).toEqual({ attachment_ids: ['d1', 'p1', 'p2'] });

    await fireEvent.click(screen.getByRole('button', { name: 'Rename or describe Manual' }));
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Drill manual' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(done).toContain('Attachment saved.'));
    expect(sent.find((r) => r.path === '/assets/a1/attachments/d1')!.body).toEqual({ name: 'Drill manual', description: 'The maker’s manual' });
    expect(await screen.findByRole('link', { name: 'Download Drill manual' })).toBeTruthy();

    const item = screen.getByRole('img', { name: 'Photo 1 of Drill, shown in lists' }).closest('li')!;
    await fireEvent.click(within(item).getByRole('button', { name: 'Remove' }));
    await fireEvent.click(within(within(item).getByRole('group')).getByRole('button', { name: 'Remove' }));
    await vi.waitFor(() => expect(done).toContain('Attachment removed.'));
  });

  it('shows a category’s, and only shows them to those who cannot change them', async () => {
    fakeApi({ 'GET /categories/c1/attachments': () => ({ attachments: [pdf('d1')], limits }) });
    render(AttachmentsPanel, { owner: 'category', ownerId: 'c1', ownerName: 'Drills', canEdit: false, onchange: () => {} });
    expect((await screen.findByRole('link', { name: 'Download Manual' })).getAttribute('href')).toBe('/api/v1/categories/c1/attachments/d1');
    expect(screen.queryByLabelText('Add a file')).toBeNull();
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('shows a vendor’s, from the vendor’s own address', async () => {
    fakeApi({ 'GET /vendors/v1/attachments': () => ({ attachments: [pdf('d1'), image('i1')], limits }) });
    render(AttachmentsPanel, { owner: 'vendor', ownerId: 'v1', ownerName: 'Acme', canEdit: false, onchange: () => {} });
    expect((await screen.findByRole('link', { name: 'Download Manual' })).getAttribute('href')).toBe('/api/v1/vendors/v1/attachments/d1');
    expect(screen.getByRole('img', { name: 'Photo 1 of Acme' }).getAttribute('src')).toBe('/api/v1/vendors/v1/attachments/i1?size=thumb');
  });

  it('says when there are none, and what may be added', async () => {
    fakeApi({ 'GET /categories/c1/attachments': () => ({ attachments: [], limits: { ...limits, allowed_types: ['application/zip'] } }) });
    render(AttachmentsPanel, { owner: 'category', ownerId: 'c1', ownerName: 'Drills', canEdit: true, onchange: () => {} });
    expect(await screen.findByText('No attachments.')).toBeTruthy();
    expect((screen.getByLabelText('Add a file') as HTMLInputElement).accept).toContain('application/zip');
    expect(screen.getByText(/and application\/zip up to/)).toBeTruthy();
  });
});
