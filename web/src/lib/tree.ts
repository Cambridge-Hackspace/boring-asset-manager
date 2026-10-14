// The category tree, flattened for display.

import type { Category } from './models';

export interface TreeRow {
  category: Category;
  depth: number;
}

/**
 * Orders categories as a tree, parents before children, siblings by name.
 * Categories whose parent is missing are treated as roots.
 */
export function flattenTree(categories: Category[]): TreeRow[] {
  const ids = new Set(categories.map((c) => c.id));
  const children = new Map<string | null, Category[]>();
  for (const c of categories) {
    const parent = c.parent_id && ids.has(c.parent_id) ? c.parent_id : null;
    const list = children.get(parent) ?? [];
    list.push(c);
    children.set(parent, list);
  }
  for (const list of children.values()) list.sort((a, b) => a.name.localeCompare(b.name));

  const rows: TreeRow[] = [];
  const seen = new Set<string>();
  const walk = (parent: string | null, depth: number) => {
    for (const c of children.get(parent) ?? []) {
      if (seen.has(c.id)) continue; // a cycle; the server prevents these
      seen.add(c.id);
      rows.push({ category: c, depth });
      walk(c.id, depth + 1);
    }
  };
  walk(null, 0);
  return rows;
}

/** The IDs of a category and everything below it. */
export function subtree(categories: Category[], id: string): Set<string> {
  const result = new Set([id]);
  let grew = true;
  while (grew) {
    grew = false;
    for (const c of categories) {
      if (c.parent_id && result.has(c.parent_id) && !result.has(c.id)) {
        result.add(c.id);
        grew = true;
      }
    }
  }
  return result;
}

/** The names from the root down to a category, for breadcrumbs. */
export function ancestry(categories: Category[], id: string): Category[] {
  const byId = new Map(categories.map((c) => [c.id, c]));
  const path: Category[] = [];
  let current = byId.get(id);
  while (current && path.length < 100) {
    path.unshift(current);
    current = current.parent_id ? byId.get(current.parent_id) : undefined;
  }
  return path;
}
