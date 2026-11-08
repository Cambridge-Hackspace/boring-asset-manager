import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import RelationsPage from './RelationsPage.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';
import type { RelationType } from '../lib/models';

const contains: RelationType = {
  id: 'c', name: 'contains', inverse_name: 'contained in', is_symmetric: false, is_single: true, is_location: true, is_followed: false, created_at: '', updated_at: '', link_count: 4,
};

beforeEach(() => {
  session.permissions = ['categories.manage'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('RelationsPage', () => {
  it('makes a symmetric relation, and keeps one in use from changing how it behaves', async () => {
    let types = [contains];
    const { sent } = fakeApi({
      'GET /relation-types': () => ({ relation_types: types }),
      'POST /relation-types': (body) => {
        types = [...types, { ...contains, id: 'p', link_count: 0, ...(body as object) }];
        return { status: 201, body: {} };
      },
    });
    render(RelationsPage, { params: {} });
    const card = (await screen.findByRole('heading', { name: 'contains / contained in' })).closest('li')!;
    expect(card.textContent).toContain('4 links');
    expect(within(card).queryByRole('button', { name: 'Delete' })).toBeNull();

    await fireEvent.click(within(card).getByRole('button', { name: 'Edit contains' }));
    expect((screen.getByRole('checkbox', { name: /Reads the same both ways/ }) as HTMLInputElement).disabled).toBe(true);
    expect(screen.getByText('Links use it, so only its names, and whether it is followed, can change.')).toBeTruthy();
    // Whether it is followed changes while links use it.
    expect((screen.getByRole('checkbox', { name: /Follow its links through several hops/ }) as HTMLInputElement).disabled).toBe(false);
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

    await fireEvent.click(screen.getByRole('button', { name: 'New relation' }));
    await fireEvent.input(screen.getByLabelText('Name, from the first asset'), { target: { value: 'pairs with' } });
    await fireEvent.click(screen.getByRole('checkbox', { name: /Reads the same both ways/ }));
    expect((screen.getByLabelText('Name, from the second') as HTMLInputElement).disabled).toBe(true);
    await fireEvent.click(screen.getByRole('button', { name: 'Create relation' }));
    await screen.findByText('Saved pairs with.');
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({
      name: 'pairs with', inverse_name: 'pairs with', is_symmetric: true, is_single: false, is_location: false, is_followed: false,
    });
  });

  it('marks a relation followed, and says so', async () => {
    let types = [contains];
    const { sent } = fakeApi({
      'GET /relation-types': () => ({ relation_types: types }),
      'POST /relation-types': (body) => {
        types = [...types, { ...contains, id: 'd', link_count: 0, ...(body as object) }];
        return { status: 201, body: {} };
      },
    });
    render(RelationsPage, { params: {} });
    await screen.findByRole('heading', { name: 'contains / contained in' });
    await fireEvent.click(screen.getByRole('button', { name: 'New relation' }));
    await fireEvent.input(screen.getByLabelText('Name, from the first asset'), { target: { value: 'powered by' } });
    await fireEvent.input(screen.getByLabelText('Name, from the second'), { target: { value: 'powers' } });
    await fireEvent.click(screen.getByRole('checkbox', { name: /Follow its links through several hops/ }));
    await fireEvent.click(screen.getByRole('button', { name: 'Create relation' }));
    await screen.findByText('Saved powered by.');
    expect((sent.find((r) => r.method === 'POST')!.body as { is_followed: boolean }).is_followed).toBe(true);
    const card = (await screen.findByRole('heading', { name: 'powered by / powers' })).closest('li')!;
    expect(card.textContent).toContain('Followed through several hops');
  });
});
