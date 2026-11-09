// Helpers for the asset form.

import type { AssetChange, AssetEvent, AssetLink, AssetStock, AssetWithDetails, Condition, EffectiveField, Field, FieldType, GeneratorKey, LinkedAsset, RelationType } from './models';
import { personName } from './people';
import { describeReviewEvent } from './review';

/** The HTML input type for a field type (select fields use <select>). */
export function inputType(type: FieldType): string {
  switch (type) {
    case 'number':
      return 'number';
    case 'date':
      return 'date';
    case 'url':
      return 'url';
    default:
      return 'text';
  }
}

/** A link a copy makes as its original has it (GET /assets/{id}/copy). */
export interface CopiedLink {
  type_id: string;
  /** The type's name read from the copy. */
  label: string;
  asset: LinkedAsset;
  outgoing: boolean;
  is_location: boolean;
}

/** A new asset like another, to change before it is created (docs/data-model.md). */
export interface AssetCopy {
  name: string;
  category_ids: string[];
  field_values: Record<string, string>;
  stock: { quantity: number | null; unit: string; low_stock_threshold: number | null } | null;
  /** The next codes in the series of the original's. */
  generator_keys: GeneratorKey[];
  links: CopiedLink[];
  /** The original's own icon, when the build still draws it. */
  icon_name?: string;
}

export interface AssetDraft {
  name: string;
  categoryIds: string[];
  /** Codes by generator type; blank means none. */
  codes: Record<string, string>;
  /** Values by field key. */
  values: Record<string, string>;
  stock?: StockDraft;
  /** Its own icon; '' for its categories', and absent to leave it as it is. */
  icon?: string;
}

/**
 * The request body for creating or updating an asset. Every effective field
 * is sent, so clearing an input removes its value; values kept from earlier
 * categories are not sent, so they stay. Category and code lists are always
 * sent in full: an empty list clears them. Stock is sent as stockPayload
 * says, given the asset's stock before the edit, and the icon when the
 * draft has one to say: null for its categories'.
 */
export function assetPayload(draft: AssetDraft, effective: EffectiveField[], originalStock?: AssetStock | null) {
  const generator_keys: GeneratorKey[] = Object.entries(draft.codes)
    .map(([generator_type, code]) => ({ generator_type, assigned_code: code.trim() }))
    .filter((k) => k.assigned_code !== '');
  const field_values: Record<string, string> = {};
  for (const e of effective) field_values[e.field.key] = (draft.values[e.field.key] ?? '').trim();
  const body: Record<string, unknown> = { name: draft.name.trim(), category_ids: [...draft.categoryIds], generator_keys, field_values };
  if (draft.stock) {
    const stock = stockPayload(draft.stock, originalStock);
    if (stock !== undefined) body.stock = stock;
  }
  if (draft.icon !== undefined) body.icon_name = draft.icon === '' ? null : draft.icon;
  return body;
}

/** A value kept from a former category: its field's label while the field still exists. */
export interface KeptValue {
  key: string;
  label?: string;
  value: string;
}

/**
 * Values the asset keeps from categories it no longer belongs to, each
 * with its field's label where the field is among these (#171).
 */
export function keptValues(values: Record<string, string>, effective: EffectiveField[], fields: Field[] = []): KeptValue[] {
  const current = new Set(effective.map((e) => e.field.key));
  return Object.entries(values)
    .filter(([key, value]) => !current.has(key) && value !== '')
    .map(([key, value]) => {
      const label = fields.find((f) => f.key === key)?.label;
      return label ? { key, label, value } : { key, value };
    });
}

/**
 * Finds which input a server error is about: field errors name the key in
 * parentheses, code errors name "(type: code)".
 */
export function errorTarget(message: string, effective: EffectiveField[], generatorTypes: string[]): string | null {
  for (const e of effective) {
    if (message.includes(`(${e.field.key})`) || message.includes(`"${e.field.key}"`)) return `field:${e.field.key}`;
  }
  for (const t of generatorTypes) {
    if (message.includes(`(${t}:`) || message.startsWith(`${t} `) || message.includes(` ${t} `)) return `code:${t}`;
  }
  return null;
}

/** Whether a value is safe to use as a link: http or https only. */
export function isWebURL(value: string): boolean {
  try {
    const url = new URL(value);
    return url.protocol === 'http:' || url.protocol === 'https:';
  } catch {
    return false;
  }
}

/** The stock part of the asset form. Number inputs give null when blank. */
export interface StockDraft {
  counted: boolean;
  quantity: number | null;
  unit: string;
  threshold: number | null;
}

/** The form's stock for an asset's stock, or for none. */
export function stockDraft(stock: AssetStock | null | undefined): StockDraft {
  return stock
    ? { counted: true, quantity: stock.quantity, unit: stock.unit, threshold: stock.low_stock_threshold }
    : { counted: false, quantity: 0, unit: '', threshold: null };
}

export interface StockPayload {
  quantity?: number;
  unit: string;
  low_stock_threshold: number | null;
}

/**
 * The stock to send (docs/stock-and-lending.md): undefined leaves it as it is, null stops
 * counting it. An edit sends the quantity only when it was changed, so saving
 * cannot undo a use someone made while the form was open.
 */
export function stockPayload(draft: StockDraft, original?: AssetStock | null): StockPayload | null | undefined {
  if (!draft.counted) return original ? null : undefined;
  const payload: StockPayload = { unit: draft.unit.trim(), low_stock_threshold: draft.threshold ? draft.threshold : null };
  if (!original) payload.quantity = draft.quantity ?? 0;
  else if (draft.quantity !== null && draft.quantity !== original.quantity) payload.quantity = draft.quantity;
  return payload;
}

/** A quantity with its unit, if it has one. */
export function amount(quantity: number, unit: string | undefined): string {
  return unit ? `${quantity} ${unit}` : String(quantity);
}

/** What fired a hook, as "… fired" ends, by the kind of event (docs/hooks.md). */
const hookCauses: Record<string, string> = {
  'asset.created': 'as it was added',
  'asset.edited': 'as it was edited',
  'asset.restored': 'as it was restored',
  'asset.quantity_changed': 'as its quantity changed',
  'asset.running_low': 'as it ran low',
  'asset.restocked': 'as it was restocked',
  'asset.condition_changed': 'as its condition changed',
  'asset.checked_out': 'as it was lent',
  'asset.checked_in': 'as it was returned',
  'asset.overdue': 'as its loan became overdue',
  'asset.flow_overdue': 'as it passed its date',
  'asset.label_printed': 'as its label was printed',
  'asset.review_due': 'as its review fell due',
};

/** What an event in an asset's history says, without who did it. */
export function describeEvent(e: AssetEvent): string {
  const p = e.payload;
  const of = (n: number | undefined) => amount(n ?? 0, p.unit);
  switch (e.kind) {
    case 'asset.quantity_changed':
      if (p.change === undefined) return `Started counting at ${of(p.quantity)}`;
      if (p.move) {
        return p.change < 0
          ? `Moved ${of(-p.change)} to ${p.move.to.name}; ${of(p.quantity)} left`
          : `Moved ${of(p.change)} here from ${p.move.from.name}; ${of(p.quantity)} now`;
      }
      if (p.change < 0) return `Used ${of(-p.change)}; ${of(p.quantity)} left`;
      return `Added ${of(p.change)}; ${of(p.quantity)} now`;
    case 'asset.running_low':
      return p.trigger === 'threshold' ? `Running low: below the threshold of ${of(p.threshold)}` : 'Marked running low';
    case 'asset.restocked':
      return p.trigger === 'threshold' ? `Restocked: back to the threshold of ${of(p.threshold)} or more` : 'Marked restocked';
    case 'asset.created':
      return p.trigger === 'import' ? 'Created by an import' : 'Created';
    case 'asset.edited':
      // A merge of people says so (docs/data-model.md#person-fields).
      if (p.note) return p.note.replace(/\.$/, '');
      return p.trigger === 'import' ? 'Edited by an import' : 'Edited';
    case 'asset.archived':
      return 'Archived';
    case 'asset.restored':
      return 'Restored';
    case 'asset.condition_changed': {
      const to = p.changes?.[0]?.to;
      const said = p.note ? `: “${p.note}”` : '';
      if (to === 'out_of_service') return `Reported out of service${said}`;
      if (to === 'needs_maintenance') return `Reported as needing maintenance${said}`;
      return 'Marked as working';
    }
    case 'asset.checked_out': {
      const due = p.due_at ? `, due back ${dueDate(p.due_at)}` : '';
      return `Lent to ${p.borrower ? personName(p.borrower) : 'someone'}${due}`;
    }
    case 'asset.checked_in':
      return `Returned by ${p.borrower ? personName(p.borrower) : 'someone'}`;
    case 'asset.overdue':
      return `Overdue: ${p.borrower ? personName(p.borrower) : 'the borrower'} was due to bring it back ${p.due_at ? dueDate(p.due_at) : ''}`.trim();
    case 'asset.hook': {
      const label = p.hook?.label ?? 'A hook';
      if (p.hook?.trigger === 'schedule') return `${label} fired on its schedule`;
      if (p.hook?.trigger === 'event') return `${label} fired ${hookCauses[p.hook.cause?.kind ?? ''] ?? 'by an event'}`;
      return `${label} triggered`;
    }
    case 'comment.added':
      return `${commentOn(p.comment)}: “${clipped(p.comment?.text ?? '')}”`;
    case 'comment.edited':
      return `Comment edited${commentWhere(p.comment)}`;
    case 'comment.deleted':
      return `Comment deleted${commentWhere(p.comment)}`;
    case 'asset.review_changed':
    case 'asset.review_due':
      return describeReviewEvent(e);
    case 'warranty.expiring':
      // The day it ends, as written: a date, not a moment.
      return p.due_at ? `Warranty ending on ${p.due_at.slice(0, 10)}` : 'Warranty ending';
    case 'asset.label_printed': {
      const l = p.label;
      if (!l) return 'Label printed';
      const what = l.copies === 1 ? 'its label' : `${l.copies} labels`;
      return l.download ? `Downloaded ${what} (${l.template})` : `Printed ${what} on ${l.printer} (${l.template})`;
    }
    default:
      return e.kind;
  }
}

/** What a comment event's comment was on, as its history says. */
function commentOn(c: AssetEvent['payload']['comment']): string {
  if (c?.on === 'loan') return 'Commented on its loan';
  if (c?.on === 'filing') return `Commented on its filing${c.flow ? ` by ${c.flow.name}` : ''}`;
  return 'Commented';
}

function commentWhere(c: AssetEvent['payload']['comment']): string {
  if (c?.on === 'loan') return ' on its loan';
  if (c?.on === 'filing') return ` on its filing${c.flow ? ` by ${c.flow.name}` : ''}`;
  return '';
}

/** Text cut to a line's length, for history. */
function clipped(text: string): string {
  const flat = text.replace(/\s+/g, ' ').trim();
  return flat.length > 120 ? `${flat.slice(0, 119)}…` : flat;
}

/** One line of what a created, edited, archived or restored event changed. */
export function describeChange(c: AssetChange, kind: string): string {
  switch (c.what) {
    case 'category':
      return c.to ? `Added to ${c.to}` : `Removed from ${c.from}`;
    case 'stock':
      return c.to ? 'Started counting stock' : 'Stopped counting stock';
    case 'code':
      if (kind === 'asset.archived') return `Released ${c.label ?? c.key} ${c.from}`;
      if (kind === 'asset.restored') {
        return c.to ? `Got back ${c.label ?? c.key} ${c.to}` : `Not given back ${c.label ?? c.key} ${c.from}: another asset has it`;
      }
      return fromTo(c.label ?? c.key ?? 'Code', c);
    case 'field':
    case 'person':
      return fromTo(c.label ?? c.key ?? 'Field', c);
    case 'unit':
      return fromTo('Unit', c);
    case 'threshold':
      return fromTo('Low-stock threshold', c);
    case 'photo':
      return c.to ? `Photo added: ${c.to}` : `Photo removed: ${c.from}`;
    case 'attachment':
      return c.to ? `Attachment added: ${c.to}` : `Attachment removed: ${c.from}`;
    case 'comment':
      // Its text before: an edit's earlier version, or what a deleted one said.
      return c.to ? `Was “${clipped(c.from ?? '')}”, now “${clipped(c.to)}”` : `It said “${clipped(c.from ?? '')}”`;
    case 'icon':
      if (!c.to) return `Own icon ${c.from} dropped: drawn with its categories' again`;
      return fromTo('Own icon', c);
    case 'place':
      if (!c.to) return `Put back with the rest in ${c.label}`;
      return c.from ? `Moved in ${c.label}: ${c.from} → ${c.to}` : `Placed in ${c.label} at ${c.to}`;
    case 'link': {
      const label = c.label ?? 'linked to';
      return c.to ? `${capitalized(label)} ${c.to}` : `No longer ${label} ${c.from}`;
    }
    case 'vendor':
      return fromTo('Vendor', c);
    case 'order_date':
      return fromTo('Ordered', c);
    case 'received_date':
      return fromTo('Received', c);
    case 'price':
      return fromTo('Price', c);
    case 'order_number':
      return fromTo('Order number', c);
    case 'warranty_end':
      return fromTo('Warranty ends', c);
    case 'invoice':
      return c.to ? `Invoice: ${c.to}` : `No longer the invoice: ${c.from}`;
    case 'condition':
      return `Was ${conditionLabel((c.from ?? 'ok') as Condition).toLowerCase()}`;
    default:
      return fromTo('Name', c);
  }
}

function fromTo(label: string, c: AssetChange): string {
  if (!c.from) return `${label}: ${c.to}`;
  if (!c.to) return `${label}: ${c.from} removed`;
  return `${label}: ${c.from} → ${c.to}`;
}

/** How a condition reads. */
export function conditionLabel(c: Condition): string {
  switch (c) {
    case 'needs_maintenance':
      return 'Needs maintenance';
    case 'out_of_service':
      return 'Out of service';
    default:
      return 'Working';
  }
}

/** A loan's due date, as the person reading it would write it. */
export function dueDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
}

/**
 * The end of a day picked in a date input, in the browser's time zone, as
 * the API takes it: a loan due on a day is due by the end of it.
 */
export function endOfDay(day: string): string {
  const [y, m, d] = day.split('-').map(Number);
  return new Date(y!, m! - 1, d!, 23, 59, 59).toISOString();
}

function capitalized(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

/**
 * The ways one asset can be linked to another, read from it: each type's
 * name and, unless it reads the same both ways, its inverse. Types the
 * asset's categories suggest come first.
 */
export function linkChoices(types: RelationType[], suggested: string[]): { key: string; typeId: string; outgoing: boolean; label: string; suggested: boolean }[] {
  const choices = types.flatMap((t) => {
    const s = suggested.includes(t.id);
    const out = { key: `${t.id}:out`, typeId: t.id, outgoing: true, label: t.name, suggested: s };
    return t.is_symmetric ? [out] : [out, { key: `${t.id}:in`, typeId: t.id, outgoing: false, label: t.inverse_name, suggested: s }];
  });
  return [...choices.filter((c) => c.suggested), ...choices.filter((c) => !c.suggested)];
}

/** The asset something is directly in, or null when it is in nothing. */
export function placeOf(a: Pick<AssetWithDetails, 'location'>): LinkedAsset | null {
  return a.location.at(-1) ?? null;
}

/** The link putting an asset where it is: from the place, of a type that makes locations. */
export function placeLink(a: Pick<AssetWithDetails, 'location' | 'links'>, types: RelationType[]): AssetLink | null {
  const place = placeOf(a);
  const located = new Set(types.filter((t) => t.is_location).map((t) => t.id));
  return a.links.find((l) => !l.outgoing && located.has(l.type_id) && l.asset.id === place?.id) ?? null;
}

/** What an asset holds: the assets its location links lead to. */
export function contentsOf(a: Pick<AssetWithDetails, 'links'>, types: RelationType[]): AssetLink[] {
  const located = new Set(types.filter((t) => t.is_location).map((t) => t.id));
  return a.links.filter((l) => l.outgoing && located.has(l.type_id));
}
