import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import AccountLinkPage from './AccountLinkPage.svelte';
import { fakeApi } from '../test/fetch';
import { navigate, router } from '../lib/router.svelte';
import { session } from '../lib/session.svelte';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  session.user = null;
  navigate('/sign-in', true);
});

describe('AccountLinkPage', () => {
  it('accepts an invitation with a password, signing in, or offers the providers', async () => {
    navigate('/invite?token=t1', true);
    const ada = { id: 'u1', email: 'ada@test.org', name: 'Ada', is_active: true, created_at: '', updated_at: '' };
    const { sent } = fakeApi({
      'GET /auth/invite': () => ({ email: 'ada@test.org', name: 'Ada', local_passwords: true, providers: [{ key: 'hackspace', label: 'Sign in with Hackspace' }] }),
      'POST /auth/invite': () => ({ user: ada }),
      'GET /auth/me': () => ({ user: ada, permissions: [] }),
    });
    render(AccountLinkPage);
    expect(await screen.findByText(/Welcome, Ada/)).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Sign in with Hackspace' }).getAttribute('href')).toBe('/api/v1/auth/providers/hackspace/start?invite=t1');
    await fireEvent.input(screen.getByLabelText('New password'), { target: { value: 'AdaSecret123!' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Set it and sign in' }));
    await vi.waitFor(() => expect(router.path).toBe('/'));
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ token: 't1', password: 'AdaSecret123!' });
    expect(session.user?.email).toBe('ada@test.org');
  });

  it('says when a link no longer works', async () => {
    navigate('/invite?token=old', true);
    fakeApi({ 'GET /auth/invite': () => ({ status: 404, body: { error: 'This invitation has expired, or was used already.' } }) });
    render(AccountLinkPage);
    expect((await screen.findByRole('alert')).textContent).toContain('has expired');
    expect(screen.queryByLabelText('New password')).toBeNull();
  });

  it('asks for a reset link, and sets a new password with one', async () => {
    navigate('/reset-password', true);
    const { sent } = fakeApi({
      'POST /auth/password-reset': () => ({ status: 202, body: { message: 'If that email has an account here, a link is on its way.' } }),
      'GET /auth/password-reset': () => ({ email: 'ada@test.org' }),
      'POST /auth/password-reset/confirm': () => ({ message: 'Your password is set, and you are signed out everywhere.' }),
    });
    render(AccountLinkPage);
    await fireEvent.input(screen.getByLabelText('Email'), { target: { value: 'ada@test.org' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Email me a link' }));
    expect((await screen.findByRole('status')).textContent).toContain('a link is on its way');
    expect(sent[0]!.body).toEqual({ email: 'ada@test.org' });
    cleanup();

    navigate('/reset-password?token=r1', true);
    render(AccountLinkPage);
    await fireEvent.input(await screen.findByLabelText('New password'), { target: { value: 'AdaNew123456!' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Set my password' }));
    expect((await screen.findByRole('status')).textContent).toContain('signed out everywhere');
    expect(screen.getByRole('link', { name: 'Sign in' })).toBeTruthy();
    expect(sent.find((r) => r.path === '/auth/password-reset/confirm')!.body).toEqual({ token: 'r1', password: 'AdaNew123456!' });
  });

  it('removes a lost second factor, warning first what that means', async () => {
    navigate('/recover-factor?token=f1', true);
    const { sent } = fakeApi({
      'GET /auth/factor-recovery': () => ({ email: 'ada@test.org' }),
      'POST /auth/factor-recovery': () => ({ message: 'Your second factor is removed.' }),
    });
    render(AccountLinkPage);
    expect(await screen.findByText(/email is a way around two-factor sign-in/)).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Remove my second factor' }));
    expect((await screen.findByRole('status')).textContent).toContain('removed');
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ token: 'f1' });
  });
});
