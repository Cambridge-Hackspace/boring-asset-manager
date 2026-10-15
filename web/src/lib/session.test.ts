import { afterEach, describe, expect, it, vi } from 'vitest';
import { request } from './api';
import { navigate, router } from './router.svelte';
import { afterSignIn, session, signInPath } from './session.svelte';

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

const me = {
  user: { id: 'u1', email: 'ada@hackspace.org', name: '', is_active: true, created_at: '', updated_at: '' },
  permissions: ['assets:read', 'labels:print'],
};

afterEach(() => vi.unstubAllGlobals());

describe('sign-in redirects', () => {
  it('only returns to local paths', () => {
    expect(afterSignIn('/assets/1?x=2')).toBe('/assets/1?x=2');
    expect(afterSignIn(null)).toBe('/');
    expect(afterSignIn('https://evil.example/')).toBe('/');
    expect(afterSignIn('//evil.example/')).toBe('/');
    expect(afterSignIn('/sign-in?next=/x')).toBe('/');
    expect(signInPath('/')).toBe('/sign-in');
    expect(signInPath('/assets?search=a b')).toBe('/sign-in?next=%2Fassets%3Fsearch%3Da%20b');
  });
});

describe('session', () => {
  it('loads the user and permissions', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json(200, me)));
    await session.load();
    expect(session.loaded).toBe(true);
    expect(session.user?.email).toBe('ada@hackspace.org');
    expect(session.can('assets:read')).toBe(true);
    expect(session.can('users:manage')).toBe(false);
    expect(session.canAny('users:manage', 'labels:print')).toBe(true);
  });

  it('treats 401 as signed out', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json(401, { error: 'Authentication required' })));
    await session.load();
    expect(session.loaded).toBe(true);
    expect(session.user).toBeNull();
    expect(session.permissions).toEqual([]);
  });

  it('sends the user to sign in when a request finds the session gone', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json(200, me)));
    await session.load();
    navigate('/assets?search=drill');

    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json(401, { error: 'Authentication required' })));
    await request('GET', '/assets').catch(() => {});
    expect(session.user).toBeNull();
    expect(router.path).toBe('/sign-in');
    expect(router.query.get('next')).toBe('/assets?search=drill');
  });
});
