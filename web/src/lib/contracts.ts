// Contracts (docs/contracts.md): warranties, service and maintenance
// agreements, leases and licenses, as their pages, the asset page and
// history show them.

import type { AssetEvent, Billing, Contract, ContractCover, ContractKind, ContractRequest } from './models';

/** The kinds a contract may be, in the order the form offers them, with their labels. */
export const contractKinds: { key: ContractKind; label: string }[] = [
  { key: 'warranty', label: 'Warranty' },
  { key: 'service', label: 'Service' },
  { key: 'maintenance', label: 'Maintenance' },
  { key: 'lease', label: 'Lease' },
  { key: 'license', label: 'License' },
  { key: 'other', label: 'Other' },
];

/** How often a contract's cost may be paid, with their labels. */
export const billings: { key: Billing; label: string }[] = [
  { key: '', label: 'Not said' },
  { key: 'once', label: 'Once' },
  { key: 'monthly', label: 'Monthly' },
  { key: 'quarterly', label: 'Quarterly' },
  { key: 'yearly', label: 'Yearly' },
];

/** A kind as a label, such as Maintenance. */
export function kindLabel(kind: string): string {
  return contractKinds.find((k) => k.key === kind)?.label ?? kind;
}

/** How often it is paid, as a label; empty when not said. */
export function billingLabel(billing: string | undefined): string {
  return billing ? (billings.find((b) => b.key === billing)?.label ?? billing) : '';
}

const months = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];

/** A date written YYYY-MM-DD as its month and year, such as June 2027; as written when it is not one. */
export function monthYear(date: string): string {
  const m = /^(\d{4})-(\d{2})-\d{2}$/.exec(date);
  const name = m ? months[Number(m[2]) - 1] : undefined;
  return m && name ? `${name} ${m[1]}` : date;
}

/** Today in the viewer's time, YYYY-MM-DD. */
export function todayOf(now = new Date()): string {
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`;
}

/** Whether a contract has ended by today: its end is past. */
export function contractEnded(c: { end_date: string }, today = todayOf()): boolean {
  return !!c.end_date && c.end_date < today;
}

/**
 * A contract covering an asset, in a sentence: "Under maintenance contract
 * with X until June 2027", or for one ended, "Maintenance contract with X
 * ended May 2025".
 */
export function coverSentence(c: Pick<ContractCover, 'kind' | 'vendor_name' | 'end_date'>, today = todayOf()): string {
  const vendor = c.vendor_name || 'a vendor';
  if (contractEnded(c, today)) {
    const what = c.kind === 'warranty' ? 'Warranty' : c.kind === 'other' ? 'Contract' : `${kindLabel(c.kind)} contract`;
    return `${what} with ${vendor} ended ${monthYear(c.end_date)}`;
  }
  let lead: string;
  switch (c.kind) {
    case 'warranty':
      lead = `Under warranty with ${vendor}`;
      break;
    case 'lease':
      lead = `Leased from ${vendor}`;
      break;
    case 'license':
      lead = `Licensed from ${vendor}`;
      break;
    case 'other':
      lead = `Under contract with ${vendor}`;
      break;
    default:
      lead = `Under ${c.kind} contract with ${vendor}`;
  }
  return c.end_date ? `${lead} until ${monthYear(c.end_date)}` : lead;
}

/** A cost with its currency and how often it is paid, such as "120.00 GBP, yearly"; empty without a cost. */
export function costText(c: { cost?: string; currency?: string; billing?: string }): string {
  if (!c.cost) return '';
  const cost = `${c.cost} ${c.currency ?? ''}`.trim();
  const often = billingLabel(c.billing);
  return often ? `${cost}, ${often.toLowerCase()}` : cost;
}

/** What a contract covers, in a few words: "2 assets, 1 category". */
export function coverageText(c: Pick<Contract, 'asset_ids' | 'category_ids'>): string {
  const parts: string[] = [];
  const a = c.asset_ids?.length ?? 0;
  const k = c.category_ids?.length ?? 0;
  if (a) parts.push(`${a} asset${a === 1 ? '' : 's'}`);
  if (k) parts.push(`${k} categor${k === 1 ? 'y' : 'ies'}`);
  return parts.length ? parts.join(', ') : 'Nothing yet';
}

/** The form's starting values: a contract's details, or a new one's with a vendor. */
export function contractRequest(c: Contract | null, vendorId = ''): ContractRequest {
  return {
    vendor_id: c?.vendor_id ?? vendorId,
    kind: c?.kind ?? 'service',
    name: c?.name ?? '',
    reference: c?.reference ?? '',
    start_date: c?.start_date ?? '',
    end_date: c?.end_date ?? '',
    auto_renews: c?.auto_renews ?? false,
    notice_days: c?.notice_days ?? 0,
    cost: c?.cost ?? '',
    currency: c?.currency ?? '',
    billing: c?.billing ?? '',
    notes: c?.notes ?? '',
  };
}

/** A problem with a contract as typed, or '' when it will do. */
export function contractProblem(r: ContractRequest): string {
  if (!r.name.trim()) return 'Give the contract a name.';
  if (!r.vendor_id) return 'Choose the vendor it is with.';
  if (r.start_date && r.end_date && r.end_date < r.start_date) return 'It cannot end before it starts.';
  if (r.auto_renews && !r.end_date) return 'A contract that renews automatically needs an end date: when it renews.';
  const cost = r.cost.trim();
  if (cost && !/^\d*\.?\d+$/.test(cost)) return 'Write the cost as a number, such as 120.00, without a currency symbol.';
  if (cost && !/^[A-Za-z]{3}$/.test(r.currency.trim())) return 'Give the cost’s currency, as its three-letter code, such as GBP.';
  return '';
}

/** Orders contracts by when they end, soonest first and those with no end last; descending reverses those with ends, still leaving those without last. */
export function byEnd(list: Contract[], descending = false): Contract[] {
  return [...list].sort((a, b) => {
    if (a.end_date !== b.end_date) {
      if (!a.end_date) return 1;
      if (!b.end_date) return -1;
      return descending ? b.end_date.localeCompare(a.end_date) : a.end_date.localeCompare(b.end_date);
    }
    return a.name.localeCompare(b.name);
  });
}

/** One change to a contract, as it read then; an absent from or to means none. */
export interface ContractChange {
  what: string;
  key?: string;
  from?: string;
  to?: string;
}

/** An event in a contract's history. */
export interface ContractEvent {
  id: string;
  kind: string;
  created_at: string;
  payload: { actor?: AssetEvent['payload']['actor']; changes?: ContractChange[]; due_at?: string };
}

/** What an event in a contract's history was. */
export function describeContractEvent(e: ContractEvent): string {
  switch (e.kind) {
    case 'contract.created':
      return 'Created';
    case 'contract.edited':
      return 'Edited';
    case 'contract.expiring':
      return e.payload.due_at ? `Reminder: ends or needs notice by ${e.payload.due_at.slice(0, 10)}` : 'Reminder sent';
    case 'contract.expired':
      return 'Ended';
    default:
      return e.kind;
  }
}

const detailLabels: Record<string, string> = {
  vendor: 'Vendor',
  kind: 'Kind',
  name: 'Name',
  reference: 'Reference',
  start_date: 'Starts',
  end_date: 'Ends',
  auto_renews: 'Renews automatically',
  notice_days: 'Notice period (days)',
  cost: 'Cost',
  billing: 'Paid',
};

/** One line of what a contract's event changed. */
export function describeContractChange(c: ContractChange): string {
  switch (c.what) {
    case 'notes':
      return 'Notes changed';
    case 'asset':
      return c.to ? `Covers the asset ${c.to}` : `No longer covers the asset ${c.from}`;
    case 'category':
      return c.to ? `Covers the category ${c.to}` : `No longer covers the category ${c.from}`;
    case 'photo':
      return c.to ? `Photo added: ${c.to}` : `Photo removed: ${c.from}`;
    case 'attachment':
      return c.to ? `Attachment added: ${c.to}` : `Attachment removed: ${c.from}`;
    default: {
      const label = detailLabels[c.what] ?? c.what;
      const show = (v?: string) => (c.what === 'kind' && v ? kindLabel(v) : c.what === 'billing' && v ? billingLabel(v) : v);
      if (!c.from) return `${label}: ${show(c.to)}`;
      if (!c.to) return `${label}: ${show(c.from)} removed`;
      return `${label}: ${show(c.from)} → ${show(c.to)}`;
    }
  }
}
