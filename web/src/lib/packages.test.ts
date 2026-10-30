import { describe, expect, it } from 'vitest';
import { anyChosen, categoryTree, needed, neededBySaid, planSummary, subtreeIds, toggleSubtree, treeState, type Plan } from './packages';

const cats = [
  { id: 'k', name: 'Kitchen', parent_id: null },
  { id: 't', name: 'Tools', parent_id: null },
  { id: 'd', name: 'Drills', parent_id: 't' },
  { id: 'c', name: 'Cordless', parent_id: 'd' },
  { id: 's', name: 'Saws', parent_id: 't' },
];

describe('the category tree', () => {
  const tree = categoryTree(cats);
  const tools = tree.find((n) => n.id === 't')!;
  const drills = tools.children.find((n) => n.id === 'd')!;

  it('nests categories under their parents, each level by name', () => {
    expect(tree.map((n) => n.name)).toEqual(['Kitchen', 'Tools']);
    expect(tools.children.map((n) => n.name)).toEqual(['Drills', 'Saws']);
    expect(subtreeIds(tools)).toEqual(['t', 'd', 'c', 's']);
  });

  it('ticks and unticks a category with everything under it', () => {
    let chosen = toggleSubtree([], tools, true);
    expect([...chosen].sort()).toEqual(['c', 'd', 's', 't']);
    expect(treeState(chosen, tools)).toBe('all');
    chosen = toggleSubtree(chosen, drills, false);
    expect([...chosen].sort()).toEqual(['s', 't']);
    // some of Tools is chosen: its box shows neither ticked nor not
    expect(treeState(chosen, tools)).toBe('some');
    expect(treeState(chosen, drills)).toBe('none');
    chosen = toggleSubtree(chosen, tools, false);
    expect(chosen).toEqual([]);
  });
});

describe('what is needed', () => {
  const plan: Plan = {
    items: [
      { kind: 'category', id: 't', name: 'Tools', chosen: true, needed_by: [] },
      { kind: 'field', id: 'f', name: 'Serial', chosen: false, needed_by: [{ kind: 'category', id: 't', name: 'Tools' }] },
      {
        kind: 'action',
        id: 'a',
        name: 'Open',
        chosen: false,
        needed_by: [
          { kind: 'category', id: 't', name: 'Tools' },
          { kind: 'flow', id: 'l', name: 'Lend a tool' },
          { kind: 'flow', id: 'r', name: 'Return a tool' },
        ],
      },
      // chosen and needed too: it can still be unticked
      { kind: 'field', id: 'g', name: 'Bought', chosen: true, needed_by: [{ kind: 'category', id: 't', name: 'Tools' }] },
    ],
    left_out: [],
  };

  it('marks what was not chosen and is needed, with what needs it', () => {
    const n = needed(plan);
    expect([...n.keys()]).toEqual(['field:f', 'action:a']);
    expect(neededBySaid(n.get('field:f')!)).toBe('Needed by Tools');
    expect(neededBySaid(n.get('action:a')!)).toBe('Needed by Tools, Lend a tool and Return a tool');
    expect(needed(null).size).toBe(0);
  });

  it('says what the package holds, in words', () => {
    expect(planSummary(plan, false)).toBe('The package holds 4 things.');
    const withAssets = { ...plan, assets: { assets: 1, photos: 3, links: 0, events: 0, unreadable: 2 } };
    expect(planSummary(withAssets, true)).toBe('The package holds 4 things and 1 asset with 3 photos. 2 assets you cannot see are left out.');
  });

  it('knows when nothing is chosen', () => {
    expect(anyChosen({ categories: [], fields: [] })).toBe(false);
    expect(anyChosen({ fields: ['f'] })).toBe(true);
    expect(anyChosen({ icons: true })).toBe(true);
    expect(anyChosen({ assets: { all: true, photos: false, links: false, history: false } })).toBe(true);
  });
});
