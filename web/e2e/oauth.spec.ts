import AxeBuilder from '@axe-core/playwright';
import { createHash } from 'node:crypto';
import { expect, test, unique } from './fixtures';

// An assistant signs a person in by OAuth: it registers, the person
// approves it on the consent page, and its code buys a token acting as them.

const pkce = (verifier: string) => createHash('sha256').update(verifier).digest('base64url');

test('approve an assistant signing in, and it acts as you', async ({ signedIn: page }) => {
  const cb = 'http://127.0.0.1:4711/callback';
  const name = unique('Claude');
  const register = await page.request.post('/oauth/register', { data: { client_name: name, redirect_uris: [cb] } });
  expect(register.status()).toBe(201);
  const { client_id } = await register.json();
  const verifier = 'v'.repeat(50);
  const ask = new URLSearchParams({ response_type: 'code', client_id, redirect_uri: cb, code_challenge: pkce(verifier), code_challenge_method: 'S256', state: 'xyz' });

  // The callback is the assistant's; stand in for it.
  let back = '';
  await page.route('http://127.0.0.1:4711/**', async (route) => {
    back = route.request().url();
    await route.fulfill({ status: 200, body: 'signed in' });
  });
  await page.goto('/oauth/authorize?' + ask.toString());
  const consent = page.getByRole('region', { name: `${name} wants to work with BAM! as you` });
  await expect(consent).toBeVisible();
  await expect(consent).toContainText('127.0.0.1:4711');
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations.map((v) => v.id)).toEqual([]);
  await consent.getByRole('button', { name: 'Allow' }).click();
  await expect.poll(() => back).toContain('code=');
  const code = new URL(back).searchParams.get('code')!;
  expect(new URL(back).searchParams.get('state')).toBe('xyz');

  const token = await page.request.post('/oauth/token', {
    form: { grant_type: 'authorization_code', code, redirect_uri: cb, client_id, code_verifier: verifier },
  });
  expect(token.status()).toBe(200);
  const { access_token } = await token.json();
  const whoami = await page.request.post('/api/v1/mcp', {
    headers: { Authorization: `Bearer ${access_token}`, Accept: 'application/json, text/event-stream' },
    data: { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name: 'whoami', arguments: {} } },
  });
  expect(await whoami.text()).toContain('through their personal token');

  // It is listed among the person's assistants, signed in.
  await page.goto('/account');
  await expect(page.getByRole('region', { name: 'Assistants' }).getByText(name, { exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Assistants' })).toContainText('Signed in');
});
