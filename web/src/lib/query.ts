// The asset search language (docs/data-model.md#queries), as the query
// builder writes it: keys, the comparisons each takes, and quoting.

import type { Category, Field } from './models';

export type KeyKind = 'text' | 'number' | 'date' | 'choice' | 'person' | 'bool';

/** Something a query can search by. */
export interface QueryKey {
  key: string;
  label: string;
  kind: KeyKind;
  /** For choices: what the key may be, as [value written, label shown]. */
  choices?: [string, string][];
  /** Whether IS EMPTY means anything for it. */
  emptiable: boolean;
}

/** A comparison the builder offers: what it writes, and how it reads. */
export interface QueryOp {
  op: string;
  label: string;
  /** IS EMPTY and IS NOT EMPTY take no value. */
  noValue?: boolean;
}

const textOps: QueryOp[] = [
  { op: '~', label: 'contains' },
  { op: '=', label: 'is' },
  { op: '!=', label: 'is not' },
  { op: '!~', label: 'does not contain' },
];
// Text also matches through related terms and despite typing mistakes (docs/data-model.md#related-terms).
const wordOps: QueryOp[] = [...textOps, { op: '~~', label: 'contains, or a related word' }];
const numberOps: QueryOp[] = [
  { op: '=', label: '=' },
  { op: '!=', label: '≠' },
  { op: '<', label: '<' },
  { op: '<=', label: '≤' },
  { op: '>', label: '>' },
  { op: '>=', label: '≥' },
];
const dateOps: QueryOp[] = [
  { op: '=', label: 'on' },
  { op: '<', label: 'before' },
  { op: '>', label: 'after' },
  { op: '<=', label: 'on or before' },
  { op: '>=', label: 'on or after' },
];
const choiceOps: QueryOp[] = [
  { op: '=', label: 'is' },
  { op: '!=', label: 'is not' },
];
const boolOps: QueryOp[] = [{ op: '=', label: 'is' }];
const emptyOps: QueryOp[] = [
  { op: 'IS EMPTY', label: 'is empty', noValue: true },
  { op: 'IS NOT EMPTY', label: 'is not empty', noValue: true },
];

/** The comparisons a key takes. */
export function opsFor(k: QueryKey): QueryOp[] {
  const base = { text: wordOps, person: textOps, number: numberOps, date: dateOps, choice: choiceOps, bool: boolOps }[k.kind];
  return k.emptiable ? [...base, ...emptyOps] : base;
}

/**
 * The categories as choices, each once: by name, or by its path (as
 * "Shop > Saws", which the server also matches) where categories share a
 * name, so that each choice says which category it means.
 */
export function categoryChoices(categories: Category[]): [string, string][] {
  const byId = new Map(categories.map((c) => [c.id, c]));
  const path = (c: Category): string => {
    const parts = [c.name];
    for (let p = c.parent_id ? byId.get(c.parent_id) : undefined, d = 0; p && d < 100; p = p.parent_id ? byId.get(p.parent_id) : undefined, d++) {
      parts.unshift(p.name);
    }
    return parts.join(' > ');
  };
  const named = new Map<string, number>();
  for (const c of categories) named.set(c.name, (named.get(c.name) ?? 0) + 1);
  const seen = new Set<string>();
  const out: [string, string][] = [];
  for (const c of categories) {
    const value = named.get(c.name)! > 1 ? path(c) : c.name;
    if (seen.has(value)) continue;
    seen.add(value);
    out.push([value, value]);
  }
  return out;
}

/**
 * What a query can search by: the keys every asset has, then each field,
 * by its key. Categories are offered as categoryChoices; signedIn adds
 * favorites.
 */
export function queryKeys(fields: Field[], categories: Category[], signedIn: boolean): QueryKey[] {
  const keys: QueryKey[] = [
    { key: 'name', label: 'Name', kind: 'text', emptiable: false },
    { key: 'code', label: 'Code', kind: 'text', emptiable: true },
    { key: 'category', label: 'Category', kind: 'choice', emptiable: false, choices: categoryChoices(categories) },
    { key: 'in', label: 'Where it is', kind: 'text', emptiable: true },
    {
      key: 'condition',
      label: 'Condition',
      kind: 'choice',
      emptiable: false,
      choices: [
        ['ok', 'Working'],
        ['problem', 'Needs attention'],
        ['needs_maintenance', 'Needs maintenance'],
        ['out_of_service', 'Out of service'],
      ],
    },
    {
      key: 'loan',
      label: 'Loan',
      kind: 'choice',
      emptiable: true,
      choices: [
        ['open', 'Lent out'],
        ['overdue', 'Overdue'],
      ],
    },
    { key: 'assigned', label: 'Assigned to', kind: 'person', emptiable: true },
    { key: 'created', label: 'Added', kind: 'date', emptiable: false },
    { key: 'updated', label: 'Changed', kind: 'date', emptiable: false },
    { key: 'quantity', label: 'Quantity', kind: 'number', emptiable: true },
  ];
  if (signedIn) keys.push({ key: 'favorite', label: 'Starred', kind: 'bool', emptiable: false, choices: [['true', 'Yes'], ['false', 'No']] });
  for (const f of [...fields].sort((a, b) => a.label.localeCompare(b.label))) {
    const k: QueryKey = { key: `field.${f.key}`, label: f.label, kind: 'text', emptiable: true };
    switch (f.validation_type) {
      case 'number':
        k.kind = 'number';
        break;
      case 'date':
        k.kind = 'date';
        break;
      case 'select':
        k.kind = 'choice';
        k.choices = (f.options?.choices ?? []).map((c) => [c.value, c.label]);
        break;
      case 'person':
        k.kind = 'person';
        break;
    }
    keys.push(k);
  }
  return keys;
}

// Words the language keeps for itself, which must be quoted as values.
const reserved = new Set(['and', 'or', 'not', 'in', 'is', 'empty', 'order', 'by', 'asc', 'desc']);

/**
 * A value as a query writes it: bare where it can be, in double quotes
 * where it has spaces, quotes or characters the language uses.
 */
export function quote(value: string): string {
  if (/^[^\s()=!~<>,"']+$/.test(value) && !reserved.has(value.toLowerCase())) return value;
  return '"' + value.replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"';
}

/** One condition in the builder. */
export interface Condition {
  key: string;
  op: string;
  value: string;
}

/** One condition as a query writes it; '' when it isn't finished. */
export function writeCondition(c: Condition): string {
  if (!c.key || !c.op) return '';
  if (c.op === 'IS EMPTY' || c.op === 'IS NOT EMPTY') return `${c.key} ${c.op}`;
  const v = c.value.trim();
  if (!v) return '';
  // me, unquoted, is whoever searches.
  return `${c.key} ${c.op} ${v.toLowerCase() === 'me' ? 'me' : quote(v)}`;
}

/**
 * The query the builder's conditions make: joined by AND for all of them,
 * OR for any, then sorted by sort if given. A query with OR and an order
 * keeps its order outside the conditions, which need no parentheses.
 */
export function buildQuery(conditions: Condition[], all: boolean, sort = '', descending = false): string {
  const parts = conditions.map(writeCondition).filter(Boolean);
  let q = parts.join(all ? ' AND ' : ' OR ');
  if (sort) q += `${q ? ' ' : ''}ORDER BY ${sort}${descending ? ' DESC' : ''}`;
  return q;
}

/** Where a query's mistake is, as the API reports it. */
export interface QueryProblem {
  message: string;
  start: number;
  end: number;
}

/** The query problem in an API error's body, if it holds one. */
export function queryProblemOf(body: unknown): QueryProblem | null {
  const qe = (body as { query_error?: QueryProblem } | null | undefined)?.query_error;
  return qe && typeof qe.start === 'number' && typeof qe.end === 'number' ? qe : null;
}
