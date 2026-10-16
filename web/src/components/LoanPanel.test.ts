import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import LoanPanel from './LoanPanel.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';
import type { AssetWithDetails, Loan } from '../lib/models';

const drill: AssetWithDetails = {
  asset: { id: 'a1', name: 'Drill', created_at: '', updated_at: '', condition: 'ok', condition_note: '' },
  categories: [],
  generator_keys: [],
  field_values: {},
  effective_fields: [],
  lendable: true,
  loan: null,
  links: [],
  location: [],
  suggested_relation_type_ids: [],
};
const lentTo = (id: string, name: string, overdue = false): AssetWithDetails => ({
  ...drill,
  loan: {
    id: 'l1',
    asset_id: 'a1',
    asset_name: 'Drill',
    borrower: { id, email: `${name.toLowerCase()}@hackspace.org`, name },
    checked_out_at: '2026-10-15T10:00:00Z',
    due_at: '2026-10-20T22:59:59Z',
    checked_in_at: null,
    overdue,
  } as Loan,
});

beforeEach(() => {
  session.user = { id: 'me', email: 'me@hackspace.org', name: 'Me', is_active: true, created_at: '', updated_at: '' };
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('LoanPanel', () => {
  it('lets a member borrow it, with a due date, and check it back in', async () => {
    session.permissions = ['assets:read', 'assets:borrow'];
    const { sent } = fakeApi({
      'POST /assets/a1/checkout': () => ({ asset: lentTo('me', 'Me') }),
      'POST /assets/a1/checkin': () => ({ asset: drill }),
    });
    const changes: string[] = [];
    const { rerender } = render(LoanPanel, { asset: drill, onchange: (_a: AssetWithDetails, done: string) => changes.push(done) });
    expect(screen.queryByLabelText('Lend to')).toBeNull();
    await fireEvent.input(screen.getByLabelText('Due back (optional)'), { target: { value: '2026-10-20' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Borrow it myself' }));
    await vi.waitFor(() => expect(changes).toEqual(['Lent to you.']));
    const body = sent[0]!.body as { due_at: string; borrower_id?: string };
    expect(body.borrower_id).toBeUndefined();
    expect(new Date(body.due_at).getHours()).toBe(23);

    await rerender({ asset: lentTo('me', 'Me') });
    expect(screen.getByText(/Lent to/).textContent).toContain('you');
    await fireEvent.click(screen.getByRole('button', { name: 'Check in' }));
    await vi.waitFor(() => expect(changes).toEqual(['Lent to you.', 'Checked in.']));
  });

  it('does not let a member check in someone else’s loan', async () => {
    session.permissions = ['assets:read', 'assets:borrow'];
    fakeApi({});
    render(LoanPanel, { asset: lentTo('bo', 'Bo', true), onchange: () => {} });
    expect(screen.getByText(/Lent to/).textContent).toContain('Bo');
    expect(screen.getByText('Overdue')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Check in' })).toBeNull();
  });

  it('lets a lender lend to someone they choose', async () => {
    session.permissions = ['assets:read', 'assets:lend'];
    const { sent } = fakeApi({
      'GET /people': () => ({ people: [{ id: 'bo', email: 'bo@hackspace.org', name: 'Bo' }] }),
      'POST /assets/a1/checkout': () => ({ asset: lentTo('bo', 'Bo') }),
    });
    const changes: string[] = [];
    render(LoanPanel, { asset: drill, onchange: (_a: AssetWithDetails, done: string) => changes.push(done) });
    expect(screen.queryByRole('button', { name: 'Borrow it myself' })).toBeNull();
    await screen.findByRole('option', { name: 'Bo (bo@hackspace.org)' });
    await fireEvent.change(screen.getByLabelText('Lend to'), { target: { value: 'bo' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Lend' }));
    await vi.waitFor(() => expect(changes).toEqual(['Lent to Bo.']));
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ borrower_id: 'bo' });
  });

  it('says why an asset cannot be lent now', async () => {
    session.permissions = ['assets:read', 'assets:lend'];
    fakeApi({ 'GET /people': () => ({ people: [] }) });
    render(LoanPanel, { asset: { ...drill, asset: { ...drill.asset, condition: 'out_of_service' } }, onchange: () => {} });
    expect(screen.getByText(/out of service, so it cannot be lent/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Lend' })).toBeNull();
  });
});
