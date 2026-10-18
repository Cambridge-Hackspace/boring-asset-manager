import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import SetupChecklist from './SetupChecklist.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';

beforeEach(() => {
  localStorage.clear();
  session.user = { id: 'u1', email: 'ada@hackspace.org', name: '' } as typeof session.user;
  session.permissions = ['users.manage', 'settings.manage', 'categories.manage'];
});
afterEach(() => {
  cleanup();
  session.user = null;
  session.permissions = [];
});

const setUp = (printers: number, channels: number) =>
  fakeApi({
    'GET /printers': () => ({ printers: Array.from({ length: printers }, (_, i) => ({ id: `p${i}` })) }),
    'GET /categories': () => ({ categories: [{ id: 'c' }] }),
    'GET /roles': () => ({ roles: [{ id: 'a', protected: true, public: false }, { id: 'p', protected: false, public: true }] }),
    'GET /users': () => ({ users: [{ id: 'u1' }] }),
    'GET /scopes': () => ({ scopes: [{ name: 'public', category_count: 0 }] }),
    'GET /notifiers': () => ({ channels: Array.from({ length: channels }, (_, i) => ({ id: `n${i}` })) }),
  });

describe('SetupChecklist', () => {
  it('ticks what is done, links the rest, and can be hidden', async () => {
    setUp(1, 0);
    render(SetupChecklist);
    expect(await screen.findByRole('heading', { name: 'Setting up' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Give yourself a name, which history shows' }).getAttribute('href')).toBe('/account');
    expect(screen.queryByRole('link', { name: 'Add a label printer' })).toBeNull();
    expect(screen.getByText('Add a label printer')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Create a role for members, saying what they may do' }).getAttribute('href')).toBe('/roles');
    expect(screen.getByRole('link', { name: 'Add the people who use it' })).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Hide' }));
    expect(screen.queryByRole('heading', { name: 'Setting up' })).toBeNull();
    expect(localStorage.getItem('bam.setup-hidden')).toBe('1');
  });

  it('goes once everything but the optional steps is done', async () => {
    session.user = { id: 'u1', email: 'ada@hackspace.org', name: 'Ada' } as typeof session.user;
    session.permissions = ['settings.manage', 'categories.manage'];
    setUp(1, 0);
    render(SetupChecklist);
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByRole('heading', { name: 'Setting up' })).toBeNull();
  });
});
