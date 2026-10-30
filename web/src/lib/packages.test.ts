import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  actionSaid,
  aKind,
  anyChosen,
  categoryTree,
  clashes,
  importJob,
  importSummary,
  needed,
  neededBySaid,
  planSummary,
  subtreeIds,
  toggleSubtree,
  treeState,
  type ImportItem,
  type ImportPlan,
  type Plan,
} from './packages';
import { fakeApi } from '../test/fetch';

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

describe('importing', () => {
  afterEach(() => vi.unstubAllGlobals());

  const items: ImportItem[] = [
    { kind: 'category', id: 'c', name: 'Tools', action: 'create' },
    { kind: 'field', id: 'f', name: 'Serial', action: 'copy', as: 'serial_2', existing: { kind: 'field', id: 'h', name: 'Serial' }, choices: ['existing', 'copy', 'skip'] },
    { kind: 'action', id: 'a', name: 'Open', action: 'existing', existing: { kind: 'action', id: 'h2', name: 'Open (open)' }, identical: true, choices: ['existing', 'copy', 'skip'] },
    { kind: 'relation_type', id: 'r', name: 'Stores', action: 'skip', existing: { kind: 'relation_type', id: 'h3', name: 'Stores' }, choices: ['existing', 'copy', 'skip'] },
  ];
  const plan = { items, assets: { assets: 1, photos: 0, links: 2 } } as unknown as ImportPlan;

  it('names a kind with its article, and says what replacing does', () => {
    expect(aKind('field')).toBe('a field');
    expect(aKind('icon_library_file')).toBe('an icon library file');
    expect(actionSaid({ kind: 'palette_token', id: 'sky', name: 'sky', action: 'replace' })).toBe('Replaces the one here');
  });

  it('lists what clashes, to choose for', () => {
    expect(clashes(plan).map((i) => i.id)).toEqual(['f', 'a', 'r']);
    expect(clashes(null)).toEqual([]);
  });

  it('says what it does with each thing, and with all of them', () => {
    expect(items.map(actionSaid)).toEqual(['Created', 'Imported as a copy, serial_2', 'Uses Open (open), here already', 'Skipped']);
    expect(importSummary(plan)).toBe('It creates 2 things (1 of them copies), uses 1 here already, skips 1, and brings 1 asset with 0 photos and 2 links.');
  });

  it('waits for a check, and gives its plan even when refused', async () => {
    let asked = 0;
    fakeApi({
      'POST /import/check': () => ({ status: 202, body: { id: 'j', done: false } }),
      'GET /import/jobs/j': () => (++asked < 2 ? { id: 'j', done: false } : { id: 'j', done: true, error: 'needs flows.manage', result: { plan } }),
    });
    const out = await importJob('/import/check', { upload_id: 'u', anchor: '', choices: {} }, { sleep: async () => {} });
    expect(out.error).toBe('needs flows.manage');
    expect(out.plan?.items).toHaveLength(4);
    expect(asked).toBe(2);
  });
});
