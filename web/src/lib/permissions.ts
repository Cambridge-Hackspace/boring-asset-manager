// What each permission allows, in the order role forms list them.

import type { Permission } from './models';

export const permissions: { key: Permission; label: string; description: string }[] = [
  { key: 'assets:read', label: 'Read assets', description: 'See assets, categories, fields, history and label templates.' },
  { key: 'stock:record', label: 'Record stock', description: 'Use and restock quantities, and mark things running low or restocked.' },
  { key: 'assets:write', label: 'Edit assets', description: 'Create, edit and import assets.' },
  { key: 'assets:delete', label: 'Archive assets', description: 'Archive and restore assets.' },
  { key: 'labels:print', label: 'Print labels', description: 'Send labels to printers.' },
  { key: 'categories:manage', label: 'Manage categories', description: 'Create and edit categories and fields.' },
  { key: 'templates:manage', label: 'Manage templates', description: 'Design label templates and upload their images.' },
  { key: 'settings:manage', label: 'Manage settings', description: 'Set up printers and notification channels.' },
  { key: 'users:manage', label: 'Manage users', description: 'Create and archive users, and manage roles and what they allow.' },
];

/** A permission's label, or its key if it is not one this app knows. */
export function permissionLabel(key: string): string {
  return permissions.find((p) => p.key === key)?.label ?? key;
}
