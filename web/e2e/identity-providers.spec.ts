import { expect, goTo, signIn, test, unique } from './fixtures';

// Signing in through identity providers (docs/access.md): the stack offers
// the test protocol, which signs in as whoever its provider's settings name
// (tools/web/e2e.sh turns it on). Each test makes its own provider.

/** Makes a test provider through the API, answering it. */
async function provider(api: { call: (m: string, p: string, b?: unknown) => Promise<any> }, settings: Record<string, string>, extra: object = {}) {
  const key = unique('idp').toLowerCase().replace(/[^a-z0-9]/g, '');
  const name = `Hackspace ${key}`;
  const { provider } = await api.call('POST', '/auth/providers', { key, name, protocol: 'test', enabled: true, settings, ...extra });
  return provider as { id: string; key: string; name: string; button_label: string };
}

test('an account is made on first sign-in through a provider, with roles from its claims', async ({ api, browser }) => {
  const email = `${unique('ada')}@hackspace.org`.toLowerCase();
  const { role } = await api.call('POST', '/roles', { name: unique('Woodshop'), description: '', permissions: ['assets.read'] });
  const p = await provider(api, { subject: unique('sub'), email, email_verified: 'yes', name: 'Ada Provisioned', groups: 'woodshop' }, {
    provision: true,
    rules: [{ claim: 'groups', value: 'woodshop', role_id: role.id, scope_ids: [] }],
  });

  const context = await browser.newContext();
  const page = await context.newPage();
  await page.goto('/sign-in?next=%2Faccount');
  await page.getByRole('link', { name: p.button_label }).click();
  await expect(page).toHaveURL(/\/account$/);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Your account');
  await expect(page.getByRole('region', { name: 'Signing in elsewhere' })).toContainText(p.name);
  await context.close();

  const { users } = await api.call('GET', '/users');
  const made = users.find((u: { email: string }) => u.email === email);
  expect(made.name).toBe('Ada Provisioned');
  expect(made.role_ids).toContain(role.id);
});

test('a provider that does not know you says so on the sign-in page', async ({ api, browser }) => {
  const p = await provider(api, { subject: unique('nobody'), email: 'nobody@elsewhere.org', email_verified: 'yes' });
  // api signs the test's own page in, so a stranger needs a browser of their own.
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.goto('/sign-in');
  await page.getByRole('link', { name: p.button_label }).click();
  await expect(page).toHaveURL(/\/sign-in/);
  await expect(page.getByRole('alert')).toHaveText('You have no account here. Ask whoever manages users to make one.');
  await context.close();
});

test('link a provider under Your account, then sign in through it', async ({ api, browser }) => {
  const email = `${unique('bob')}@hackspace.org`.toLowerCase();
  await api.call('POST', '/users', { email, name: unique('Bob'), password: 'BobLinks123!' });
  const p = await provider(api, { subject: unique('bob-at-idp') });

  const context = await browser.newContext();
  const page = await context.newPage();
  await signIn(page, email, 'BobLinks123!');
  await page.goto('/account');
  const section = page.getByRole('region', { name: 'Signing in elsewhere' });
  await section.getByRole('link', { name: `Link ${p.name}` }).click();
  await expect(section.getByRole('status')).toHaveText(`Linked your ${p.name} account.`);

  await context.clearCookies();
  await page.goto('/sign-in');
  await page.getByRole('link', { name: p.button_label }).click();
  await expect(page).not.toHaveURL(/\/sign-in/);
  const me = await (await page.request.get('/api/v1/auth/me')).json();
  expect(me.user.email).toBe(email);
  await context.close();
});

test('with passwords off, the sign-in page has only buttons, and the break-glass page still signs an admin in', async ({ api, signedIn: page, browser }) => {
  const p = await provider(api, { subject: unique('s') });
  await goTo(page, 'Sign-in');
  await expect(page.getByRole('heading', { name: p.name })).toBeVisible();
  const passwords = page.getByLabel('People can sign in with a password');
  await passwords.uncheck();
  await expect(page.getByRole('status')).toHaveText('Signing in with a password is off.');
  try {
    const context = await browser.newContext();
    const other = await context.newPage();
    await other.goto('/sign-in');
    await expect(other.getByRole('link', { name: p.button_label })).toBeVisible();
    await expect(other.getByLabel('Password')).toHaveCount(0);

    await other.goto('/signin/local');
    await other.getByLabel('Email').fill(process.env.E2E_EMAIL ?? 'ada@hackspace.org');
    await other.getByLabel('Password').fill(process.env.E2E_PASSWORD ?? 'Pass1234!');
    await other.getByRole('button', { name: 'Sign in' }).click();
    await expect(other).not.toHaveURL(/\/signin\/local/);
    await context.close();
  } finally {
    await api.call('PUT', '/auth/settings', { local_passwords: true });
  }
});
