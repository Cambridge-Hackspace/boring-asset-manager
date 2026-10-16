import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import ImportPage from './ImportPage.svelte';
import { fakeApi } from '../../test/fetch';
import type { ImportPlan } from '../../lib/models';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const plan = (over: Partial<ImportPlan> = {}): ImportPlan => ({
  columns: [
    { header: 'Name', suggested: 'name', target: 'name', sample: 'Saw' },
    { header: 'Colour', suggested: '', target: '', sample: 'red' },
  ],
  rows: 2,
  create: 0,
  update: 0,
  unchanged: 0,
  problems: [{ row: 1, column: 'Colour', message: 'column "Colour" is not mapped; choose where it goes or ignore it' }],
  problem_count: 1,
  unknown_categories: [],
  links_added: 0,
  links_moved: 0,
  links_removed: 0,
  link_changes: [],
  remove_links: false,
  ...over,
});

function mappingOf(body: unknown): unknown {
  const raw = (body as FormData).get('mapping');
  return raw === null ? null : JSON.parse(raw as string);
}

async function chooseFile() {
  const input = screen.getByLabelText('CSV file') as HTMLInputElement;
  const file = new File(['Name,Colour\nSaw,red\nGlue,blue\n'], 'assets.csv', { type: 'text/csv' });
  await fireEvent.change(input, { target: { files: [file] } });
}

describe('ImportPage', () => {
  it('suggests a mapping, rechecks after changes and imports only a clean plan', async () => {
    const previews: unknown[] = [];
    const { sent } = fakeApi({
      'GET /fields': () => ({ fields: [{ id: 'f1', key: 'color', label: 'Color', validation_type: 'text', created_at: '', updated_at: '' }] }),
      'GET /generators': () => ({ generators: [{ type: 'qr', name: 'QR Code', error_correction_levels: [], code_rules: '' }] }),
      'POST /assets/import/preview': (body) => {
        const mapping = mappingOf(body);
        previews.push(mapping);
        if (mapping === null) return plan();
        return plan({
          columns: plan().columns.map((c, i) => ({ ...c, target: (mapping as string[])[i]! })),
          create: 2,
          problems: [],
          problem_count: 0,
        });
      },
      'POST /assets/import': () => ({ created: 2, updated: 0, unchanged: 0, created_ids: ['a', 'b'] }),
    });
    render(ImportPage, { params: {} });
    await chooseFile();

    expect(await screen.findByText(/Header, Colour: column "Colour" is not mapped/)).toBeTruthy();
    const importButton = screen.getByRole('button', { name: /^Import/ }) as HTMLButtonElement;
    expect(importButton.disabled).toBe(true);
    expect((screen.getByLabelText('Where Name goes') as HTMLSelectElement).value).toBe('name');
    expect(screen.getByText('Saw')).toBeTruthy();

    // Mapping the column means checking again before importing.
    await fireEvent.change(screen.getByLabelText('Where Colour goes'), { target: { value: 'field.color' } });
    expect(screen.queryByRole('button', { name: /^Import/ })).toBeNull();
    await fireEvent.click(screen.getByRole('button', { name: 'Check again' }));
    expect(previews.at(-1)).toEqual(['name', 'field.color']);
    expect(await screen.findByText('2 rows: 2 new, 0 updated, 0 unchanged.')).toBeTruthy();

    await fireEvent.click(screen.getByRole('button', { name: 'Import 2 assets' }));
    expect(await screen.findByText('Imported: 2 new, 0 updated, 0 unchanged.')).toBeTruthy();
    const imported = sent.find((r) => r.path === '/assets/import')!;
    expect(mappingOf(imported.body)).toEqual(['name', 'field.color']);
    expect((imported.body as FormData).get('file')).toBeInstanceOf(File);
  });

  it('warns about two columns going to the same place', async () => {
    fakeApi({
      'GET /fields': () => ({ fields: [] }),
      'GET /generators': () => ({ generators: [] }),
      'POST /assets/import/preview': () => plan(),
    });
    render(ImportPage, { params: {} });
    await chooseFile();
    await screen.findByLabelText('Where Colour goes');
    await fireEvent.change(screen.getByLabelText('Where Colour goes'), { target: { value: 'name' } });
    expect(screen.getAllByText('Another column goes here too.')).toHaveLength(2);
  });

  it('shows the server findings when the import is refused', async () => {
    const clean = plan({ columns: [plan().columns[0]!], create: 1, problems: [], problem_count: 0 });
    fakeApi({
      'GET /fields': () => ({ fields: [] }),
      'GET /generators': () => ({ generators: [] }),
      'POST /assets/import/preview': () => clean,
      'POST /assets/import': () => ({
        status: 422,
        body: {
          error: 'The file has 1 problems; nothing was imported',
          details: { ...clean, create: 0, problem_count: 1, unknown_categories: ['Garden'], problems: [{ row: 2, column: 'Name', message: 'unknown category "Garden"' }] },
        },
      }),
    });
    render(ImportPage, { params: {} });
    await chooseFile();
    await fireEvent.click(await screen.findByRole('button', { name: 'Import 1 asset' }));
    expect(await screen.findByText(/nothing was imported/)).toBeTruthy();
    expect(screen.getByText(/These categories do not exist yet.*Garden/)).toBeTruthy();
    expect(screen.getByText('Line 2, Name: unknown category "Garden"')).toBeTruthy();
  });

  it('reports unreadable files', async () => {
    fakeApi({
      'GET /fields': () => ({ fields: [] }),
      'GET /generators': () => ({ generators: [] }),
      'POST /assets/import/preview': () => ({ status: 400, body: { error: 'unreadable file: the file is empty' } }),
    });
    render(ImportPage, { params: {} });
    await chooseFile();
    expect(await screen.findByText('unreadable file: the file is empty')).toBeTruthy();
    expect(screen.queryByText('Columns')).toBeNull();
  });
});
