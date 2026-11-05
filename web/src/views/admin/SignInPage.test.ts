import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import SignInPage from './SignInPage.svelte';
import IdentitiesSection from '../IdentitiesSection.svelte';
import { fakeApi } from '../../test/fetch';
import { navigate } from '../../lib/router.svelte';
import type { Provider } from '../../lib/models';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const protocols = [
  {
    name: 'oidc',
    label: 'OpenID Connect',
    fields: [
      { name: 'issuer', label: 'Issuer', required: true, secret: false },
      { name: 'client_secret', label: 'Client secret', required: true, secret: true },
    ],
  },
];
const roles = [
  { id: 'r-members', name: 'Members' },
  { id: 'r-admins', name: 'Admins' },
];
const provider = (over: Partial<Provider> = {}): Provider => ({
  id: 'p1',
  key: 'hackspace',
  name: 'Hackspace',
  protocol: 'oidc',
  settings: { issuer: 'https://id.example' },
  secrets_set: { client_secret: true },
  callback_url: 'https://bam.example/api/v1/auth/providers/hackspace/callback',
  button_label: 'Sign in with Hackspace',
  icon: '',
  enabled: true,
  position: 0,
  provision: false,
  default_role_ids: [],
  allowed_domains: [],
  allowed_groups: [],
  link_by_email: false,
  trust_email: false,
  mfa_amr: ['mfa', 'otp', 'hwk', 'swk', 'fpt'],
  mfa_acr: [],
  mfa_fallback: false,
  mfa_always_local: false,
  from_config: false,
  rules: [],
  ...over,
});

describe('SignInPage', () => {
  it('says which host passkeys are bound to, and how many were added at another', async () => {
    fakeApi({
      'GET /auth/providers': () => ({
        providers: [],
        protocols,
        roles,
        scopes: [],
        local_passwords: true,
        secret_key: true,
        passkey_host: 'bam.example.org',
        passkeys_elsewhere: 2,
      }),
    });
    render(SignInPage);
    const section = await screen.findByRole('region', { name: 'Passkeys' });
    expect(await within(section).findByText('bam.example.org')).toBeTruthy();
    expect(within(section).getByRole('status').textContent).toMatch(/2 passkeys were added at another host/);
  });

  it('adds a provider with its settings, secret and a rule', async () => {
    let list: Provider[] = [];
    const { sent } = fakeApi({
      'GET /auth/providers': () => ({ providers: list, protocols, roles, scopes: [], local_passwords: true, secret_key: true }),
      'POST /auth/providers': (body) => {
        list = [provider()];
        return { status: 201, body: { provider: { ...provider(), ...(body as object) } } };
      },
    });
    render(SignInPage, { params: {} });
    await fireEvent.click(await screen.findByRole('button', { name: 'New provider' }));
    const form = screen.getByRole('form', { name: 'New identity provider' });
    await fireEvent.input(within(form).getByLabelText('Name'), { target: { value: 'Hackspace' } });
    await fireEvent.input(within(form).getByLabelText('Issuer'), { target: { value: 'https://id.example' } });
    await fireEvent.input(within(form).getByLabelText('Client secret'), { target: { value: 's3cret' } });
    await fireEvent.click(within(form).getByLabelText(/Make an account for someone/));
    await fireEvent.click(within(form).getByLabelText('Members'));
    await fireEvent.click(within(form).getByRole('button', { name: 'Add a rule' }));
    await fireEvent.input(within(form).getByLabelText('Holds'), { target: { value: 'makerspace-admins' } });
    await fireEvent.change(within(form).getByLabelText('Gives the role'), { target: { value: 'r-admins' } });
    await fireEvent.input(within(form).getByLabelText(/How they signed in/), { target: { value: 'mfa, hwk' } });
    await fireEvent.click(within(form).getByLabelText(/When it does not say so/));
    await fireEvent.click(within(form).getByRole('button', { name: 'Add provider' }));

    expect(await screen.findByText('Provider saved.')).toBeTruthy();
    expect(sent.find((r) => r.path === '/auth/providers' && r.method === 'POST')!.body).toMatchObject({
      key: 'hackspace',
      name: 'Hackspace',
      protocol: 'oidc',
      settings: { issuer: 'https://id.example' },
      secrets: { client_secret: 's3cret' },
      provision: true,
      default_role_ids: ['r-members'],
      rules: [{ claim: 'groups', value: 'makerspace-admins', role_id: 'r-admins', scope_ids: [] }],
      mfa_amr: ['mfa', 'hwk'],
      mfa_acr: [],
      mfa_fallback: true,
      mfa_always_local: false,
    });
    expect(await screen.findByRole('heading', { name: 'Hackspace' })).toBeTruthy();
  });

  it('keeps a stored secret when left blank, and shows where the provider sends people back', async () => {
    const { sent } = fakeApi({
      'GET /auth/providers': () => ({ providers: [provider()], protocols, roles, scopes: [], local_passwords: true, secret_key: true }),
      'PUT /auth/providers/p1': () => ({ provider: provider() }),
    });
    render(SignInPage, { params: {} });
    await fireEvent.click(await screen.findByRole('button', { name: 'Edit Hackspace' }));
    const form = screen.getByRole('form', { name: 'Edit Hackspace' });
    expect(within(form).getByText('https://bam.example/api/v1/auth/providers/hackspace/callback')).toBeTruthy();
    const secret = within(form).getByLabelText('Client secret') as HTMLInputElement;
    expect(secret.placeholder).toBe('Set; leave blank to keep it');
    expect(secret.required).toBe(false);
    await fireEvent.click(within(form).getByRole('button', { name: 'Save provider' }));
    await screen.findByText('Provider saved.');
    expect((sent.find((r) => r.method === 'PUT')!.body as { secrets: object }).secrets).toEqual({});
  });

  it('turns passwords off, and shows a provider from the configuration read-only', async () => {
    const { sent } = fakeApi({
      'GET /auth/providers': () => ({ providers: [provider({ from_config: true })], protocols, roles, scopes: [], local_passwords: true, secret_key: true }),
      'PUT /auth/settings': () => ({ local_passwords: false }),
    });
    render(SignInPage, { params: {} });
    expect(await screen.findByText('Set in the configuration file: change it there.')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Edit Hackspace' })).toBeNull();
    await fireEvent.click(screen.getByLabelText('People can sign in with a password'));
    expect(await screen.findByText('Signing in with a password is off.')).toBeTruthy();
    expect(sent.find((r) => r.path === '/auth/settings')!.body).toEqual({ local_passwords: false });
  });

  it('turns account emails on, only once links can be emailed', async () => {
    let mails = { password_reset: false, factor_recovery_email: false, factor_recovery_email_required: false, can_email_links: false };
    const { sent } = fakeApi({
      'GET /auth/providers': () => ({ providers: [], protocols, roles, scopes: [], local_passwords: true, secret_key: true, account_emails: mails }),
      'PUT /auth/settings': (body) => {
        mails = { ...mails, ...(body as object) };
        return { local_passwords: true, account_emails: mails };
      },
    });
    render(SignInPage, { params: {} });
    const reset = (await screen.findByLabelText(/Offer Forgot password/)) as HTMLInputElement;
    expect(reset.disabled).toBe(true);
    expect(screen.getByText(/Until then, invitations cannot be sent/)).toBeTruthy();
    cleanup();

    mails.can_email_links = true;
    render(SignInPage, { params: {} });
    await fireEvent.click(await screen.findByLabelText(/Offer Forgot password/));
    expect(await screen.findByText('Forgot password is offered.')).toBeTruthy();
    await fireEvent.click(screen.getByLabelText(/remove it by email/));
    await fireEvent.click(await screen.findByLabelText(/Also for those whose roles require/));
    const puts = sent.filter((r) => r.path === '/auth/settings').map((r) => r.body);
    expect(puts).toEqual([{ password_reset: true }, { factor_recovery_email: true }, { factor_recovery_email_required: true }]);
  });
});

describe('IdentitiesSection', () => {
  it('lists, links and unlinks one’s identities', async () => {
    navigate('/account?linked=Hackspace', true);
    let identities = [
      { id: 'i1', user_id: 'u1', provider_id: 'p1', provider_key: 'hackspace', provider_name: 'Hackspace', subject: 's', email: 'ada@hackspace.org', created_at: '' },
    ];
    const { sent } = fakeApi({
      'GET /auth/me/identities': () => ({ identities }),
      'GET /auth/providers/public': () => ({
        providers: [
          { key: 'hackspace', label: 'Sign in with Hackspace' },
          { key: 'github', label: 'Sign in with GitHub' },
        ],
        local_passwords: true,
      }),
      'DELETE /auth/me/identities/i1': () => {
        identities = [];
        return { status: 204 };
      },
    });
    render(IdentitiesSection);
    expect(await screen.findByText('Linked your Hackspace account.')).toBeTruthy();
    expect(screen.getByText('Hackspace')).toBeTruthy();
    // Only the providers not linked yet offer a link.
    expect(screen.getByRole('link', { name: 'Link GitHub' }).getAttribute('href')).toBe('/api/v1/auth/providers/github/start?link=1');
    expect(screen.queryByRole('link', { name: 'Link Hackspace' })).toBeNull();
    await fireEvent.click(screen.getByRole('button', { name: 'Unlink' }));
    await fireEvent.click(await screen.findByRole('button', { name: /^(Unlink|Yes|Confirm)/ }));
    expect(await screen.findByText('Unlinked your Hackspace account.')).toBeTruthy();
    expect(sent.some((r) => r.method === 'DELETE' && r.path === '/auth/me/identities/i1')).toBe(true);
    navigate('/', true);
  });
});
