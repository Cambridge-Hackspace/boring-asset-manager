import { describe, expect, it } from 'vitest';
import {
  billingLabel,
  byEnd,
  contractEnded,
  contractProblem,
  contractRequest,
  costText,
  coverageText,
  coverSentence,
  describeContractChange,
  describeContractEvent,
  kindLabel,
  monthYear,
} from './contracts';
import type { Contract } from './models';

const contract = (over: Partial<Contract> = {}): Contract => ({
  id: 'k1', vendor_id: 'v1', vendor_name: 'Acme', kind: 'maintenance', name: 'Upkeep', reference: '', start_date: '', end_date: '2027-06-30',
  auto_renews: false, notice_days: 0, notes: '', asset_ids: [], category_ids: [], assets: [], categories: [], created_at: '', updated_at: '', ...over,
});
const today = '2026-11-06';

describe('contracts', () => {
  it('says what covers an asset, by kind, and when it ends or ended', () => {
    expect(coverSentence({ kind: 'maintenance', vendor_name: 'X', end_date: '2027-06-30' }, today)).toBe('Under maintenance contract with X until June 2027');
    expect(coverSentence({ kind: 'service', vendor_name: 'X', end_date: '' }, today)).toBe('Under service contract with X');
    expect(coverSentence({ kind: 'warranty', vendor_name: 'X', end_date: '2027-01-31' }, today)).toBe('Under warranty with X until January 2027');
    expect(coverSentence({ kind: 'lease', vendor_name: 'X', end_date: '2027-01-31' }, today)).toBe('Leased from X until January 2027');
    expect(coverSentence({ kind: 'license', vendor_name: 'X', end_date: '' }, today)).toBe('Licensed from X');
    expect(coverSentence({ kind: 'other', vendor_name: 'X', end_date: '2027-01-31' }, today)).toBe('Under contract with X until January 2027');
    // Ended, it says so.
    expect(coverSentence({ kind: 'maintenance', vendor_name: 'X', end_date: '2025-05-31' }, today)).toBe('Maintenance contract with X ended May 2025');
    expect(coverSentence({ kind: 'warranty', vendor_name: 'X', end_date: '2025-05-31' }, today)).toBe('Warranty with X ended May 2025');
    expect(coverSentence({ kind: 'other', vendor_name: 'X', end_date: '2025-05-31' }, today)).toBe('Contract with X ended May 2025');
    // It is in force through its last day.
    expect(contractEnded({ end_date: today }, today)).toBe(false);
    expect(contractEnded({ end_date: '' }, today)).toBe(false);
  });

  it('writes months, kinds, billing, costs and coverage', () => {
    expect(monthYear('2027-12-01')).toBe('December 2027');
    expect(monthYear('soon')).toBe('soon');
    expect(kindLabel('license')).toBe('License');
    expect(billingLabel('quarterly')).toBe('Quarterly');
    expect(billingLabel('')).toBe('');
    expect(costText({ cost: '120.00', currency: 'GBP', billing: 'yearly' })).toBe('120.00 GBP, yearly');
    expect(costText({ cost: '5', currency: 'JPY' })).toBe('5 JPY');
    expect(costText({})).toBe('');
    expect(coverageText(contract({ asset_ids: ['a'], category_ids: ['c', 'd'] }))).toBe('1 asset, 2 categories');
    expect(coverageText(contract())).toBe('Nothing yet');
  });

  it('orders by end, soonest first or last, those with none last either way', () => {
    const list = [contract({ id: 'a', name: 'A', end_date: '' }), contract({ id: 'b', name: 'B', end_date: '2028-01-01' }), contract({ id: 'c', name: 'C', end_date: '2027-01-01' })];
    expect(byEnd(list).map((c) => c.id)).toEqual(['c', 'b', 'a']);
    expect(byEnd(list, true).map((c) => c.id)).toEqual(['b', 'c', 'a']);
  });

  it('checks the form before it is sent', () => {
    const ok = contractRequest(contract());
    expect(contractProblem(ok)).toBe('');
    expect(contractProblem({ ...ok, name: ' ' })).toMatch(/name/);
    expect(contractProblem({ ...ok, vendor_id: '' })).toMatch(/vendor/);
    expect(contractProblem({ ...ok, start_date: '2028-01-01' })).toMatch(/before it starts/);
    expect(contractProblem({ ...ok, auto_renews: true, end_date: '' })).toMatch(/needs an end date/);
    expect(contractProblem({ ...ok, cost: '£5' })).toMatch(/number/);
    expect(contractProblem({ ...ok, cost: '5' })).toMatch(/currency/);
    expect(contractProblem({ ...ok, cost: '5', currency: 'gbp' })).toBe('');
    expect(contractRequest(null, 'v9')).toMatchObject({ vendor_id: 'v9', kind: 'service', name: '' });
  });

  it('describes its history', () => {
    expect(describeContractEvent({ id: 'e', kind: 'contract.created', created_at: '', payload: {} })).toBe('Created');
    expect(describeContractEvent({ id: 'e', kind: 'contract.expired', created_at: '', payload: {} })).toBe('Ended');
    expect(describeContractEvent({ id: 'e', kind: 'contract.expiring', created_at: '', payload: { due_at: '2027-06-30T00:00:00Z' } })).toBe('Reminder: ends or needs notice by 2027-06-30');
    expect(describeContractChange({ what: 'kind', from: 'service', to: 'maintenance' })).toBe('Kind: Service → Maintenance');
    expect(describeContractChange({ what: 'end_date', to: '2027-06-30' })).toBe('Ends: 2027-06-30');
    expect(describeContractChange({ what: 'asset', key: 'a1', to: 'Laser' })).toBe('Covers the asset Laser');
    expect(describeContractChange({ what: 'category', key: 'c1', from: 'Tools' })).toBe('No longer covers the category Tools');
    expect(describeContractChange({ what: 'notes' })).toBe('Notes changed');
    expect(describeContractChange({ what: 'attachment', to: 'contract.pdf' })).toBe('Attachment added: contract.pdf');
  });
});
