import { expect, signIn, test, unique } from './fixtures';

test('star an asset and a report, find them on the home page and in the list, and unstar one', async ({ signedIn: page, api }) => {
  const { category } = await api.call('POST', '/categories', { name: unique('Saws'), color_hex: '#0ea5e9', is_consumable: false, is_lendable: false });
  const saw = unique('Bandsaw');
  const { asset } = await api.call('POST', '/assets', { name: saw, category_ids: [category.id] });
  const file = unique('File');
  await api.call('POST', '/assets', { name: file, category_ids: [category.id] });
  const reportName = unique('Saw report');
  await api.call('POST', '/reports', { name: reportName, spec: { sections: [{ type: 'table', columns: [{ heading: 'Asset', binding: '{{asset.name}}' }] }] } });

  // On the asset's page.
  await page.goto(`/assets/${asset.asset.id}`);
  const star = page.getByRole('button', { name: `Favorite ${saw}` });
  await expect(star).toHaveAttribute('aria-pressed', 'false');
  await star.click();
  await expect(star).toHaveAttribute('aria-pressed', 'true');

  // On a report's row in the list.
  await page.goto('/reports');
  await page.getByRole('button', { name: `Favorite ${reportName}` }).click();
  await expect(page.getByRole('button', { name: `Favorite ${reportName}` })).toHaveAttribute('aria-pressed', 'true');

  // The home page has both, by kind.
  await page.goto('/');
  const card = page.getByRole('region', { name: 'Favorites' });
  await expect(card.getByRole('link', { name: saw })).toHaveAttribute('href', `/assets/${asset.asset.id}`);
  await expect(card.getByRole('link', { name: reportName })).toBeVisible();

  // The list's filter keeps the starred asset, not its neighbor.
  await page.goto(`/assets?category=${category.id}`);
  await expect(page.getByRole('link', { name: file })).toBeVisible();
  await page.getByLabel('Favorites').selectOption({ label: 'Only favorites' });
  await expect(page).toHaveURL(/favorite=true/);
  await expect(page.getByRole('link', { name: saw })).toBeVisible();
  await expect(page.getByRole('link', { name: file })).toHaveCount(0);

  // Unstarred from the list, it leaves the home page.
  await page.getByRole('button', { name: `Favorite ${saw}` }).click();
  await expect(page.getByRole('button', { name: `Favorite ${saw}` })).toHaveAttribute('aria-pressed', 'false');
  await page.goto('/');
  await expect(page.getByRole('region', { name: 'Favorites' }).getByRole('link', { name: reportName })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Favorites' }).getByRole('link', { name: saw })).toHaveCount(0);
});

test("favorites are each person's own", async ({ signedIn: page, api }) => {
  const { category } = await api.call('POST', '/categories', { name: unique('Clamps'), color_hex: '#0ea5e9', is_consumable: false, is_lendable: false });
  const clamp = unique('Clamp');
  const { asset } = await api.call('POST', '/assets', { name: clamp, category_ids: [category.id] });
  await api.call('PUT', `/auth/me/favorites/asset/${asset.asset.id}`);
  const { role } = await api.call('POST', '/roles', { name: unique('Readers'), description: '', permissions: ['assets.read'] });
  const email = `${unique('reader')}@hackspace.org`;
  const { user } = await api.call('POST', '/users', { email, name: unique('Reader'), password: 'ReaderPass123!' });
  await api.call('PUT', `/users/${user.id}/roles`, { role_ids: [role.id] });

  await page.context().clearCookies();
  await signIn(page, email, 'ReaderPass123!');
  const card = page.getByRole('region', { name: 'Favorites' });
  await expect(card).toContainText('Star an asset, category, report or flow on its page to find it here.');
  await page.goto(`/assets/${asset.asset.id}`);
  await expect(page.getByRole('button', { name: `Favorite ${clamp}` })).toHaveAttribute('aria-pressed', 'false');
});
