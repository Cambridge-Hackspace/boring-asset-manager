// Helpers for the asset form.

import type { EffectiveField, FieldType, GeneratorKey } from './models';

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
}

/**
 * The request body for creating or updating an asset. Every effective field
 * is sent, so clearing an input removes its value; values kept from earlier
 * categories are not sent, so they stay. Category and code lists are always
 * sent in full: an empty list clears them.
 */
export function assetPayload(draft: AssetDraft, effective: EffectiveField[]) {
  const generator_keys: GeneratorKey[] = Object.entries(draft.codes)
    .map(([generator_type, code]) => ({ generator_type, assigned_code: code.trim() }))
    .filter((k) => k.assigned_code !== '');
  const field_values: Record<string, string> = {};
  for (const e of effective) field_values[e.field.key] = (draft.values[e.field.key] ?? '').trim();
  return { name: draft.name.trim(), category_ids: [...draft.categoryIds], generator_keys, field_values };
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
