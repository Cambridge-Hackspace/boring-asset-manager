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
  it('shows each table in groups, with its CSV and the assets linked', async () => {
    fakeApi({ 'GET /reports/low/run': () => ({ report: lowReport, result }) });
    render(ReportView, { params: { id: 'low' } });
    expect(await screen.findByRole('heading', { name: 'Running low', level: 1 })).toBeTruthy();
    const table = screen.getByRole('region', { name: 'Shopping list' });
    expect(within(table).getByRole('heading', { name: /Filament/ }).textContent).toContain('Running low');
    expect(within(table).getByRole('link', { name: 'Blue PLA' }).getAttribute('href')).toBe('/assets/a1');
    expect(within(table).getByRole('cell', { name: '200 g' })).toBeTruthy();
    expect(within(table).getByRole('link', { name: /Download CSV/ }).getAttribute('href')).toBe('/api/v1/reports/low/sections/1/csv');
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

    await fireEvent.click(screen.getByRole('checkbox', { name: 'Filament' }));
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
