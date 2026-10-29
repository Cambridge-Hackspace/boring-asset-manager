import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import BackupsPage from './BackupsPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';

beforeEach(() => {
  session.permissions = ['users.manage', 'settings.manage'];
  vi.useFakeTimers({ shouldAdvanceTime: true });
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  localStorage.clear();
});

const header = {
  format: 'bam-archive',
  bam_version: 'v0.15.0',
  schema_version: 46,
  created_at: '2026-10-29T12:00:00Z',
  dialect: 'sqlite',
  kinds: ['assets', 'catalog', 'people', 'auth'],
  secrets: true,
  set: { id: 's', part: 1 },
};

function server() {
  return fakeApi({
    'POST /uploads': () => ({ status: 201, body: { id: 'u1', size: 7, part_size: 1 << 20, received: 0, complete: false } }),
    'PUT /uploads/u1': () => ({ id: 'u1', size: 7, part_size: 1 << 20, received: 7, complete: true }),
    'POST /backup/check': () => ({ status: 202, body: { id: 'j1', done: false } }),
    'GET /backup/jobs/j1': () => ({
      id: 'j1',
      done: true,
      result: {
        header,
        tables: { assets: 3, users: 2 },
        files: 4,
        file_bytes: 4000,
        missing_files: 0,
        parts: 1,
        schema_current: true,
        instance_host: 'bam.example.org',
        warnings: ['This archive holds authentication, which is never imported through the web.'],
      },
    }),
    'POST /backup/replace': () => ({ status: 202, body: { id: 'j2', done: false } }),
    'GET /backup/jobs/j2': () => ({
      id: 'j2',
      done: true,
      result: {
        summary: { kinds: ['assets'], tables: { assets: 3 }, files: 4, file_bytes: 4000, already: 0, unnamed: 0, warnings: [], removed: { assets: 1 }, lost: {} },
        before_upload_id: 'b1',
      },
    }),
  });
}

async function chooseBackup() {
  await fireEvent.change(screen.getByTestId('archive-file'), { target: { files: [new File(['archive'], 'bam.bam.tar.gz')] } });
  await waitFor(() => expect(screen.getByTestId('checked')).toBeTruthy());
}

describe('BackupsPage', () => {
  it('offers a backup to download', () => {
    server();
    render(BackupsPage, { params: {} });
    expect(screen.getByTestId('download-backup').getAttribute('href')).toBe('/api/v1/backup');
  });

  it('checks an uploaded backup, and replaces only the kinds ticked once confirmed', async () => {
    const { sent } = server();
    render(BackupsPage, { params: {} });
    await chooseBackup();
    expect(screen.getByText(/never imported through the web/)).toBeTruthy();
    // Authentication is never offered.
    const boxes = screen.getAllByRole('checkbox') as HTMLInputElement[];
    expect(boxes.map((b) => b.value)).toEqual(['assets', 'catalog', 'people']);
    expect(boxes.every((b) => b.checked)).toBe(true);
    await fireEvent.click(boxes[1]!);
    await fireEvent.click(boxes[2]!);

    const replace = screen.getByRole('button', { name: 'Replace' }) as HTMLButtonElement;
    expect(replace.disabled).toBe(true);
    await fireEvent.input(screen.getByTestId('confirm'), { target: { value: 'bam.example' } });
    expect(replace.disabled).toBe(true);
    await fireEvent.input(screen.getByTestId('confirm'), { target: { value: 'bam.example.org' } });
    expect(replace.disabled).toBe(false);
    await fireEvent.click(replace);

    await waitFor(() => expect(screen.getByTestId('replaced')).toBeTruthy());
    expect(sent.find((r) => r.method === 'POST' && r.path === '/backup/replace')!.body).toEqual({
      upload_id: 'u1',
      kinds: ['assets'],
      confirm: 'bam.example.org',
      part: 1,
    });
    expect(screen.getByTestId('replaced').textContent).toMatch(/Replaced assets: 3\s+rows and 4 files/);
    expect(screen.getByText('download it').getAttribute('href')).toBe('/api/v1/uploads/b1/content');
  });

  it('says what is needed without both permissions', () => {
    session.permissions = ['settings.manage'];
    server();
    render(BackupsPage, { params: {} });
    expect(screen.queryByTestId('download-backup')).toBeNull();
    expect(screen.getByText(/needs both users.manage and settings.manage/)).toBeTruthy();
  });
});
