// Uploading an archive in parts (README §2.3, #141): its hash made from
// slices of the file, then each part sent on its own, retried with backoff
// when it fails, and resumed where the server says it got to, after a
// dropped connection, a pause or a page closed. The file is only ever read
// a slice at a time.

import { Sha256 } from './sha256';

/** An upload as the server describes it. */
export interface UploadState {
  id: string;
  filename: string;
  size: number;
  sha256: string;
  part_size: number;
  received: number;
  complete: boolean;
}

export type UploadPhase = 'hashing' | 'uploading' | 'done';

export interface UploadProgress {
  phase: UploadPhase;
  /** Bytes hashed, or bytes the server has. */
  done: number;
  total: number;
}

/** What uploadFile needs from outside, which tests replace. */
export interface UploadOptions {
  onProgress?: (p: UploadProgress) => void;
  /** Aborting pauses: the upload resumes from where it got to. */
  signal?: AbortSignal;
  fetch?: typeof fetch;
  /** Where unfinished uploads are remembered, to resume. */
  storage?: Pick<Storage, 'getItem' | 'setItem' | 'removeItem' | 'key' | 'length'>;
  sleep?: (ms: number) => Promise<void>;
  /** Tries for each part before giving up. */
  tries?: number;
}

/** A failure that sending again will not mend. */
export class UploadError extends Error {
  constructor(message: string, readonly status = 0) {
    super(message);
    this.name = 'UploadError';
  }
}

const HASH_SLICE = 8 << 20;
const PREFIX = 'bam-upload:';

/** The key an unfinished upload of a file is remembered by. */
export function resumeKey(file: Pick<File, 'name' | 'size' | 'lastModified'>): string {
  return `${PREFIX}${file.name}:${file.size}:${file.lastModified}`;
}

interface Remembered {
  id: string;
  name: string;
  size: number;
  received: number;
}

/** The unfinished uploads remembered here, to offer to resume. */
export function unfinishedUploads(storage: UploadOptions['storage'] = defaultStorage()): Remembered[] {
  const out: Remembered[] = [];
  if (!storage) return out;
  for (let i = 0; i < storage.length; i++) {
    const key = storage.key(i);
    if (!key?.startsWith(PREFIX)) continue;
    try {
      out.push(JSON.parse(storage.getItem(key) ?? '') as Remembered);
    } catch {
      // Not ours to read.
    }
  }
  return out;
}

function defaultStorage(): UploadOptions['storage'] | undefined {
  try {
    return globalThis.localStorage;
  } catch {
    return undefined;
  }
}

function remember(storage: UploadOptions['storage'], key: string, r: Remembered | null) {
  try {
    if (r) storage?.setItem(key, JSON.stringify(r));
    else storage?.removeItem(key);
  } catch {
    // Without storage, an upload resumes only while the page is open.
  }
}

/** The SHA-256 of a file, read a slice at a time. */
export async function hashFile(file: Blob, onProgress?: (done: number) => void, signal?: AbortSignal): Promise<string> {
  const h = new Sha256();
  for (let at = 0; at < file.size; at += HASH_SLICE) {
    signal?.throwIfAborted();
    h.update(new Uint8Array(await file.slice(at, at + HASH_SLICE).arrayBuffer()));
    onProgress?.(Math.min(at + HASH_SLICE, file.size));
  }
  return h.hex();
}

async function errorText(res: Response): Promise<string> {
  try {
    const body = await res.json();
    if (typeof body?.error === 'string') return body.error;
  } catch {
    // Not JSON.
  }
  return `${res.status} ${res.statusText}`.trim();
}

/**
 * Uploads a file in parts, resuming an unfinished upload of the same file
 * if one is remembered, and returns the complete upload. Aborting the
 * signal pauses it: call again to resume.
 */
export async function uploadFile(file: File, opts: UploadOptions = {}): Promise<UploadState> {
  const f = opts.fetch ?? globalThis.fetch.bind(globalThis);
  const storage = opts.storage ?? defaultStorage();
  const sleep = opts.sleep ?? ((ms: number) => new Promise<void>((r) => setTimeout(r, ms)));
  const tries = opts.tries ?? 6;
  const signal = opts.signal;
  const key = resumeKey(file);
  const progress = (phase: UploadPhase, done: number) => opts.onProgress?.({ phase, done, total: file.size });
  const init = (extra: RequestInit = {}): RequestInit => ({ credentials: 'same-origin', signal, ...extra });

  const status = async (id: string): Promise<UploadState | null> => {
    const res = await f(`/api/v1/uploads/${encodeURIComponent(id)}`, init({ headers: { Accept: 'application/json' } }));
    if (res.status === 404) return null;
    if (!res.ok) throw new UploadError(await errorText(res), res.status);
    return (await res.json()) as UploadState;
  };

  // An unfinished upload of this file, if the server still has it.
  let up: UploadState | null = null;
  const saved = storage?.getItem(key);
  if (saved) {
    try {
      up = await status((JSON.parse(saved) as Remembered).id);
    } catch (err) {
      if (signal?.aborted) throw err;
      up = null;
    }
    if (!up) remember(storage, key, null);
  }
  if (!up) {
    const sha256 = await hashFile(file, (n) => progress('hashing', n), signal);
    const res = await f(
      '/api/v1/uploads',
      init({
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify({ filename: file.name, size: file.size, sha256 }),
      }),
    );
    if (!res.ok) throw new UploadError(await errorText(res), res.status);
    up = (await res.json()) as UploadState;
  }
  const rec = { id: up.id, name: file.name, size: file.size, received: up.received };
  remember(storage, key, rec);
  progress('uploading', up.received);

  let failures = 0;
  while (!up.complete) {
    signal?.throwIfAborted();
    const first: number = up.received;
    const last: number = Math.min(first + up.part_size, file.size) - 1;
    let res: Response | null = null;
    try {
      res = await f(
        `/api/v1/uploads/${encodeURIComponent(up.id)}`,
        init({
          method: 'PUT',
          headers: {
            'Content-Type': 'application/octet-stream',
            'Content-Range': `bytes ${first}-${last}/${file.size}`,
            Accept: 'application/json',
          },
          body: file.slice(first, last + 1),
        }),
      );
    } catch (err) {
      if (signal?.aborted) throw err;
      res = null; // The connection dropped: try again.
    }
    if (res?.ok) {
      up = (await res.json()) as UploadState;
      failures = 0;
    } else if (res?.status === 409) {
      // The server has more, or less, than thought: carry on from there.
      const body: { received?: unknown; error?: string } = await res.json().catch(() => ({}));
      if (typeof body?.received !== 'number') throw new UploadError(body?.error ?? 'The upload cannot go on', 409);
      up = { ...up, received: body.received };
    } else if (res && res.status !== 400 && res.status < 500 && res.status !== 429) {
      if (res.status === 404 || res.status === 422) remember(storage, key, null);
      throw new UploadError(await errorText(res), res.status);
    } else {
      failures++;
      if (failures >= tries) {
        throw new UploadError(res ? await errorText(res) : 'The connection keeps failing; resume the upload to try again', res?.status ?? 0);
      }
      await sleep(Math.min(30000, 500 * 2 ** (failures - 1)));
      // Ask where to go on from: a part may have arrived though its answer did not.
      try {
        const now = await status(up.id);
        if (!now) {
          remember(storage, key, null);
          throw new UploadError('The upload is no longer on the server; start it again', 404);
        }
        up = now;
      } catch (err) {
        if (err instanceof UploadError || signal?.aborted) throw err;
      }
    }
    rec.received = up.received;
    remember(storage, key, rec);
    progress('uploading', up.received);
  }
  remember(storage, key, null);
  progress('done', file.size);
  return up;
}

/** Cancels an unfinished upload of a file: removed from the server, and forgotten. */
export async function forgetUpload(file: Pick<File, 'name' | 'size' | 'lastModified'>, opts: Pick<UploadOptions, 'fetch' | 'storage'> = {}): Promise<void> {
  const storage = opts.storage ?? defaultStorage();
  const key = resumeKey(file);
  const saved = storage?.getItem(key);
  remember(storage, key, null);
  if (!saved) return;
  try {
    const { id } = JSON.parse(saved) as Remembered;
    await (opts.fetch ?? globalThis.fetch.bind(globalThis))(`/api/v1/uploads/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      credentials: 'same-origin',
    });
  } catch {
    // Left for the server to remove after a day.
  }
}

/** A size in bytes as people read it: 1.2 GB. */
export function formatBytes(n: number): string {
  const units = ['bytes', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (n >= 1000 && i < units.length - 1) {
    n /= 1000;
    i++;
  }
  return i === 0 ? `${n} ${n === 1 ? 'byte' : 'bytes'}` : `${n < 10 ? n.toFixed(1) : Math.round(n)} ${units[i]}`;
}
