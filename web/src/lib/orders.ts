// Purchase orders (docs/purchase-orders.md): orders to vendors, as their
// pages, the receive page, the running-low list and history show them.

import type { AssetEvent, OrderLineRequest, OrderRequest, OrderState, PurchaseOrder, PurchaseOrderLine, ReceiptLineRequest } from './models';
import { todayOf } from './contracts';

/** The states an order may be in, in the order it goes through them, with their labels. */
export const orderStates: { key: OrderState; label: string }[] = [
  { key: 'draft', label: 'Draft' },
  { key: 'ordered', label: 'Ordered' },
  { key: 'partly_received', label: 'Partly received' },
  { key: 'received', label: 'Received' },
  { key: 'canceled', label: 'Canceled' },
];

/** A state as a label, such as Partly received. */
export function stateLabel(state: string): string {
  return orderStates.find((s) => s.key === state)?.label ?? state;
}

/** Whether an order is still to come: drafted, placed, or partly received. */
export function orderOpen(o: Pick<PurchaseOrder, 'state'>): boolean {
  return o.state === 'draft' || o.state === 'ordered' || o.state === 'partly_received';
}

/** Whether a placed order is past the date it was expected, and not all here. */
export function orderOverdue(o: Pick<PurchaseOrder, 'state' | 'expected_on'>, today = todayOf()): boolean {
  return (o.state === 'ordered' || o.state === 'partly_received') && !!o.expected_on && o.expected_on < today;
}

/** A line's text: its part number and description. */
export function lineText(l: Pick<PurchaseOrderLine, 'part_number' | 'description'>): string {
  return [l.part_number, l.description].filter(Boolean).join(' ') || 'A line';
}

/** A line's pack, such as "100 screws"; empty when not known. */
export function linePack(l: Pick<PurchaseOrderLine, 'pack_size' | 'pack_unit'>): string {
  return [l.pack_size ?? '', l.pack_unit].join(' ').trim();
}

/** How many packs of a line are still to come. */
export function outstanding(l: Pick<PurchaseOrderLine, 'quantity' | 'quantity_received'>): number {
  return Math.max(0, l.quantity - l.quantity_received);
}

/** A money amount with its currency, such as "12.50 GBP"; empty without one. */
export function moneyText(amount: string | undefined, currency: string | undefined): string {
  return amount ? `${amount} ${currency ?? ''}`.trim() : '';
}

/** The form's starting values: an order's details, or a new one's with a vendor. */
export function orderRequest(o: PurchaseOrder | null, vendorId = ''): OrderRequest {
  return {
    vendor_id: o?.vendor_id ?? vendorId,
    currency: o?.currency ?? '',
    vendor_reference: o?.vendor_reference ?? '',
    ordered_on: o?.ordered_on ?? '',
    expected_on: o?.expected_on ?? '',
    deliver_to: o?.deliver_to ?? '',
    notes: o?.notes ?? '',
  };
}

/** A line as the form edits it: the API's, with its price as typed and what is known of it. */
export interface LineDraft extends OrderLineRequest {
  /** Its name here, for a line drawn from the catalog or for a category or an asset. */
  linked_name?: string;
  /** Packs come already, which it cannot order fewer than. */
  received: number;
}

/** An order's lines as the form edits them. */
export function lineDrafts(o: PurchaseOrder | null): LineDraft[] {
  return (o?.lines ?? []).map((l) => ({
    id: l.id,
    catalog_entry_id: l.catalog_entry_id,
    category_id: l.category_id,
    asset_id: l.asset_id,
    linked_name: l.linked_name,
    part_number: l.part_number,
    description: l.description,
    pack_size: l.pack_size,
    pack_unit: l.pack_unit,
    quantity: l.quantity,
    unit_price: l.unit_price ?? '',
    received: l.quantity_received,
  }));
}

/** Lines as the API takes them; prices are left out, and so kept, where they are hidden. */
export function lineRequests(lines: LineDraft[], pricesHidden = false): OrderLineRequest[] {
  return lines.map((l) => {
    const out: OrderLineRequest = {
      part_number: l.part_number.trim(),
      description: l.description.trim(),
      pack_size: l.pack_size,
      pack_unit: l.pack_unit.trim(),
      quantity: Number(l.quantity) || 0,
    };
    if (l.id) out.id = l.id;
    if (l.catalog_entry_id) out.catalog_entry_id = l.catalog_entry_id;
    else if (l.category_id) out.category_id = l.category_id;
    else if (l.asset_id) out.asset_id = l.asset_id;
    if (!pricesHidden && l.unit_price !== undefined) out.unit_price = String(l.unit_price).trim();
    return out;
  });
}

/** A problem with an order as typed, or '' when it will do. */
export function orderProblem(r: OrderRequest, lines: LineDraft[]): string {
  if (!r.vendor_id) return 'Choose the vendor it is to.';
  for (const [i, l] of lines.entries()) {
    const n = i + 1;
    if (!l.part_number.trim() && !l.description.trim()) return `Line ${n} needs a part number or a description.`;
    const q = Number(l.quantity);
    if (!Number.isInteger(q) || q < 1 || q > 1000000) return `Line ${n}: order from 1 to 1000000 packs.`;
    if (q < l.received) return `Line ${n}: ${l.received} packs have come, so it cannot order fewer.`;
    const price = String(l.unit_price ?? '').trim();
    if (price && !/^\d*\.?\d+$/.test(price)) return `Line ${n}: write the price as a number, such as 4.50, without a currency symbol.`;
  }
  const priced = lines.some((l) => String(l.unit_price ?? '').trim());
  if (priced && !/^[A-Za-z]{3}$/.test(r.currency.trim())) return 'Give the order’s currency, as its three-letter code, such as GBP.';
  return '';
}

/** What receiving a line does, as the receive page says it, and its defaults. */
export interface ReceiptDraft {
  line: PurchaseOrderLine;
  /** Whether it arrives in this delivery. */
  on: boolean;
  quantity: number;
  /** For an asset's line: how much it restocks; blank when the pack's size is not known. */
  restock: string;
  /** For a category's line: how many assets to make, a name, and an asset to make them like. */
  assets: string;
  name: string;
  like: string;
  /** In a category that weighs: how much each asset holds. */
  each: string;
  warranty_end: string;
}

/** A receipt's starting values: every line outstanding, all of it arriving. */
export function receiptDrafts(o: PurchaseOrder): ReceiptDraft[] {
  return o.lines
    .filter((l) => outstanding(l) > 0)
    .map((l) => {
      const q = outstanding(l);
      return {
        line: l,
        on: true,
        quantity: q,
        restock: l.pack_size ? String(q * l.pack_size) : '',
        assets: String(q),
        name: l.description || l.part_number || l.linked_name || '',
        like: '',
        each: '',
        warranty_end: '',
      };
    });
}

/** When the packs arriving change, the defaults that follow them. */
export function followQuantity(d: ReceiptDraft, quantity: number): ReceiptDraft {
  const out = { ...d, quantity };
  if (d.line.pack_size && d.restock === String(d.quantity * d.line.pack_size)) out.restock = String(quantity * d.line.pack_size);
  if (d.assets === String(d.quantity)) out.assets = String(quantity);
  return out;
}

/** What receiving a line will do, in a sentence. */
export function receiptEffect(d: ReceiptDraft, unit = ''): string {
  const l = d.line;
  if (l.asset_id) {
    const by = Number(d.restock);
    if (!d.restock.trim() || !Number.isFinite(by)) return `Say how much it adds to ${l.linked_name || 'the asset'}.`;
    return `Adds ${by}${unit ? ` ${unit}` : ''} to ${l.linked_name || 'the asset'}.`;
  }
  if (l.category_id) {
    const n = Number(d.assets) || 0;
    if (n === 0) return 'Makes no assets.';
    return `Makes ${n} asset${n === 1 ? '' : 's'} in ${l.linked_name || 'the category'}.`;
  }
  return 'Counted in; nothing is kept for it.';
}

/** The lines arriving, as the API takes them. */
export function receiptRequests(drafts: ReceiptDraft[]): ReceiptLineRequest[] {
  return drafts
    .filter((d) => d.on && d.quantity > 0)
    .map((d) => {
      const out: ReceiptLineRequest = { line_id: d.line.id, quantity: Number(d.quantity) };
      if (d.line.asset_id && d.restock.trim()) out.restock = Number(d.restock);
      if (d.line.category_id) {
        if (d.assets.trim()) out.assets = Number(d.assets);
        if (d.like) out.like = d.like;
        else if (d.name.trim()) out.name = d.name.trim();
        if (d.each.trim()) out.each = Number(d.each);
        if (d.warranty_end) out.warranty_end = d.warranty_end;
      }
      return out;
    });
}

/** One change to an order, as it read then; an absent from or to means none. */
export interface OrderChange {
  what: string;
  key?: string;
  label?: string;
  from?: string;
  to?: string;
}

/** An event in an order's history. */
export interface OrderEvent {
  id: string;
  kind: string;
  created_at: string;
  payload: {
    actor?: AssetEvent['payload']['actor'];
    changes?: OrderChange[];
    order_lines?: { line_id: string; part_number?: string; description?: string; quantity: number; restocked?: number; assets?: { id: string; name: string }[] }[];
  };
}

/** What an event in an order's history was. */
export function describeOrderEvent(e: OrderEvent): string {
  switch (e.kind) {
    case 'purchase_order.created':
      return 'Created';
    case 'purchase_order.edited':
      return 'Edited';
    case 'purchase_order.ordered':
      return 'Placed with the vendor';
    case 'purchase_order.received':
      return 'Delivery received';
    case 'purchase_order.canceled':
      return 'Canceled';
    default:
      return e.kind;
  }
}

const detailLabels: Record<string, string> = {
  vendor: 'Vendor',
  currency: 'Currency',
  vendor_reference: 'Vendor’s reference',
  ordered_on: 'Ordered',
  expected_on: 'Expected',
  deliver_to: 'Deliver to',
};

/** One line of what an order's event changed. */
export function describeOrderChange(c: OrderChange): string {
  switch (c.what) {
    case 'notes':
      return 'Notes changed';
    case 'line':
      if (!c.from) return `Line added: ${c.to}`;
      if (!c.to) return `Line removed: ${c.from}`;
      return `Line: ${c.from} → ${c.to}`;
    case 'price':
      if (!c.from) return `Price of ${c.label}: ${c.to}`;
      if (!c.to) return `Price of ${c.label}: ${c.from} removed`;
      return `Price of ${c.label}: ${c.from} → ${c.to}`;
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

/** What a delivery received brought, line by line. */
export function describeReceipt(e: OrderEvent): string[] {
  return (e.payload.order_lines ?? []).map((l) => {
    const text = [l.part_number, l.description].filter(Boolean).join(' ');
    const parts = [`${l.quantity} × ${text}`];
    if (l.restocked) parts.push(`restocked by ${l.restocked}`);
    if (l.assets?.length) parts.push(`made ${l.assets.map((a) => a.name).join(', ')}`);
    return parts.join(': ');
  });
}

/** Open orders by what their lines are for, the oldest first: what is on order. */
export function onOrderByThing(orders: PurchaseOrder[]): Map<string, PurchaseOrder> {
  const out = new Map<string, PurchaseOrder>();
  for (const o of [...orders].sort((a, b) => a.number - b.number)) {
    if (!orderOpen(o)) continue;
    for (const l of o.lines) {
      const thing = l.category_id || l.asset_id;
      if (thing && !out.has(thing)) out.set(thing, o);
    }
  }
  return out;
}

/** What an open order a thing is on says, such as "On order: PO-0012" or "In draft PO-0013". */
export function onOrderText(o: Pick<PurchaseOrder, 'state' | 'reference'>): string {
  return o.state === 'draft' ? `In draft ${o.reference}` : `On order: ${o.reference}`;
}

/** The example a numbering shows, such as PO-0012 for prefix PO- padded to 4. */
export function numberingExample(prefix: string, padding: number, number = 12): string {
  return `${prefix}${String(number).padStart(Math.max(0, Math.min(9, padding)), '0')}`;
}

/** A problem with a numbering as typed, or '' when it will do. */
export function numberingProblem(prefix: string, padding: number): string {
  if (prefix.trim().length > 16) return 'The prefix is at most 16 characters.';
  if (!/^[A-Za-z0-9_-]*$/.test(prefix.trim())) return 'The prefix may have only letters A to Z, digits, hyphens and underscores, so it is safe in a file’s name and on a label.';
  if (!Number.isInteger(padding) || padding < 0 || padding > 9) return 'Pad numbers to from 0 to 9 digits.';
  return '';
}
