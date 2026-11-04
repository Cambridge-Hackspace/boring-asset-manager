import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import FactorSetupPage from './FactorSetupPage.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  session.factorSetup = false;
  session.user = null;
});

const app = { id: 'f1', kind: 'totp', created_at: '2026-11-01T00:00:00Z', confirmed_at: '2026-11-01T00:00:00Z' };
const ada = { id: 'u1', email: 'ada@test.org', name: 'Ada', is_active: true, created_at: '', updated_at: '' };

describe('FactorSetupPage', () => {
  it('has someone whose role requires it set up an app, and lets them in once the codes are put away', async () => {
    let on = false;
    fakeApi({
      'GET /auth/me/factors': () => ({ factors: on ? [app] : [], available: true, recovery_codes_left: on ? 10 : 0 }),
      'POST /auth/me/factors/totp': () => ({ qr_svg: '<svg/>', secret: 'ABCD EFGH' }),
      'POST /auth/me/factors/totp/confirm': () => {
        on = true;
        return { factor: app, recovery_codes: ['4f8kq-29xmd'] };
      },
      'GET /auth/me': () => ({ user: ada, permissions: ['assets.read'], factor_setup_required: !on }),
    });
    session.user = ada as never;
    session.factorSetup = true;
    render(FactorSetupPage);
    expect(screen.getByRole('heading', { name: 'Set up two-factor sign-in', level: 1 })).toBeTruthy();
    expect(screen.getByText(/A role you hold requires signing in with a second factor/)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Sign out' })).toBeTruthy();

    await fireEvent.click(await screen.findByRole('button', { name: 'Set up an authenticator app' }));
    await fireEvent.input(await screen.findByLabelText('Code from the app'), { target: { value: '123456' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Turn on' }));
    await screen.findByRole('heading', { name: 'Your recovery codes' });
    // Held until the codes are put away, so they are not lost.
    expect(session.factorSetup).toBe(true);
    await fireEvent.click(screen.getByRole('button', { name: 'I have saved them' }));
    await vi.waitFor(() => expect(session.factorSetup).toBe(false));
  });
});
