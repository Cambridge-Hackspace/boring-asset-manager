import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import LoansPage from './LoansPage.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';
import type { Loan } from '../lib/models';

const loan = (id: string, asset: string, borrower: string, over: Partial<Loan> = {}): Loan => ({
  id,
  asset_id: `a-${id}`,
  asset_name: asset,
  borrower: { id: borrower, email: `${borrower}@hackspace.org`, name: borrower === 'me' ? 'Me' : 'Bo' },
  checked_out_at: '2026-10-01T10:00:00Z',
  due_at: null,
  checked_in_at: null,
  overdue: false,
  ...over,
});

beforeEach(() => {
  session.user = { id: 'me', email: 'me@hackspace.org', name: 'Me', is_active: true, created_at: '', updated_at: '' };
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('LoansPage', () => {
  it('shows a member their own loans, past ones too, and checks one in', async () => {
    session.permissions = ['assets:read', 'assets:borrow'];
    let loans = [loan('1', 'Drill', 'me'), loan('2', 'Saw', 'me', { checked_in_at: '2026-10-05T10:00:00Z' })];
    const { fetchMock, sent } = fakeApi({
      'GET /loans': () => ({ loans }),
      'POST /assets/a-1/checkin': () => {
        loans = loans.map((l) => (l.id === '1' ? { ...l, checked_in_at: '2026-10-16T10:00:00Z' } : l));
        return { asset: {} };
      },
    });
    render(LoansPage, { params: {} });
    expect(await screen.findByRole('heading', { name: 'Your loans' })).toBeTruthy();
    const saw = (await screen.findByRole('link', { name: 'Saw' })).closest('tr')!;
    expect(String(fetchMock.mock.calls[0]![0])).toContain('borrower_id=me');
    expect(screen.queryByLabelText('Borrower')).toBeNull();
    expect(saw.textContent).toContain('Returned');
    expect(within(saw).queryByRole('button')).toBeNull();

    await fireEvent.click(screen.getByRole('button', { name: 'Check in Drill' }));
    await screen.findByText('Checked in Drill.');
    expect(sent.some((r) => r.path === '/assets/a-1/checkin')).toBe(true);
  });

  it('shows a lender everyone’s open loans, with who has each', async () => {
    session.permissions = ['assets:read', 'assets:lend'];
    const { fetchMock } = fakeApi({
      'GET /people': () => ({ people: [{ id: 'bo', email: 'bo@hackspace.org', name: 'Bo' }] }),
      'GET /loans': () => ({ loans: [loan('1', 'Drill', 'bo', { overdue: true, due_at: '2026-10-10T22:59:59Z' })] }),
    });
    render(LoansPage, { params: {} });
    const row = (await screen.findByRole('link', { name: 'Drill' })).closest('tr')!;
    expect(row.textContent).toContain('Bo');
    expect(row.textContent).toContain('Overdue');
    expect(String(fetchMock.mock.calls.at(-1)![0])).toContain('state=open');
    await fireEvent.change(screen.getByLabelText('Borrower'), { target: { value: 'bo' } });
    await vi.waitFor(() => expect(String(fetchMock.mock.calls.at(-1)![0])).toContain('borrower_id=bo'));
  });
});
