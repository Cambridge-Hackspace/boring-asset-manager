import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import ImportPage from './ImportPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { ImportPlan } from '../../lib/models';

afterEach(() => {
  cleanup();
  localStorage.clear();
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
  unresolved: 0,
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

async function chooseFiles(...names: string[]) {
  const input = screen.getByLabelText('CSV file') as HTMLInputElement;
  const files = names.map((n) => new File([`Name\n${n}\n`], n, { type: 'text/csv' }));
  await fireEvent.change(input, { target: { files } });
}

const fileOf = (body: unknown) => ((body as FormData).get('file') as File).name;
const section = (name: string) => screen.getByRole('region', { name: new RegExp(name.replace('.', '\\.')) });
const clean = (over: Partial<ImportPlan> = {}) => plan({ columns: [plan().columns[0]!], create: 1, problems: [], problem_count: 0, ...over });
const empty = { 'GET /fields': () => ({ fields: [] }), 'GET /generators': () => ({ generators: [] }) };

describe('ImportPage', () => {
  it('suggests a mapping, rechecks after changes and imports only a clean plan', async () => {
    const previews: unknown[] = [];
    const { sent } = fakeApi({
      'GET /fields': () => ({ fields: [{ id: 'f1', key: 'color', label: 'Color', validation_type: 'text', created_at: '', updated_at: '' }] }),
      'GET /generators': () => ({ generators: [{ type: 'qr', name: 'QR Code', error_correction_levels: [], code_rules: '', printable: true }] }),
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

  it('adds the new assets’ labels to the print queue, for those who print them', async () => {
    session.permissions = ['assets.read', 'assets.write', 'labels.print'];
    session.user = { id: 'ada', email: 'ada@hackspace.org', name: 'Ada', is_active: true, created_at: '', updated_at: '' };
    localStorage.clear();
    const { sent } = fakeApi({
      'GET /fields': () => ({ fields: [] }),
      'GET /generators': () => ({ generators: [] }),
      'POST /assets/import/preview': () => plan({ columns: plan().columns.map((c, i) => ({ ...c, target: i === 0 ? 'name' : 'ignore' })), create: 2, problems: [], problem_count: 0 }),
      'POST /assets/import': () => ({ created: 2, updated: 0, unchanged: 0, created_ids: ['a', 'b'] }),
      'POST /spool': () => ({ status: 201, body: { added: [{ id: 'e1' }, { id: 'e2' }], already_waiting: [], skipped: [] } }),
    });
    render(ImportPage, { params: {} });
    await chooseFile();
    const box = (await screen.findByRole('checkbox', { name: 'Add the new assets’ labels to the print queue' })) as HTMLInputElement;
    expect(box.checked).toBe(true);
    await fireEvent.click(screen.getByRole('button', { name: 'Import 2 assets' }));
    expect(await screen.findByText('Added 2 labels to the print queue.')).toBeTruthy();
    expect(sent.find((r) => r.path === '/spool')!.body).toEqual({ asset_ids: ['a', 'b'] });
    session.permissions = [];
  });

  it('previews several files apart and imports them in the order listed', async () => {
    const { sent } = fakeApi({
      ...empty,
      'POST /assets/import/preview': (body) => (fileOf(body) === 'bins.csv' ? clean({ create: 3 }) : clean()),
      'POST /assets/import': (body) => ({ created: fileOf(body) === 'bins.csv' ? 3 : 1, updated: 0, unchanged: 0 }),
    });
    render(ImportPage, { params: {} });
    await chooseFiles('tools.csv', 'bins.csv');

    expect(await within(section('bins.csv')).findByText('2 rows: 3 new, 0 updated, 0 unchanged.')).toBeTruthy();
    expect(within(section('tools.csv')).getByText('2 rows: 1 new, 0 updated, 0 unchanged.')).toBeTruthy();
    // Put the bins first.
    await fireEvent.click(screen.getByRole('button', { name: 'Move bins.csv up' }));
    expect(screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent?.replace(/\s+/g, ' ').trim())).toEqual(['1. bins.csv', '2. tools.csv']);

    await fireEvent.click(screen.getByRole('button', { name: 'Import 2 files in order' }));
    expect(await screen.findByText('Imported all 2 files.')).toBeTruthy();
    expect(sent.filter((r) => r.path === '/assets/import').map((r) => fileOf(r.body))).toEqual(['bins.csv', 'tools.csv']);
    expect(within(section('bins.csv')).getByText('Imported: 3 new, 0 updated, 0 unchanged.')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Back to the assets' })).toBeTruthy();
  });

  it('stops at a refused file, and says which were imported', async () => {
    const { sent } = fakeApi({
      ...empty,
      'POST /assets/import/preview': () => clean(),
      'POST /assets/import': (body) =>
        fileOf(body) === 'b.csv'
          ? { status: 422, body: { error: 'The file has 1 problems; nothing was imported', details: clean({ create: 0, problem_count: 1, problems: [{ row: 2, column: 'Name', message: 'name is required' }] }) } }
          : { created: 1, updated: 0, unchanged: 0 },
    });
    render(ImportPage, { params: {} });
    await chooseFiles('a.csv', 'b.csv', 'c.csv');
    await fireEvent.click(await screen.findByRole('button', { name: 'Import 3 files in order' }));

    expect(await screen.findByText('Imported a.csv. b.csv was refused, so c.csv was not imported.')).toBeTruthy();
    expect(sent.filter((r) => r.path === '/assets/import').map((r) => fileOf(r.body))).toEqual(['a.csv', 'b.csv']);
    expect(within(section('a.csv')).getByText(/^Imported: 1 new/)).toBeTruthy();
    expect(within(section('b.csv')).getByText('Line 2, Name: name is required')).toBeTruthy();
    expect(within(section('c.csv')).getByText('Not imported, because a file before it failed.')).toBeTruthy();
  });

  it('lets a file wait on assets an earlier one creates, but not the first', async () => {
    const waiting = clean({ create: 0, update: 1, problem_count: 1, unresolved: 1, problems: [{ row: 2, column: 'Links', message: 'no asset is "Bin A3"', ref: 'Bin A3' }] });
    fakeApi({
      ...empty,
      'POST /assets/import/preview': (body) => (fileOf(body) === 'links.csv' ? waiting : clean()),
      'POST /assets/import': () => ({ created: 1, updated: 0, unchanged: 0 }),
    });
    render(ImportPage, { params: {} });
    await chooseFiles('links.csv', 'bins.csv');
    const links = await within(section('links.csv')).findByText(/A link names an asset that does not exist yet/);
    expect(links.textContent).toMatch(/add it and put it before this one/);
    // Once both are checked, the first must be ready.
    expect(((await screen.findByRole('button', { name: 'Import 2 files in order' })) as HTMLButtonElement).disabled).toBe(true);

    await fireEvent.click(screen.getByRole('button', { name: 'Move links.csv down' }));
    expect(within(section('links.csv')).getByText(/A link names an asset/).textContent).toMatch(/If a file\s+before this one creates them/);
    expect((screen.getByRole('button', { name: 'Import 2 files in order' }) as HTMLButtonElement).disabled).toBe(false);
  });

  it('starts a file from the mapping last used for the same headers', async () => {
    session.user = { id: 'ada', email: 'ada@hackspace.org', name: 'Ada', is_active: true, created_at: '', updated_at: '' };
    localStorage.clear();
    const previews: unknown[] = [];
    fakeApi({
      ...empty,
      'POST /assets/import/preview': (body) => {
        const mapping = mappingOf(body);
        previews.push(mapping);
        const targets = (mapping as string[] | null) ?? ['name', ''];
        return plan({ columns: plan().columns.map((c, i) => ({ ...c, target: targets[i]! })), create: 2, problems: [], problem_count: mapping ? 0 : 1 });
      },
      'POST /assets/import': () => ({ created: 2, updated: 0, unchanged: 0 }),
    });
    render(ImportPage, { params: {} });
    await chooseFile();
    await fireEvent.change(await screen.findByLabelText('Where Colour goes'), { target: { value: 'ignore' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Check again' }));
    await fireEvent.click(await screen.findByRole('button', { name: 'Import 2 assets' }));
    await screen.findByText(/^Imported: 2 new/);

    // The next file with the same headers starts from it.
    await chooseFile();
    expect(await screen.findByText('Started from the mapping you last used for these columns.')).toBeTruthy();
    expect((screen.getByLabelText('Where Colour goes') as HTMLSelectElement).value).toBe('ignore');
    expect(previews.slice(-2)).toEqual([null, ['name', 'ignore']]);
    // And can go back to the suggested one.
    await fireEvent.click(screen.getByRole('button', { name: 'Use the suggested mapping' }));
    expect(previews.at(-1)).toEqual(['name', '']);
    session.user = null;
  });

  it('makes an import of each sheet of a workbook, and refuses other spreadsheets', async () => {
    const { sent } = fakeApi({
      ...empty,
      'POST /assets/import/preview': (body) => {
        const form = body as FormData;
        if (!form.get('sheet')) return { sheets: ['Tools', 'Bins'] };
        return clean({ create: form.get('sheet') === 'Bins' ? 3 : 1 });
      },
      'POST /assets/import': (body) => ({ created: (body as FormData).get('sheet') === 'Bins' ? 3 : 1, updated: 0, unchanged: 0 }),
    });
    render(ImportPage, { params: {} });
    const input = screen.getByLabelText('CSV file') as HTMLInputElement;
    expect(input.accept).toContain('.xlsx');
    const book = new File(['PK'], 'Stock.xlsx', { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' });
    const old = new File(['x'], 'Old.xls');
    await fireEvent.change(input, { target: { files: [book, old] } });

    // The sheets are listed once the workbook is read.
    const bins = await screen.findByRole('region', { name: /Stock › Bins/ });
    expect(await within(bins).findByText('2 rows: 3 new, 0 updated, 0 unchanged.')).toBeTruthy();
    expect(within(section('Stock › Tools')).getByText('2 rows: 1 new, 0 updated, 0 unchanged.')).toBeTruthy();
    expect(screen.getByText('Old.xls is not taken: save it as an Excel workbook (.xlsx) or CSV.')).toBeTruthy();
    // A sheet can be left out.
    await fireEvent.click(screen.getByRole('button', { name: 'Remove Stock › Tools' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Import 3 assets' }));
    await screen.findByText(/^Imported: 3 new/);
    const imports = sent.filter((r) => r.path === '/assets/import');
    expect(imports.map((r) => (r.body as FormData).get('sheet'))).toEqual(['Bins']);
  });

  it('takes at most 20 files at once', async () => {
    fakeApi({ ...empty, 'POST /assets/import/preview': () => clean() });
    render(ImportPage, { params: {} });
    await chooseFiles(...Array.from({ length: 22 }, (_, i) => `f${i + 1}.csv`));
    expect(await screen.findByText('At most 20 files or sheets are imported together; 2 were left out.')).toBeTruthy();
    expect(screen.getAllByRole('heading', { level: 2 })).toHaveLength(20);
  });
});
