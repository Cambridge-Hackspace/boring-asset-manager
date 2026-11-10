import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../api';
import { fakeApi } from '../../test/fetch';
import type { Me } from '../models';
import { findByCode, isStale, searchCopy, shoppingGroups, suggestedRestock, type OfflineCopy, type ShoppingItem } from './copy';
import { Offline } from './offline.svelte';
import { bodyFor, outcomeOf, pendingFor, settle, type QueuedChange } from './queue';
import { memoryStorage } from './storage';
import { session } from '../session.svelte';

const copy: OfflineCopy = {
  made_at: '2026-11-10T09:00:00Z',
  user_id: 'u1',
  settings: { enabled: true, refresh_minutes: 60, thumbnails: true },
  categories: [
    { id: 'c1', path: 'Stock › Fasteners' },
    { id: 'c2', path: 'Tools' },
  ],
  assets: [
    { id: 'a1', name: 'M3 screws', codes: ['HS-100'], categories: ['c1'], where: 'Workshop › Drawer 3', stock: { quantity: 3, unit: 'pcs', threshold: 50, low: true }, record: true },
    { id: 'a2', name: 'Drill', codes: ['HS-101', 'hs-x'], categories: ['c2'], where: 'Workshop' },
    { id: 'a3', name: 'Cordless drill screws', codes: ['HS-X'], categories: ['c1'] },
  ],
  shopping: { made_at: '2026-11-10T09:00:00Z', can_order: false, items: [] },
};

const me: Me = { user: { id: 'u1', email: 'ada@hackspace.org', name: 'Ada' } as Me['user'], permissions: ['assets.read', 'stock.record'] };

beforeEach(() => localStorage.clear());
afterEach(() => {
  vi.unstubAllGlobals();
});

describe('the copy', () => {
  it('finds an asset by its code, its ID, or its code in another case when only one has it', () => {
    expect(findByCode(copy, ' HS-100 ')?.id).toBe('a1');
    expect(findByCode(copy, 'a2')?.id).toBe('a2');
    expect(findByCode(copy, 'hs-100')?.id).toBe('a1');
    // Exactly, one each; in any case, two: none, as the server chooses none.
    expect(findByCode(copy, 'HS-X')?.id).toBe('a3');
    expect(findByCode(copy, 'Hs-X')).toBeNull();
    expect(findByCode(copy, 'nothing')).toBeNull();
  });

  it('searches every word over names, codes, categories and places, whole names first', () => {
    expect(searchCopy(copy, 'drill').map((a) => a.name)).toEqual(['Cordless drill screws', 'Drill']);
    expect(searchCopy(copy, 'fasteners drawer').map((a) => a.id)).toEqual(['a1']);
    expect(searchCopy(copy, 'hs-101').map((a) => a.id)).toEqual(['a2']);
    expect(searchCopy(copy, '   ')).toEqual([]);
  });

  it('is stale once its refresh interval has passed', () => {
    expect(isStale(copy, new Date('2026-11-10T09:59:00Z'))).toBe(false);
    expect(isStale(copy, new Date('2026-11-10T10:00:00Z'))).toBe(true);
    expect(isStale(copy, new Date('2026-11-10T09:20:00Z'), 15)).toBe(true);
  });

  it('groups the shopping list by category, and suggests whole packs up to the threshold', () => {
    const item = (name: string, group: string, more: Partial<ShoppingItem> = {}): ShoppingItem => ({ name, group, level: 3, threshold: 50, can_restock: true, ...more });
    expect(shoppingGroups([item('a', 'X'), item('b', 'X'), item('c', 'Y')]).map((g) => `${g.group}:${g.items.length}`)).toEqual(['X:2', 'Y:1']);
    expect(suggestedRestock(item('a', 'X', { unit: 'pcs', vendor: { vendor_id: 'v', vendor_name: 'V', pack_size: 20, pack_unit: 'pcs' } }))).toBe(60);
    expect(suggestedRestock(item('a', 'X'))).toBe(47);
    expect(suggestedRestock(item('a', 'X', { threshold: null }))).toBe(1);
    // A pack in another unit says nothing of how many.
    expect(suggestedRestock(item('a', 'X', { unit: 'g', vendor: { vendor_id: 'v', vendor_name: 'V', pack_size: 20, pack_unit: 'pcs' } }))).toBe(47);
  });
});

const restock: QueuedChange = {
  id: 'q1', owner: 'u1', kind: 'restock', asset_id: 'a1', name: 'M3 screws', unit: 'pcs', amount: 50, since: copy.made_at, at: '2026-11-10T10:00:00Z', state: 'waiting',
};

describe('the queue', () => {
  it('sends a restock refusing to be recorded past a restock since its copy, unless told to', () => {
    expect(bodyFor(restock)).toEqual({ adjust: 50, unless_restocked_since: copy.made_at });
    expect(bodyFor(restock, true)).toEqual({ adjust: 50 });
    expect(bodyFor({ ...restock, kind: 'use', amount: 2 })).toEqual({ adjust: -2 });
  });

  it('holds a conflict for the person, with what was restocked, and keeps waiting with no connection', () => {
    const conflict = new ApiError(409, 'Restocked since then', [], {
      restocked: [{ created_at: '2026-11-10T09:30:00Z', payload: { change: 40, actor: { name: 'Grace' } } }],
      asset: { asset: { stock: { quantity: 43 } } },
    });
    const held = settle(restock, outcomeOf(conflict, true));
    expect(held).toMatchObject({ state: 'conflict', now: 43, restocked: [{ at: '2026-11-10T09:30:00Z', change: 40, by: 'Grace' }] });
    expect(settle(restock, outcomeOf(new TypeError('Failed to fetch'), true, { onLine: false }))).toBe(restock);
    expect(settle(restock, outcomeOf(new ApiError(401, 'Sign in'), true))).toBe(restock);
    expect(settle(restock, outcomeOf(new ApiError(404, 'Asset not found'), true))).toMatchObject({ state: 'failed' });
    expect(settle(restock, outcomeOf(new ApiError(409, 'not enough stock: 3 left'), true))).toMatchObject({ state: 'conflict', message: 'not enough stock: 3 left' });
    expect(settle(restock, outcomeOf({ asset: { asset: { stock: { quantity: 53 } } } }, false))).toBeNull();
  });

  it('adds up what waits to be sent to an asset', () => {
    expect(pendingFor([restock, { ...restock, id: 'q2', kind: 'use', amount: 5 }, { ...restock, id: 'q3', state: 'conflict' }], 'a1')).toBe(45);
  });
});

describe('keeping the copy', () => {
  it('keeps one only where it is chosen, the installed app by default, and refreshes it when due', async () => {
    const storage = memoryStorage();
    const o = new Offline(storage);
    fakeApi({ 'GET /settings/offline': () => copy.settings, 'GET /offline': () => copy });
    await o.sync(me);
    expect(storage.copy).toBeNull();

    o.standalone = true;
    // Who is signed in comes from the session, whose state is reactive; it
    // is kept as plain values, as IndexedDB keeps only those.
    session.user = me.user;
    session.permissions = [...me.permissions];
    session.roles = ['Stores'];
    await o.sync(session.me);
    expect(o.error).toBe('');
    expect(storage.copy?.copy.user_id).toBe('u1');
    expect(storage.copy?.me.roles).toEqual(['Stores']);
    expect(storage.copy?.me.user?.id).toBe('u1');
    expect(o.kept?.bytes).toBeGreaterThan(100);
    // Thumbnails only when chosen.
    expect(storage.thumbs).toEqual([]);

    o.setPrefs({ keep: false });
    await o.sync(me);
    expect(storage.copy).toBeNull();
  });

  it("clears another person's copy and changes when someone signs in, and the copy when nobody is", async () => {
    const storage = memoryStorage();
    storage.copy = { copy, me, bytes: 1 };
    storage.queue = [restock];
    const o = new Offline(storage);
    o.standalone = true;
    const { sent } = fakeApi({ 'GET /offline': () => ({ ...copy, user_id: 'u2' }), 'GET /settings/offline': () => copy.settings });
    await o.sync({ user: { id: 'u2' } as Me['user'], permissions: ['assets.read'] });
    expect(o.queue).toEqual([]);
    expect(sent.some((r) => r.method === 'POST')).toBe(false);
    expect(o.copy?.user_id).toBe('u2');

    o.queue = [{ ...restock, owner: 'u2' }];
    await o.sync({ user: null, permissions: [] });
    expect(o.copy).toBeNull();
    expect(storage.copy).toBeNull();
    // Their changes wait for them to sign in again.
    expect(o.queue).toHaveLength(1);
  });

  it('clears the copy and the changes when a refresh says they may no longer have one', async () => {
    const storage = memoryStorage();
    storage.copy = { copy: { ...copy, made_at: '2020-01-01T00:00:00Z' }, me, bytes: 1 };
    storage.queue = [{ ...restock, state: 'failed' }];
    const o = new Offline(storage);
    o.standalone = true;
    fakeApi({ 'GET /offline': () => ({ status: 403, body: { error: 'Offline copies are turned off on this instance' } }) });
    await o.sync(me);
    expect(storage.copy).toBeNull();
    expect(storage.queue).toEqual([]);
    expect(o.error).toContain('turned off');
  });

  it('sends waiting changes in order, holds a conflict, and records it anyway when asked', async () => {
    const storage = memoryStorage();
    storage.copy = { copy, me, bytes: 1 };
    const o = new Offline(storage);
    let restocked = false;
    const { sent } = fakeApi({
      'POST /assets/a1/quantity': (body) =>
        (body as { unless_restocked_since?: string }).unless_restocked_since && !restocked
          ? { status: 409, body: { error: 'Restocked since then', restocked: [{ created_at: '2026-11-10T09:30:00Z', payload: { change: 40 } }], asset: { asset: { stock: { quantity: 43 } } } } }
          : { asset: { asset: { stock: { quantity: 93 } } } },
    });
    await o.load();
    const held = await o.enqueue({ owner: 'u1', kind: 'restock', asset_id: 'a1', name: 'M3 screws', unit: 'pcs', amount: 50, since: copy.made_at });
    expect(held?.state).toBe('conflict');
    expect(o.held).toHaveLength(1);
    expect(storage.queue[0]?.state).toBe('conflict');

    await o.resolve(held!.id, 'anyway');
    expect(sent.at(-1)?.body).toEqual({ adjust: 50 });
    expect(o.queue).toEqual([]);
    // The copy shows what it left until it is next refreshed.
    expect(o.copy?.assets[0]?.stock?.quantity).toBe(93);
  });

  it('keeps changes made with no connection, and sends them once there is one', async () => {
    const o = new Offline(memoryStorage());
    vi.stubGlobal('fetch', vi.fn(async () => Promise.reject(new TypeError('Failed to fetch'))));
    const waiting = await o.enqueue({ owner: 'u1', kind: 'use', asset_id: 'a1', name: 'M3 screws', amount: 2, since: copy.made_at });
    expect(waiting?.state).toBe('waiting');
    const { sent } = fakeApi({ 'POST /assets/a1/quantity': () => ({ asset: { asset: { stock: { quantity: 1 } } } }) });
    await o.flush();
    expect(sent[0]?.body).toEqual({ adjust: -2 });
    expect(o.queue).toEqual([]);
  });
});
