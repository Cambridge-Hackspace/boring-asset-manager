// Where the offline copy and the changes waiting to be sent are kept on the
// device (docs/installing.md#offline): IndexedDB, beside the service worker's
// cache of the shell, and the thumbnails in a cache of their own. A thin
// layer, so that what decides what to keep (offline.svelte.ts) can be
// tested with another in its place.

import type { KeptCopy } from './copy';
import type { QueuedChange } from './queue';

export interface OfflineStorage {
  readCopy(): Promise<KeptCopy | null>;
  writeCopy(kept: KeptCopy): Promise<void>;
  readQueue(): Promise<QueuedChange[]>;
  writeQueue(queue: QueuedChange[]): Promise<void>;
  /** Keeps these photos' thumbnails, by address, and drops every other. */
  keepThumbs(urls: string[], fetchOne: (url: string) => Promise<Response>): Promise<void>;
  /** A kept thumbnail, as an address an <img> can show, or null. */
  thumb(url: string): Promise<string | null>;
  /** Removes the copy and its thumbnails; with queue, the waiting changes too. */
  clear(queue: boolean): Promise<void>;
}

const dbName = 'bam-offline';
const storeName = 'kept';
/** The cache the thumbnails are kept in; the service worker removes only its own, bam-shell-*. */
export const thumbCache = 'bam-offline-thumbs';

function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(dbName, 1);
    req.onupgradeneeded = () => req.result.createObjectStore(storeName);
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

async function run<T>(mode: IDBTransactionMode, act: (store: IDBObjectStore) => IDBRequest | void): Promise<T | undefined> {
  const db = await open();
  try {
    return await new Promise<T | undefined>((resolve, reject) => {
      const tx = db.transaction(storeName, mode);
      const req = act(tx.objectStore(storeName));
      tx.oncomplete = () => resolve(req ? (req.result as T) : undefined);
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error);
    });
  } finally {
    db.close();
  }
}

/** The device's own storage, where the browser has IndexedDB. */
export const deviceStorage: OfflineStorage = {
  async readCopy() {
    return (await run<KeptCopy>('readonly', (s) => s.get('copy'))) ?? null;
  },
  async writeCopy(kept) {
    await run('readwrite', (s) => s.put(kept, 'copy'));
  },
  async readQueue() {
    return (await run<QueuedChange[]>('readonly', (s) => s.get('queue'))) ?? [];
  },
  async writeQueue(queue) {
    await run('readwrite', (s) => s.put(queue, 'queue'));
  },
  async keepThumbs(urls, fetchOne) {
    if (typeof caches === 'undefined') return;
    const cache = await caches.open(thumbCache);
    const wanted = new Set(urls.map((u) => new URL(u, location.origin).href));
    for (const req of await cache.keys()) if (!wanted.has(req.url)) await cache.delete(req);
    for (const url of wanted) {
      if (await cache.match(url)) continue;
      try {
        const res = await fetchOne(url);
        if (res.ok) await cache.put(url, res);
      } catch {
        // Gone, or the connection dropped: the next refresh tries again.
        return;
      }
    }
  },
  async thumb(url) {
    if (typeof caches === 'undefined') return null;
    const res = await (await caches.open(thumbCache)).match(new URL(url, location.origin).href);
    return res ? URL.createObjectURL(await res.blob()) : null;
  },
  async clear(queue) {
    await run('readwrite', (s) => {
      s.delete('copy');
      if (queue) s.delete('queue');
    });
    if (typeof caches !== 'undefined') await caches.delete(thumbCache);
  },
};

/**
 * Storage in memory, for tests and for browsers without IndexedDB (a private
 * window, say). It clones what it keeps, as IndexedDB does, so that what
 * IndexedDB would refuse, such as reactive state, is refused here too.
 */
export function memoryStorage(): OfflineStorage & { copy: KeptCopy | null; queue: QueuedChange[]; thumbs: string[] } {
  return {
    copy: null,
    queue: [],
    thumbs: [],
    async readCopy() {
      return this.copy;
    },
    async writeCopy(kept) {
      this.copy = structuredClone(kept);
    },
    async readQueue() {
      return this.queue;
    },
    async writeQueue(queue) {
      this.queue = structuredClone(queue);
    },
    async keepThumbs(urls) {
      this.thumbs = [...urls];
    },
    async thumb(url) {
      return this.thumbs.includes(url) ? url : null;
    },
    async clear(queue) {
      this.copy = null;
      this.thumbs = [];
      if (queue) this.queue = [];
    },
  };
}
