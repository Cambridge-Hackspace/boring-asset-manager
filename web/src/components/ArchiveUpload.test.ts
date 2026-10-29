import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import ArchiveUpload from './ArchiveUpload.svelte';
import { resumeKey } from '../lib/upload';

afterEach(() => {
  cleanup();
  localStorage.clear();
});

// A server taking a whole upload in one part.
function server() {
  const calls: string[] = [];
  let received = 0;
  const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status });
  const fetch = vi.fn(async (url: string, init: RequestInit = {}) => {
    calls.push(`${init.method ?? 'GET'} ${url}`);
    const view = (size: number) => ({ id: 'u1', filename: 'b', size, sha256: 'x', part_size: 1 << 20, received, complete: received === size && size > 0 });
    if (init.method === 'POST') return json(201, view(JSON.parse(init.body as string).size));
    if (init.method === 'PUT') {
      received = (init.body as Blob).size;
      return json(200, view(received));
    }
    if (init.method === 'DELETE') return new Response(null, { status: 204 });
    return json(404, { error: 'Upload not found' });
  });
  return { fetch, calls };
}

describe('ArchiveUpload', () => {
  it('uploads the file chosen and hands on the upload', async () => {
    const { fetch, calls } = server();
    const onuploaded = vi.fn();
    render(ArchiveUpload, { onuploaded, fetch: fetch as unknown as typeof globalThis.fetch });
    const file = new File(['an archive'], 'bam.bam.tar.gz');
    await fireEvent.change(screen.getByTestId('archive-file'), { target: { files: [file] } });
    await waitFor(() => expect(onuploaded).toHaveBeenCalled());
    expect(onuploaded.mock.calls[0]![0]).toMatchObject({ id: 'u1', complete: true });
    expect(calls).toEqual(['POST /api/v1/uploads', 'PUT /api/v1/uploads/u1']);
    expect(screen.getByTestId('upload-status').textContent).toBe('Uploaded 10 bytes.');
  });

  it('offers to resume an unfinished upload, and cancels one', async () => {
    const file = new File(['an archive'], 'old.bam.tar.gz', { lastModified: 5 });
    localStorage.setItem(resumeKey(file), JSON.stringify({ id: 'u9', name: 'old.bam.tar.gz', size: 10, received: 4 }));
    render(ArchiveUpload, { onuploaded: () => {}, fetch: server().fetch as unknown as typeof globalThis.fetch });
    expect(screen.getByText(/old\.bam\.tar\.gz: 4 bytes of 10 bytes\. Choose this file again/)).toBeTruthy();
  });

  it('asks before the page is left while uploading', async () => {
    // A part that never arrives, until it is given up, as fetch does.
    const fetch = vi.fn((_url: string, init: RequestInit = {}) =>
      init.method === 'POST'
        ? Promise.resolve(new Response(JSON.stringify({ id: 'u1', size: 3, part_size: 8, received: 0, complete: false }), { status: 201 }))
        : new Promise<Response>((_, reject) => init.signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))),
    );
    render(ArchiveUpload, { onuploaded: () => {}, fetch: fetch as unknown as typeof globalThis.fetch });
    await fireEvent.change(screen.getByTestId('archive-file'), { target: { files: [new File(['abc'], 'a.tar.gz')] } });
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
    const leaving = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(leaving);
    expect(leaving.defaultPrevented).toBe(true);
    await fireEvent.click(screen.getByRole('button', { name: 'Pause' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Resume' })).toBeTruthy());
    const after = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(after);
    expect(after.defaultPrevented).toBe(false);
  });
});
