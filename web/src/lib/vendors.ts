// Vendors (docs/vendors.md): the suppliers a space buys from, as their
// pages, the running-low list and history show them.

import type { AssetEvent, Vendor, VendorContact } from './models';

/** One change to a vendor, as it read then; an absent from or to means none. */
export interface VendorChange {
  what: 'name' | 'website' | 'account_number' | 'address' | 'notes' | 'contact' | 'scope' | 'photo' | 'attachment';
  key?: string;
  from?: string;
  to?: string;
}

/** An event in a vendor's history: made, or edited. */
export interface VendorEvent {
  id: string;
  kind: string;
  created_at: string;
  payload: { actor?: AssetEvent['payload']['actor']; changes?: VendorChange[] };
}

/** What an event in a vendor's history was. */
export function describeVendorEvent(e: VendorEvent): string {
  switch (e.kind) {
    case 'vendor.created':
      return 'Created';
    case 'vendor.edited':
      return 'Edited';
    default:
      return e.kind;
  }
}

const detailLabels: Record<string, string> = { name: 'Name', website: 'Website', account_number: 'Account number', address: 'Address' };

/** One line of what a vendor's event changed. */
export function describeVendorChange(c: VendorChange): string {
  switch (c.what) {
    case 'notes':
      return 'Notes changed';
    case 'contact':
      if (!c.from) return `Contact added: ${c.to}`;
      if (!c.to) return `Contact removed: ${c.from}`;
      return `Contact: ${c.from} → ${c.to}`;
    case 'scope':
      return c.to ? `Put in the scope ${c.to}` : `Taken out of the scope ${c.from}`;
    case 'photo':
      return c.to ? `Photo added: ${c.to}` : `Photo removed: ${c.from}`;
    case 'attachment':
      return c.to ? `Attachment added: ${c.to}` : `Attachment removed: ${c.from}`;
    default: {
      const label = detailLabels[c.what] ?? c.what;
      if (!c.from) return `${label}: ${c.to}`;
      if (!c.to) return `${label}: ${c.from} removed`;
      return `${label}: ${c.from} → ${c.to}`;
    }
  }
}

/** A website as a list shows it: without its scheme, a leading www. or a trailing slash. */
export function siteName(url: string): string {
  return url
    .trim()
    .replace(/^https?:\/\//i, '')
    .replace(/^www\./i, '')
    .replace(/\/$/, '');
}

/**
 * The address a website can be opened at: as written when it is http or
 * https, https added when it names no scheme, and none for any other
 * scheme, such as javascript:, which a link must never follow.
 */
export function websiteHref(url: string): string | null {
  const u = url.trim();
  if (!u) return null;
  if (/^https?:\/\//i.test(u)) return u;
  if (/^[a-z][a-z0-9+.-]*:/i.test(u)) return null;
  return `https://${u.replace(/^\/+/, '')}`;
}

/** The contact a list shows for a vendor: the first, which people put first for that. */
export function mainContact(v: Vendor): VendorContact | undefined {
  return v.contacts[0];
}

/** A contact in a few words: their name or how to reach them, and their role. */
export function contactSummary(c: VendorContact): string {
  const who = c.name || c.email || c.phone;
  return c.role ? `${who} (${c.role})` : who;
}

/** A phone number as a link dials it: its digits, and a leading plus. */
export function telHref(phone: string): string {
  const p = phone.trim();
  return `tel:${p.startsWith('+') ? '+' : ''}${p.replace(/\D/g, '')}`;
}

/** Where to reorder from a vendor: its website, or its page here when it has none. */
export function reorderHref(v: Vendor): { href: string; external: boolean } {
  const site = websiteHref(v.website);
  return site ? { href: site, external: true } : { href: `/vendors/${encodeURIComponent(v.id)}`, external: false };
}
