import { expect, goTo, test, unique } from './fixtures';

test('weigh filament: a category totals its spools in grams, and one lent comes back lighter', async ({ signedIn: page, api }) => {
  const name = unique('Filament');
  // Made through the form, with a unit.
  await goTo(page, 'Categories');
  await page.getByRole('link', { name: 'New category' }).click();
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByRole('checkbox', { name: 'Consumable' }).check();
  await page.getByLabel('Unit (optional)').fill('g');
  await page.getByLabel('Low stock threshold (optional)').fill('1000');
  await page.getByRole('checkbox', { name: 'Lendable' }).check();
  await page.getByRole('button', { name: 'Create category' }).click();
  await expect(page).toHaveURL(/\/categories\/[0-9a-f-]{36}$/);
  const categoryUrl = page.url();
  const categoryId = categoryUrl.split('/').pop()!;

  const spool = async (label: string, grams: number) =>
    (await api.call('POST', '/assets', { name: unique(label), category_ids: [categoryId], stock: { quantity: grams, unit: 'g' } })).asset.asset.id as string;
  const blue = await spool('Blue PLA', 1000);
  await spool('Red PLA', 500);
  await page.reload();
  const stock = page.getByRole('region', { name: 'Stock' });
  await expect(stock).toContainText('In stock. 1500 g here and in its subcategories, in 2 active assets; flagged automatically below 1000 g.');

  // Lent and back with 200 g left: 700 g in all, below the threshold.
  await page.goto(`/assets/${blue}`);
  const loan = page.getByRole('region', { name: 'Loan' });
  await loan.getByRole('button', { name: 'Borrow it myself' }).click();
  await expect(loan).toContainText('Lent to you since');
  await loan.getByLabel('Left now (g), if weighed').fill('200');
  await loan.getByRole('button', { name: 'Check in' }).click();
  await expect(page.getByRole('region', { name: 'Stock' })).toContainText('200 g left');
  await expect(page.getByRole('region', { name: 'History' }).getByRole('listitem').first()).toContainText('Used 800 g; 200 g left');

  await page.goto(categoryUrl);
  await expect(page.getByRole('region', { name: 'Stock' })).toContainText('Running low. 700 g here');
});
