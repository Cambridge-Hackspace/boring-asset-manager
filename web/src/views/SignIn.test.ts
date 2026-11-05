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

// A browser with passkeys, whose one answers with bytes 7 to 18.
function stubPasskeys() {
  const bytes = (...b: number[]) => new Uint8Array(b).buffer;
  const get = vi.fn().mockResolvedValue({
    rawId: bytes(7, 8, 9),
    response: { clientDataJSON: bytes(10, 11, 12), authenticatorData: bytes(13, 14, 15), signature: bytes(16, 17, 18) },
  });
  vi.stubGlobal('PublicKeyCredential', class {});
  Object.defineProperty(navigator, 'credentials', { value: { get, create: vi.fn() }, configurable: true });
  return get;
}

afterEach(() => {
  cleanup();
  Object.defineProperty(navigator, 'credentials', { value: undefined, configurable: true });
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
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(json(200, { factors: ['totp'] }))
      .mockResolvedValueOnce(json(200, { user: me.user }))
      .mockResolvedValueOnce(json(200, me));
    stubFetch(fetchMock);
    render(SignIn);
    const code = await screen.findByLabelText('Code from your authenticator app');
    expect(screen.getByText(/for your account now/)).toBeTruthy();
    // Which factors can finish it is asked for: the provider did not say.
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/auth/login/factor');
    expect(fetchMock.mock.calls[0]![1].method).toBe('GET');
    await fireEvent.input(code, { target: { value: '123456' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));
    await vi.waitFor(() => expect(router.path).toBe('/assets'));
    expect(fetchMock.mock.calls[1]![0]).toBe('/api/v1/auth/login/factor');
  });

  it('signs in with a passkey, the browser answering the challenge', async () => {
    navigate('/sign-in?next=%2Fassets');
    const get = stubPasskeys();
    const me = { user: { id: 'u1', email: 'ada@hackspace.org', name: '', is_active: true, created_at: '', updated_at: '' }, permissions: [] };
    const options = { challenge: 'AQID', rpId: 'bam.example.org', allowCredentials: [{ type: 'public-key', id: 'BAUG' }] };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(json(200, { factor_required: true, factors: ['passkey'] }))
      .mockResolvedValueOnce(json(200, { public_key: options }))
      .mockResolvedValueOnce(json(200, { user: me.user }))
      .mockResolvedValueOnce(json(200, me));
    stubFetch(fetchMock);
    render(SignIn);
    await fireEvent.input(screen.getByLabelText('Email'), { target: { value: 'ada@hackspace.org' } });
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'right' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));

    // With a passkey alone, there is no code to type.
    const use = await screen.findByRole('button', { name: 'Use a passkey' });
    expect(screen.queryByLabelText('Code from your authenticator app')).toBeNull();
    await fireEvent.click(use);
    await vi.waitFor(() => expect(router.path).toBe('/assets'));
    expect(fetchMock.mock.calls[1]![0]).toBe('/api/v1/auth/login/factor/passkey');
    const asked = get.mock.calls[0]![0].publicKey;
    expect(new Uint8Array(asked.challenge)).toEqual(new Uint8Array([1, 2, 3]));
    expect(new Uint8Array(asked.allowCredentials[0].id)).toEqual(new Uint8Array([4, 5, 6]));
    expect(JSON.parse(fetchMock.mock.calls[2]![1].body)).toEqual({
      passkey: { id: 'BwgJ', client_data_json: 'CgsM', authenticator_data: 'DQ4P', signature: 'EBES' },
    });
  });

  it('says why a passkey did not answer, and offers a recovery code', async () => {
    const get = stubPasskeys();
    get.mockRejectedValueOnce(new DOMException('cancelled', 'NotAllowedError'));
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(json(200, { factor_required: true, factors: ['passkey'] }))
      .mockResolvedValueOnce(json(200, { public_key: { challenge: 'AQID' } }));
    stubFetch(fetchMock);
    render(SignIn);
    await fireEvent.input(screen.getByLabelText('Email'), { target: { value: 'ada@hackspace.org' } });
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'right' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));
    await fireEvent.click(await screen.findByRole('button', { name: 'Use a passkey' }));
    expect((await screen.findByRole('alert')).textContent).toMatch(/cancelled, or took too long/);
    await fireEvent.click(screen.getByRole('button', { name: 'Use a recovery code' }));
    expect(screen.getByLabelText('Recovery code')).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Use a passkey' }));
    expect(screen.queryByLabelText('Recovery code')).toBeNull();
  });

  it('asks for a recovery code where the browser has no passkeys', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(json(200, { factor_required: true, factors: ['passkey'] }));
    stubFetch(fetchMock);
    render(SignIn);
    await fireEvent.input(screen.getByLabelText('Email'), { target: { value: 'ada@hackspace.org' } });
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'right' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByLabelText('Recovery code')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Use a passkey' })).toBeNull();
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

  it('offers Forgot password, and removing a lost second factor by email, only when they are on', async () => {
    stubFetch(vi.fn());
    render(SignIn);
    expect(await screen.findByText(/Ask whoever manages users here to set a new one/)).toBeTruthy();
    cleanup();

    navigate('/sign-in?factor=required', true);
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(json(200, { factors: ['totp'] }))
      .mockResolvedValueOnce(json(202, { message: 'A link to remove your second factor is on its way to ada@hackspace.org.' }));
    stubFetch(fetchMock, { providers: [], local_passwords: true, password_reset: true, factor_recovery_email: true });
    render(SignIn);
    await screen.findByLabelText('Code from your authenticator app');
    await fireEvent.click(await screen.findByRole('button', { name: 'Email me a link to remove my second factor' }));
    expect((await screen.findByRole('status')).textContent).toContain('is on its way');
    expect(fetchMock.mock.calls[1]![0]).toBe('/api/v1/auth/login/factor/recover');
    cleanup();

    navigate('/sign-in', true);
    stubFetch(vi.fn(), { providers: [], local_passwords: true, password_reset: true });
    render(SignIn);
    expect((await screen.findByRole('link', { name: 'Forgot password' })).getAttribute('href')).toBe('/reset-password');
  });
});
