import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import TwoFactorSection from './TwoFactorSection.svelte';
import { fakeApi } from '../test/fetch';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const app = { id: 'f1', kind: 'totp', created_at: '2026-11-01T00:00:00Z', confirmed_at: '2026-11-01T00:00:00Z' };
const codes = ['4f8kq-29xmd', 'a1b2c-d3e4f', 'zz9yy-8xx7w', '00000-11111', '22222-33333', '44444-55555', '66666-77777', '88888-99999', 'abcde-fghjk', 'mnpqr-stvwx'];

describe('TwoFactorSection', () => {
  it('shows the recovery codes once, as the app is turned on', async () => {
    let on = false;
    fakeApi({
      'GET /auth/me/factors': () => ({ factors: on ? [app] : [], available: true, recovery_codes_left: on ? 10 : 0 }),
      'POST /auth/me/factors/totp': () => ({ qr_svg: '<svg/>', secret: 'ABCD EFGH' }),
      'POST /auth/me/factors/totp/confirm': () => {
        on = true;
        return { factor: app, recovery_codes: codes };
      },
    });
    render(TwoFactorSection);
    await fireEvent.click(await screen.findByRole('button', { name: 'Set up an authenticator app' }));
    await fireEvent.input(await screen.findByLabelText('Code from the app'), { target: { value: '123456' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Turn on' }));

    const heading = await screen.findByRole('heading', { name: 'Your recovery codes' });
    await vi.waitFor(() => expect(document.activeElement).toBe(heading));
    const list = screen.getByRole('list', { name: 'Recovery codes' });
    expect(within(list).getAllByRole('listitem').map((li) => li.textContent)).toEqual(codes);
    const download = screen.getByRole('link', { name: 'Download' });
    expect(download.getAttribute('download')).toBe('bam-recovery-codes.txt');
    expect(decodeURIComponent(download.getAttribute('href')!)).toContain(codes.join('\n'));

    // Put away, they are gone for good; the count stays.
    await fireEvent.click(screen.getByRole('button', { name: 'I have saved them' }));
    expect(screen.queryByText('4f8kq-29xmd')).toBeNull();
    expect(screen.getByText(/10 left/)).toBeTruthy();
    expect(screen.queryByText(/Few are left/)).toBeNull();
  });

  it('warns when few are left, and makes new ones with a code from the app', async () => {
    let left = 2;
    const { sent } = fakeApi({
      'GET /auth/me/factors': () => ({ factors: [app], available: true, recovery_codes_left: left }),
      'POST /auth/me/factors/recovery': () => {
        left = 10;
        return { recovery_codes: codes };
      },
    });
    render(TwoFactorSection);
    expect(await screen.findByText(/Few are left: make new ones/)).toBeTruthy();
    expect(screen.getByText(/2 left/)).toBeTruthy();

    await fireEvent.click(screen.getByRole('button', { name: 'Make new recovery codes…' }));
    await fireEvent.input(screen.getByLabelText('A code from the app'), { target: { value: '654321' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Make new codes' }));
    expect(await screen.findByRole('heading', { name: 'Your recovery codes' })).toBeTruthy();
    expect(sent.find((r) => r.path === '/auth/me/factors/recovery')!.body).toEqual({ code: '654321' });
    await fireEvent.click(screen.getByRole('button', { name: 'I have saved them' }));
    expect(screen.getByText(/10 left/)).toBeTruthy();
    expect(screen.queryByText(/Few are left/)).toBeNull();
  });

  it('says when none are left', async () => {
    fakeApi({ 'GET /auth/me/factors': () => ({ factors: [app], available: true, recovery_codes_left: 0 }) });
    render(TwoFactorSection);
    expect(await screen.findByText(/None are left: make new ones/)).toBeTruthy();
  });
});
