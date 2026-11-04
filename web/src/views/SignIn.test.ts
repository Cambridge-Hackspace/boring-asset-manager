import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import SignIn from './SignIn.svelte';
import { navigate, router } from '../lib/router.svelte';
import { session } from '../lib/session.svelte';
import type { PublicProviders } from '../lib/models';

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

// The sign-in page asks for its buttons as it opens; the rest goes to mock.
function stubFetch(mock: (url: string, init?: RequestInit) => Promise<Response>, providers: PublicProviders = { providers: [], local_passwords: true }) {
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: RequestInit) => (url === '/api/v1/auth/providers/public' ? Promise.resolve(json(200, providers)) : mock(url, init))),
  );
}

afterEach(() => {
  cleanup();
  // Signed in by one test is not signed in for the next.
  session.user = null;
  // A query one test leaves, such as ?factor=required, is not the next one's.
  navigate('/sign-in', true);
  vi.unstubAllGlobals();
});

describe('SignIn', () => {
  it('shows the server error for bad credentials', async () => {
    stubFetch(vi.fn().mockResolvedValue(json(401, { error: 'Invalid email or password' })));
    render(SignIn);
    await fireEvent.input(screen.getByLabelText('Email'), { target: { value: 'ada@hackspace.org' } });
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'wrong' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'Invalid email or password');
  });

  it('signs in and returns to the page asked for', async () => {
    navigate('/sign-in?next=%2Fassets');
    const me = { user: { id: 'u1', email: 'ada@hackspace.org', name: '', is_active: true, created_at: '', updated_at: '' }, permissions: [] };
    const fetchMock = vi.fn().mockResolvedValueOnce(json(200, { user: me.user })).mockResolvedValueOnce(json(200, me));
    stubFetch(fetchMock);
    render(SignIn);
    await fireEvent.input(screen.getByLabelText('Email'), { target: { value: 'ada@hackspace.org' } });
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'right' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));
    await vi.waitFor(() => expect(router.path).toBe('/assets'));
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/auth/login');
  });

  it('asks for a code after the password, when there is a second factor', async () => {
    navigate('/sign-in?next=%2Fassets');
    const me = { user: { id: 'u1', email: 'ada@hackspace.org', name: '', is_active: true, created_at: '', updated_at: '' }, permissions: [] };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(json(200, { factor_required: true, factors: ['totp'] }))
      .mockResolvedValueOnce(json(401, { error: 'That code is not right. Enter the code your app shows now.' }))
      .mockResolvedValueOnce(json(200, { user: me.user }))
      .mockResolvedValueOnce(json(200, me));
    stubFetch(fetchMock);
    render(SignIn);
    await fireEvent.input(screen.getByLabelText('Email'), { target: { value: 'ada@hackspace.org' } });
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'right' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));

    const code = await screen.findByLabelText('Code from your authenticator app');
    await vi.waitFor(() => expect(document.activeElement).toBe(code));
    expect(router.path).toBe('/sign-in');
    await fireEvent.input(code, { target: { value: '000000' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'That code is not right. Enter the code your app shows now.');
    // A wrong code is not a lost session: still here, still asked for a code.
    expect(screen.getByLabelText('Code from your authenticator app')).toBeTruthy();

    await fireEvent.input(screen.getByLabelText('Code from your authenticator app'), { target: { value: '123456' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));
    await vi.waitFor(() => expect(router.path).toBe('/assets'));
    expect(fetchMock.mock.calls[2]![0]).toBe('/api/v1/auth/login/factor');
    expect(JSON.parse(fetchMock.mock.calls[2]![1].body)).toEqual({ code: '123456' });
  });

  it('takes a recovery code in place of the app', async () => {
    navigate('/sign-in?next=%2Fassets');
    const me = { user: { id: 'u1', email: 'ada@hackspace.org', name: '', is_active: true, created_at: '', updated_at: '' }, permissions: [] };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(json(200, { factor_required: true, factors: ['totp'] }))
      .mockResolvedValueOnce(json(200, { user: me.user }))
      .mockResolvedValueOnce(json(200, me));
    stubFetch(fetchMock);
    render(SignIn);
    await fireEvent.input(screen.getByLabelText('Email'), { target: { value: 'ada@hackspace.org' } });
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'right' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));

    await fireEvent.click(await screen.findByRole('button', { name: 'Use a recovery code' }));
    const code = screen.getByLabelText('Recovery code');
    await vi.waitFor(() => expect(document.activeElement).toBe(code));
    expect(code.getAttribute('pattern')).toBeNull();
    await fireEvent.input(code, { target: { value: '4f8kq-29xmd' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));
    await vi.waitFor(() => expect(router.path).toBe('/assets'));
    expect(fetchMock.mock.calls[1]![0]).toBe('/api/v1/auth/login/factor');
    expect(JSON.parse(fetchMock.mock.calls[1]![1].body)).toEqual({ code: '4f8kq-29xmd' });
  });

  it('offers the providers, and says why one did not sign in', async () => {
    navigate('/sign-in?next=%2Fassets&error=You+have+no+account+here.');
    stubFetch(vi.fn(), { providers: [{ key: 'hackspace', label: 'Sign in with Hackspace' }], local_passwords: true });
    render(SignIn);
    const button = await screen.findByRole('link', { name: 'Sign in with Hackspace' });
    expect(button.getAttribute('href')).toBe('/api/v1/auth/providers/hackspace/start?next=%2Fassets');
    expect(screen.getByRole('alert').textContent).toBe('You have no account here.');
    expect(screen.getByLabelText('Password')).toBeTruthy();
  });

  it('shows only the providers while passwords are off', async () => {
    navigate('/sign-in');
    stubFetch(vi.fn(), { providers: [{ key: 'hackspace', label: 'Sign in with Hackspace' }], local_passwords: false });
    render(SignIn);
    expect(await screen.findByRole('link', { name: 'Sign in with Hackspace' })).toBeTruthy();
    await vi.waitFor(() => expect(screen.queryByLabelText('Password')).toBeNull());
  });

  it('signs in with a password at the break-glass page while passwords are off', async () => {
    navigate('/signin/local');
    const me = { user: { id: 'u1', email: 'ada@hackspace.org', name: '', is_active: true, created_at: '', updated_at: '' }, permissions: [] };
    const fetchMock = vi.fn().mockResolvedValueOnce(json(200, { user: me.user })).mockResolvedValueOnce(json(200, me));
    stubFetch(fetchMock, { providers: [{ key: 'hackspace', label: 'Sign in with Hackspace' }], local_passwords: false });
    render(SignIn, { local: true });
    expect(await screen.findByText(/This page is for those who manage users/)).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Sign in with Hackspace' })).toBeNull();
    await fireEvent.input(screen.getByLabelText('Email'), { target: { value: 'ada@hackspace.org' } });
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'right' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));
    await vi.waitFor(() => expect(router.path).toBe('/'));
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/auth/login/local');
  });

  it('asks for the code after a provider, for someone with a second factor', async () => {
    navigate('/sign-in?factor=required&next=%2Fassets');
    const me = { user: { id: 'u1', email: 'ada@hackspace.org', name: '', is_active: true, created_at: '', updated_at: '' }, permissions: [] };
    const fetchMock = vi.fn().mockResolvedValueOnce(json(200, { user: me.user })).mockResolvedValueOnce(json(200, me));
    stubFetch(fetchMock);
    render(SignIn);
    const code = await screen.findByLabelText('Code from your authenticator app');
    expect(screen.getByText(/for your account now/)).toBeTruthy();
    await fireEvent.input(code, { target: { value: '123456' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));
    await vi.waitFor(() => expect(router.path).toBe('/assets'));
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/auth/login/factor');
  });

  it('starts again from the password when the second step has expired', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(json(200, { factor_required: true, factors: ['totp'] }))
      .mockResolvedValueOnce(json(401, { error: 'The sign-in has expired. Sign in again.' }));
    stubFetch(fetchMock);
    render(SignIn);
    await fireEvent.input(screen.getByLabelText('Email'), { target: { value: 'ada@hackspace.org' } });
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'right' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));
    await fireEvent.input(await screen.findByLabelText('Code from your authenticator app'), { target: { value: '123456' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'The sign-in has expired. Sign in again.');
    expect(screen.getByLabelText('Password')).toBeTruthy();
  });
});
