import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import ImportSection from './ImportSection.svelte';
import { fakeApi } from '../../test/fetch';
import type { ImportPlan } from '../../lib/packages';

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  localStorage.clear();
});

const tree = [{ id: 'lab', name: 'Lab', children: [{ id: 'bench', name: 'Bench', children: [] }] }];

function planWith(over: Partial<ImportPlan> = {}): ImportPlan {
  return {
    header: { type: 'package', created_at: '2026-10-30T00:00:00Z', kinds: ['catalog', 'assets'], package: { name: 'Workshop', version: '1.0', author: 'Hackspace' } },
    kinds: ['catalog', 'assets'],
    anchor: null,
    items: [
      { kind: 'category', id: 'c1', name: 'Tools', action: 'create' },
      {
        kind: 'field',
        id: 'f1',
        name: 'Serial (serial)',
        action: 'copy',
        as: 'serial_2',
        existing: { kind: 'field', id: 'here', name: 'Serial number (serial)' },
        identical: false,
        choices: ['existing', 'copy', 'skip'],
      },
    ],
    assets: { assets: 2, photos: 1, links: 0, events: 0, values_dropped: 0, links_dropped: 0, codes_taken: 0 },
    files: 1,
    file_bytes: 2048,
    permissions: ['categories.manage', 'assets.write'],
    missing: [],
    warnings: [],
    problems: [],
    fingerprint: 'fp1',
    ...over,
  };
}

function server(plan: ImportPlan, imported: ImportPlan = plan) {
  return fakeApi({
    'POST /uploads': () => ({ status: 201, body: { id: 'u1', size: 7, part_size: 1 << 20, received: 0, complete: false } }),
    'PUT /uploads/u1': () => ({ id: 'u1', size: 7, part_size: 1 << 20, received: 7, complete: true }),
    'POST /import/check': () => ({ status: 202, body: { id: 'j1', done: false } }),
    'GET /import/jobs/j1': () => ({ id: 'j1', done: true, result: { plan } }),
    'POST /import': () => ({ status: 202, body: { id: 'j2', done: false } }),
    'GET /import/jobs/j2': () => ({ id: 'j2', done: true, result: { plan: imported } }),
  });
}

async function choosePackage() {
  await fireEvent.change(screen.getByTestId('archive-file'), { target: { files: [new File(['archive'], 'workshop.bam.tar.gz')] } });
  await waitFor(() => expect(screen.getByTestId('import-plan')).toBeTruthy());
}

describe('ImportSection', () => {
  it('checks an upload, offers a choice for what clashes, checks again with it, and imports as checked', async () => {
    const { sent } = server(planWith(), planWith({ anchor: { kind: 'category', id: 'bench', name: 'Bench' } }));
    const onimported = vi.fn();
    render(ImportSection, { tree, onimported });
    await choosePackage();
    expect(screen.getByTestId('import-plan').textContent).toMatch(/the package Workshop 1\.0, by Hackspace/);
    expect(screen.getByTestId('import-plan').textContent).toMatch(/creates 2 things \(1 of them copies\), and brings 2 assets with 1 photo/);

    // what clashes is chosen for, the default chosen
    const choice = screen.getByRole('combobox', { name: 'What to do with the field Serial (serial)' }) as HTMLSelectElement;
    expect(choice.value).toBe('copy');
    expect([...choice.options].map((o) => o.textContent)).toEqual(['Use the one here', 'Import as a copy', 'Skip']);
    expect((screen.getByTestId('import') as HTMLButtonElement).disabled).toBe(false);

    // changing what is asked checks again before importing
    await fireEvent.change(choice, { target: { value: 'existing' } });
    await fireEvent.change(screen.getByTestId('anchor'), { target: { value: 'bench' } });
    expect(screen.queryByTestId('import')).toBeNull();
    await fireEvent.click(screen.getByTestId('check-again'));
    await waitFor(() => expect(screen.getByTestId('import')).toBeTruthy());
    expect(sent.filter((r) => r.path === '/import/check').at(-1)!.body).toEqual({ upload_id: 'u1', anchor: 'bench', choices: { 'field:f1': 'existing' } });

    await fireEvent.click(screen.getByTestId('import'));
    await waitFor(() => expect(screen.getByTestId('imported')).toBeTruthy());
    expect(sent.find((r) => r.method === 'POST' && r.path === '/import')!.body).toEqual({
      upload_id: 'u1',
      anchor: 'bench',
      choices: { 'field:f1': 'existing' },
      fingerprint: 'fp1',
    });
    expect(screen.getByTestId('imported').textContent).toMatch(/Imported the package Workshop 1\.0 under Bench/);
    expect(onimported).toHaveBeenCalled();
  });

  it('says what stops an import, and does not offer it', async () => {
    server(planWith({ missing: ['flows.manage'], problems: ['flow "Lend" presses the action "open", which is skipped'] }));
    render(ImportSection, { tree });
    await choosePackage();
    expect(screen.getByTestId('import-missing').textContent).toMatch(/permissions you do not have: flows\.manage/);
    expect(screen.getByTestId('import-problems').textContent).toMatch(/which is skipped/);
    expect((screen.getByTestId('import') as HTMLButtonElement).disabled).toBe(true);
  });

  it('imports an icon set, replacing what changed, with no category to choose', async () => {
    const icons = planWith({
      header: { type: 'icons', created_at: '', kinds: ['icons'], package: { name: 'My shapes' } },
      kinds: ['icons'],
      items: [
        { kind: 'icon_library_file', id: 'shared/crates.ilx', name: 'shared/crates.ilx', action: 'replace', existing: { kind: 'icon_library_file', id: 'l1', name: 'shared/crates.ilx' } },
        { kind: 'palette_token', id: 'sky', name: 'sky', action: 'create' },
      ],
      assets: undefined,
      files: 0,
    });
    const { sent } = server(icons);
    render(ImportSection, { tree });
    await choosePackage();
    expect(screen.queryByTestId('anchor')).toBeNull();
    expect(screen.getByTestId('import-plan').textContent).toMatch(/the icon set My shapes:\s+It creates 1 thing, replaces 1\./);
    expect(screen.getByTestId('import-items').textContent).toMatch(/shared\/crates\.ilx, an icon library file: Replaces the one here/);
    await fireEvent.click(screen.getByTestId('import'));
    await waitFor(() => expect(screen.getByTestId('imported')).toBeTruthy());
    expect(sent.find((r) => r.method === 'POST' && r.path === '/import')!.body).toEqual({ upload_id: 'u1', anchor: '', choices: {}, fingerprint: 'fp1' });
  });

  it('merges the kinds chosen of a backup', async () => {
    const backup = planWith({ header: { type: 'backup', created_at: '2026-10-30T00:00:00Z', kinds: ['catalog', 'assets', 'people', 'auth'] }, warnings: ['People and roles are an instance’s own.'] });
    const { sent } = server(backup);
    render(ImportSection, { tree });
    await choosePackage();
    expect(screen.getByText(/People and roles/)).toBeTruthy();
    const boxes = screen.getAllByRole('checkbox') as HTMLInputElement[];
    expect(boxes.map((b) => b.value)).toEqual(['catalog', 'assets']);
    expect(boxes.every((b) => b.checked)).toBe(true);
    await fireEvent.click(boxes[1]!);
    await fireEvent.click(screen.getByTestId('check-again'));
    await waitFor(() => expect(sent.filter((r) => r.path === '/import/check').length).toBe(2));
    expect(sent.filter((r) => r.path === '/import/check').at(-1)!.body).toMatchObject({ kinds: ['catalog'] });
  });
});
