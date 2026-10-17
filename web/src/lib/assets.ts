// Helpers for the asset form.

import type { AssetChange, AssetEvent, AssetStock, Condition, EffectiveField, FieldType, GeneratorKey, RelationType } from './models';
import { personName } from './people';

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

export interface AssetDraft {
  name: string;
  categoryIds: string[];
  /** Codes by generator type; blank means none. */
  codes: Record<string, string>;
  /** Values by field key. */
  values: Record<string, string>;
  stock?: StockDraft;
}

/**
 * The request body for creating or updating an asset. Every effective field
 * is sent, so clearing an input removes its value; values kept from earlier
 * categories are not sent, so they stay. Category and code lists are always
 * sent in full: an empty list clears them. Stock is sent as stockPayload
 * says, given the asset's stock before the edit.
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
  return body;
}

/** Values the asset keeps from categories it no longer belongs to. */
export function keptValues(values: Record<string, string>, effective: EffectiveField[]): [string, string][] {
  const current = new Set(effective.map((e) => e.field.key));
  return Object.entries(values).filter(([key, value]) => !current.has(key) && value !== '');
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
 * The stock to send (README §5.1): undefined leaves it as it is, null stops
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

/** What an event in an asset's history says, without who did it. */
export function describeEvent(e: AssetEvent): string {
  const p = e.payload;
  const of = (n: number | undefined) => amount(n ?? 0, p.unit);
  switch (e.kind) {
    case 'asset.quantity_changed':
      if (p.change === undefined) return `Started counting at ${of(p.quantity)}`;
      if (p.change < 0) return `Used ${of(-p.change)}; ${of(p.quantity)} left`;
      return `Added ${of(p.change)}; ${of(p.quantity)} now`;
    case 'asset.running_low':
      return p.trigger === 'threshold' ? `Running low: below the threshold of ${of(p.threshold)}` : 'Marked running low';
    case 'asset.restocked':
      return p.trigger === 'threshold' ? `Restocked: back to the threshold of ${of(p.threshold)} or more` : 'Marked restocked';
    case 'asset.created':
      return p.trigger === 'import' ? 'Created by an import' : 'Created';
    case 'asset.edited':
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
      return `Checked in from ${p.borrower ? personName(p.borrower) : 'someone'}`;
    case 'asset.overdue':
      return `Overdue: ${p.borrower ? personName(p.borrower) : 'the borrower'} was due to bring it back ${p.due_at ? dueDate(p.due_at) : ''}`.trim();
    case 'asset.action':
      return `${p.action?.label ?? 'An action'} pressed`;
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
      return fromTo(c.label ?? c.key ?? 'Field', c);
    case 'unit':
      return fromTo('Unit', c);
    case 'threshold':
      return fromTo('Low-stock threshold', c);
    case 'photo':
      return c.to ? `Photo added: ${c.to}` : `Photo removed: ${c.from}`;
    case 'link': {
      const label = c.label ?? 'linked to';
      return c.to ? `${capitalized(label)} ${c.to}` : `No longer ${label} ${c.from}`;
    }
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
