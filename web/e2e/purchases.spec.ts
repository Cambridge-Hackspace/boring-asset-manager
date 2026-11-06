import { expect, test, unique } from './fixtures';

test('record where an asset came from, see it on its page and its vendor’s, and leave it out of a copy', async ({ signedIn: page, api }) => {
  const vendorName = unique('Tool shop');
  const { vendor } = await api.call('POST', '/vendors', { name: vendorName });
  const name = unique('Drill press');

  // Made with its purchase details.
  await page.goto('/assets/new');
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByText('Purchase (none yet)').click();
  await page.getByLabel('Vendor', { exact: true }).selectOption({ label: vendorName });
  await page.getByLabel('Ordered').fill('2026-01-05');
  await page.getByLabel('Price', { exact: true }).fill('129.5');
  await page.getByLabel('Currency').fill('GBP');
  await page.getByLabel('Order or invoice number').fill('PO-17');
  await page.getByLabel('Warranty ends').fill('2099-01-09');
  await page.getByRole('button', { name: 'Create asset' }).click();
  await expect(page).toHaveURL(/\/assets\/[0-9a-f-]{36}$/);
  const id = page.url().split('/').pop()!;

  const purchase = page.getByRole('region', { name: 'Purchase' });
  await expect(purchase).toContainText('129.50 GBP');
  await expect(purchase).toContainText('PO-17');
  await expect(purchase).toContainText('Ends 2099-01-09');
  await expect(purchase.getByRole('link', { name: vendorName, exact: true })).toHaveAttribute('href', `/vendors/${vendor.id}`);
  // Its history says so.
  await expect(page.getByRole('region', { name: 'History' })).toContainText('Price: 129.50 GBP');

  // Its vendor's page lists it.
  await page.goto(`/vendors/${vendor.id}`);
  await page.getByRole('navigation', { name: 'Parts of this vendor' }).getByRole('link', { name: 'Assets', exact: true }).click();
  const bought = page.getByRole('region', { name: 'Bought from it' });
  await expect(bought.getByRole('link', { name, exact: true })).toHaveAttribute('href', `/assets/${id}`);
  await expect(bought).toContainText('129.50 GBP');

  // A copy starts with none.
  await page.goto(`/assets/new?like=${id}`);
  await expect(page.getByText('Purchase (none yet)')).toBeVisible();
});
