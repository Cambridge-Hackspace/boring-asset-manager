// The category picker's view of the tree (components/CategoryPicker.svelte):
// one level at a time, the way back up, and a search across every level.

import type { Category } from './models';

/** A category's parent, or null at the top, treating a missing parent as none. */
function parentOf(byID: Map<string, Category>, c: Category): string | null {
  return c.parent_id && byID.has(c.parent_id) ? c.parent_id : null;
}

/** The categories directly under parent (null for the top), by name. */
export function childrenOf(categories: Category[], parent: string | null): Category[] {
  const byID = new Map(categories.map((c) => [c.id, c]));
  return categories.filter((c) => parentOf(byID, c) === parent).sort((a, b) => a.name.localeCompare(b.name));
}

/** How many categories sit directly under each one. */
export function childCounts(categories: Category[]): Map<string, number> {
  const byID = new Map(categories.map((c) => [c.id, c]));
  const out = new Map<string, number>();
  for (const c of categories) {
    const p = parentOf(byID, c);
    if (p) out.set(p, (out.get(p) ?? 0) + 1);
  }
  return out;
}

/** The categories from the top down to id, id last; empty for none. */
export function pathTo(categories: Category[], id: string | null): Category[] {
  const byID = new Map(categories.map((c) => [c.id, c]));
  const out: Category[] = [];
  for (let c = id ? byID.get(id) : undefined; c && out.length < 100; c = byID.get(parentOf(byID, c) ?? '')) {
    if (out.includes(c)) break; // a cycle; the server prevents these
    out.unshift(c);
  }
  return out;
}

/** Categories whose name holds the query, ignoring case, by name, each with the names above it. */
export function searchCategories(categories: Category[], query: string): { category: Category; above: string[] }[] {
  const q = query.trim().toLocaleLowerCase();
  if (!q) return [];
  return categories
    .filter((c) => c.name.toLocaleLowerCase().includes(q))
    .sort((a, b) => a.name.localeCompare(b.name))
    .map((c) => ({ category: c, above: pathTo(categories, c.id).slice(0, -1).map((p) => p.name) }));
}
