import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import PdfDownload from './PdfDownload.svelte';
import DocumentSettings from '../views/packages/DocumentSettings.svelte';
import { fakeApi } from '../test/fetch';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

async function openIt() {
  const details = screen.getByText('PDF…').closest('details')!;
  details.open = true;
  await fireEvent(details, new Event('toggle'));
}

const linkQuery = () => new URL(screen.getByRole('link', { name: 'Download PDF' }).getAttribute('href')!, 'http://x').searchParams;

describe('PdfDownload', () => {
  it('asks nothing until opened, then starts from the instance’s page size', async () => {
    const { sent } = fakeApi({ 'GET /settings/documents': () => ({ instance_name: 'Shop', page_size: 'a4' }) });
    render(PdfDownload, { base: '/api/v1/reports/low/export.pdf' });
    expect(sent).toHaveLength(0);
    await openIt();
    await vi.waitFor(() => expect(linkQuery().get('page_size')).toBe('a4'));
    expect(screen.getByRole('link', { name: 'Download PDF' }).getAttribute('href')).toMatch(/^\/api\/v1\/reports\/low\/export\.pdf\?/);
    expect(screen.queryByRole('group', { name: 'Columns' })).toBeNull();

    await fireEvent.change(screen.getByLabelText('Orientation'), { target: { value: 'landscape' } });
    await fireEvent.click(screen.getByLabelText("Each asset's photo"));
    await fireEvent.click(screen.getByLabelText("Each asset's label"));
    expect(linkQuery().get('orientation')).toBe('landscape');
    expect(linkQuery().get('photos')).toBe('1');
    expect(linkQuery().get('labels')).toBe('1');
  });

  it('chooses the asset list’s columns, fields among them, with its filters', async () => {
    fakeApi({
      'GET /settings/documents': () => ({ instance_name: 'Shop', page_size: 'letter' }),
      'GET /fields': () => ({ fields: [{ id: 'f', key: 'serial', label: 'Serial number' }] }),
    });
    render(PdfDownload, { base: '/api/v1/assets/export.pdf', query: { search: 'drill', category_id: '' }, assetColumns: true });
    await openIt();
    await fireEvent.click(await screen.findByLabelText('Serial number'));
    await fireEvent.click(screen.getByLabelText('Quantity'));
    const q = linkQuery();
    expect(q.get('columns')).toBe('name,categories,field.serial');
    expect(q.get('search')).toBe('drill');
    expect(q.has('category_id')).toBe(false);
  });
});

describe('DocumentSettings', () => {
  it('saves the name on every page and the page size', async () => {
    const { sent } = fakeApi({
      'GET /settings/documents': () => ({ instance_name: 'BAM!', page_size: 'letter' }),
      'PUT /settings/documents': (body) => body,
    });
    render(DocumentSettings);
    const name = (await screen.findByLabelText('Heading on every page')) as HTMLInputElement;
    await vi.waitFor(() => expect(name.value).toBe('BAM!'));
    await fireEvent.input(name, { target: { value: 'Cambridge Hackspace' } });
    await fireEvent.change(screen.getByLabelText('Page size'), { target: { value: 'a4' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('Saved.')).toBeTruthy();
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({ instance_name: 'Cambridge Hackspace', page_size: 'a4' });
  });
});
