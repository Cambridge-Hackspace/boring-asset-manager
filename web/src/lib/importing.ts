// Helpers for CSV import and export (README §6.1).

import type { Field, GeneratorInfo, ImportPlan, ImportProblem } from './models';

export const IGNORE = 'ignore';

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
    { value: 'quantity', label: 'Quantity', group: 'Stock' },
    { value: 'unit', label: 'Unit', group: 'Stock' },
    { value: 'low_stock_threshold', label: 'Low stock threshold', group: 'Stock' },
    ...generators.map((g) => ({ value: `code.${g.type}`, label: g.name, group: 'Codes' as const })),
    ...[...fields]
      .sort((a, b) => a.label.localeCompare(b.label))
      .map((f) => ({ value: `field.${f.key}`, label: `${f.label} (${f.key})`, group: 'Fields' as const })),
  ];
}

/** The form an import request sends. */
export function importForm(file: File, mapping?: string[]): FormData {
  const form = new FormData();
  form.append('file', file);
  if (mapping) form.append('mapping', JSON.stringify(mapping));
  return form;
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
  return `${rows}: ${plan.create} new, ${plan.update} updated, ${plan.unchanged} unchanged.`;
}
