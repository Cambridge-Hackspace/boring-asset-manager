import { expect, signIn, test, unique, ADMIN_EMAIL, ADMIN_PASSWORD } from './fixtures';

test('scanning a bin: anyone sees what is in it, and those allowed use some', async ({ signedIn: page, api }) => {
  const { scopes } = await api.call('GET', '/scopes');
  const publicScope = (scopes as { id: string; name: string }[]).find((s) => s.name === 'public')!;
  const { category } = await api.call('POST', '/categories', { name: unique('Shelves'), color_hex: '#1d4ed8' });
  await api.call('PUT', `/categories/${category.id}/scopes`, { scope_ids: [publicScope.id] });
  const code = unique('BIN').toUpperCase();
  const { asset: bin } = await api.call('POST', '/assets', { name: unique('Bin'), category_ids: [category.id], generator_keys: [{ generator_type: 'code128', assigned_code: code }] });
  const screws = unique('Screws');
  const { asset: inside } = await api.call('POST', '/assets', { name: screws, category_ids: [category.id], stock: { quantity: 40, unit: 'pcs' } });
  const { relation_types: types } = await api.call('GET', '/relation-types');
  const contains = (types as { id: string; name: string }[]).find((t) => t.name === 'contains')!;
  await api.call('POST', `/assets/${bin.asset.id}/links`, { type_id: contains.id, asset_id: inside.asset.id, outgoing: true });

  // Signed out, by the code on its label: what it holds, and nothing to do.
  await page.context().clearCookies();
  await page.goto(`/scan/${code}`);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(bin.asset.name);
  await expect(page.getByRole('region', { name: 'In it' })).toContainText(screws);
  await expect(page.getByRole('button')).toHaveCount(0);

  // Signed in, the screws' own scan page uses some.
  await signIn(page, ADMIN_EMAIL, ADMIN_PASSWORD);
  await page.goto(`/scan/${inside.asset.id}`);
  await page.getByLabel('How many (pcs)').fill('5');
  await page.getByRole('button', { name: 'Use' }).click();
  await expect(page.getByRole('status')).toHaveText('Used 5 pcs.');
  await expect(page.getByText('35 pcs').first()).toBeVisible();
  await page.getByRole('link', { name: 'Open its page' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(screws);
});
