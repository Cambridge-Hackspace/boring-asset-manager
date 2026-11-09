import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import ReviewPanel from './ReviewPanel.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';
import type { AssetReview, AssetWithDetails } from '../lib/models';

const review = (over: Partial<AssetReview>): AssetReview => ({
  state: 'draft', set_by: null, set_at: null, confirmed_by: null, confirmed_at: null, reviewed_at: null, due_at: null,
  due: false, overdue: false, rules: { enabled: true, months: 12, on_edit: 'draft', self_verify: false }, proposal: null, ...over,
});

const asset = (r: AssetReview): AssetWithDetails => ({
  asset: { id: 'a1', name: 'Band saw', created_at: '', updated_at: '', condition: 'ok', condition_note: '', stock: null },
  categories: [], generator_keys: [], field_values: {}, effective_fields: [], lendable: false, weighed: false, loan: null, links: [], location: [],
  suggested_relation_type_ids: [], scopes: ['shop'], scan_template: { template_id: 'default' }, label_template: { template_id: '' }, hooks: [], photos: [], review: r,
});

beforeEach(() => {
  session.user = { id: 'me', email: 'me@hackspace.org', name: 'Me' } as typeof session.user;
  session.permissions = ['assets.read', 'shop:assets.write', 'shop:assets.confirm', 'shop:assets.verify', 'shop:assets.delete'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('ReviewPanel', () => {
  it('confirms a draft, with the permission in its scopes', async () => {
    const confirmed = asset(review({ state: 'confirmed' }));
    const { sent } = fakeApi({ 'POST /assets/a1/review': () => ({ asset: confirmed }) });
    const changed = vi.fn();
    const a = asset(review({}));
    render(ReviewPanel, { asset: a, review: a.review!, onchange: changed });
    expect(screen.getByText('Draft')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Verify' })).toBeNull();
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await vi.waitFor(() => expect(changed).toHaveBeenCalledWith(confirmed, 'Confirmed.'));
    expect(sent[0]?.body).toMatchObject({ move: 'confirm' });
  });

  it('offers nothing without the permissions in its scopes', () => {
    session.permissions = ['assets.read', 'other:assets.confirm'];
    const a = asset(review({ state: 'confirmed' }));
    render(ReviewPanel, { asset: a, review: a.review!, onchange: vi.fn() });
    expect(screen.queryAllByRole('button')).toHaveLength(0);
  });

  it('does not let whoever confirmed it verify it, and returns it to draft with a note', async () => {
    const { sent } = fakeApi({ 'POST /assets/a1/review': () => ({ asset: asset(review({})) }) });
    const changed = vi.fn();
    const a = asset(review({ state: 'confirmed', confirmed_by: { id: 'me', email: '', name: 'Me' }, confirmed_at: '2026-11-01T00:00:00Z', due_at: '2027-11-01T00:00:00Z', due: true }));
    render(ReviewPanel, { asset: a, review: a.review!, onchange: changed });
    expect(screen.getByText('Review due by 2027-11-01')).toBeTruthy();
    expect(screen.getByText(/Confirmed 2026-11-01 by you/)).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Verify' }) as HTMLButtonElement).disabled).toBe(true);
    await fireEvent.click(screen.getByRole('button', { name: 'Return to draft…' }));
    const form = screen.getByRole('form', { name: 'Return to draft' });
    await fireEvent.input(within(form).getByLabelText('What is wrong with it?'), { target: { value: 'Wrong blade' } });
    await fireEvent.click(within(form).getByRole('button', { name: 'Return to draft' }));
    await vi.waitFor(() => expect(changed).toHaveBeenCalledWith(expect.anything(), 'Returned to draft.'));
    expect(sent[0]?.body).toEqual({ move: 'return', note: 'Wrong blade' });
  });

  it('accepts or rejects an assistant’s changes', async () => {
    const { sent } = fakeApi({
      'POST /assets/a1/proposal/accept': () => ({ asset: asset(review({ state: 'confirmed' })) }),
      'POST /assets/a1/proposal/reject': () => ({ asset: asset(review({ state: 'verified' })) }),
    });
    const changed = vi.fn();
    const a = asset(review({ proposal: { new: false, undoable: true, prior_state: 'verified', by: { id: 'u2', email: '', name: 'Ada' }, via: 'Claude Desktop', created_at: '', updated_at: '2026-11-08T09:00:00Z' } }));
    render(ReviewPanel, { asset: a, review: a.review!, onchange: changed });
    const banner = screen.getByRole('region', { name: 'Assistant changes' });
    expect(banner.textContent).toContain('Changed through Claude Desktop by Ada, 2026-11-08.');
    expect(screen.queryByRole('button', { name: 'Confirm' })).toBeNull();
    await fireEvent.click(within(banner).getByRole('button', { name: 'Accept and confirm' }));
    await vi.waitFor(() => expect(changed).toHaveBeenCalledWith(expect.anything(), 'Accepted and confirmed.'));
    expect(sent[0]?.body).toEqual({ confirm: true });
    await fireEvent.click(within(banner).getByRole('button', { name: 'Reject…' }));
    await fireEvent.click(within(screen.getByRole('form', { name: 'Reject the changes' })).getByRole('button', { name: 'Reject' }));
    await vi.waitFor(() => expect(changed).toHaveBeenCalledWith(expect.anything(), 'Rejected; it is back as it was.'));
  });

  it('offers rejecting a new asset only to those who may archive it', () => {
    session.permissions = ['assets.read', 'shop:assets.write'];
    const a = asset(review({ proposal: { new: true, undoable: true, prior_state: 'draft', by: null, via: 'MCP', created_at: '', updated_at: '2026-11-08T09:00:00Z' } }));
    render(ReviewPanel, { asset: a, review: a.review!, onchange: vi.fn() });
    expect(screen.getByRole('button', { name: 'Accept' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Accept and confirm' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Reject…' })).toBeNull();
  });
});
