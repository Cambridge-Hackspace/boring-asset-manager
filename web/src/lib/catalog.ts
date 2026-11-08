// Vendors' catalogs (docs/vendors.md#catalogs): what each vendor sells, as
// vendors', categories' and assets' pages and the running-low list show it.

import type { CatalogEntry, CatalogEntryRequest, Vendor } from './models';
import { reorderHref, websiteHref } from './vendors';

/** An entry's pack in a few words, such as "100 screws"; empty when not known. */
export function packText(e: Pick<CatalogEntry, 'pack_size' | 'pack_unit'>): string {
  return [e.pack_size ?? '', e.pack_unit].join(' ').trim();
}

/** An entry's price with its currency, and its pack, such as "4.50 GBP for 100 screws"; empty without a price. */
export function entryPrice(e: CatalogEntry): string {
  if (!e.price) return '';
  const price = `${e.price} ${e.currency ?? ''}`.trim();
  const pack = packText(e);
  return pack ? `${price} for ${pack}` : price;
}

/** What an entry is called: its part number, or its description when it has none. */
export function entryName(e: Pick<CatalogEntry, 'part_number' | 'description'>): string {
  return e.part_number || e.description || 'An entry';
}

/**
 * Where to reorder an entry from: its product page, or else its vendor's
 * website, or the vendor's page here when it has neither.
 */
export function entryHref(e: CatalogEntry, vendor?: Vendor): { href: string; external: boolean } {
  const page = websiteHref(e.url);
  if (page) return { href: page, external: true };
  if (vendor) return reorderHref(vendor);
  return { href: `/vendors/${encodeURIComponent(e.vendor_id)}`, external: false };
}

/** The preferred entry of each category and asset, by its ID. */
export function preferredByThing(entries: CatalogEntry[]): Map<string, CatalogEntry> {
  const out = new Map<string, CatalogEntry>();
  for (const e of entries) {
    const thing = e.category_id || e.asset_id;
    if (e.preferred && thing) out.set(thing, e);
  }
  return out;
}

/** The form's values for an entry, or a new one for a vendor, category or asset. */
export function entryRequest(e: Partial<CatalogEntry> = {}): CatalogEntryRequest {
  return {
    vendor_id: e.vendor_id ?? '',
    category_id: e.category_id ?? '',
    asset_id: e.asset_id ?? '',
    part_number: e.part_number ?? '',
    description: e.description ?? '',
    url: e.url ?? '',
    pack_size: e.pack_size ?? null,
    pack_unit: e.pack_unit ?? '',
    price: e.price ?? '',
    currency: e.currency ?? '',
    price_checked: e.price_checked ?? '',
    preferred: e.preferred ?? false,
  };
}

/** A problem with an entry as typed, or '' when it will do. */
export function entryProblem(r: CatalogEntryRequest): string {
  if (!r.vendor_id) return 'Choose the vendor that sells it.';
  if (!r.category_id && !r.asset_id) return 'Choose the category or asset it is for.';
  if (!r.part_number.trim() && !r.description.trim()) return 'Give its part number or a description.';
  const url = r.url.trim();
  if (url && !/^https?:\/\/[^/\s]+/i.test(url)) return 'The product page is a web address starting https://.';
  if (r.pack_size !== null && (!Number.isInteger(r.pack_size) || r.pack_size < 1)) return 'A pack holds a whole number of at least 1.';
  const price = r.price.trim();
  if (price && !/^\d*\.?\d+$/.test(price)) return 'Write the price as a number, such as 12.50, without a currency symbol.';
  if (price && !/^[A-Za-z]{3}$/.test(r.currency.trim())) return 'Give the price’s currency, as its three-letter code, such as GBP.';
  return '';
}
