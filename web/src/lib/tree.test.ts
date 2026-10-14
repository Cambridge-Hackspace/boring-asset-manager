import { describe, expect, it } from 'vitest';
import type { Category } from './models';
import { ancestry, flattenTree, subtree } from './tree';

function cat(id: string, name: string, parent: string | null = null): Category {
  return { id, name, parent_id: parent, color_hex: '#000000', is_consumable: false, is_running_low: false, created_at: '', updated_at: '' };
}

const cats = [
  cat('saws', 'Saws', 'tools'),
  cat('tools', 'Tools'),
  cat('drills', 'Drills', 'tools'),
  cat('bits', 'Bits', 'drills'),
  cat('paint', 'Paint'),
  cat('orphan', 'Orphan', 'gone'),
];

describe('flattenTree', () => {
  it('puts parents first and orders siblings by name', () => {
    expect(flattenTree(cats).map((r) => `${'-'.repeat(r.depth)}${r.category.id}`)).toEqual([
      'orphan',
      'paint',
      'tools',
      '-drills',
      '--bits',
      '-saws',
    ]);
  });

  it('survives a cycle', () => {
    const loop = [cat('a', 'A', 'b'), cat('b', 'B', 'a'), cat('c', 'C')];
    expect(flattenTree(loop).map((r) => r.category.id)).toEqual(['c']);
  });
});

describe('subtree and ancestry', () => {
  it('finds a category and its descendants', () => {
    expect([...subtree(cats, 'tools')].sort()).toEqual(['bits', 'drills', 'saws', 'tools']);
    expect([...subtree(cats, 'paint')]).toEqual(['paint']);
  });

  it('lists the path from the root', () => {
    expect(ancestry(cats, 'bits').map((c) => c.name)).toEqual(['Tools', 'Drills', 'Bits']);
  });
});
