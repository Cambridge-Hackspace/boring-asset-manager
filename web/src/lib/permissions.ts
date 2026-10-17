// What each permission allows, in the order role forms list them.

import type { Permission, PermissionKey } from './models';

export const permissions: { key: Permission; label: string; description: string }[] = [
  { key: 'assets.read', label: 'Read assets', description: 'See assets, categories, fields, history and label templates.' },
  { key: 'assets.scan', label: 'Scan codes', description: 'Look assets up by their codes, and see their scan pages.' },
  { key: 'assets.track', label: 'See who', description: 'See who borrowed assets and who changed them, by name, and the loans list.' },
  { key: 'stock.record', label: 'Record stock', description: 'Use and restock quantities, and mark things running low or restocked.' },
  { key: 'assets.report', label: 'Report problems', description: 'Report an asset as needing maintenance or out of service.' },
  { key: 'assets.act', label: 'Take actions', description: 'Press the actions categories offer on assets, such as Identify.' },
  { key: 'assets.borrow', label: 'Borrow', description: 'Check lendable assets out to themselves, and back in.' },
  { key: 'assets.lend', label: 'Lend', description: 'Check lendable assets out to anyone, and any loan back in.' },
  { key: 'assets.write', label: 'Edit assets', description: 'Create, edit and import assets, and mark them working again.' },
  { key: 'assets.delete', label: 'Archive assets', description: 'Archive and restore assets.' },
  { key: 'labels.print', label: 'Print labels', description: 'Send labels to printers.' },
  { key: 'categories.manage', label: 'Manage categories', description: 'Create and edit categories and fields.' },
  { key: 'templates.manage', label: 'Manage templates', description: 'Design label templates and upload their images.' },
  { key: 'reports.manage', label: 'Manage reports', description: 'Create, edit and delete saved reports; anyone who reads assets can run them.' },
  { key: 'settings.manage', label: 'Manage settings', description: 'Set up printers and notification channels.' },
  { key: 'users.manage', label: 'Manage users', description: 'Create and archive users, and manage roles and what they allow.' },
];

/** A permission's label, or its key if it is not one this app knows. */
export function permissionLabel(key: string): string {
  return permissions.find((p) => p.key === key)?.label ?? key;
}

/** The permissions a role may hold in some scopes only: those acting on assets. */
export const scopable: Permission[] = ['assets.read', 'assets.scan', 'assets.act', 'assets.track', 'stock.record', 'assets.report', 'assets.borrow', 'assets.lend', 'assets.write', 'assets.delete'];

/** The reserved scope of assets that no category puts in another. */
export const DEFAULT_SCOPE = 'default';

/** Where a role holds a permission: in every scope, in some, or nowhere (an empty list). */
export function heldIn(keys: readonly string[], p: Permission): 'all' | string[] {
  if (keys.includes(p)) return 'all';
  return keys.filter((k) => k.endsWith(':' + p)).map((k) => k.slice(0, -(p.length + 1)));
}

/** Keys with a permission held where given instead of where it was. */
export function setHeld(keys: readonly PermissionKey[], p: Permission, where: 'all' | string[]): PermissionKey[] {
  const rest = keys.filter((k) => k !== p && !k.endsWith(':' + p));
  if (where === 'all') return [...rest, p];
  return [...rest, ...where.map((s) => `${s}:${p}` as PermissionKey)];
}

/** A role's permission key in words, such as "Read assets in tools". */
export function describeKey(key: string): string {
  const at = key.indexOf(':');
  return at < 0 ? permissionLabel(key) : `${permissionLabel(key.slice(at + 1))} in ${key.slice(0, at)}`;
}

/** What the public role, which everyone holds, may hold: looking, never changing. */
export const publicAllowed: Permission[] = ['assets.read', 'assets.scan'];
