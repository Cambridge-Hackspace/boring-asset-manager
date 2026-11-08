import { expect, test, unique } from './fixtures';

test('record what vendors sell, choose the preferred one, and reorder it from the running-low list', async ({ signedIn: page, api }) => {
  const acmeName = unique('Acme fasteners');
  const shopName = unique('Corner shop');
  const { vendor: acme } = await api.call('POST', '/vendors', { name: acmeName, website: 'https://acme.example' });
  const { vendor: shop } = await api.call('POST', '/vendors', { name: shopName });
  const category = unique('Wood screws');
  const { category: cat } = await api.call('POST', '/categories', { name: category, color_hex: '#7c3aed', is_consumable: true });
  const solderName = unique('Solder');
  const solder = await api.call('POST', '/assets', { name: solderName, stock: { quantity: 1, unit: 'reels', low_stock_threshold: 2 } });
  const solderID = solder.asset.asset.id;

  // On the vendor's Catalog tab: an entry for the category, preferred.
  await page.goto(`/vendors/${acme.id}`);
  await page.getByRole('navigation', { name: 'Parts of this vendor' }).getByRole('link', { name: 'Catalog', exact: true }).click();
  await page.getByRole('button', { name: 'Add a catalog entry' }).click();
  const form = page.getByRole('form', { name: 'Add a catalog entry' });
  await form.getByLabel('A category, for things counted by items').selectOption({ label: category });
  await form.getByLabel('Part number', { exact: true }).fill('WS-100');
  await form.getByLabel('Product page (optional)').fill('https://acme.example/ws-100');
  await form.getByLabel('Pack size (optional)').fill('100');
  await form.getByLabel('Pack unit (optional)').fill('screws');
  await form.getByLabel('Price of a pack (optional)').fill('4.5');
  await form.getByLabel('Currency').fill('GBP');
  await form.getByLabel('Preferred: reorder from this one').check();
  await form.getByRole('button', { name: 'Add entry' }).click();
  const acmeEntry = page.getByRole('listitem', { name: `WS-100 for ${category}`, exact: true });
  await expect(acmeEntry).toContainText('4.50 GBP for 100 screws');
  await expect(acmeEntry).toContainText('Preferred');
  await expect(page.getByRole('link', { name: 'Export CSV' })).toHaveAttribute('href', `/api/v1/vendors/${acme.id}/catalog.csv`);

  // On the asset's page: an entry from the shop for the solder, counted by quantity.
  await page.goto(`/assets/${solderID}`);
  const catalog = page.getByRole('region', { name: 'Catalog', exact: true });
  await catalog.getByRole('button', { name: 'Add a catalog entry' }).click();
  const assetForm = page.getByRole('form', { name: 'Add a catalog entry' });
  await assetForm.getByLabel('Vendor', { exact: true }).selectOption({ label: shopName });
  await assetForm.getByLabel('Part number', { exact: true }).fill('SR-500');
  await assetForm.getByLabel('Description (optional with a part number)').fill('Solder, 500 g');
  await expect(assetForm.getByLabel('Preferred: reorder from this one')).toBeChecked();
  await assetForm.getByRole('button', { name: 'Add entry' }).click();
  await expect(catalog.getByRole('listitem', { name: `SR-500 from ${shopName}`, exact: true })).toContainText('Preferred');

  // The shop sells the screws too; on the category's Catalog tab, it becomes preferred.
  await api.call('POST', '/catalog', { vendor_id: shop.id, category_id: cat.id, part_number: 'CS-7' });
  await page.goto(`/categories/${cat.id}?tab=catalog`);
  const shopEntry = page.getByRole('listitem', { name: `CS-7 from ${shopName}`, exact: true });
  await shopEntry.getByRole('button', { name: `Prefer CS-7 from ${shopName}`, exact: true }).click();
  await expect(page.getByRole('status')).toHaveText(`CS-7 from ${shopName} is preferred.`);
  await expect(shopEntry).toContainText('Preferred');
  await expect(page.getByRole('listitem', { name: `WS-100 from ${acmeName}`, exact: true })).not.toContainText('Preferred');
  // And back to Acme's.
  await page.getByRole('button', { name: `Prefer WS-100 from ${acmeName}`, exact: true }).click();
  await expect(page.getByRole('listitem', { name: `WS-100 from ${acmeName}`, exact: true })).toContainText('Preferred');

  // Running low, each shows its preferred entry with its part number, price and link.
  await api.call('PUT', `/categories/${cat.id}/running-low`, { running_low: true });
  await page.goto('/');
  const low = page.getByRole('region', { name: 'Running low' });
  const reorderScrews = low.getByRole('link', { name: `Reorder part WS-100 of ${category} from ${acmeName}`, exact: true });
  await expect(reorderScrews).toHaveAttribute('href', 'https://acme.example/ws-100');
  await expect(reorderScrews).toHaveAttribute('target', '_blank');
  await expect(reorderScrews.locator('xpath=..')).toContainText('4.50 GBP for 100 screws');
  const reorderSolder = low.getByRole('link', { name: `Reorder part SR-500 of ${solderName} from ${shopName}`, exact: true });
  await expect(reorderSolder).toHaveAttribute('href', `/vendors/${shop.id}`);

  // A price list imported as CSV: checked, then imported.
  await page.goto(`/vendors/${acme.id}?tab=catalog`);
  await page.getByLabel('Import a CSV or Excel file').setInputFiles({
    name: 'prices.csv',
    mimeType: 'text/csv',
    buffer: Buffer.from(`category,part_number,description,price,currency\n${category},WS-200,Longer screws,5.25,GBP\n`),
  });
  await page.getByRole('button', { name: 'Check the file' }).click();
  await expect(page.getByRole('region', { name: 'What importing would do' })).toContainText('Importing would add 1, change 0');
  await page.getByRole('button', { name: 'Import', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Catalog imported' })).toHaveText('Catalog imported: 1 added, 0 changed.');
  await expect(page.getByRole('listitem', { name: `WS-200 for ${category}`, exact: true })).toContainText('5.25 GBP');

  // Its history says what was added.
  await page.getByRole('navigation', { name: 'Parts of this vendor' }).getByRole('link', { name: 'History', exact: true }).click();
  await expect(page.getByRole('region', { name: 'History' }).getByRole('listitem').first()).toContainText(`Catalog entry added: WS-200 (${category})`);
});
