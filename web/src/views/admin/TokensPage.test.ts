import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import TokensPage from './TokensPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { APIToken } from '../../lib/models';

const roles = [{ id: 'counters', name: 'Counters', description: '', permissions: ['stock:record'], user_count: 0, protected: false }];
const old: APIToken = {
  id: 't1',
  name: 'Old script',
  role_ids: ['counters'],
  created_at: '2026-01-01T00:00:00Z',
  expires_at: null,
  last_used_at: null,
  revoked_at: '2026-02-01T00:00:00Z',
};

beforeEach(() => {
  session.permissions = ['users:manage'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('TokensPage', () => {
  it('makes a token, shows its secret once, and revokes it', async () => {
    let tokens: APIToken[] = [old];
    const { sent } = fakeApi({
      'GET /tokens': () => ({ tokens }),
      'GET /roles': () => ({ roles }),
      'POST /tokens': (body) => {
        const b = body as { name: string; role_ids: string[]; expires_at: string | null };
        const t: APIToken = { ...old, id: 't2', name: b.name, role_ids: b.role_ids, expires_at: b.expires_at, revoked_at: null };
        tokens = [t, ...tokens];
        return { status: 201, body: { token: t, secret: 'bam_secret' } };
      },
      'DELETE /tokens/t2': () => {
        tokens = tokens.map((t) => (t.id === 't2' ? { ...t, revoked_at: '2026-10-16T00:00:00Z' } : t));
        return { token: tokens[0] };
      },
    });
    render(TokensPage, { params: {} });

    const oldRow = (await screen.findByRole('cell', { name: 'Old script' })).closest('tr')!;
    expect(oldRow.textContent).toContain('Revoked');
    expect(within(oldRow).queryByRole('button')).toBeNull();

    await fireEvent.click(screen.getByRole('button', { name: 'New token' }));
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Dashboard' } });
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Counters' }));
    await fireEvent.change(screen.getByLabelText('Expires after'), { target: { value: '' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Create token' }));

    expect(((await screen.findByLabelText('The new token')) as HTMLInputElement).value).toBe('bam_secret');
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ name: 'Dashboard', role_ids: ['counters'], expires_at: null });
    const row = (await screen.findByRole('cell', { name: 'Dashboard' })).closest('tr')!;
    expect(row.textContent).toContain('Never expires');
    await fireEvent.click(screen.getByRole('button', { name: 'Done' }));
    expect(screen.queryByLabelText('The new token')).toBeNull();

    await fireEvent.click(within(row).getByRole('button', { name: 'Revoke' }));
    await fireEvent.click(within(within(row).getByRole('group')).getByRole('button', { name: 'Revoke' }));
    await screen.findByText('Revoked Dashboard.');
  });

  it('sends an expiry when one is chosen', async () => {
    const { sent } = fakeApi({
      'GET /tokens': () => ({ tokens: [] }),
      'GET /roles': () => ({ roles }),
      'POST /tokens': () => ({ status: 201, body: { token: { ...old, revoked_at: null }, secret: 'bam_x' } }),
    });
    render(TokensPage, { params: {} });
    await screen.findByText('No tokens yet.');
    await fireEvent.click(screen.getByRole('button', { name: 'New token' }));
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Monthly' } });
    await fireEvent.change(screen.getByLabelText('Expires after'), { target: { value: '30' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Create token' }));
    await screen.findByLabelText('The new token');
    const expires = new Date((sent.find((r) => r.method === 'POST')!.body as { expires_at: string }).expires_at);
    const days = (expires.getTime() - Date.now()) / 86_400_000;
    expect(days).toBeGreaterThan(29.9);
    expect(days).toBeLessThan(30.1);
  });
});
