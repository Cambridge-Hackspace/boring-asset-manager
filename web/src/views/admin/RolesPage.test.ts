import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import RolesPage from './RolesPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { Role, Scope } from '../../lib/models';

const admin: Role = { id: 'admin', name: 'admin', description: 'Everything', permissions: ['assets.read', 'users.manage'], user_count: 1, protected: true, public: false };
const stores: Role = { id: 'stores', name: 'Stores', description: '', permissions: ['assets.read', 'stock.record'], user_count: 2, protected: false, public: false };
const defaultScope: Scope = { id: 'd', name: 'default', created_at: '', reserved: true, category_count: 0, role_count: 0, assignment_count: 0 };
const tools: Scope = { id: 't', name: 'tools', created_at: '', reserved: false, category_count: 1, role_count: 1, assignment_count: 0 };

beforeEach(() => {
  session.permissions = ['users.manage'];
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
      'GET /scopes': () => ({ scopes: [defaultScope] }),
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
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ name: 'Readers', description: '', permissions: ['assets.read'] });

    await fireEvent.click(within(storesCard).getByRole('button', { name: 'Edit Stores' }));
    await fireEvent.click(screen.getByRole('checkbox', { name: /Print labels/ }));
    await fireEvent.click(screen.getByRole('button', { name: 'Save role' }));
    await screen.findByText('Saved Stores.');
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({ name: 'Stores', description: '', permissions: ['assets.read', 'stock.record', 'labels.print'] });

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
      'GET /scopes': () => ({ scopes: [defaultScope] }),
      'DELETE /roles/stores': () => ({ status: 400, body: { error: "anti-lockout guard: operation rejected because at least one active user must retain 'users.manage'" } }),
    });
    render(RolesPage, { params: {} });
    const card = (await screen.findByRole('heading', { name: 'Stores' })).closest('li')!;
    await fireEvent.click(within(card).getByRole('button', { name: 'Delete' }));
    await fireEvent.click(within(screen.getByRole('group')).getByRole('button', { name: 'Delete' }));
    expect((await screen.findByRole('alert')).textContent).toContain('would remove the last one');
  });

  it('gives permissions in some scopes only, and manages scopes', async () => {
    const keepers: Role = { id: 'keepers', name: 'Keepers', description: '', permissions: ['assets.read', 'tools:assets.write'], user_count: 0, protected: false, public: false };
    let scopes = [defaultScope, tools];
    const { sent } = fakeApi({
      'GET /roles': () => ({ roles: [keepers] }),
      'GET /scopes': () => ({ scopes }),
      'PUT /roles/keepers': (body) => ({ role: { ...keepers, ...(body as object) } }),
      'POST /scopes': () => {
        scopes = [...scopes, { ...tools, id: 'k', name: 'kitchen', category_count: 0, role_count: 0 }];
        return { status: 201, body: {} };
      },
      'PUT /scopes/t': () => ({}),
      'DELETE /scopes/k': () => {
        scopes = scopes.filter((s) => s.id !== 'k');
        return { message: 'Scope deleted' };
      },
    });
    render(RolesPage, { params: {} });

    const card = (await screen.findByRole('heading', { name: 'Keepers' })).closest('li')!;
    expect(card.textContent).toContain('Read assets, Edit assets in tools');

    // Editing in the default scope as well as the tools.
    await fireEvent.click(within(card).getByRole('button', { name: 'Edit Keepers' }));
    expect((screen.getByRole('combobox', { name: 'Where: Read assets' }) as HTMLSelectElement).value).toBe('all');
    expect((screen.getByRole('combobox', { name: 'Where: Edit assets' }) as HTMLSelectElement).value).toBe('some');
    expect((screen.getByRole('checkbox', { name: 'Edit assets in tools' }) as HTMLInputElement).checked).toBe(true);
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Edit assets in default' }));
    // Unscoped permissions offer no choice.
    await fireEvent.click(screen.getByRole('checkbox', { name: /Print labels/ }));
    expect(screen.queryByRole('combobox', { name: 'Where: Print labels' })).toBeNull();
    // Narrowing reading to some scopes keeps it chosen until one is ticked.
    await fireEvent.change(screen.getByRole('combobox', { name: 'Where: Read assets' }), { target: { value: 'some' } });
    expect((screen.getByRole('checkbox', { name: /^Read assets — / }) as HTMLInputElement).checked).toBe(true);
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Read assets in tools' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Save role' }));
    await screen.findByText('Saved Keepers.');
    expect((sent.find((r) => r.method === 'PUT')!.body as Role).permissions).toEqual([
      'tools:assets.write',
      'default:assets.write',
      'labels.print',
      'tools:assets.read',
    ]);

    // Scopes: the default cannot be changed; others are added, renamed and deleted.
    const defaultCard = screen.getByRole('heading', { name: 'default' }).closest('li')!;
    expect(within(defaultCard).queryByRole('button')).toBeNull();
    expect(screen.getByRole('heading', { name: 'tools' }).closest('li')!.textContent).toContain('On 1 category; used by 1 role.');
    await fireEvent.input(screen.getByLabelText('New scope'), { target: { value: 'kitchen' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Create scope' }));
    await screen.findByText('Created the scope kitchen.');
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ name: 'kitchen' });

    await fireEvent.click(screen.getByRole('button', { name: 'Rename tools' }));
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'workshop' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Rename' }));
    await screen.findByText('Renamed the scope to workshop.');
    expect(sent.find((r) => r.method === 'PUT' && r.path === '/scopes/t')!.body).toEqual({ name: 'workshop' });

    const kitchen = (await screen.findByRole('heading', { name: 'kitchen' })).closest('li')!;
    await fireEvent.click(within(kitchen).getByRole('button', { name: 'Delete scope' }));
    await fireEvent.click(within(within(kitchen).getByRole('group')).getByRole('button', { name: 'Delete scope' }));
    await screen.findByText('Deleted the scope kitchen.');
  });

  it('lets the public role only look, under its own name', async () => {
    const publicRole: Role = { id: 'p', name: 'public', description: 'Everyone, signed in or not', permissions: ['public:assets.read'], user_count: 0, protected: false, public: true };
    const publicScope: Scope = { ...tools, id: 'ps', name: 'public', reserved: true, category_count: 2, role_count: 1 };
    fakeApi({
      'GET /roles': () => ({ roles: [publicRole] }),
      'GET /scopes': () => ({ scopes: [defaultScope, publicScope] }),
    });
    render(RolesPage, { params: {} });
    // The role, then the scope of the same name.
    const card = (await screen.findAllByRole('heading', { name: 'public' }))[0]!.closest('li')!;
    expect(card.textContent).toContain('Held by everyone, signed in or not.');
    expect(card.textContent).toContain('Read assets in public');
    expect(within(card).queryByRole('button', { name: 'Delete' })).toBeNull();

    await fireEvent.click(within(card).getByRole('button', { name: 'Edit public' }));
    expect((screen.getByLabelText('Name') as HTMLInputElement).readOnly).toBe(true);
    const offered = screen.getAllByRole('checkbox', { name: / — / });
    // Looking and scanning, nothing else.
    expect(offered.map((c) => c.parentElement!.textContent!.trim().split(' — ')[0])).toEqual(['Read assets', 'Scan codes']);

    const scopeCard = screen.getAllByRole('heading', { name: 'public' })[1]!.closest('li')!;
    expect(scopeCard.textContent).toContain('make their assets public. On 2 categories.');
    expect(within(scopeCard).queryByRole('button')).toBeNull();
  });
});
