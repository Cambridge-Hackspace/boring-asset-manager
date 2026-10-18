import { ADMIN_EMAIL, ADMIN_PASSWORD, expect, signIn, test, unique } from './fixtures';

test('signed-out visitors sign in and return to the page they asked for', async ({ page }) => {
  await page.goto('/categories?x=1');
  await expect(page).toHaveURL(/\/sign-in\?next=%2Fcategories%3Fx%3D1$/);

  await page.getByLabel('Email').fill(ADMIN_EMAIL);
  await page.getByLabel('Password').fill('not the password');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('alert')).toHaveText('Invalid email or password');

  await page.getByLabel('Password').fill(ADMIN_PASSWORD);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/categories\?x=1$/);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Categories');
});

test('signing out ends the session', async ({ signedIn: page }) => {
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page).toHaveURL(/\/sign-in$/);
  await page.goto('/assets');
  await expect(page).toHaveURL(/\/sign-in\?next=%2Fassets$/);
  const me = await page.request.get('/api/v1/auth/me');
  expect((await me.json()).user).toBeNull();
});

test('the app is served for client routes and refuses unknown API paths', async ({ page }) => {
  const app = await page.request.get('/templates/anything/edit');
  expect(app.status()).toBe(200);
  expect(app.headers()['content-security-policy']).toContain("default-src 'self'");
  const api = await page.request.get('/api/v1/no-such-thing');
  expect(api.status()).toBe(404);
  expect(api.headers()['content-type']).toBe('application/json');
});

test('every page says which build is running, signed in or not', async ({ page }) => {
  // The test server is built from the working tree, not a release.
  await page.goto('/sign-in');
  await expect(page.getByRole('contentinfo')).toContainText('BAM! development build');
  await signIn(page);
  await expect(page.getByRole('contentinfo')).toContainText('BAM! development build');
});

test('signed-out visitors see public assets, and nothing else', async ({ signedIn: page, api }) => {
  const { scopes } = await api.call('GET', '/scopes');
  const publicScope = (scopes as { id: string; name: string }[]).find((s) => s.name === 'public')!;
  const { category } = await api.call('POST', '/categories', { name: unique('Lobby'), color_hex: '#1d4ed8' });
  await api.call('PUT', `/categories/${category.id}/scopes`, { scope_ids: [publicScope.id] });
  const { asset: shown } = await api.call('POST', '/assets', { name: unique('Kiosk'), category_ids: [category.id] });
  const { asset: hidden } = await api.call('POST', '/assets', { name: unique('Safe') });

  await page.context().clearCookies();
  await page.goto(`/assets/${shown.asset.id}`);
  await expect(page.getByRole('heading', { name: shown.asset.name })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Sign in' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Edit' })).toHaveCount(0);
  await expect(page.getByRole('navigation', { name: 'Main' }).getByRole('link')).toHaveCount(0);

  // Asked to sign in, without saying whether it exists; and the catalog
  // asks to sign in too.
  await page.goto(`/assets/${hidden.asset.id}`);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Sign in to see this asset');
  await expect(page.getByText(hidden.asset.name)).toHaveCount(0);
  await page.goto('/assets');
  await expect(page).toHaveURL(/\/sign-in\?next=%2Fassets$/);
});
