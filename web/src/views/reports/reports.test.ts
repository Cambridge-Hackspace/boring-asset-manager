import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import ReportEditor from './ReportEditor.svelte';
import ReportView from './ReportView.svelte';
import ReportsPage from './ReportsPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { Report, ReportResult } from '../../lib/models';

const lowReport: Report = {
  id: 'low',
  name: 'Running low',
  kind: 'report',
  default: false,
  spec: {
    filter: { running_low: true },
    sections: [{ type: 'table', title: 'Shopping list', columns: [{ heading: 'Asset', binding: '{{asset.name}}' }, { heading: 'In stock', binding: '{{stock.quantity}} {{stock.unit}}' }], group_by: 'category' }],
  },
  created_at: '',
  updated_at: '',
};
const result: ReportResult = {
  assets: 1,
  sections: [
    {
      type: 'table',
      title: 'Shopping list',
      columns: ['Asset', 'In stock'],
      groups: [{ title: 'Filament', running_low: true, rows: [{ asset_id: 'a1', cells: ['Blue PLA', '200 g'] }] }],
    },
  ],
};
const categories = [{ id: 'fil', name: 'Filament', parent_id: null, color_hex: '#000000', is_consumable: true, unit: '', is_lendable: false, is_running_low: true, created_at: '', updated_at: '' }];

beforeEach(() => {
  session.permissions = ['assets.read', 'reports.manage'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('ReportView', () => {
  it('shows each table in groups, with its downloads and the assets linked', async () => {
    fakeApi({ 'GET /reports/low/run': () => ({ report: lowReport, result }) });
    render(ReportView, { params: { id: 'low' } });
    expect(await screen.findByRole('heading', { name: 'Running low', level: 1 })).toBeTruthy();
    const table = screen.getByRole('region', { name: 'Shopping list' });
    expect(within(table).getByRole('heading', { name: /Filament/ }).textContent).toContain('Running low');
    expect(within(table).getByRole('link', { name: 'Blue PLA' }).getAttribute('href')).toBe('/assets/a1');
    expect(within(table).getByRole('cell', { name: '200 g' })).toBeTruthy();
    expect(within(table).getByRole('link', { name: /Download CSV/ }).getAttribute('href')).toBe('/api/v1/reports/low/sections/1/csv');
    expect(within(table).getByRole('link', { name: /Download Excel/ }).getAttribute('href')).toBe('/api/v1/reports/low/sections/1/xlsx');
    expect(screen.getByRole('link', { name: 'Download all as Excel' }).getAttribute('href')).toBe('/api/v1/reports/low/xlsx');
    expect(screen.getByRole('link', { name: 'Edit' })).toBeTruthy();
  });
});

describe('ReportEditor', () => {
  it('edits a report, previews it, and saves only what was set', async () => {
    const { sent } = fakeApi({
      'GET /categories': () => ({ categories }),
      'GET /fields': () => ({ fields: [] }),
      'GET /generators': () => ({ generators: [] }),
      'GET /reports/low': () => ({ report: lowReport }),
      'POST /reports/preview': () => ({ result }),
      'PUT /reports/low': () => ({ report: lowReport }),
    });
    render(ReportEditor, { params: { id: 'low' } });
    expect(((await screen.findByLabelText('Name')) as HTMLInputElement).value).toBe('Running low');
    expect((screen.getByRole('checkbox', { name: /running low/ }) as HTMLInputElement).checked).toBe(true);

    // Its categories, chosen in the picker.
    await fireEvent.click(screen.getByRole('button', { name: 'Choose categories' }));
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Filament' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Done' }));
    expect(screen.getByRole('button', { name: 'Remove Filament' })).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Add column' }));
    await fireEvent.input(screen.getByLabelText('Heading of column 3'), { target: { value: 'Where' } });
    await fireEvent.input(screen.getByLabelText('What column 3 shows'), { target: { value: '{{asset.location}}' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Preview' }));
    expect(await screen.findByRole('heading', { name: 'Preview' })).toBeTruthy();

    await fireEvent.click(screen.getByRole('button', { name: 'Save report' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'PUT')).toBe(true));
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({
      name: 'Running low',
      spec: {
        filter: { category_ids: ['fil'], running_low: true },
        sections: [
          {
            type: 'table',
            title: 'Shopping list',
            group_by: 'category',
            columns: [
              { heading: 'Asset', binding: '{{asset.name}}' },
              { heading: 'In stock', binding: '{{stock.quantity}} {{stock.unit}}' },
              { heading: 'Where', binding: '{{asset.location}}' },
            ],
          },
        ],
      },
    });
  });
});

describe('ReportsPage', () => {
  it('lists reports, and copies one to change', async () => {
    const { sent } = fakeApi({
      'GET /reports': () => ({ reports: [lowReport] }),
      'POST /reports': (body) => ({ status: 201, body: { report: { ...lowReport, id: 'copy', ...(body as object) } } }),
    });
    render(ReportsPage, { params: {} });
    expect(await screen.findByRole('link', { name: 'Running low' })).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Copy Running low' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'POST')).toBe(true));
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ name: 'Running low (copy)', spec: lowReport.spec });
  });

  it('only lists them for people who cannot manage reports', async () => {
    session.permissions = ['assets.read'];
    fakeApi({ 'GET /reports': () => ({ reports: [lowReport] }) });
    render(ReportsPage, { params: {} });
    await screen.findByRole('link', { name: 'Running low' });
    expect(screen.queryByRole('link', { name: 'New report' })).toBeNull();
    expect(screen.queryByRole('button', { name: /Copy/ })).toBeNull();
  });
});

describe('ReportEditor, charts', () => {
  it('adds a chart and saves it as the API takes it', async () => {
    const { sent } = fakeApi({
      'GET /categories': () => ({ categories }),
      'GET /fields': () => ({ fields: [] }),
      'GET /generators': () => ({ generators: [] }),
      'POST /reports': () => ({ status: 201, body: { report: { ...lowReport, id: 'new' } } }),
    });
    render(ReportEditor, { params: {} });
    await fireEvent.input(await screen.findByLabelText('Name'), { target: { value: 'Use' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Add chart' }));
    await fireEvent.change(screen.getByLabelText('Chart'), { target: { value: 'line' } });
    await fireEvent.change(screen.getByLabelText('Per'), { target: { value: 'week' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Remove table 1' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Save report' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'POST')).toBe(true));
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({
      name: 'Use',
      spec: { filter: {}, sections: [{ type: 'chart', chart: 'line', measure: 'used', period: 'week' }] },
    });
  });
});

describe('ReportEditor, purchases', () => {
  it('filters by vendor, when bought and when the warranty ends', async () => {
    const { sent } = fakeApi({
      'GET /categories': () => ({ categories }),
      'GET /fields': () => ({ fields: [] }),
      'GET /generators': () => ({ generators: [] }),
      'GET /vendors': () => ({ vendors: [{ id: 'acme', name: 'Acme' }] }),
      'POST /reports': () => ({ status: 201, body: { report: { ...lowReport, id: 'new' } } }),
    });
    render(ReportEditor, { params: {} });
    await fireEvent.input(await screen.findByLabelText('Name'), { target: { value: 'Warranties' } });
    await vi.waitFor(() => expect(screen.getByRole('option', { name: 'Acme' })).toBeTruthy());
    await fireEvent.change(screen.getByLabelText('Bought from'), { target: { value: 'acme' } });
    await fireEvent.input(screen.getByLabelText('Bought on or after'), { target: { value: '2026-01-01' } });
    await fireEvent.input(screen.getByLabelText('Warranty ends before'), { target: { value: '2027-01-01' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save report' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'POST')).toBe(true));
    expect((sent.find((r) => r.method === 'POST')!.body as { spec: { filter: unknown } }).spec.filter).toEqual({
      vendor: 'acme',
      purchased_between: { from: '2026-01-01' },
      warranty_ends_before: '2027-01-01',
    });
  });
});

describe('ReportEditor, contracts', () => {
  it('filters by the contract covering assets', async () => {
    const { sent } = fakeApi({
      'GET /categories': () => ({ categories }),
      'GET /fields': () => ({ fields: [] }),
      'GET /generators': () => ({ generators: [] }),
      'GET /vendors': () => ({ vendors: [] }),
      'GET /contracts': () => ({ contracts: [{ id: 'k1', name: 'Laser upkeep', vendor_name: 'Acme' }] }),
      'POST /reports': () => ({ status: 201, body: { report: { ...lowReport, id: 'new' } } }),
    });
    render(ReportEditor, { params: {} });
    await fireEvent.input(await screen.findByLabelText('Name'), { target: { value: 'Covered' } });
    await vi.waitFor(() => expect(screen.getByRole('option', { name: 'Laser upkeep (Acme)' })).toBeTruthy());
    await fireEvent.change(screen.getByLabelText('Covered by contract'), { target: { value: 'k1' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save report' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'POST')).toBe(true));
    expect((sent.find((r) => r.method === 'POST')!.body as { spec: { filter: unknown } }).spec.filter).toEqual({ contract: 'k1' });
  });
});

describe('Reports by pool', () => {
  it('shows a pool\'s line with its total and threshold over its places', async () => {
    const byPool: ReportResult = {
      assets: 2,
      sections: [{
        type: 'table', title: 'Stock', columns: ['Place', 'Left'],
        groups: [{
          title: 'M5 bolts', running_low: false, pool: { category_id: 'bolts', total: 440, unit: 'pcs', low_stock_threshold: 200 },
          rows: [{ asset_id: 'a', cells: ['Drawer bin', '140'] }, { asset_id: 'b', cells: ['Bay bin', '300'] }],
        }],
      }],
    };
    fakeApi({ 'GET /reports/low/run': () => ({ report: lowReport, result: byPool }) });
    render(ReportView, { params: { id: 'low' } });
    const table = await screen.findByRole('region', { name: 'Stock' });
    expect(within(table).getByRole('heading', { name: 'M5 bolts: 440 pcs (threshold 200 pcs)' })).toBeTruthy();
    expect(within(table).getAllByRole('link').map((a) => a.textContent)).toContain('Bay bin');
  });

  it('chooses by pool or by bin for each table', async () => {
    const { sent } = fakeApi({
      'GET /categories': () => ({ categories }),
      'GET /fields': () => ({ fields: [] }),
      'GET /generators': () => ({ generators: [] }),
      'GET /reports/low': () => ({ report: lowReport }),
      'PUT /reports/low': () => ({ report: lowReport }),
    });
    render(ReportEditor, { params: { id: 'low' } });
    const group = (await screen.findByLabelText('Group')) as HTMLSelectElement;
    expect([...group.options].map((o) => o.value)).toEqual(['', 'category', 'pool']);
    await fireEvent.change(group, { target: { value: 'pool' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save report' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'PUT')).toBe(true));
    const spec = (sent.find((r) => r.method === 'PUT')!.body as { spec: { sections: { group_by?: string }[] } }).spec;
    expect(spec.sections[0]!.group_by).toBe('pool');
  });
});
