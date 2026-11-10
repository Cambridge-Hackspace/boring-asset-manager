// The copy of what a person can see that the installed app keeps, to look
// things up and shop with no signal (docs/installing.md#offline), as
// GET /api/v1/offline makes it, and the plain functions that read it: a
// code to its asset, a search, the shopping list by category, and how old
// it is.

import type { Me } from '../models';

export interface OfflineSettings {
  enabled: boolean;
  refresh_minutes: number;
  thumbnails: boolean;
}

export interface OfflineCategory {
  id: string;
  path: string;
}

export interface OfflineAsset {
  id: string;
  name: string;
  codes?: string[];
  categories?: string[];
  where?: string;
  stock?: { quantity: number; unit?: string; threshold?: number; low?: boolean };
  condition?: string;
  loan?: { borrower?: string; due?: string };
  review?: string;
  /** Its first photo, whose thumbnail may be kept. */
  photo?: string;
  /** The person may use and restock it. */
  record?: boolean;
}

export interface ShoppingVendor {
  vendor_id: string;
  vendor_name: string;
  part_number?: string;
  description?: string;
  url?: string;
  pack_size?: number;
  pack_unit?: string;
  price?: string;
  currency?: string;
}

export interface ShoppingItem {
  asset_id?: string;
  category_id?: string;
  name: string;
  group: string;
  level: number;
  threshold: number | null;
  unit?: string;
  where?: string;
  can_restock: boolean;
  vendor?: ShoppingVendor;
  on_order?: { id: string; reference: string; state: string };
}

export interface ShoppingList {
  /** When it was read, by the server's clock. */
  made_at: string;
  items: ShoppingItem[];
  can_order: boolean;
}

export interface OfflineCopy {
  made_at: string;
  user_id: string;
  settings: OfflineSettings;
  categories: OfflineCategory[];
  assets: OfflineAsset[];
  shopping: ShoppingList;
}

/** The copy as it is kept: who it is for, as /auth/me said when it was made, and its size. */
export interface KeptCopy {
  copy: OfflineCopy;
  me: Me;
  /** Its size as kept, in bytes of JSON. */
  bytes: number;
}

/**
 * The asset a code finds: one whose label carries it, or whose ID it is,
 * as GET /scan does. Codes are matched exactly, as the server matches
 * them, and then without regard to case. Null for none, or for more than
 * one, which the server would not choose between either.
 */
export function findByCode(copy: OfflineCopy, code: string): OfflineAsset | null {
  const c = code.trim();
  if (!c) return null;
  const exact = copy.assets.filter((a) => a.codes?.includes(c));
  if (exact.length === 1) return exact[0]!;
  if (exact.length > 1) return null;
  const byID = copy.assets.find((a) => a.id === c);
  if (byID) return byID;
  const lower = c.toLowerCase();
  const loose = copy.assets.filter((a) => a.codes?.some((k) => k.toLowerCase() === lower));
  return loose.length === 1 ? loose[0]! : null;
}

/** The paths of an asset's categories in the copy. */
export function categoryPaths(copy: OfflineCopy, asset: OfflineAsset): string[] {
  const byID = new Map(copy.categories.map((c) => [c.id, c.path]));
  return (asset.categories ?? []).map((id) => byID.get(id) ?? '').filter(Boolean);
}

/**
 * The assets a search finds: those with every word of it in their name,
 * a code, a category or where they are, ignoring case; those whose name
 * has it whole first, then by name. At most limit.
 */
export function searchCopy(copy: OfflineCopy, query: string, limit = 100): OfflineAsset[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) return [];
  const byID = new Map(copy.categories.map((c) => [c.id, c.path.toLowerCase()]));
  const whole = query.trim().toLowerCase();
  const found = copy.assets.filter((a) => {
    const text = [a.name, ...(a.codes ?? []), ...(a.categories ?? []).map((id) => byID.get(id) ?? ''), a.where ?? ''].join('\n').toLowerCase();
    return words.every((w) => text.includes(w));
  });
  found.sort((a, b) => {
    const wa = a.name.toLowerCase().includes(whole) ? 0 : 1;
    const wb = b.name.toLowerCase().includes(whole) ? 0 : 1;
    return wa - wb || a.name.localeCompare(b.name);
  });
  return found.slice(0, limit);
}

/** The shopping list's items by their category, in the order the server gave them. */
export function shoppingGroups(items: ShoppingItem[]): { group: string; items: ShoppingItem[] }[] {
  const groups: { group: string; items: ShoppingItem[] }[] = [];
  for (const item of items) {
    const last = groups[groups.length - 1];
    if (last && last.group === item.group) last.items.push(item);
    else groups.push({ group: item.group, items: [item] });
  }
  return groups;
}

/**
 * How many to check off as bought, to start with: enough to bring it back
 * to its threshold, in whole packs of its preferred entry when they are in
 * its unit; else one pack, or one.
 */
export function suggestedRestock(item: ShoppingItem): number {
  const pack = item.vendor?.pack_size && (!item.vendor.pack_unit || !item.unit || item.vendor.pack_unit.toLowerCase() === item.unit.toLowerCase()) ? item.vendor.pack_size : 0;
  const short = item.threshold != null ? item.threshold - item.level : 0;
  if (short < 1) return pack || 1;
  return pack ? Math.ceil(short / pack) * pack : short;
}

/** Whether the copy is older than its refresh interval, or than the instance's interval now. */
export function isStale(copy: OfflineCopy, now: Date = new Date(), refreshMinutes = copy.settings.refresh_minutes): boolean {
  return now.getTime() - new Date(copy.made_at).getTime() >= refreshMinutes * 60_000;
}

/** How long ago something was, in words: "just now", "5 minutes ago", "3 days ago". */
export function ago(at: string, now: Date = new Date()): string {
  const minutes = Math.floor((now.getTime() - new Date(at).getTime()) / 60_000);
  if (minutes < 1) return 'just now';
  if (minutes < 60) return minutes === 1 ? '1 minute ago' : `${minutes} minutes ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return hours === 1 ? '1 hour ago' : `${hours} hours ago`;
  const days = Math.floor(hours / 24);
  return days === 1 ? '1 day ago' : `${days} days ago`;
}

/** A size in bytes, as "12 KB" or "1.4 MB". */
export function size(bytes: number): string {
  if (bytes < 1000) return `${bytes} bytes`;
  if (bytes < 1_000_000) return `${Math.round(bytes / 1000)} KB`;
  return `${(bytes / 1_000_000).toFixed(1)} MB`;
}
