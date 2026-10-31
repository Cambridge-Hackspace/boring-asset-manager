import { describe, expect, it } from 'vitest';
import type { Category } from './models';
import { childCounts, childrenOf, pathTo, searchCategories } from './picker';

const cat = (id: string, name: string, parent_id: string | null = null) => ({ id, name, parent_id }) as Category;
const categories = [
  cat('s', 'Storage'),
  cat('p', "Members' projects", 's'),
  cat('b', "Members' boxes", 's'),
  cat('b1', 'Small boxes', 'b'),
  cat('t', 'Tools'),
  cat('o', 'Orphan', 'gone'),
];

describe('the category picker', () => {
  it('lists one level at a time, by name, with a missing parent counting as the top', () => {
    expect(childrenOf(categories, null).map((c) => c.name)).toEqual(['Orphan', 'Storage', 'Tools']);
    expect(childrenOf(categories, 's').map((c) => c.name)).toEqual(["Members' boxes", "Members' projects"]);
    expect(childrenOf(categories, 'b1')).toEqual([]);
  });

  it('counts what is directly under each category', () => {
    const n = childCounts(categories);
    expect(n.get('s')).toBe(2);
    expect(n.get('b')).toBe(1);
    expect(n.get('t')).toBeUndefined();
  });

  it('finds the way down to a category', () => {
    expect(pathTo(categories, 'b1').map((c) => c.id)).toEqual(['s', 'b', 'b1']);
    expect(pathTo(categories, 'o').map((c) => c.id)).toEqual(['o']);
    expect(pathTo(categories, null)).toEqual([]);
  });

  it('survives a cycle', () => {
    expect(pathTo([cat('x', 'X', 'y'), cat('y', 'Y', 'x')], 'x').map((c) => c.id)).toEqual(['y', 'x']);
  });

  it('searches every level, ignoring case, with the names above each', () => {
    expect(searchCategories(categories, 'BOX')).toEqual([
      { category: categories[2], above: ['Storage'] },
      { category: categories[3], above: ['Storage', "Members' boxes"] },
    ]);
    expect(searchCategories(categories, '  ')).toEqual([]);
  });
});
