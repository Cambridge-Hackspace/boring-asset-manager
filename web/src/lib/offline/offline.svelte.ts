// The offline copy and the changes waiting to be sent, as the app uses them
// (docs/installing.md#offline): what is kept, for whom, when it is
// refreshed, and when it is cleared.
//
// - It is kept only where its person chose to keep one, which they do by
//   installing the app, unless they say otherwise, and only while the
//   instance allows copies.
// - It is the person's own: a copy or a change of anyone else's is cleared
//   when someone signs in.
// - It is cleared on signing out, when the server says nobody is signed in
//   any more, and when a refresh is refused because they may no longer read
//   assets or copies are turned off.

import { ApiError, request } from '../api';
import { isOffline } from '../install/app.svelte';
import type { Me } from '../models';
import { isStale, type KeptCopy, type OfflineCopy, type OfflineSettings } from './copy';
import { bodyFor, outcomeOf, settle, type QueuedChange } from './queue';
import { deviceStorage, memoryStorage, type OfflineStorage } from './storage';
import { photoUrl } from '../models';

/** What a person chose for this device; unset follows the defaults. */
export interface OfflinePrefs {
  /** Keep a copy here: by default, only in the installed app. */
  keep?: boolean;
  /** Keep thumbnails of the assets' photos in it: by default, not. */
  thumbnails?: boolean;
}

const prefsKey = 'bam-offline-prefs';

function readPrefs(): OfflinePrefs {
  try {
    return JSON.parse(localStorage.getItem(prefsKey) ?? '{}') as OfflinePrefs;
  } catch {
    return {};
  }
}

// randomUUID is only in secure contexts; an instance on plain HTTP has none.
function newID(): string {
  return typeof crypto.randomUUID === 'function' ? crypto.randomUUID() : `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function hasIndexedDB(): boolean {
  try {
    return typeof indexedDB !== 'undefined';
  } catch {
    return false;
  }
}

export class Offline {
  kept = $state<KeptCopy | null>(null);
  queue = $state<QueuedChange[]>([]);
  /** The app runs from the copy: it was opened with no connection. */
  active = $state(false);
  refreshing = $state(false);
  /** Why the last refresh did not keep a copy, if it did not. */
  error = $state('');
  /** The instance's settings, as the last copy or refresh read them. */
  settings = $state<OfflineSettings | null>(null);
  prefs = $state<OfflinePrefs>({});
  /** Whether the app runs installed, which keeps a copy unless the person says otherwise. */
  standalone = $state(false);

  #storage: OfflineStorage;
  #loading: Promise<void> | null = null;
  #flushing = false;

  constructor(storage?: OfflineStorage) {
    this.#storage = storage ?? (hasIndexedDB() ? deviceStorage : memoryStorage());
  }

  /** Reads what this device keeps, once. */
  load(): Promise<void> {
    this.prefs = readPrefs();
    this.#loading ??= (async () => {
      try {
        this.kept = await this.#storage.readCopy();
        this.queue = await this.#storage.readQueue();
        if (this.kept) this.settings = this.kept.copy.settings;
      } catch {
        // Storage refused, as in some private windows: nothing is kept.
      }
    })();
    return this.#loading;
  }

  get copy(): OfflineCopy | null {
    return this.kept?.copy ?? null;
  }

  /** Whether this device keeps a copy for its person. */
  get keepsHere(): boolean {
    return (this.prefs.keep ?? this.standalone) && this.settings?.enabled !== false;
  }

  get keepsThumbnails(): boolean {
    return !!this.prefs.thumbnails && this.settings?.thumbnails !== false;
  }

  /** The changes held for the person to decide: conflicts, and those refused. */
  get held(): QueuedChange[] {
    return this.queue.filter((c) => c.state !== 'waiting');
  }

  setPrefs(prefs: OfflinePrefs): void {
    this.prefs = { ...this.prefs, ...prefs };
    try {
      localStorage.setItem(prefsKey, JSON.stringify(this.prefs));
    } catch {
      // Kept for this visit only.
    }
  }

  /**
   * Who is signed in, as the server says, online: what was kept for anyone
   * else goes; their own waiting changes are sent; and the copy is
   * refreshed if it is kept and due, or cleared if it is no longer kept.
   */
  async sync(me: Me): Promise<void> {
    await this.load();
    this.active = false;
    const user = me.user;
    if (!user) {
      // Nobody is signed in now: the copy goes. Their waiting changes stay
      // for them, until someone else signs in.
      if (this.kept) await this.clear(false);
      return;
    }
    if (this.kept && this.kept.copy.user_id !== user.id) await this.clear(false);
    if (this.queue.some((c) => c.owner !== user.id)) await this.#saveQueue(this.queue.filter((c) => c.owner === user.id));
    await this.flush();
    if (!this.settings) {
      try {
        this.settings = await request<OfflineSettings>('GET', '/settings/offline');
      } catch {
        // Read with the next refresh.
      }
    }
    if (!this.keepsHere) {
      if (this.kept) await this.clear(false);
      return;
    }
    if (!this.kept || isStale(this.kept.copy, new Date(), this.settings?.refresh_minutes)) await this.refresh(me);
  }

  /** Makes a new copy for the person signed in, replacing the old. */
  async refresh(me: Me): Promise<void> {
    if (this.refreshing || !me.user) return;
    this.refreshing = true;
    this.error = '';
    try {
      const copy = await request<OfflineCopy>('GET', '/offline');
      this.settings = copy.settings;
      if (copy.user_id !== me.user.id) throw new Error('The copy is for someone else; sign in again.');
      // Who it is for comes from the session, whose state is reactive: a
      // plain copy of it, as IndexedDB keeps only plain values.
      const plain = $state.snapshot(me) as Me;
      const kept: KeptCopy = { copy, me: { user: plain.user, permissions: plain.permissions, roles: plain.roles, role_scopes: plain.role_scopes }, bytes: 0 };
      kept.bytes = JSON.stringify(kept).length;
      await this.#storage.writeCopy(kept);
      this.kept = kept;
      // Kept against the browser making room, where it allows.
      void navigator.storage?.persist?.().catch(() => false);
      const thumbs = this.keepsThumbnails ? copy.assets.filter((a) => a.photo).map((a) => photoUrl(a.id, a.photo!, true)) : [];
      await this.#storage.keepThumbs(thumbs, (url) => fetch(url, { credentials: 'same-origin' }));
    } catch (err) {
      if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
        // Signed out, or no longer allowed a copy: what was kept goes, and
        // with it, once they may not read assets, their waiting changes.
        await this.clear(err.status === 403);
        this.error = err.message;
      } else if (!isOffline(err)) {
        this.error = err instanceof Error ? err.message : String(err);
      }
    } finally {
      this.refreshing = false;
    }
  }

  /** Runs the app from the copy, opened with no connection; false when nothing is kept. */
  async goOffline(): Promise<Me | null> {
    await this.load();
    if (!this.kept) return null;
    this.active = true;
    return this.kept.me;
  }

  /** Records a change to send, and sends it now if it can: null once sent, else as it waits or is held. */
  async enqueue(change: Omit<QueuedChange, 'id' | 'at' | 'state'>): Promise<QueuedChange | null> {
    const full: QueuedChange = { ...change, id: newID(), at: new Date().toISOString(), state: 'waiting' };
    await this.#saveQueue([...this.queue, full]);
    await this.flush();
    return this.queue.find((c) => c.id === full.id) ?? null;
  }

  /** Sends the waiting changes, in order, until one cannot go for want of a connection. */
  async flush(): Promise<void> {
    if (this.#flushing) return;
    this.#flushing = true;
    try {
      for (const change of [...this.queue]) {
        if (change.state !== 'waiting') continue;
        if (!(await this.#send(change, false))) break;
      }
    } finally {
      this.#flushing = false;
    }
  }

  /** The person's decision on a held change: record it anyway, try it again, or drop it. */
  async resolve(id: string, decision: 'anyway' | 'retry' | 'drop'): Promise<void> {
    const change = this.queue.find((c) => c.id === id);
    if (!change) return;
    if (decision === 'drop') {
      await this.#saveQueue(this.queue.filter((c) => c.id !== id));
      return;
    }
    await this.#send({ ...change, state: 'waiting' }, decision === 'anyway');
  }

  // Sends one change; false when it waits for a connection or a session.
  async #send(change: QueuedChange, anyway: boolean): Promise<boolean> {
    let outcome;
    try {
      outcome = outcomeOf(await request('POST', `/assets/${encodeURIComponent(change.asset_id)}/quantity`, bodyFor(change, anyway)), false);
    } catch (err) {
      outcome = outcomeOf(err, true);
    }
    const next = settle(change, outcome);
    await this.#saveQueue(next ? this.queue.map((c) => (c.id === change.id ? next : c)) : this.queue.filter((c) => c.id !== change.id));
    if (outcome.kind === 'sent' && outcome.quantity !== undefined) await this.#noteQuantity(change.asset_id, outcome.quantity);
    return outcome.kind !== 'offline' && outcome.kind !== 'signed-out';
  }

  // What a sent change left: the copy shows it until its next refresh.
  async #noteQuantity(assetID: string, quantity: number): Promise<void> {
    const kept = this.kept;
    const asset = kept?.copy.assets.find((a) => a.id === assetID);
    if (!kept || !asset?.stock) return;
    asset.stock = { ...asset.stock, quantity };
    this.kept = { ...kept };
    try {
      await this.#storage.writeCopy($state.snapshot(this.kept) as KeptCopy);
    } catch {
      // Shown here until the next refresh.
    }
  }

  async #saveQueue(queue: QueuedChange[]): Promise<void> {
    this.queue = queue;
    try {
      await this.#storage.writeQueue($state.snapshot(queue) as QueuedChange[]);
    } catch {
      // Held in memory while the app is open.
    }
  }

  /** A kept thumbnail of an asset's photo, as an address an <img> shows, or null. */
  async thumb(assetID: string, photoID: string): Promise<string | null> {
    if (!this.keepsThumbnails) return null;
    try {
      return await this.#storage.thumb(photoUrl(assetID, photoID, true));
    } catch {
      return null;
    }
  }

  /** Removes the copy and its thumbnails, and with queue, the waiting changes. */
  async clear(queue = true): Promise<void> {
    this.kept = null;
    this.active = false;
    if (queue) this.queue = [];
    try {
      await this.#storage.clear(queue);
    } catch {
      // Nothing was kept.
    }
  }
}

export const offline = new Offline();
