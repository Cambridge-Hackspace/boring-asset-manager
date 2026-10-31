import { expect, goTo, test, unique } from './fixtures';

test('fields and categories, with inheritance', async ({ signedIn: page }) => {
  const label = unique('Condition');
  const key = label.toLowerCase().replace(/[^a-z0-9]+/g, '_');

  await goTo(page, 'Fields');
  await page.getByRole('button', { name: 'New field' }).click();
  await page.getByLabel('Label', { exact: true }).fill(label);
  await expect(page.getByLabel('Key', { exact: true })).toHaveValue(key);
  await page.getByLabel('Type').selectOption('select');
  await page.getByLabel('Choice 1 label').fill('Good');
  await page.getByLabel('Choice 1 value').fill('good');
  await page.getByRole('button', { name: 'Add choice' }).click();
  await page.getByLabel('Choice 2 label').fill('Broken');
  await page.getByLabel('Choice 2 value').fill('broken');
  await page.getByRole('button', { name: 'Create field' }).click();
  await expect(page.getByRole('cell', { name: key, exact: true })).toBeVisible();

  const parent = unique('Tools');
  await goTo(page, 'Categories');
  await page.getByRole('link', { name: 'New category' }).click();
  await page.getByLabel('Name', { exact: true }).fill(parent);
  await page.getByRole('button', { name: 'Create category' }).click();
  await expect(page).toHaveURL(/\/categories\/[0-9a-f-]{36}$/);
  await page.getByRole('navigation', { name: 'Parts of this category' }).getByRole('link', { name: 'Fields' }).click();
  await page.getByLabel('Add a field').selectOption({ label });
  await page.getByLabel('Required').check();
  await page.getByRole('button', { name: 'Add field' }).click();
  await expect(page.getByRole('cell', { name: 'This category', exact: true })).toBeVisible();

  await page.getByRole('navigation', { name: 'Parts of this category' }).getByRole('link', { name: 'About' }).click();
  await page.getByRole('link', { name: 'New subcategory' }).click();
  await expect(page.getByLabel('Parent')).toHaveValue(/[0-9a-f-]{36}/);
  await page.getByLabel('Name', { exact: true }).fill('Drills');
  await page.getByRole('button', { name: 'Create category' }).click();
  await page.getByRole('navigation', { name: 'Parts of this category' }).getByRole('link', { name: 'Fields' }).click();
  const row = page.getByRole('row').filter({ hasText: label });
  await expect(row).toContainText('Required');
  await expect(row).toContainText(`Inherited from ${parent}`);
  await expect(row.getByRole('button', { name: 'Remove' })).toHaveCount(0);

  // Running low only for consumables.
  await page.getByRole('navigation', { name: 'Parts of this category' }).getByRole('link', { name: 'Stock and lending' }).click();
  await expect(page.getByLabel('Running low')).toBeDisabled();
  await page.getByLabel('Consumable').check();
  await expect(page.getByLabel('Running low')).toBeEnabled();
});

test('duplicate a category: its settings and fields, not its subcategories or assets', async ({ signedIn: page, api }) => {
  const name = unique('Filament');
  const { category } = await api.call('POST', '/categories', { name, color_hex: '#ffcc00', is_consumable: true });
  await api.call('POST', '/categories', { name: unique('PLA'), parent_id: category.id });
  const { field } = await api.call('POST', '/fields', { key: unique('spool_weight').toLowerCase().replace(/[^a-z0-9]+/g, '_'), label: 'Spool weight', validation_type: 'number' });
  await api.call('POST', `/categories/${category.id}/fields`, { field_id: field.id, is_required: true });
  await api.call('POST', '/assets', { name: unique('Yellow spool'), category_ids: [category.id], field_values: { [field.key]: '1000' } });

  await page.goto(`/categories/${category.id}`);
  await page.getByRole('button', { name: 'Duplicate' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(`${name} copy`);
  await expect(page).not.toHaveURL(new RegExp(category.id));
  await expect(page.getByRole('link', { name: 'New subcategory' })).toBeVisible();
  await page.getByRole('navigation', { name: 'Parts of this category' }).getByRole('link', { name: 'Stock and lending' }).click();
  await expect(page.getByLabel('Consumable')).toBeChecked();
  await page.getByRole('navigation', { name: 'Parts of this category' }).getByRole('link', { name: 'Fields' }).click();
  await expect(page.getByRole('region', { name: 'Fields' }).getByText('Spool weight')).toBeVisible();
  // Nothing in it, and nothing under it.
  await expect(page.getByText('No assets in it yet.')).toBeVisible();
});
