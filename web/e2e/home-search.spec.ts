import { expect, test, unique } from './fixtures';

test('the home page searches, and / goes to it', async ({ signedIn: page, api }) => {
  const { category } = await api.call('POST', '/categories', { name: unique('Drills'), color_hex: '#1d4ed8' });
  const name = unique('Cordless drill');
  await api.call('POST', '/assets', { name, category_ids: [category.id] });

  await page.goto('/');
  const field = page.getByRole('searchbox', { name: 'Search' });
  // It doesn't take focus by itself; / goes to it.
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  await expect(field).not.toBeFocused();
  await page.keyboard.press('/');
  await expect(field).toBeFocused();

  await field.fill(name);
  await field.press('Enter');
  await expect(page).toHaveURL(/\/assets\?search=/);
  await expect(page.getByRole('link', { name })).toBeVisible();
  await expect(page.getByLabel('Search', { exact: true })).toHaveValue(name);
});
