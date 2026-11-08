import { expect, test, unique } from './fixtures';

test('start orders from what is running low, place one, and receive it in parts', async ({ signedIn: page, api }) => {
  const vendorName = unique('Acme supplies');
  const { vendor } = await api.call('POST', '/vendors', { name: vendorName });
  const filamentName = unique('Filament');
  const { category: filament } = await api.call('POST', '/categories', { name: filamentName, color_hex: '#7c3aed', is_consumable: true, unit: 'g', low_stock_threshold: 3000 });
  const spoolName = unique('PLA spool');
  await api.call('POST', '/assets', { name: spoolName, category_ids: [filament.id], stock: { quantity: 1000, unit: 'g' } });
  const binName = unique('M5 bolts');
  const bin = (await api.call('POST', '/assets', { name: binName, stock: { quantity: 50, unit: 'pcs', low_stock_threshold: 200 } })).asset.asset;
  await api.call('PUT', `/assets/${bin.id}/running-low`, { running_low: true });
  await api.call('POST', '/catalog', { vendor_id: vendor.id, asset_id: bin.id, part_number: 'M5-100', description: 'M5 bolts', pack_size: 100, pack_unit: 'pcs', price: '4.50', currency: 'GBP', preferred: true });
  await api.call('POST', '/catalog', { vendor_id: vendor.id, category_id: filament.id, part_number: 'PLA-1K', description: 'PLA spool', pack_size: 1000, pack_unit: 'g', price: '20', currency: 'GBP', preferred: true });

  // From the running-low list: a draft for the vendor, with enough packs to
  // bring each back to its threshold.
  await page.goto('/');
  await page.getByRole('region', { name: 'Running low' }).getByRole('link', { name: 'Start orders…' }).click();
  const start = page.getByRole('group', { name: vendorName, exact: true });
  await expect(start.getByRole('checkbox', { name: new RegExp(binName) })).toBeChecked();
  await expect(start.locator('input[type=number]').nth(0)).toHaveValue('2');
  await expect(start.locator('input[type=number]').nth(1)).toHaveValue('2');
  await page.getByRole('button', { name: 'Create drafts' }).click();
  await expect(page).toHaveURL(/\/orders\?state=draft$/);
  await page.getByRole('row').filter({ hasText: vendorName }).getByRole('link').click();
  await expect(page.getByRole('table', { name: 'Lines' })).toContainText('49.00 GBP');

  // Placed, it is on order, and downloads as a PDF to send.
  await page.getByRole('button', { name: 'Place order' }).click();
  await expect(page.getByRole('status')).toHaveText('Placed with the vendor.');
  const pdf = page.getByRole('link', { name: 'PDF', exact: true });
  const pdfResponse = await page.context().request.get((await pdf.getAttribute('href'))!);
  expect(pdfResponse.headers()['content-type']).toBe('application/pdf');
  const reference = (await page.getByRole('heading', { level: 1 }).textContent())!.trim();
  await page.goto('/');
  await expect(page.getByRole('region', { name: 'Running low' }).getByRole('link', { name: `On order: ${reference}`, exact: true }).first()).toBeVisible();

  // The bolts come first, and one spool.
  await page.goBack();
  await page.getByRole('link', { name: 'Receive…' }).click();
  const bolts = page.getByRole('group', { name: /M5-100/ });
  await expect(bolts.getByText(`Adds 200 pcs to ${binName}.`)).toBeVisible();
  const spools = page.getByRole('group', { name: /PLA-1K/ });
  await expect(spools.getByLabel('Like')).toHaveValue(/.+/);
  await expect(spools.getByLabel('Each holds, in g')).toHaveValue('1000');
  await spools.getByLabel('Packs').fill('1');
  await expect(spools.getByText(`Makes 1 asset in ${filamentName}.`, { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Receive', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(reference);
  await expect(page.getByText(/^Partly received, to/)).toBeVisible();
  const restocked = (await api.call('GET', `/assets/${bin.id}`)).asset.asset;
  expect(restocked.stock.quantity).toBe(250);
  expect(restocked.stock.is_running_low).toBe(false);

  // Then the other spool, and it is all here.
  await page.getByRole('link', { name: 'Receive…' }).click();
  await expect(page.getByRole('group', { name: /M5-100/ })).toHaveCount(0);
  await page.getByRole('button', { name: 'Receive', exact: true }).click();
  await expect(page.getByText(/^Received, to/)).toBeVisible();
  const { assets } = await api.call('GET', `/assets?category_id=${filament.id}`);
  const made = assets.filter((a: { asset: { name: string } }) => a.asset.name === spoolName);
  expect(made).toHaveLength(3);
  const bought = made.filter((a: { purchase: { order_number: string } }) => a.purchase?.order_number === reference);
  expect(bought).toHaveLength(2);

  // Its history says what came.
  await page.getByRole('navigation', { name: 'Parts of this order' }).getByRole('link', { name: 'History', exact: true }).click();
  await expect(page.getByRole('region', { name: 'History' }).getByRole('listitem').first()).toContainText('Delivery received');
});
