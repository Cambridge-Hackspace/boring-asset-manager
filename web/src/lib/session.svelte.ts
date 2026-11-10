// The signed-in user and what they may do.

import { ApiError, request, setUnauthorizedHandler } from './api';
import type { Me, Permission, PermissionKey, User } from './models';
import type { PasskeyAssertion } from './passkeys';
import { offline } from './offline/offline.svelte';
import { navigate, router } from './router.svelte';
import { appearance } from './theme.svelte';

class Session {
  user = $state<User | null>(null);
  permissions = $state<PermissionKey[]>([]);
  /** The names of the roles the signed-in user holds. */
  roles = $state<string[]>([]);
  /** The scopes of each role narrowed to some, by role name. */
  roleScopes = $state<Record<string, string[]>>({});
  /** A role they hold requires a second factor and they have none: nothing opens until they set one up. */
  factorSetup = $state(false);
  /** False until the first /auth/me answer, so the app does not flash sign-in. */
  loaded = $state(false);
  /**
   * Opened with no connection, from the offline copy: who is signed in is
   * as the copy was made for, and only pages that work from it open
   * (docs/installing.md#offline).
   */
  offline = $state(false);

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
      this.offline = false;
    } catch (err) {
      if (!(err instanceof ApiError) || err.status !== 401) throw err;
      this.clear();
    } finally {
      this.loaded = true;
    }
  }

  /**
   * Signs in with a password, or, local, at the break-glass page. With a
   * second factor, that is the first step of two: it answers the kinds of
   * factor that can finish it, and finishSignIn takes one; otherwise null.
   */
  async signIn(email: string, password: string, local = false): Promise<string[] | null> {
    const res = await request<{ factor_required?: boolean; factors?: string[] }>('POST', local ? '/auth/login/local' : '/auth/login', {
      email,
      password,
    });
    if (res?.factor_required) return res.factors ?? [];
    await this.load();
    return null;
  }

  /** The second step of signing in: a code from their authenticator app, or a recovery code, or a passkey's answer. */
  async finishSignIn(proof: string | PasskeyAssertion): Promise<void> {
    await request('POST', '/auth/login/factor', typeof proof === 'string' ? { code: proof } : { passkey: proof });
    await this.load();
  }

  async signOut(): Promise<void> {
    try {
      await request('POST', '/auth/logout');
    } finally {
      // What the app kept for them goes with them, changes not yet sent
      // too: the sign-out button says so first.
      await offline.clear(true);
      this.clear();
      navigate('/sign-in');
      // Signed out, the public role still holds: public pages stay open.
      void this.load().catch(() => {});
    }
  }

  /** Who the offline copy was made for, opened with no connection. */
  adoptOffline(me: Me): void {
    this.set(me);
    this.offline = true;
    this.loaded = true;
  }

  /** Who is signed in, as /auth/me gave it. */
  get me(): Me {
    return { user: this.user, permissions: this.permissions, roles: this.roles, role_scopes: this.roleScopes };
  }

  private set(me: Me): void {
    // Signed in, the theme on their profile wins over this browser's.
    if (me.user?.theme) appearance.adopt(me.user.theme);
    this.user = me.user;
    this.permissions = me.permissions ?? [];
    this.roles = me.roles ?? [];
    this.roleScopes = me.role_scopes ?? {};
    this.factorSetup = !!me.factor_setup_required;
  }

  private clear(): void {
    this.user = null;
    this.permissions = [];
    this.roles = [];
    this.roleScopes = {};
    this.factorSetup = false;
  }
}

export const session = new Session();

// A 401 on any request means the session ended: sign in again and come back.
setUnauthorizedHandler(() => {
  const wasSignedIn = session.user !== null;
  // The session ended: the copy goes, and their waiting changes wait for
  // them to sign in again.
  void offline.clear(false);
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
