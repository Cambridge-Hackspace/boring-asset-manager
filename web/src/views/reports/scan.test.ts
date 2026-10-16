import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import ScanTemplateEditor from './ScanTemplateEditor.svelte';
import ReportsPage from './ReportsPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { Report, ReportResult } from '../../lib/models';

const template = (id: string, name: string, isDefault = false): Report => ({
  id,
  name,
  kind: 'scan',
  default: isDefault,
  spec: { filter: {}, sections: [{ type: 'actions', columns: [], actions: ['open'] }] },
  created_at: '',
  updated_at: '',
});

beforeEach(() => {
  session.permissions = ['assets.read', 'reports.manage'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('ScanTemplateEditor', () => {
  it('builds a template, previews it with an asset, and saves it as a scan template', async () => {
    const preview: ReportResult = {
      assets: 1,
      sections: [
        { type: 'details', title: '', columns: ['Name', 'Where'], groups: [{ title: '', rows: [{ asset_id: 'b', cells: ['Bin A3', 'Shelf 2'] }] }] },
        { type: 'table', title: 'In it', columns: ['What'], groups: [{ title: '', rows: [{ asset_id: 's', cells: ['Screws'] }] }] },
        { type: 'actions', title: '', actions: ['open', 'use'] },
      ],
    };
    const { sent } = fakeApi({
      'GET /fields': () => ({ fields: [] }),
      'GET /generators': () => ({ generators: [] }),
      'GET /assets': () => ({ assets: [{ asset: { id: 'b', name: 'Bin A3' } }] }),
      'POST /reports/preview': () => ({ result: preview }),
      'POST /reports': () => ({ status: 201, body: { report: template('new', 'Bins') } }),
    });
    render(ScanTemplateEditor, { params: {} });
    await fireEvent.input(await screen.findByLabelText('Name'), { target: { value: 'Bins' } });
    const actions = screen.getByRole('group', { name: 'Actions 3' });
    await fireEvent.click(within(actions).getByRole('checkbox', { name: 'Use some' }));

    await fireEvent.input(screen.getByLabelText('Preview with'), { target: { value: 'bin' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Find' }));
    await fireEvent.click(await screen.findByRole('button', { name: 'Preview' }));
    expect(await screen.findByText('Shelf 2')).toBeTruthy();
    expect(screen.getByText('Screws')).toBeTruthy();
    expect(screen.getByText('Actions: Open its page, Use some')).toBeTruthy();
    expect((sent.find((r) => r.path === '/reports/preview')!.body as { asset_id: string; kind: string })).toMatchObject({ asset_id: 'b', kind: 'scan' });

    await fireEvent.click(screen.getByRole('button', { name: 'Save scan template' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'POST' && r.path === '/reports')).toBe(true));
    const body = sent.find((r) => r.method === 'POST' && r.path === '/reports')!.body as { kind: string; spec: { sections: { actions?: string[] }[] } };
    expect(body.kind).toBe('scan');
    expect(body.spec.sections[2]!.actions).toEqual(['open', 'use']);
  });
});

describe('ReportsPage scan templates', () => {
  it('lists them apart, and makes one the default', async () => {
    let all = [template('d', 'Scanned', true), template('bins', 'Bins')];
    const { sent } = fakeApi({
      'GET /reports': () => ({ reports: all }),
      'POST /reports/bins/default': () => {
        all = [template('d', 'Scanned'), template('bins', 'Bins', true)];
        return { report: all[1] };
      },
    });
    render(ReportsPage, { params: {} });
    const section = (await screen.findByRole('heading', { name: 'Scan templates' })).closest('section')!;
    expect(screen.getByText('No reports yet.')).toBeTruthy();
    expect(within(section).getByText('(default)').parentElement!.textContent).toContain('Scanned');
    // The default cannot be deleted, nor made the default again.
    const scanned = within(section).getByText('Scanned').closest('li')!;
    expect(within(scanned).queryByRole('button', { name: 'Delete' })).toBeNull();
    await fireEvent.click(within(section).getByRole('button', { name: 'Make Bins the default' }));
    await screen.findByText('Bins is now the default scan template.');
    expect(sent.some((r) => r.path === '/reports/bins/default')).toBe(true);
  });
});
