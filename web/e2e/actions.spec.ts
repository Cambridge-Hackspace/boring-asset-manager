import { expect, goTo, test, unique } from './fixtures';

// Actions (README §5.4): defined once, offered by a category to its
// subcategories' assets, pressed on an asset's page, kept in its history.
test('define an action, offer it on a category, and press it', async ({ signedIn: page, api }) => {
  const label = unique('Identify');
  const key = label.toLowerCase();
  await goTo(page, 'Actions');
  await page.getByRole('button', { name: 'New action' }).click();
  const form = page.getByRole('form', { name: 'New action' });
  await form.getByLabel('Label').fill(label);
  await form.getByLabel('Key').fill(key);
  await form.getByLabel('Ask first (optional)').fill('Light up its drawer?');
  await form.getByRole('button', { name: 'Save action' }).click();
  await expect(page.getByText(`Saved ${label}.`)).toBeVisible();

  const cabinet = await api.call('POST', '/categories', { name: unique('Cabinet') });
  const drawers = await api.call('POST', '/categories', { name: unique('Drawers'), parent_id: cabinet.category.id });
  await page.goto(`/categories/${cabinet.category.id}`);
  await page.getByRole('navigation', { name: 'Parts of this category' }).getByRole('link', { name: 'Links and actions' }).click();
  const section = page.getByRole('region', { name: 'Actions' });
  await section.getByRole('checkbox', { name: label }).check();
  await expect(section.getByRole('checkbox', { name: 'On its subcategories’ assets too' })).toBeChecked();
  await section.getByRole('button', { name: 'Save actions' }).click();
  await expect(page.getByText('Actions saved.')).toBeVisible();

  const asset = await api.call('POST', '/assets', { name: unique('Resistors'), category_ids: [drawers.category.id] });
  await page.goto(`/assets/${asset.asset.asset.id}`);
  const actions = page.getByRole('region', { name: 'Actions' });
  await actions.getByRole('button', { name: label }).click();
  await actions.getByRole('group', { name: 'Light up its drawer?' }).getByRole('button', { name: label }).click();
  await expect(actions.getByText(`${label}: sent.`)).toBeVisible();
  await page.reload();
  await expect(page.getByText(`${label} pressed`)).toBeVisible();
});
