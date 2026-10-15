import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import RolesPage from './RolesPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { Role } from '../../lib/models';

const admin: Role = { id: 'admin', name: 'admin', description: 'Everything', permissions: ['assets:read', 'users:manage'], user_count: 1, protected: true };
const stores: Role = { id: 'stores', name: 'Stores', description: '', permissions: ['assets:read', 'stock:record'], user_count: 2, protected: false };

beforeEach(() => {
  session.permissions = ['users:manage'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('RolesPage', () => {
  it('creates, edits and deletes roles, leaving admin alone', async () => {
    let roles = [admin, stores];
    const { sent } = fakeApi({
      'GET /roles': () => ({ roles }),
      'POST /roles': (body) => {
        const r = { id: 'readers', user_count: 0, protected: false, ...(body as object) } as Role;
        roles = [...roles, r];
        return { status: 201, body: { role: r } };
      },
      'PUT /roles/stores': (body) => ({ role: { ...stores, ...(body as object) } }),
      'DELETE /roles/stores': () => {
        roles = roles.filter((r) => r.id !== 'stores');
        return { message: 'Role deleted' };
      },
    });
    render(RolesPage, { params: {} });

    const adminCard = (await screen.findByRole('heading', { name: 'admin' })).closest('li')!;
    expect(adminCard.textContent).toContain('always has every permission');
    expect(within(adminCard).queryByRole('button')).toBeNull();
    const storesCard = screen.getByRole('heading', { name: 'Stores' }).closest('li')!;
    expect(storesCard.textContent).toContain('Held by 2 users.');
    expect(storesCard.textContent).toContain('Read assets, Record stock');

    // A new role starts with read access.
    await fireEvent.click(screen.getByRole('button', { name: 'New role' }));
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Readers' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Create role' }));
    await screen.findByText('Created Readers.');
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ name: 'Readers', description: '', permissions: ['assets:read'] });

    await fireEvent.click(within(storesCard).getByRole('button', { name: 'Edit Stores' }));
    await fireEvent.click(screen.getByRole('checkbox', { name: /Print labels/ }));
    await fireEvent.click(screen.getByRole('button', { name: 'Save role' }));
    await screen.findByText('Saved Stores.');
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({ name: 'Stores', description: '', permissions: ['assets:read', 'stock:record', 'labels:print'] });

    // Deleting warns how many hold it.
    await fireEvent.click(within(storesCard).getByRole('button', { name: 'Delete' }));
    const confirm = screen.getByRole('group', { name: 'Delete Stores? 2 users will lose what it allows.' });
    await fireEvent.click(within(confirm).getByRole('button', { name: 'Delete' }));
    await screen.findByText('Deleted Stores.');
    await vi.waitFor(() => expect(screen.queryByText('Stores')).toBeNull());
  });

  it('explains an anti-lockout refusal', async () => {
    fakeApi({
      'GET /roles': () => ({ roles: [stores] }),
      'DELETE /roles/stores': () => ({ status: 400, body: { error: "anti-lockout guard: operation rejected because at least one active user must retain 'users:manage'" } }),
    });
    render(RolesPage, { params: {} });
    const card = (await screen.findByRole('heading', { name: 'Stores' })).closest('li')!;
    await fireEvent.click(within(card).getByRole('button', { name: 'Delete' }));
    await fireEvent.click(within(screen.getByRole('group')).getByRole('button', { name: 'Delete' }));
    expect((await screen.findByRole('alert')).textContent).toContain('would remove the last one');
  });
});
