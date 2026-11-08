import { describe, expect, it } from 'vitest';
import { BOX_H, BOX_W, MARGIN, assetNote, graphUrl, linkPath, listed, nested, place, summary } from './graph';
import type { AssetGraph, GraphAsset, RelationType } from './models';

const dep = { id: 'dep', name: 'depends on', inverse_name: 'needed by', is_symmetric: false } as RelationType;
const a = (id: string, name: string, over: Partial<GraphAsset> = {}): GraphAsset => ({
  id,
  name,
  categories: [],
  icon: null,
  condition: 'ok',
  hops: 0,
  layer: 0,
  row: 0,
  ...over,
});

// The extractor's graph: the laser and the saw need it, the bench needs the
// laser; it needs the supply, which needs the breaker.
const extractor: AssetGraph = {
  root: 'x',
  types: [dep],
  assets: [
    a('x', 'Extractor', { side: 'root', layer: 2 }),
    a('laser', 'Laser', { side: 'in', hops: 1, layer: 1 }),
    a('saw', 'Saw', { side: 'in', hops: 1, layer: 1, row: 1 }),
    a('bench', 'Bench', { side: 'in', hops: 2, layer: 0 }),
    a('supply', 'Supply', { side: 'out', hops: 1, layer: 3, condition: 'out_of_service' }),
    a('breaker', 'Breaker', { side: 'out', hops: 2, layer: 4, archived: true, cut: true }),
  ],
  links: [
    { id: '1', type_id: 'dep', from: 'laser', to: 'x' },
    { id: '2', type_id: 'dep', from: 'saw', to: 'x' },
    { id: '3', type_id: 'dep', from: 'bench', to: 'laser' },
    { id: '4', type_id: 'dep', from: 'x', to: 'supply' },
    { id: '5', type_id: 'dep', from: 'supply', to: 'breaker' },
  ],
  hidden: 0,
  loops: [],
  truncated: false,
};

describe('placing a graph', () => {
  it('puts each asset in its column, each column centered on the tallest', () => {
    const p = place(extractor);
    expect(p.width).toBe(2 * MARGIN + 5 * BOX_W + 4 * 72);
    expect(p.at.get('bench')).toEqual({ x: MARGIN, y: MARGIN + 0.5 * (BOX_H + 16) });
    expect(p.at.get('laser')!.y).toBe(MARGIN);
    expect(p.at.get('x')!.x).toBe(MARGIN + 2 * (BOX_W + 72));
    expect(BOX_H).toBeGreaterThanOrEqual(44);
  });

  it('draws links between columns, and bends those within one out to the right', () => {
    const mid = (BOX_W + 300) / 2;
    expect(linkPath({ x: 0, y: 0 }, { x: 300, y: 0 })).toBe(`M ${BOX_W} ${BOX_H / 2} C ${mid} ${BOX_H / 2}, ${mid} ${BOX_H / 2}, 300 ${BOX_H / 2}`);
    expect(linkPath({ x: 300, y: 0 }, { x: 0, y: 0 }).startsWith(`M 300 ${BOX_H / 2}`)).toBe(true);
    expect(linkPath({ x: 0, y: 0 }, { x: 0, y: 100 }).startsWith(`M ${BOX_W} `)).toBe(true);
  });
});

describe('the graph as a list', () => {
  it('lists what it needs and what needs it, by hops, with every asset the drawing has', () => {
    const parts = nested(extractor);
    expect(parts.map((p) => p.heading)).toEqual(['What Extractor needs', 'What needs Extractor']);
    expect(parts[0]!.items.map((i) => i.asset.name)).toEqual(['Supply']);
    expect(parts[0]!.items[0]!.children.map((i) => i.asset.name)).toEqual(['Breaker']);
    expect(parts[1]!.items.map((i) => i.asset.name)).toEqual(['Laser', 'Saw']);
    expect(parts[1]!.items[0]!.children.map((i) => i.asset.name)).toEqual(['Bench']);
    expect(listed(parts, extractor.root)).toEqual(new Set(extractor.assets.map((x) => x.id)));
  });

  it('lists a map by asset, with what each needs', () => {
    const map: AssetGraph = { ...extractor, root: undefined };
    const parts = nested(map);
    expect(parts).toHaveLength(1);
    expect(parts[0]!.items.map((i) => i.asset.name)).toEqual(['Bench', 'Laser', 'Saw', 'Extractor', 'Supply', 'Breaker']);
    expect(parts[0]!.items[3]!.children.map((i) => i.asset.name)).toEqual(['Supply']);
    expect(listed(parts)).toEqual(new Set(map.assets.map((x) => x.id)));
  });

  it('notes conditions, and what the graph leaves out', () => {
    expect(assetNote(extractor.assets[4]!)).toBe('Out of service');
    expect(assetNote(extractor.assets[5]!)).toBe('Archived, More not shown');
    expect(summary({ ...extractor, hidden: 2, truncated: true, truncated_at: 2, loops: [['x', 'supply']] })).toEqual([
      '2 assets you may not see are left out, and not followed through.',
      'A loop: Extractor → Supply, and round again. Following stops where it comes back.',
      'Cut short at 2 hops, at 6 assets.',
    ]);
    expect(summary({ ...extractor, assets: [extractor.assets[0]!], links: [] })).toEqual(['No followed links.']);
  });

  it('links the asset list filters to the Graph page', () => {
    expect(graphUrl('', '')).toBe('/assets/graph');
    expect(graphUrl(' drill ', 'c 1')).toBe('/assets/graph?category=c+1&search=drill');
  });
});
