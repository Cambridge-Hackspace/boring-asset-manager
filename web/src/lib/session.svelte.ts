// The signed-in user and what they may do.

import { ApiError, request, setUnauthorizedHandler } from './api';
import type { Me, Permission, PermissionKey, User } from './models';
import { navigate, router } from './router.svelte';

class Session {
  user = $state<User | null>(null);
  permissions = $state<PermissionKey[]>([]);
  /** The names of the roles the signed-in user holds. */
  roles = $state<string[]>([]);
  /** The scopes of each role narrowed to some, by role name. */
  roleScopes = $state<Record<string, string[]>>({});
  /** False until the first /auth/me answer, so the app does not flash sign-in. */
  loaded = $state(false);

  /** Holds the permission in every scope, or in at least one: enough to show what it opens. */
  can(permission: Permission): boolean {
    return this.permissions.some((k) => k === permission || k.endsWith(':' + permission));
  }

  canAny(...permissions: Permission[]): boolean {
    return permissions.some((p) => this.can(p));
  }

  /** Holds the permission for an asset in these scopes. */
  canOn(scopes: string[] | undefined, permission: Permission): boolean {
    return this.permissions.includes(permission) || (scopes ?? []).some((s) => this.permissions.includes(`${s}:${permission}`));
  }

  canAnyOn(scopes: string[] | undefined, ...permissions: Permission[]): boolean {
    return permissions.some((p) => this.canOn(scopes, p));
  }

  async load(): Promise<void> {
    try {
      this.set(await request<Me>('GET', '/auth/me'));
    } catch (err) {
      if (!(err instanceof ApiError) || err.status !== 401) throw err;
      this.clear();
    } finally {
      this.loaded = true;
    }
  }

  async signIn(email: string, password: string): Promise<void> {
    await request('POST', '/auth/login', { email, password });
    await this.load();
  }

  async signOut(): Promise<void> {
    try {
      await request('POST', '/auth/logout');
    } finally {
      this.clear();
      navigate('/sign-in');
      // Signed out, the public role still holds: public pages stay open.
      void this.load().catch(() => {});
    }
  }

  private set(me: Me): void {
    this.user = me.user;
    this.permissions = me.permissions ?? [];
    this.roles = me.roles ?? [];
    this.roleScopes = me.role_scopes ?? {};
  }

  private clear(): void {
    this.user = null;
    this.permissions = [];
    this.roles = [];
    this.roleScopes = {};
  }
}

export const session = new Session();

// A 401 on any request means the session ended: sign in again and come back.
setUnauthorizedHandler(() => {
  const wasSignedIn = session.user !== null;
  session.user = null;
  session.permissions = [];
  if (wasSignedIn && router.path !== '/sign-in') {
    navigate(signInPath(router.path + router.search), true);
  }
});

/** The sign-in page, returning to next afterward. */
export function signInPath(next: string): string {
  return next && next !== '/' ? '/sign-in?next=' + encodeURIComponent(next) : '/sign-in';
}

/** Where to go after signing in: next if it is a local path, else home. */
export function afterSignIn(next: string | null): string {
  return next && next.startsWith('/') && !next.startsWith('//') && !next.startsWith('/sign-in') ? next : '/';
}
