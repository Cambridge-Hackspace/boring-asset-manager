import { describe, expect, it } from 'vitest';
import { createHash } from 'node:crypto';
import { resumeKey, unfinishedUploads, uploadFile, UploadError, type UploadProgress } from './upload';

interface FakeUpload {
  id: string;
  size: number;
  sha256: string;
  data: Uint8Array;
  received: number;
  complete: boolean;
}

type Fault = 'drop' | 'lose' | 'fail' | null;

// A server keeping uploads as the API does, which can be made to drop
// connections, fail, and lose answers.
function fakeServer(partSize = 1000) {
  const uploads = new Map<string, FakeUpload>();
  let n = 0;
  const json = (status: number, body: unknown) =>
    new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
  const view = (u: FakeUpload) => ({
    id: u.id,
    filename: 'f',
    size: u.size,
    sha256: u.sha256,
    part_size: partSize,
    received: u.received,
    complete: u.complete,
  });
  const s = {
    uploads,
    puts: 0,
    // Called for each PUT: 'drop' throws as a network error before the
    // part is taken, 'lose' after it is, 'fail' answers 503.
    fault: (_put: number): Fault => null,
    fetch: async (url: string, init: RequestInit = {}): Promise<Response> => {
      init.signal?.throwIfAborted();
      if (url === '/api/v1/uploads' && init.method === 'POST') {
        const req = JSON.parse(init.body as string);
        const id = `u${++n}`;
        uploads.set(id, { id, size: req.size, sha256: req.sha256, data: new Uint8Array(req.size), received: 0, complete: false });
        return json(201, view(uploads.get(id)!));
      }
      const u = uploads.get(decodeURIComponent(url.split('/').pop()!));
      if (!u) return json(404, { error: 'Upload not found' });
      if (!init.method || init.method === 'GET') return json(200, view(u));
      const put = ++s.puts;
      const fault = s.fault(put);
      if (fault === 'drop') throw new TypeError('Failed to fetch');
      if (fault === 'fail') return json(503, { error: 'Service unavailable' });
      const m = /^bytes (\d+)-(\d+)\/(\d+)$/.exec((init.headers as Record<string, string>)['Content-Range']!)!;
      const first = Number(m[1]);
      if (first !== u.received) return json(409, { error: 'not there', received: u.received });
      const bytes = new Uint8Array(await (init.body as Blob).arrayBuffer());
      u.data.set(bytes, first);
      u.received = first + bytes.length;
      if (u.received === u.size) {
        if (createHash('sha256').update(u.data).digest('hex') !== u.sha256) {
          uploads.delete(u.id);
          return json(422, { error: 'hash differs' });
        }
        u.complete = true;
      }
      if (fault === 'lose') throw new TypeError('Failed to fetch');
      return json(200, view(u));
    },
  };
  return s;
}

function memoryStorage() {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
    key: (i: number) => [...m.keys()][i] ?? null,
    get length() {
      return m.size;
    },
  };
}

function file(size: number, name = 'bam.bam.tar.gz') {
  const data = new Uint8Array(size);
  for (let i = 0; i < size; i++) data[i] = (i * 7 + 3) & 0xff;
  return { data, file: new File([data], name, { lastModified: 1700000000000 }) };
}

const noSleep = async () => {};

describe('uploadFile', () => {
  it('uploads in parts, reporting progress, and forgets the upload once complete', async () => {
    const server = fakeServer();
    const storage = memoryStorage();
    const { data, file: f } = file(4500);
    const seen: UploadProgress[] = [];
    const up = await uploadFile(f, { fetch: server.fetch as typeof fetch, storage, sleep: noSleep, onProgress: (p) => seen.push(p) });
    expect(up.complete).toBe(true);
    expect(server.uploads.get(up.id)!.data).toEqual(data);
    expect(server.puts).toBe(5);
    expect(seen[0]!.phase).toBe('hashing');
    expect(seen.filter((p) => p.phase === 'uploading').map((p) => p.done)).toEqual([0, 1000, 2000, 3000, 4000, 4500]);
    expect(seen.at(-1)).toEqual({ phase: 'done', done: 4500, total: 4500 });
    expect(storage.length).toBe(0);
  });

  it('retries a part after a dropped connection or a failure, with backoff, and carries on when an answer was lost', async () => {
    const server = fakeServer();
    server.fault = (put) => (put === 2 ? 'drop' : put === 3 ? 'fail' : put === 5 ? 'lose' : null);
    const sleeps: number[] = [];
    const { data, file: f } = file(4500);
    const up = await uploadFile(f, {
      fetch: server.fetch as typeof fetch,
      storage: memoryStorage(),
      sleep: async (ms) => void sleeps.push(ms),
    });
    expect(server.uploads.get(up.id)!.data).toEqual(data);
    expect(sleeps).toEqual([500, 1000, 500]);
  });

  it('gives up after so many tries, and resumes from where it got to when called again', async () => {
    const server = fakeServer();
    const storage = memoryStorage();
    server.fault = (put) => (put >= 2 && put <= 4 ? 'drop' : null);
    const { data, file: f } = file(3000);
    await expect(uploadFile(f, { fetch: server.fetch as typeof fetch, storage, sleep: noSleep, tries: 3 })).rejects.toBeInstanceOf(UploadError);
    expect(unfinishedUploads(storage)).toEqual([{ id: 'u1', name: f.name, size: 3000, received: 1000 }]);
    expect(storage.getItem(resumeKey(f))).not.toBeNull();

    const puts = server.puts;
    const up = await uploadFile(f, { fetch: server.fetch as typeof fetch, storage, sleep: noSleep });
    expect(up.id).toBe('u1');
    expect(server.puts - puts).toBe(2);
    expect(server.uploads.get('u1')!.data).toEqual(data);
  });

  it('pauses when aborted, and resumes where it stopped', async () => {
    const server = fakeServer();
    const storage = memoryStorage();
    const { data, file: f } = file(5000);
    const pause = new AbortController();
    const fetchFn = server.fetch;
    const pausing = async (url: string, init?: RequestInit) => {
      const res = await fetchFn(url, init);
      if (server.puts === 2) pause.abort();
      return res;
    };
    await expect(uploadFile(f, { fetch: pausing as typeof fetch, storage, sleep: noSleep, signal: pause.signal })).rejects.toThrow();
    expect(server.uploads.get('u1')!.received).toBe(2000);
    const up = await uploadFile(f, { fetch: server.fetch as typeof fetch, storage, sleep: noSleep });
    expect(up.id).toBe('u1');
    expect(server.uploads.get('u1')!.data).toEqual(data);
  });

  it('starts again when the server no longer has a remembered upload', async () => {
    const server = fakeServer();
    const storage = memoryStorage();
    const { file: f } = file(1500);
    storage.setItem(resumeKey(f), JSON.stringify({ id: 'gone', name: f.name, size: 1500, received: 1000 }));
    const up = await uploadFile(f, { fetch: server.fetch as typeof fetch, storage, sleep: noSleep });
    expect(up.id).toBe('u1');
    expect(up.complete).toBe(true);
  });

  it('fails, and forgets the upload, when what arrived is not the file hashed', async () => {
    const server = fakeServer();
    const storage = memoryStorage();
    const { file: f } = file(1500);
    const fetchFn = server.fetch;
    const changing = async (url: string, init?: RequestInit) => {
      const res = await fetchFn(url, init);
      if (init?.method === 'POST') server.uploads.get('u1')!.sha256 = '0'.repeat(64);
      return res;
    };
    await expect(uploadFile(f, { fetch: changing as typeof fetch, storage, sleep: noSleep })).rejects.toMatchObject({ status: 422 });
    expect(storage.length).toBe(0);
  });
});
