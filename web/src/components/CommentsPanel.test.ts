import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import CommentsPanel from './CommentsPanel.svelte';
import { fakeApi } from '../test/fetch';
import type { Comment } from '../lib/models';

const comment = (id: string, over: Partial<Comment> = {}): Comment => ({
  id,
  on: 'asset',
  on_id: 'a1',
  asset_id: 'a1',
  body: `Body of ${id}`,
  html: `<p>Body of ${id}</p>`,
  author_id: 'u1',
  author_name: 'Ada',
  mine: false,
  created_at: '2026-11-05T10:00:00Z',
  edited_at: null,
  deleted_at: null,
  can_edit: false,
  can_delete: false,
  ...over,
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('CommentsPanel', () => {
  it('lists its own comments, then its categories’, each linking to its category, and narrows to its own', async () => {
    fakeApi({
      'GET /assets/a1/comments': () => ({
        comments: [
          comment('c1', { html: '<p>The left motor <strong>whines</strong></p>', edited_at: '2026-11-05T11:00:00Z' }),
          comment('c2', { author_name: 'Bo', on: 'category', on_id: 'k1', asset_id: undefined, category: { id: 'k1', name: 'Drills' } }),
          comment('c3', { author_name: 'Cy', deleted_at: '2026-11-05T12:00:00Z', body: '', html: '', deleted_by_name: 'Mo' }),
        ],
      }),
    });
    render(CommentsPanel, { id: 'comments', path: '/assets/a1/comments', canWrite: false, own: 'this asset’s' });

    const first = await screen.findByRole('article', { name: 'Comment by Ada' });
    expect(first.id).toBe('comment-c1');
    expect(within(first).getByText('whines').tagName).toBe('STRONG');
    expect(within(first).getByText('(edited)')).toBeTruthy();
    const link = screen.getByRole('link', { name: 'Drills' });
    expect(link.getAttribute('href')).toBe('/categories/k1?tab=comments#comment-c2');
    expect(screen.getByText(/Deleted by Mo/)).toBeTruthy();
    expect(screen.queryByLabelText('Add a comment')).toBeNull();

    await fireEvent.click(screen.getByLabelText('Only this asset’s'));
    expect(screen.queryByRole('link', { name: 'Drills' })).toBeNull();
    expect(screen.getByText(/whines/)).toBeTruthy();
  });

  it('adds, edits and deletes one’s own, attaching a file while writing', async () => {
    let added: Comment | null = null;
    const { sent } = fakeApi({
      'GET /assets/a1/comments': () => ({ comments: [] }),
      'POST /assets/a1/comments/attachments': () => ({ status: 201, body: { markdown: '![belt.png](/api/v1/assets/a1/attachments/f1)' } }),
      'POST /assets/a1/comments': (body) => {
        added = comment('c9', { body: (body as { body: string }).body, mine: true, can_edit: true, can_delete: true });
        return { status: 201, body: { comment: added } };
      },
      'PUT /comments/c9': (body) => ({ comment: { ...added!, body: (body as { body: string }).body, html: '<p>Fitted</p>', edited_at: '2026-11-05T13:00:00Z' } }),
      'DELETE /comments/c9': () => ({ comment: { ...added!, body: '', html: '', deleted_at: '2026-11-05T14:00:00Z', deleted_by_name: 'Ada' } }),
      'POST /markdown': () => ({ html: '<p><em>preview</em></p>' }),
    });
    const done: string[] = [];
    render(CommentsPanel, {
      id: 'comments',
      path: '/assets/a1/comments',
      uploadPath: '/assets/a1/comments/attachments',
      canWrite: true,
      own: 'this asset’s',
      onchange: (d: string) => done.push(d),
    });
    expect(await screen.findByText('No comments yet.')).toBeTruthy();

    const box = screen.getByLabelText('Add a comment') as HTMLTextAreaElement;
    await fireEvent.input(box, { target: { value: 'Belt ordered' } });
    await fireEvent.change(screen.getByLabelText('Attach a file'), { target: { files: [new File(['png'], 'belt.png', { type: 'image/png' })] } });
    await vi.waitFor(() => expect(box.value).toBe('Belt ordered\n![belt.png](/api/v1/assets/a1/attachments/f1)\n'));
    expect(sent.find((r) => r.path === '/assets/a1/comments/attachments')!.body).toBeInstanceOf(FormData);

    await fireEvent.click(screen.getByRole('button', { name: 'Preview' }));
    expect((await screen.findByText('preview')).tagName).toBe('EM');
    await fireEvent.click(screen.getByRole('button', { name: 'Comment' }));
    await vi.waitFor(() => expect(done).toContain('Comment added.'));
    expect(sent.find((r) => r.method === 'POST' && r.path === '/assets/a1/comments')!.body).toEqual({ body: 'Belt ordered\n![belt.png](/api/v1/assets/a1/attachments/f1)\n' });

    const mine = await screen.findByRole('article', { name: 'Comment by You' });
    await fireEvent.click(within(mine).getByRole('button', { name: 'Edit' }));
    await fireEvent.input(screen.getByLabelText('Edit the comment'), { target: { value: 'Fitted' } });
    await fireEvent.click(within(mine).getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(done).toContain('Comment edited.'));
    expect(await within(mine).findByText('(edited)')).toBeTruthy();

    await fireEvent.click(within(mine).getByRole('button', { name: 'Delete' }));
    await fireEvent.click(within(within(mine).getByRole('group')).getByRole('button', { name: 'Delete' }));
    await vi.waitFor(() => expect(done).toContain('Comment deleted.'));
    expect(await within(mine).findByText(/Deleted by Ada/)).toBeTruthy();
  });
});
