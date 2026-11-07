import { expect, goTo, test, unique } from './fixtures';

test('make a contract with a vendor, cover an asset with it, and see it on the contracts list, the vendor and the asset', async ({ signedIn: page, api }) => {
  const vendorName = unique('Laser service co');
  const { vendor } = await api.call('POST', '/vendors', { name: vendorName });
  const assetName = unique('Laser cutter');
  const { asset } = await api.call('POST', '/assets', { name: assetName });
  const assetId = asset.asset.id;
  const name = unique('Laser upkeep');

  // Made from the Contracts page.
  await goTo(page, 'Contracts');
  await page.getByRole('link', { name: 'New contract' }).click();
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByLabel('Vendor', { exact: true }).selectOption({ label: vendorName });
  await page.getByLabel('Kind', { exact: true }).selectOption('maintenance');
  await page.getByLabel('Reference (optional)').fill('MA-9');
  await page.getByLabel('Starts (optional)').fill('2026-07-01');
  await page.getByLabel('Ends (optional)').fill('2099-06-30');
  await page.getByLabel('Renews automatically unless notice is given').check();
  await page.getByLabel('Notice period, in days').fill('30');
  await page.getByLabel('Cost (optional)').fill('1200');
  await page.getByLabel('Currency').fill('GBP');
  await page.getByLabel('Paid').selectOption('yearly');
  await page.getByRole('button', { name: 'Create contract' }).click();
  await expect(page).toHaveURL(/\/contracts\/[0-9a-f-]{36}$/);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(name);
  const id = page.url().split('/').pop()!;
  const details = page.locator('#tab-details');
  await expect(details).toContainText('1200.00 GBP, yearly');
  await expect(details).toContainText('by 2099-05-31');

  // It covers the asset, found by a search.
  await page.getByRole('navigation', { name: 'Parts of this contract' }).getByRole('link', { name: 'Covers', exact: true }).click();
  await page.getByLabel('Find an asset').fill(assetName);
  await page.getByRole('button', { name: 'Find', exact: true }).click();
  await page.getByRole('button', { name: `Cover ${assetName}` }).click();
  await page.getByRole('button', { name: 'Save what it covers' }).click();
  await expect(page.getByRole('status')).toHaveText('What it covers is saved.');
  await page.getByRole('navigation', { name: 'Parts of this contract' }).getByRole('link', { name: 'History', exact: true }).click();
  await expect(page.getByRole('region', { name: 'History' }).getByRole('listitem').first()).toContainText(`Covers the asset ${assetName}`);

  // The contracts list has it, by when it ends.
  await goTo(page, 'Contracts');
  await page.getByLabel('Search').fill(name);
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  const row = page.getByRole('table', { name: 'Contracts' }).getByRole('row').filter({ hasText: name });
  await expect(row).toContainText('Maintenance');
  await expect(row).toContainText(vendorName);
  await expect(row).toContainText('2099-06-30');
  await expect(row.getByRole('link', { name, exact: true })).toHaveAttribute('href', `/contracts/${id}`);

  // So does its vendor's page.
  await page.goto(`/vendors/${vendor.id}`);
  await page.getByRole('navigation', { name: 'Parts of this vendor' }).getByRole('link', { name: 'Contracts', exact: true }).click();
  const withIt = page.getByRole('region', { name: 'Contracts with it' });
  await expect(withIt.getByRole('link', { name, exact: true })).toHaveAttribute('href', `/contracts/${id}`);
  await expect(withIt).toContainText('1 asset');

  // And the asset's page says what covers it.
  await page.goto(`/assets/${assetId}`);
  const covering = page.getByRole('region', { name: 'Contracts', exact: true });
  await expect(covering.getByRole('link', { name: `Under maintenance contract with ${vendorName} until June 2099` })).toHaveAttribute('href', `/contracts/${id}`);

  // A vendor with contracts is not deleted.
  await expect(api.call('DELETE', `/vendors/${vendor.id}`)).rejects.toThrow(/409/);
});
