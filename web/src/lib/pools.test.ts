import { describe, expect, it } from 'vitest';
import type { AssetPool, Category } from './models';
import { categoryPath, linkChoices, otherPlaces, poolLine, whereElse } from './pools';

const cat = (id: string, name: string, parent?: string): Category => ({
  id, name, parent_id: parent ?? null, color_hex: '#000000', is_consumable: false, unit: '', is_lendable: false, is_running_low: false, created_at: '', updated_at: '',
});
const categories = [cat('w', 'Workshop'), cat('f', 'Fasteners', 'w'), cat('m5', 'M5', 'f'), cat('s', 'Stock'), cat('b', 'M5 bolts', 's')];
const pool: AssetPool = {
  category_id: 'b', name: 'M5 bolts', unit: 'pcs', low_stock_threshold: 200, total: 440,
  places: [
    { asset_id: 'here', name: 'Drawer bin', quantity: 140, location: [{ id: 'ws', name: 'Workshop' }] },
    { asset_id: 'bay', name: 'Bay bin', quantity: 300, location: [{ id: 'lb', name: 'Laser room' }, { id: 'lbay', name: 'laser bay' }] },
    { asset_id: 'loose', name: 'Loose box', quantity: 5, location: [] },
    { asset_id: 'empty', name: 'Empty tray', quantity: 0, location: [] },
  ],
};

describe('pools', () => {
  it('names a category with those above it', () => {
    expect(categoryPath(categories, 'm5')).toBe('Workshop › Fasteners › M5');
    expect(categoryPath(categories, 'gone', 'a deleted category')).toBe('a deleted category');
  });

  it('says where else to look, here first, by where each place is, leaving out empty places', () => {
    expect(whereElse(pool, 'here')).toBe('140 pcs here, 300 pcs in laser bay, 5 pcs in Loose box');
  });

  it('reads a pool\'s line with its total and threshold', () => {
    expect(poolLine('M5 bolts', pool)).toBe('M5 bolts: 440 pcs (threshold 200 pcs)');
    expect(poolLine('PPE', { total: 3, unit: '', low_stock_threshold: null })).toBe('PPE: 3');
  });

  it('lists the other places once, for moving stock', () => {
    const twice = { ...pool, category_id: 'x', name: 'Fasteners' };
    expect(otherPlaces([pool, twice], 'here').map((p) => p.asset_id)).toEqual(['bay', 'loose', 'empty']);
  });

  it('offers neither itself, Uncategorized, what is linked, nor what is inside it', () => {
    const all = [...categories, cat('uncategorized', 'Uncategorized')];
    expect(linkChoices(all, 'w', ['b'], new Set(['w', 'f', 'm5']), 'uncategorized').map((c) => c.id)).toEqual(['s']);
  });
});
