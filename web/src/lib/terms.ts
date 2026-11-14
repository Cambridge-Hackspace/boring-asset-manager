// Related terms, which search expands through (docs/data-model.md#related-terms).

import type { Term, TermImport, TermImportItem, TermLink, TermSource } from './models';

/** Where a term or link came from, as a person reads it: who added it, through which assistant, or in which import. */
export function sourceOf(s: TermSource, imports: TermImport[] = []): string {
  if (s.import_id) {
    const imp = imports.find((i) => i.id === s.import_id);
    return imp ? `From the import ${imp.name}` : 'From an import';
  }
  const who = s.added_by_name ? `by ${s.added_by_name}` : '';
  if (s.via) return ['Added', who, `through ${s.via}`].filter(Boolean).join(' ');
  return who ? `Added ${who}` : '';
}

export interface Neighbors {
  broader: string[];
  narrower: string[];
  related: string[];
}

/** The terms a term links to, by ID: those broader and narrower than it, and those related to it. */
export function neighborsOf(id: string, links: TermLink[]): Neighbors {
  const out: Neighbors = { broader: [], narrower: [], related: [] };
  for (const l of links) {
    if (l.relation === 'broader') {
      if (l.term_id === id) out.broader.push(l.other_id);
      else if (l.other_id === id) out.narrower.push(l.term_id);
    } else if (l.relation === 'related') {
      if (l.term_id === id) out.related.push(l.other_id);
      else if (l.other_id === id) out.related.push(l.term_id);
    }
  }
  return out;
}

/** A term's other names, one per line or separated by commas, trimmed, with blanks left out. */
export function splitNames(text: string): string[] {
  return text
    .split(/[\n,]/)
    .map((s) => s.trim())
    .filter(Boolean);
}

/** Whether a term is called text, by its label or another name, ignoring case and spacing. */
export function termMatches(t: Term, text: string): boolean {
  const want = fold(text);
  if (!want) return true;
  return [t.label, ...t.alt_labels, t.note].some((l) => fold(l).includes(want));
}

function fold(s: string): string {
  return s.toLowerCase().split(/\s+/).filter(Boolean).join(' ');
}

/**
 * The terms in a file to import: JSON, either {"name", "terms"} or a list of terms, each
 * {"label", "alt_labels", "note", "broader", "related"} with broader and related terms by
 * label. Throws an Error saying what is wrong.
 */
export function parseImport(text: string, fallbackName: string): { name: string; terms: TermImportItem[] } {
  let data: unknown;
  try {
    data = JSON.parse(text);
  } catch {
    throw new Error('The file is not JSON.');
  }
  let name = fallbackName;
  let list: unknown = data;
  if (data && typeof data === 'object' && !Array.isArray(data)) {
    const o = data as { name?: unknown; terms?: unknown };
    if (typeof o.name === 'string' && o.name.trim()) name = o.name.trim();
    list = o.terms;
  }
  if (!Array.isArray(list) || list.length === 0) throw new Error('The file holds no terms: give a list of them, or {"name", "terms"}.');
  const strings = (v: unknown): string[] => (Array.isArray(v) ? v.filter((s): s is string => typeof s === 'string') : []);
  const terms = list.map((item, i): TermImportItem => {
    const o = (item ?? {}) as Record<string, unknown>;
    if (typeof o.label !== 'string' || !o.label.trim()) throw new Error(`Term ${i + 1} has no label.`);
    return { label: o.label, alt_labels: strings(o.alt_labels), note: typeof o.note === 'string' ? o.note : '', broader: strings(o.broader), related: strings(o.related) };
  });
  return { name, terms };
}
