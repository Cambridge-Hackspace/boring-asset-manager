import type { Page } from '@playwright/test';
import { expect, signIn, test, unique } from './fixtures';

// Light or dark (#168): chosen in the footer, over the system's, and kept in
// the browser and on the person's profile.

const background = (page: Page) => page.evaluate(() => getComputedStyle(document.body).backgroundColor);
const paper = 'rgb(250, 247, 240)';
const ink = 'rgb(11, 11, 10)';

test('chooses dark on a light system, and keeps it on the profile for another device', async ({ signedIn: admin, api, browser }) => {
  const email = `${unique('theme')}@hackspace.org`;
  await api.call('POST', '/users', { email, name: 'Dee Dark', password: 'DarkPass123!' });
  await admin.close();

  const laptop = await browser.newContext({ colorScheme: 'light' });
  const page = await laptop.newPage();
  await signIn(page, email, 'DarkPass123!');
  const appearance = page.getByRole('group', { name: 'Appearance' });
  await expect(appearance.getByRole('button', { name: 'System' })).toHaveAttribute('aria-pressed', 'true');
  expect(await background(page)).toBe(paper);

  await appearance.getByRole('button', { name: 'Dark' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect.poll(() => background(page)).toBe(ink);
  await page.reload();
  await expect(page.getByRole('group', { name: 'Appearance' }).getByRole('button', { name: 'Dark' })).toHaveAttribute('aria-pressed', 'true');
  expect(await background(page)).toBe(ink);

  // Another device, light too, takes it from the profile on signing in.
  const phone = await browser.newContext({ colorScheme: 'light' });
  const other = await phone.newPage();
  await signIn(other, email, 'DarkPass123!');
  await expect(other.locator('html')).toHaveAttribute('data-theme', 'dark');
  expect(await background(other)).toBe(ink);

  // Back to the system's, which is light.
  await other.getByRole('group', { name: 'Appearance' }).getByRole('button', { name: 'System' }).click();
  await expect(other.locator('html')).not.toHaveAttribute('data-theme');
  await expect.poll(() => background(other)).toBe(paper);
  await laptop.close();
  await phone.close();
});

test('chooses light on a dark system, signed out, in this browser alone', async ({ browser }) => {
  const context = await browser.newContext({ colorScheme: 'dark' });
  const page = await context.newPage();
  await page.goto('/sign-in');
  expect(await background(page)).toBe(ink);
  await page.getByRole('group', { name: 'Appearance' }).getByRole('button', { name: 'Light' }).click();
  await expect.poll(() => background(page)).toBe(paper);
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  expect(await background(page)).toBe(paper);
  await context.close();
});
