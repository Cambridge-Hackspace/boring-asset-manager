import { expect, goTo, test, unique } from './fixtures';

// Hooks (docs/hooks.md): defined once, offered by a category to its
// subcategories' assets, triggered on an asset's page, kept in its history.
test('define a hook, offer it on a category, and trigger it', async ({ signedIn: page, api }) => {
  const label = unique('Identify');
  // Made from the label, as field keys are (#171).
  const key = label.toLowerCase().replace(/[^a-z0-9]+/g, '_');
  await goTo(page, 'Hooks');
  await page.getByRole('button', { name: 'New hook' }).click();
  const form = page.getByRole('form', { name: 'New hook' });
  await form.getByLabel('Label').fill(label);
  await expect(form.getByText(`Devices listen for ${key}`)).toBeVisible();
  await form.getByLabel('Ask first (optional)').fill('Light up its drawer?');
  await form.getByRole('button', { name: 'Save hook' }).click();
  await expect(page.getByText(`Saved ${label}.`)).toBeVisible();

  const cabinet = await api.call('POST', '/categories', { name: unique('Cabinet') });
  const drawers = await api.call('POST', '/categories', { name: unique('Drawers'), parent_id: cabinet.category.id });
  await page.goto(`/categories/${cabinet.category.id}`);
  await page.getByRole('navigation', { name: 'Parts of this category' }).getByRole('link', { name: 'Links and hooks' }).click();
  const section = page.getByRole('region', { name: 'Hooks' });
  await section.getByRole('checkbox', { name: label }).check();
  await expect(section.getByRole('checkbox', { name: 'On its subcategories’ assets too' })).toBeChecked();
  await section.getByRole('button', { name: 'Save hooks' }).click();
  await expect(page.getByText('Hooks saved.')).toBeVisible();

  const asset = await api.call('POST', '/assets', { name: unique('Resistors'), category_ids: [drawers.category.id] });
  await page.goto(`/assets/${asset.asset.asset.id}`);
  const hooks = page.getByRole('region', { name: 'Hooks' });
  await hooks.getByRole('button', { name: label }).click();
  await hooks.getByRole('group', { name: 'Light up its drawer?' }).getByRole('button', { name: label }).click();
  await expect(hooks.getByText(`${label}: sent.`)).toBeVisible();
  await page.reload();
  await expect(page.getByText(`${label} triggered`)).toBeVisible();
});

// Hooks were actions (#173): the old addresses of the page and its help
// topic lead to where they are now.
test('the addresses of actions lead to hooks', async ({ signedIn: page }) => {
  await page.goto('/actions');
  await expect(page).toHaveURL(/\/hooks$/);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Hooks');
  await page.goto('/help/actions');
  await expect(page).toHaveURL(/\/help\/hooks$/);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Hooks');
});
