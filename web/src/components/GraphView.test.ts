import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import GraphView from './GraphView.svelte';
import { fakeApi } from '../test/fetch';
import type { AssetGraph, RelationType } from '../lib/models';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const dep = { id: 'dep', name: 'depends on', inverse_name: 'needed by', is_followed: true } as RelationType;
const powered = { id: 'pow', name: 'powered by', inverse_name: 'powers', is_followed: true } as RelationType;
const graph: AssetGraph = {
  root: 'x',
  types: [dep, powered],
  assets: [
    { id: 'x', name: 'Extractor', categories: [], icon: null, condition: 'ok', hops: 0, side: 'root', layer: 1, row: 0 },
    { id: 'laser', name: 'Laser cutter', categories: [], icon: null, condition: 'ok', hops: 1, side: 'in', layer: 0, row: 0 },
    { id: 'supply', name: 'Three-phase', categories: [], icon: null, condition: 'out_of_service', hops: 1, side: 'out', layer: 2, row: 0 },
  ],
  links: [
    { id: '1', type_id: 'dep', from: 'laser', to: 'x' },
    { id: '2', type_id: 'dep', from: 'x', to: 'supply' },
  ],
  hidden: 1,
  loops: [],
  truncated: false,
};

describe('GraphView', () => {
  it('draws and lists the same assets, and redraws as asked', async () => {
    const { fetchMock } = fakeApi({
      'GET /relation-types': () => ({ relation_types: [dep, powered, { id: 'c', name: 'contains', is_followed: false }] }),
      'GET /assets/x/graph': () => ({ graph }),
    });
    const { container } = render(GraphView, { assetId: 'x' });
    const drawing = await screen.findByRole('region', { name: 'Drawing' });
    const drawn = [...drawing.querySelectorAll('[data-graph-asset]')].map((e) => e.getAttribute('data-graph-asset'));
    const list = [...container.querySelectorAll('[data-graph-listed]')].map((e) => e.getAttribute('data-graph-listed'));
    expect(new Set(drawn)).toEqual(new Set(['x', 'laser', 'supply']));
    expect(new Set([...list, 'x'])).toEqual(new Set(drawn));
    // Each box opens its asset's graph, and says its condition and hops.
    const supply = within(drawing).getByRole('link', { name: 'Three-phase, Out of service, 1 hop' });
    expect(supply.getAttribute('href')).toBe('/assets/supply?tab=graph');
    expect(screen.getByText('1 asset you may not see is left out, and not followed through.')).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'What Extractor needs' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Drawing as PDF' }).getAttribute('href')).toBe('/api/v1/assets/x/graph.pdf');

    // Only the followed types are offered; one left out, and fewer hops, one way.
    expect(screen.queryByRole('checkbox', { name: 'contains' })).toBeNull();
    await fireEvent.click(screen.getByRole('checkbox', { name: 'powered by' }));
    await fireEvent.change(screen.getByLabelText('Which way'), { target: { value: 'in' } });
    await fireEvent.change(screen.getByLabelText('Hops'), { target: { value: '2' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Draw' }));
    await vi.waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThan(2));
    expect(String(fetchMock.mock.calls.at(-1)![0])).toBe('/api/v1/assets/x/graph?types=dep&direction=in&depth=2');
    expect(screen.getByRole('link', { name: 'Drawing as SVG' }).getAttribute('href')).toBe('/api/v1/assets/x/graph.svg?types=dep&direction=in&depth=2');
  });

  it('maps a category, with its filters', async () => {
    const map: AssetGraph = { ...graph, root: undefined, hidden: 0 };
    const { fetchMock } = fakeApi({
      'GET /relation-types': () => ({ relation_types: [dep] }),
      'GET /assets/graph': () => ({ graph: map }),
    });
    render(GraphView, { query: { category_id: 'tools', search: '' } });
    await screen.findByRole('region', { name: 'Drawing' });
    expect(String(fetchMock.mock.calls.at(-1)![0])).toBe('/api/v1/assets/graph?category_id=tools');
    expect(screen.queryByLabelText('Hops')).toBeNull();
    expect(screen.getByRole('heading', { name: 'Every asset, with what it needs' })).toBeTruthy();
  });
});
