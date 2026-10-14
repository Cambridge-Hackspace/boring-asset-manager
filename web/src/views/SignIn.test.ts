import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import SignIn from './SignIn.svelte';
import { navigate, router } from '../lib/router.svelte';

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

afterEach(() => {
  cleanup();
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
    const me = { user: { id: 'u1', email: 'ada@hackspace.org', is_active: true, created_at: '', updated_at: '' }, permissions: [] };
    const fetchMock = vi.fn().mockResolvedValueOnce(json(200, { user: me.user })).mockResolvedValueOnce(json(200, me));
    vi.stubGlobal('fetch', fetchMock);
    render(SignIn);
    await fireEvent.input(screen.getByLabelText('Email'), { target: { value: 'ada@hackspace.org' } });
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'right' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));
    await vi.waitFor(() => expect(router.path).toBe('/assets'));
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/auth/login');
  });
});
