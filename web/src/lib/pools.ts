import { amount } from './assets';
import type { AssetPool, Category, PoolPlace, PoolSummary } from './models';
import { ancestry } from './tree';

// Categories that imply other categories, and the pools they make
// (docs/stock-and-lending.md#pools).

/** A category with those above it: "Workshop › Fasteners › M5". */
export function categoryPath(categories: Category[], id: string, fallback = 'a category'): string {
  const chain = ancestry(categories, id);
  return chain.length ? chain.map((c) => c.name).join(' › ') : fallback;
}

/** Where a place is, by its innermost container, or by its own name when it is in nothing. */
export function placeName(place: PoolPlace): string {
  const inner = place.location[place.location.length - 1];
  return inner ? inner.name : place.name;
}

/** "140 pcs here, 300 pcs in laser bay": what the asset holds, then where else the pool's stock is, leaving out empty places. */
export function whereElse(pool: AssetPool, hereId: string): string {
  return pool.places
    .filter((p) => p.asset_id === hereId || p.quantity > 0)
    .map((p) => (p.asset_id === hereId ? `${amount(p.quantity, pool.unit)} here` : `${amount(p.quantity, pool.unit)} in ${placeName(p)}`))
    .join(', ');
}

/** "M5 bolts: 440 pcs (threshold 200 pcs)": a pool's line. */
export function poolLine(name: string, pool: Pick<PoolSummary, 'total' | 'unit' | 'low_stock_threshold'>): string {
  const threshold = pool.low_stock_threshold ? ` (threshold ${amount(pool.low_stock_threshold, pool.unit)})` : '';
  return `${name}: ${amount(pool.total, pool.unit)}${threshold}`;
}

/** The places other than here, once each, to move stock to or from. */
export function otherPlaces(pools: AssetPool[], hereId: string): PoolPlace[] {
  const seen = new Set<string>();
  const out: PoolPlace[] = [];
  for (const pool of pools) {
    for (const p of pool.places) {
      if (p.asset_id !== hereId && !seen.has(p.asset_id)) {
        seen.add(p.asset_id);
        out.push(p);
      }
    }
  }
  return out;
}

/**
 * The categories a category may be linked to on the given side: not
 * itself, not Uncategorized, not one already linked, and, for what it
 * counts in, none inside it, since that would lead back to it.
 */
export function linkChoices(categories: Category[], id: string, taken: string[], inside: Set<string>, uncategorized: string): Category[] {
  return categories.filter((c) => c.id !== id && c.id !== uncategorized && !taken.includes(c.id) && !inside.has(c.id));
}
