import { expect, test, unique } from './fixtures';

test('choose people for a person field, add someone without an account, and find what is assigned to me', async ({ signedIn: page, api }) => {
  const tag = unique('pf').replace(/-/g, '_');
  const { field } = await api.call('POST', '/fields', { key: `keeper_${tag}`, label: `Keeper ${tag}`, validation_type: 'person', options: { multiple: true } });
  const category = unique('Machines');
  const { category: cat } = await api.call('POST', '/categories', { name: category, color_hex: '#fbbf24', is_consumable: false, is_running_low: false });
  await api.call('POST', `/categories/${cat.id}/fields`, { field_id: field.id, is_required: false });
  const me = (await api.call('GET', '/auth/me')).user;
  const { asset } = await api.call('POST', '/assets', { name: unique('Lathe'), category_ids: [cat.id] });
  const id = asset.asset.id;
  const newcomer = unique('Zed');
  const known = unique('Yan');
  await api.call('POST', '/people', { name: known });

  // Choosing: typing a name matching no one offers to add them.
  await page.goto(`/assets/${id}/edit`);
  const picker = page.getByRole('combobox', { name: `Keeper ${tag}` });
  await picker.fill(newcomer);
  await page.getByRole('option', { name: `Add “${newcomer}” as a person without an account` }).click();
  await expect(page.getByText(`${newcomer} (no account)`)).toBeVisible();
  // And typing part of someone's name offers them.
  await picker.fill(known.slice(0, 8));
  await page.getByRole('option', { name: `${known} (no account)`, exact: true }).click();
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/assets/${id}$`));

  // The asset names them, never by email.
  const value = page.getByRole('definition').filter({ hasText: newcomer });
  await expect(value).toHaveText(`${newcomer} (no account), ${known} (no account)`);
  await expect(page.locator('main')).not.toContainText(me.email);

  // Named in it, the asset is found by Assigned to: me.
  await api.call('PUT', `/assets/${id}`, { field_values: { [`keeper_${tag}`]: me.id } });
  // Assigned to: me finds it.
  await page.goto('/assets');
  await page.getByLabel('Assigned to', { exact: true }).selectOption('me');
  await expect(page.getByRole('link', { name: asset.asset.name, exact: true })).toBeVisible();

  // The Users page lists the newcomer as unable to sign in.
  await page.goto('/users');
  await page.getByRole('searchbox', { name: 'Find someone' }).fill(newcomer);
  const row = page.getByRole('row').filter({ hasText: newcomer });
  await expect(row).toContainText('No account: cannot sign in');
});
