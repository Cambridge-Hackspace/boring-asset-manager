import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import SignIn from './SignIn.svelte';
import { navigate, router } from '../lib/router.svelte';
import { session } from '../lib/session.svelte';

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

afterEach(() => {
  cleanup();
  // Signed in by one test is not signed in for the next.
  session.user = null;
  vi.unstubAllGlobals();
});

describe('SignIn', () => {
  it('shows the server error for bad credentials', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json(401, { error: 'Invalid email or password' })));
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
    vi.stubGlobal('fetch', fetchMock);
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
    vi.stubGlobal('fetch', fetchMock);
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

  it('starts again from the password when the second step has expired', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(json(200, { factor_required: true, factors: ['totp'] }))
      .mockResolvedValueOnce(json(401, { error: 'The sign-in has expired. Sign in again.' }));
    vi.stubGlobal('fetch', fetchMock);
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
