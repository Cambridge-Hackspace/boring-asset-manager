import { ADMIN_EMAIL, ADMIN_PASSWORD, expect, signIn, test } from './fixtures';

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
  expect(me.status()).toBe(401);
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
