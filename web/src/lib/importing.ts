// Helpers for CSV import and export (docs/csv.md).

import type { Field, GeneratorInfo, ImportPlan, ImportProblem, LinkChange } from './models';

export const IGNORE = 'ignore';

/** The most files imported as one set. */
export const MAX_FILES = 20;

export interface TargetOption {
  value: string;
  label: string;
  group: 'Asset' | 'Stock' | 'Codes' | 'Fields';
}

/** Everywhere a column can go, grouped for a select. */
export function targetOptions(fields: Field[], generators: GeneratorInfo[]): TargetOption[] {
  return [
    { value: 'id', label: 'ID (updates that asset)', group: 'Asset' },
    { value: 'name', label: 'Name', group: 'Asset' },
    { value: 'categories', label: 'Categories', group: 'Asset' },
    { value: 'links', label: 'Links (verb:asset, such as contained_in:Bin A3)', group: 'Asset' },
    { value: 'quantity', label: 'Quantity', group: 'Stock' },
    { value: 'unit', label: 'Unit', group: 'Stock' },
    { value: 'low_stock_threshold', label: 'Low stock threshold', group: 'Stock' },
    ...generators.map((g) => ({ value: `code.${g.type}`, label: g.name, group: 'Codes' as const })),
    ...[...fields]
      .sort((a, b) => a.label.localeCompare(b.label))
      .map((f) => ({ value: `field.${f.key}`, label: `${f.label} (${f.key})`, group: 'Fields' as const })),
  ];
}

/** The form an import request sends; removeLinks removes links no row states, and sheet names a workbook's sheet. */
export function importForm(file: File, mapping?: string[], removeLinks = false, sheet = ''): FormData {
  const form = new FormData();
  form.append('file', file);
  if (sheet) form.append('sheet', sheet);
  if (mapping) form.append('mapping', JSON.stringify(mapping));
  if (removeLinks) form.append('remove_links', 'true');
  return form;
}

/** What a file is, by its name: a CSV, an Excel workbook, or one of the spreadsheets not taken. */
export function fileKind(name: string): 'csv' | 'xlsx' | 'other' {
  const n = name.toLowerCase();
  if (n.endsWith('.xlsx')) return 'xlsx';
  if (n.endsWith('.xls') || n.endsWith('.ods') || n.endsWith('.numbers')) return 'other';
  return 'csv';
}

/** The import page's file picker accepts these. */
export const IMPORT_ACCEPT = '.csv,text/csv,.xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet';

const mappingKey = (userId: string, headers: string[]) => `bam.import.mapping.${userId}.${JSON.stringify(headers)}`;

/** The mapping this person last used for a file with these headers, if any. */
export function rememberedMapping(userId: string, headers: string[]): string[] | null {
  try {
    const raw = localStorage.getItem(mappingKey(userId, headers));
    const mapping: unknown = raw === null ? null : JSON.parse(raw);
    return Array.isArray(mapping) && mapping.length === headers.length && mapping.every((t) => typeof t === 'string') ? mapping : null;
  } catch {
    return null;
  }
}

/** Remembers a mapping for the next file with the same headers. */
export function rememberMapping(userId: string, headers: string[], mapping: string[]): void {
  try {
    localStorage.setItem(mappingKey(userId, headers), JSON.stringify(mapping));
  } catch {
    // Not remembered: the next file starts from the suggested mapping.
  }
}

/** The export address for the asset list's filters. */
export function exportUrl(search: string, categoryId: string): string {
  const q = new URLSearchParams();
  if (search.trim()) q.set('search', search.trim());
  if (categoryId) q.set('category_id', categoryId);
  const query = q.toString();
  return '/api/v1/assets/export.csv' + (query ? `?${query}` : '');
}

/** Columns used more than once, other than ignored ones. */
export function duplicateTargets(mapping: string[]): Set<string> {
  const seen = new Set<string>();
  const dups = new Set<string>();
  for (const t of mapping) {
    if (!t || t === IGNORE) continue;
    if (seen.has(t)) dups.add(t);
    seen.add(t);
  }
  return dups;
}

/** A problem as one line of text. */
export function describeProblem(p: ImportProblem): string {
  const where = p.row === 1 ? 'Header' : `Line ${p.row}`;
  return p.column ? `${where}, ${p.column}: ${p.message}` : `${where}: ${p.message}`;
}

/** A plan's outcome in a sentence. */
export function summarize(plan: ImportPlan): string {
  const rows = plan.rows === 1 ? '1 row' : `${plan.rows} rows`;
  const links = linkCounts(plan.links_added, plan.links_moved, plan.links_removed);
  return `${rows}: ${plan.create} new, ${plan.update} updated, ${plan.unchanged} unchanged${links ? `; ${links}` : ''}.`;
}

/** Link changes in words, or '' for none. */
export function linkCounts(added = 0, moved = 0, removed = 0): string {
  const parts: string[] = [];
  if (added) parts.push(`${added} ${added === 1 ? 'link' : 'links'} added`);
  if (moved) parts.push(`${moved} moved`);
  if (removed) parts.push(`${removed} removed`);
  return parts.join(', ');
}

/** A link change as a line of the preview. */
export function describeLinkChange(c: LinkChange): string {
  const what = { add: 'Add', move: 'Move', remove: 'Remove' }[c.change];
  return `${what}: ${c.asset} ${c.label} ${c.other}${c.row ? ` (line ${c.row})` : ''}`;
}
