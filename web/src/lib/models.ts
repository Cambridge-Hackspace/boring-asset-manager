// Types mirroring internal/models as the API returns them.

export type Permission =
  | 'assets:read'
  | 'assets:write'
  | 'assets:delete'
  | 'templates:manage'
  | 'categories:manage'
  | 'settings:manage'
  | 'users:manage'
  | 'labels:print';

export interface User {
  id: string;
  email: string;
  is_active: boolean;
  created_at: string;
  updated_at: string;
  archived_at?: string | null;
}

export interface Me {
  user: User;
  permissions: Permission[];
}
