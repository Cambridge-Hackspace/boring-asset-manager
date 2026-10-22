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

test('a label scanned signed out asks to sign in, and the Scan page looks codes up', async ({ signedIn: page, api }) => {
  const code = unique('DRILL').toUpperCase();
  const name = unique('Drill');
  await api.call('POST', '/assets', { name, generator_keys: [{ generator_type: 'qr', assigned_code: code }] });

  // Not public: signing in, from the page, comes back to it.
  await page.context().clearCookies();
  await page.goto(`/scan/${code}`);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Sign in to see this asset');
  await page.getByRole('main').getByRole('link', { name: 'Sign in' }).click();
  await page.getByLabel('Email').fill(ADMIN_EMAIL);
  await page.getByLabel('Password').fill(ADMIN_PASSWORD);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(name);

  // The Scan page, from the menu: a code typed, or a scan link pasted.
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Scan', exact: true }).click();
  await page.getByLabel('Code').fill(code);
  await page.getByRole('button', { name: 'Look up' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(name);
  await page.goto('/scan');
  await page.getByLabel('Code').fill(`https://bam.example.org/scan/${code}`);
  await page.getByLabel('Code').press('Enter');
  await expect(page).toHaveURL(new RegExp(`/scan/${code}$`));
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(name);
});

test('scanning a spool adds another like it, and scanning a bin puts something in it', async ({ signedIn: page, api }) => {
  const { report } = await api.call('POST', '/reports', {
    name: unique('Adding'),
    kind: 'scan',
    spec: { sections: [{ type: 'actions', actions: ['open', 'add_similar', 'add_inside'] }] },
  });
  const { category } = await api.call('POST', '/categories', { name: unique('Spools') });
  await api.call('PUT', `/categories/${category.id}/scan-template`, { scan_template_id: report.id });
  const name = unique('Yellow PLA');
  const code = unique('SPOOL').toUpperCase();
  const { asset } = await api.call('POST', '/assets', { name, category_ids: [category.id], generator_keys: [{ generator_type: 'code128', assigned_code: code }] });

  await page.goto(`/scan/${code}`);
  await page.getByRole('link', { name: 'Add another like it' }).click();
  await expect(page.getByRole('heading', { name: 'New asset like this one' })).toBeVisible();
  await expect(page.getByLabel('Name')).toHaveValue(name);
  await expect(page.getByRole('checkbox', { name: category.name })).toBeChecked();

  await page.goto(`/scan/${code}`);
  await page.getByRole('link', { name: 'Put something in it' }).click();
  const sample = unique('Test print');
  await page.getByLabel('Name').fill(sample);
  await page.getByRole('button', { name: 'Create asset' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(sample);
  await expect(page.locator('dt', { hasText: /^Where$/ }).locator('xpath=following-sibling::dd[1]')).toContainText(asset.asset.name);
});
