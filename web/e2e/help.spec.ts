import { expect, goTo, test } from './fixtures';

test('each page opens its own help, and anyone can read it', async ({ signedIn: page }) => {
  await goTo(page, 'Assets');
  await page.getByRole('link', { name: 'Help', exact: true }).click();
  await expect(page).toHaveURL(/\/help\/assets$/);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Assets');
  await page.getByRole('navigation', { name: 'Help topics' }).getByRole('link', { name: 'Scanning' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Scanning');

  // Signed out too.
  await page.context().clearCookies();
  await page.goto('/help/people');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('People and permissions');
  await expect(page.getByRole('link', { name: 'Sign in' })).toBeVisible();
});
