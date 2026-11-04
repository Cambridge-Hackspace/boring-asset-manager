import { createHash } from 'node:crypto';
import type { Browser, Page } from '@playwright/test';
import { expect, test, unique } from './fixtures';

// Signing in through OpenID Connect (docs/access.md#openid-connect): the
// stack serves a test provider at /test-oidc (tools/web/e2e.sh turns it on),
// whose sign-in form signs in as whoever is typed into it.

type Api = { call: (m: string, p: string, b?: unknown) => Promise<any> };

/** Saves an OpenID Connect provider for the test provider, answering it. */
async function oidcProvider(api: Api, baseURL: string) {
  const key = unique('oidc').toLowerCase().replace(/[^a-z0-9]/g, '');
  const { provider } = await api.call('POST', '/auth/providers', {
    key,
    name: `Hackspace ${key}`,
    protocol: 'oidc',
    enabled: true,
    provision: true,
    settings: { issuer: `${baseURL}/test-oidc`, client_id: 'bam' },
    secrets: { client_secret: 'bam-test-secret' },
  });
  return provider as { button_label: string };
}

/** A browser signed in as no one. */
async function stranger(browser: Browser): Promise<Page> {
  const context = await browser.newContext();
  return context.newPage();
}

/** Clicks the provider's button and signs in at the test provider as who. */
async function signInAt(page: Page, button: string, who: { sub: string; email: string; name: string }) {
  await page.getByRole('link', { name: button }).click();
  await expect(page.getByRole('heading', { name: 'Sign in to the test provider' })).toBeVisible();
  await page.getByLabel('Subject').fill(who.sub);
  await page.getByLabel('Email').fill(who.email);
  await page.getByLabel('Name').fill(who.name);
  await page.getByRole('button', { name: 'Sign in' }).click();
}

function someone() {
  const sub = unique('grace');
  return { sub, email: `${sub}@hackspace.org`.toLowerCase(), name: 'Grace Hopper' };
}

test('sign in through OpenID Connect, to the home page', async ({ api, browser, baseURL }) => {
  const p = await oidcProvider(api, baseURL!);
  const who = someone();
  const page = await stranger(browser);
  await page.goto('/sign-in');
  await signInAt(page, p.button_label, who);
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole('link', { name: who.name })).toHaveAttribute('href', '/account');
  await page.context().close();
});

test('an assistant asking someone signed out to approve it has them sign in through OpenID Connect, then asks', async ({ api, browser, baseURL }) => {
  const p = await oidcProvider(api, baseURL!);
  const page = await stranger(browser);
  const cb = 'http://127.0.0.1:4712/callback';
  const name = unique('Claude');
  const register = await page.request.post('/oauth/register', { data: { client_name: name, redirect_uris: [cb] } });
  const { client_id } = await register.json();
  const challenge = createHash('sha256').update('v'.repeat(50)).digest('base64url');
  const ask = new URLSearchParams({ response_type: 'code', client_id, redirect_uri: cb, code_challenge: challenge, code_challenge_method: 'S256', state: 'xyz' });

  await page.goto('/oauth/authorize?' + ask.toString());
  await expect(page).toHaveURL(/\/sign-in\?next=/);
  await signInAt(page, p.button_label, someone());
  await expect(page).toHaveURL(/\/oauth\/consent\?/);
  await expect(page.getByRole('region', { name: `${name} wants to work with BAM! as you` })).toBeVisible();
  await page.context().close();
});
