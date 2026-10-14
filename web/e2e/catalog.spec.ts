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
  await page.getByLabel('Name').fill(parent);
  await page.getByRole('button', { name: 'Create category' }).click();
  await expect(page).toHaveURL(/\/categories\/[0-9a-f-]{36}$/);
  await page.getByLabel('Add a field').selectOption({ label });
  await page.getByLabel('Required').check();
  await page.getByRole('button', { name: 'Add field' }).click();
  await expect(page.getByRole('cell', { name: 'This category', exact: true })).toBeVisible();

  await page.getByRole('link', { name: 'Add subcategory' }).click();
  await expect(page.getByLabel('Parent')).toHaveValue(/[0-9a-f-]{36}/);
  await page.getByLabel('Name').fill('Drills');
  await page.getByRole('button', { name: 'Create category' }).click();
  const row = page.getByRole('row').filter({ hasText: label });
  await expect(row).toContainText('Required');
  await expect(row).toContainText(`Inherited from ${parent}`);
  await expect(row.getByRole('button', { name: 'Remove' })).toHaveCount(0);

  // Running low only for consumables.
  await expect(page.getByLabel('Running low')).toBeDisabled();
  await page.getByLabel('Consumable').check();
  await expect(page.getByLabel('Running low')).toBeEnabled();
});
