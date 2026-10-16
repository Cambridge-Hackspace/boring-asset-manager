import { ADMIN_EMAIL, expect, goTo, test, unique } from './fixtures';

test('count stock by quantity: use it up, see it run low, restock', async ({ signedIn: page }) => {
  const name = unique('M3 screws');
  await goTo(page, 'Assets');
  await page.getByRole('link', { name: 'New asset' }).click();
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByRole('checkbox', { name: 'Count stock by quantity' }).check();
  await page.getByLabel('Quantity', { exact: true }).fill('200');
  await page.getByLabel('Unit (optional)').fill('pcs');
  await page.getByLabel('Low stock threshold (optional)').fill('50');
  await page.getByRole('button', { name: 'Create asset' }).click();
  await expect(page).toHaveURL(/\/assets\/[0-9a-f-]{36}$/);
  const assetUrl = page.url();

  const stock = page.getByRole('region', { name: 'Stock' });
  await expect(stock).toContainText('In stock. 200 pcs left; flagged automatically below 50 pcs.');
  await stock.getByLabel('How many (pcs)').fill('160');
  await stock.getByRole('button', { name: 'Use' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Used' })).toHaveText('Used 160 pcs.');
  await expect(stock).toContainText('Running low. 40 pcs left');
  const history = page.getByRole('region', { name: 'History' });
  await expect(history.getByRole('listitem').first()).toContainText('Running low: below the threshold of 50 pcs');
  await expect(history).toContainText('Used 160 pcs; 40 pcs left');
  await expect(history).toContainText('Started counting at 200 pcs');

  // Using more than there is changes nothing.
  await stock.getByLabel('How many (pcs)').fill('41');
  await stock.getByRole('button', { name: 'Use' }).click();
  await expect(page.getByRole('alert')).toContainText('not enough stock: 40 left');
  await expect(stock).toContainText('40 pcs left');

  // It is on the home page and in the list.
  await page.goto('/');
  await expect(page.getByRole('region', { name: 'Running low' }).getByRole('link', { name })).toBeVisible();
  await goTo(page, 'Assets');
  await page.getByLabel('Search').fill(name);
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(page.getByRole('row', { name: new RegExp(name) })).toContainText('40 pcs');

  // Someone uses some while the edit form is open; saving keeps their change.
  await page.goto(assetUrl + '/edit');
  await expect(page.getByLabel('Quantity', { exact: true })).toHaveValue('40');
  await page.context().request.post(new URL(assetUrl).pathname.replace('/assets/', '/api/v1/assets/') + '/quantity', { data: { adjust: -5 } });
  await page.getByLabel('Unit (optional)').fill('screws');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page).toHaveURL(assetUrl);
  await expect(stock).toContainText('35 screws left');
  // The edit is in the history with what it changed, and so is the creation.
  const edit = history.getByRole('listitem').filter({ hasText: /^Edited/ });
  await expect(edit).toContainText('Unit: pcs → screws');
  await expect(edit).toContainText(ADMIN_EMAIL);
  await expect(history.getByRole('listitem').filter({ hasText: /^Created/ })).toContainText(`Name: ${name}`);

  await stock.getByLabel('How many (screws)').fill('100');
  await stock.getByRole('button', { name: 'Add' }).click();
  await expect(stock).toContainText('In stock. 135 screws left');
  await expect(history.getByRole('listitem').first()).toContainText('Restocked: back to the threshold of 50 screws or more');
});

test('report a broken tool, find it needing attention, and mark it working', async ({ signedIn: page, api }) => {
  const name = unique('Pillar drill');
  const { asset } = await api.call('POST', '/assets', { name });
  await page.goto(`/assets/${asset.asset.id}`);
  await page.getByRole('button', { name: 'Report a problem' }).click();
  await page.getByRole('radio', { name: /Out of service/ }).check();
  await page.getByLabel('What is wrong?').fill('Chuck slips');
  await page.getByRole('button', { name: 'Report', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Reported: out of service.');
  const history = page.getByRole('region', { name: 'History' });
  await expect(history.getByRole('listitem').first()).toContainText('Reported out of service: “Chuck slips”');

  // It is on the home page and in the list's filter.
  await page.goto('/');
  await expect(page.getByRole('region', { name: 'Needs attention' })).toContainText(`${name} Out of service Chuck slips`);
  await goTo(page, 'Assets');
  await page.getByLabel('Condition').selectOption({ label: 'Needs attention' });
  await expect(page).toHaveURL(/condition=problem/);
  await expect(page.getByRole('row', { name: new RegExp(name) })).toContainText('Out of service');

  await page.getByRole('link', { name }).click();
  await page.getByRole('button', { name: 'Mark as working' }).click();
  await expect(page.getByRole('status')).toHaveText('Marked as working.');
  await expect(history.getByRole('listitem').first()).toContainText('Marked as working');
  await expect(page.getByText('Out of service', { exact: true })).toHaveCount(0);
});
