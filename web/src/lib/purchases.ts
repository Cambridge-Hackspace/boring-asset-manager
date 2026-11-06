// Where an asset came from (docs/data-model.md#purchases).

import type { Purchase, PurchaseRequest } from './models';

/** Currencies the form suggests; any three-letter ISO 4217 code is taken. */
export const currencies = ['GBP', 'EUR', 'USD', 'CAD', 'AUD', 'NZD', 'CHF', 'SEK', 'NOK', 'DKK', 'PLN', 'CZK', 'JPY', 'CNY', 'INR'];

/** Whether anything is recorded, or hidden from the viewer. */
export function hasPurchase(p: Purchase | undefined): boolean {
  if (!p) return false;
  return !!(p.vendor_id || p.vendor_name || p.order_date || p.received_date || p.warranty_end || p.price || p.currency || p.order_number || p.invoice_attachment_id || p.prices_hidden);
}

/** A price with its currency, such as "12.50 GBP"; empty without a price. */
export function priceText(p: Purchase | undefined): string {
  return p?.price ? `${p.price} ${p.currency ?? ''}`.trim() : '';
}

/** The form's starting values: the asset's details, or none. */
export function purchaseRequest(p: Purchase | undefined): PurchaseRequest {
  return {
    vendor_id: p?.vendor_id ?? '',
    order_date: p?.order_date ?? '',
    received_date: p?.received_date ?? '',
    warranty_end: p?.warranty_end ?? '',
    price: p?.price ?? '',
    currency: p?.currency ?? '',
    order_number: p?.order_number ?? '',
    invoice_attachment_id: p?.invoice_attachment_id ?? '',
  };
}

/** A problem with a price as typed, or '' when it will do: a decimal, needing a currency. */
export function priceProblem(r: PurchaseRequest): string {
  const price = r.price.trim();
  if (!price) return '';
  if (!/^\d*\.?\d+$/.test(price)) return 'Write the price as a number, such as 12.50, without a currency symbol.';
  if (!/^[A-Za-z]{3}$/.test(r.currency.trim())) return 'Give the price’s currency, as its three-letter code, such as GBP.';
  return '';
}

/** Whether a warranty that ends on this date has ended by today, in the viewer's time. */
export function warrantyEnded(end: string, today = new Date()): boolean {
  if (!end) return false;
  const y = today.getFullYear(), m = String(today.getMonth() + 1).padStart(2, '0'), d = String(today.getDate()).padStart(2, '0');
  return end < `${y}-${m}-${d}`;
}
