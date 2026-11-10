// Changes made with no signal, waiting to be sent (docs/installing.md#offline):
// restocks checked off the shopping list, and stock used, from the copy.
// They are sent when the app is next open and online, in the order they
// were made. A restock is sent refusing to be recorded if the thing was
// restocked since the copy, or list, it was made from: that conflict, and
// using more than is left, are shown for the person to decide, never
// merged.

import { ApiError } from '../api';
import { isOffline } from '../install/app.svelte';

export interface Restocked {
  at: string;
  change: number;
  /** Who, only for those who track it. */
  by?: string;
}

export interface QueuedChange {
  id: string;
  /** Whose it is: only they send it. */
  owner: string;
  kind: 'restock' | 'use';
  asset_id: string;
  name: string;
  unit?: string;
  amount: number;
  /** When what it was made from was read, by the server's clock. */
  since: string;
  /** When it was made, by this device's. */
  at: string;
  /** Waiting to be sent; or held, for the person: a conflict, or refused outright. */
  state: 'waiting' | 'conflict' | 'failed';
  /** A restock's conflict: the restocks since, and how many there are now. */
  restocked?: Restocked[];
  now?: number;
  message?: string;
}

/** What a change sends to POST /assets/{id}/quantity; anyway records a restock past its conflict. */
export function bodyFor(change: QueuedChange, anyway = false): { adjust: number; unless_restocked_since?: string } {
  if (change.kind === 'use') return { adjust: -change.amount };
  return anyway ? { adjust: change.amount } : { adjust: change.amount, unless_restocked_since: change.since };
}

/** What became of sending a change. */
export type Outcome =
  | { kind: 'sent'; quantity?: number }
  /** No connection: it waits, and so does everything after it. */
  | { kind: 'offline' }
  /** The session ended: it waits for its person to sign in again. */
  | { kind: 'signed-out' }
  | { kind: 'conflict'; restocked?: Restocked[]; now?: number; message: string }
  | { kind: 'failed'; message: string };

interface ConflictBody {
  error?: string;
  restocked?: { created_at: string; payload: { change?: number; actor?: { name?: string } } }[];
  asset?: { asset: { stock?: { quantity: number } | null } };
}

/** Sorts what sending a change came to: an answer, or the error it threw. */
export function outcomeOf(result: unknown, failed: boolean, nav: Pick<Navigator, 'onLine'> = navigator): Outcome {
  if (!failed) {
    const quantity = (result as ConflictBody | undefined)?.asset?.asset.stock?.quantity;
    return { kind: 'sent', quantity };
  }
  if (!(result instanceof ApiError)) return isOffline(result, nav) ? { kind: 'offline' } : { kind: 'failed', message: String((result as Error)?.message ?? result) };
  if (result.status === 401) return { kind: 'signed-out' };
  if (result.status === 409) {
    const body = result.body as ConflictBody | undefined;
    if (body?.restocked?.length) {
      return {
        kind: 'conflict',
        restocked: body.restocked.map((e) => ({ at: e.created_at, change: e.payload.change ?? 0, by: e.payload.actor?.name || undefined })),
        now: body.asset?.asset.stock?.quantity ?? undefined,
        message: result.message,
      };
    }
    // Using more than is left: "not enough stock: 3 left".
    return { kind: 'conflict', message: result.message };
  }
  if (result.status === 404 || result.status === 403) return { kind: 'failed', message: 'You can no longer change it: ' + result.message };
  return { kind: 'failed', message: result.message };
}

/** A change as an outcome leaves it: null once sent, or held for the person. */
export function settle(change: QueuedChange, outcome: Outcome): QueuedChange | null {
  switch (outcome.kind) {
    case 'sent':
      return null;
    case 'offline':
    case 'signed-out':
      return change;
    case 'conflict':
      return { ...change, state: 'conflict', restocked: outcome.restocked, now: outcome.now, message: outcome.message };
    case 'failed':
      return { ...change, state: 'failed', message: outcome.message };
  }
}

/** How much a change moves stock: up for a restock, down for a use. */
export function signed(change: Pick<QueuedChange, 'kind' | 'amount'>): number {
  return change.kind === 'restock' ? change.amount : -change.amount;
}

/** What the waiting changes to an asset add up to, as the copy shows beside its quantity. */
export function pendingFor(queue: QueuedChange[], assetID: string): number {
  return queue.filter((c) => c.asset_id === assetID && c.state === 'waiting').reduce((sum, c) => sum + signed(c), 0);
}

/** A change in words: "Restock M3 screws by 50 pcs". */
export function describeChange(change: QueuedChange): string {
  const unit = change.unit ? ` ${change.unit}` : '';
  return change.kind === 'restock' ? `Restock ${change.name} by ${change.amount}${unit}` : `Use ${change.amount}${unit} of ${change.name}`;
}
