import fs from 'node:fs';
import { expect, goTo, test, unique } from './fixtures';

test('create an asset, preview and print its label, export a batch', async ({ signedIn: page, api, printer }) => {
  const field = await api.call('POST', '/fields', { key: unique('serial').replace(/-/g, '_'), label: 'Serial', validation_type: 'text' });
  const category = unique('Bench');
  const cat = await api.call('POST', '/categories', { name: category, color_hex: '#fbbf24', is_consumable: false, is_running_low: false });
  await api.call('POST', `/categories/${cat.category.id}/fields`, { field_id: field.field.id, is_required: true });
  const templateName = unique('QL 90');
  const { template } = await api.call('POST', '/templates', {
    name: templateName,
    units: 'mm',
    width: 90,
    height: 29,
    dpi: 300,
    elements: [
      { type: 'generator', generator_type: 'qr', x: 3, y: 3, width: 23, height: 23, content_binding: '{{asset.code.qr}}' },
      { type: 'text', x: 28, y: 4, width: 58, height: 10, font_size: 16, font_weight: 'bold', content_binding: '{{asset.name}}' },
    ],
  });
  const templateId: string = template.id;
  const printerName = unique('QL');
  await api.call('POST', '/printers', {
    name: printerName,
    driver: 'brother_ql',
    model: 'QL-810W',
    connection: { type: 'tcp', address: `127.0.0.1:${printer.port}` },
    media: 'dk-11201',
  });

  const name = unique('Drill');
  const code = unique('D').toUpperCase();
  await goTo(page, 'Assets');
  await page.getByRole('link', { name: 'New asset' }).click();
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByRole('checkbox', { name: category }).check();
  // A required field the server enforces: leave it empty first.
  // Codes are folded away until wanted.
  await page.getByText('Codes (none yet)').click();
  await page.getByLabel('QR Code', { exact: true }).fill(code);
  await page.getByRole('button', { name: 'Create asset' }).click();
  await expect(page.getByRole('alert')).toContainText('required field');
  await expect(page.getByLabel(/Serial/)).toHaveAttribute('aria-invalid', 'true');
  await page.getByLabel(/Serial/).fill('SN-0042');
  await page.getByRole('button', { name: 'Create asset' }).click();
  await expect(page).toHaveURL(/\/assets\/[0-9a-f-]{36}$/);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(name);

  // The label panel: preview, then print two copies.
  await page.getByLabel('Template').selectOption(templateId);
  const preview = page.getByRole('img', { name: 'Preview of the label for this asset' });
  await expect.poll(() => preview.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth)).toBeGreaterThan(0);
  await page.getByLabel('Printer').selectOption({ label: printerName });
  await page.getByLabel('Copies').fill('2');
  await page.getByRole('button', { name: 'Print' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Sent' })).toHaveText(`Sent 2 labels to ${printerName}.`);
  const job = await printer.job();
  // A Brother QL raster job: 200 invalidate bytes, then ESC @.
  expect(job.subarray(0, 200).every((b) => b === 0)).toBe(true);
  expect(job.subarray(200, 202)).toEqual(Buffer.from([0x1b, 0x40]));

  // The list: search by code, then export a second asset with it.
  await api.call('POST', '/assets', {
    name: unique('Grinder'),
    category_ids: [cat.category.id],
    generator_keys: [{ generator_type: 'qr', assigned_code: unique('G').toUpperCase() }],
    field_values: { [field.field.key]: 'SN-7' },
  });
  await goTo(page, 'Assets');
  await page.getByLabel('Category').selectOption(cat.category.id);
  await expect(page).toHaveURL(/category=/);
  await expect(page.getByRole('row')).toHaveCount(3);
  await page.getByRole('checkbox', { name: 'Select all assets' }).check();
  await page.getByLabel('Template').selectOption(templateId);
  const [download] = await Promise.all([page.waitForEvent('download'), page.getByRole('button', { name: 'Export 2 labels' }).click()]);
  expect(fs.readFileSync((await download.path())!).subarray(0, 5).toString()).toBe('%PDF-');
  // Named after the template, not its ID.
  expect(download.suggestedFilename()).toBe(`labels-${templateName.replace(' ', '-')}.pdf`);

  await page.getByLabel('Search').fill(code);
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(page.getByRole('cell', { name, exact: true })).toBeVisible();
  await expect(page.getByRole('row')).toHaveCount(2);
});

test('archive an asset, find it among the archived, and restore it', async ({ signedIn: page, api }) => {
  const name = unique('Saw');
  const code = unique('S').toUpperCase();
  const { asset } = await api.call('POST', '/assets', { name, generator_keys: [{ generator_type: 'qr', assigned_code: code }] });
  await page.goto(`/assets/${asset.asset.id}`);
  await page.getByRole('button', { name: 'Archive' }).click();
  await page.getByRole('group', { name: /^Archive this asset/ }).getByRole('button', { name: 'Archive' }).click();
  await expect(page).toHaveURL(/\/assets$/);

  // Another asset takes its code meanwhile.
  await api.call('POST', '/assets', { name: unique('Other saw'), generator_keys: [{ generator_type: 'qr', assigned_code: code }] });

  await page.getByLabel('Show').selectOption({ label: 'Archived assets' });
  await expect(page).toHaveURL(/archived=true/);
  await page.getByLabel('Search').fill(name);
  await page.getByRole('button', { name: 'Search' }).click();
  await page.getByRole('link', { name }).click();
  await expect(page.getByText(/^Archived .*Its codes were released/)).toBeVisible();
  await expect(page.getByRole('link', { name: 'Edit' })).toHaveCount(0);

  await page.getByRole('button', { name: 'Restore' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Restored' })).toHaveText(
    `Restored. Other assets have taken QR Code ${code}, so it did not get them back.`,
  );
  await expect(page.getByRole('link', { name: 'Edit' })).toBeVisible();
  const history = page.getByRole('region', { name: 'History' });
  await expect(history.getByRole('listitem').first()).toContainText(`Not given back QR Code ${code}: another asset has it`);
  await expect(history.getByRole('listitem').filter({ hasText: /^Archived/ })).toContainText(`Released QR Code ${code}`);
});

test('the labels panel offers only the printers a template suits, and says why a label cannot be made', async ({ signedIn: page, api, printer }) => {
  const printerName = unique('QL');
  await api.call('POST', '/printers', {
    name: printerName, driver: 'brother_ql', model: 'QL-810W', media: 'dk-22205',
    connection: { type: 'tcp', address: `127.0.0.1:${printer.port}` },
  });
  const { template: tag } = await api.call('POST', '/templates', {
    name: unique('Code tag'), units: 'mm', width: 62, height: 29, dpi: 300,
    elements: [{ type: 'generator', generator_type: 'qr', x: 2, y: 2, width: 25, height: 25 }],
  });
  const { template: shipping } = await api.call('POST', '/templates', { name: unique('Shipping'), units: 'in', width: 4, height: 6, dpi: 203, elements: [] });
  // One that labels anything, on the QL.
  await api.call('POST', '/templates', {
    name: unique('Name only'), units: 'mm', width: 62, height: 29, dpi: 300,
    elements: [{ type: 'text', x: 2, y: 2, width: 58, height: 10, font_size: 12, content_binding: '{{asset.name}}' }],
  });
  const { category } = await api.call('POST', '/categories', { name: unique('Tagged') });
  await api.call('PUT', `/categories/${category.id}/label-template`, { label_template_ids: [tag.id] });
  const { asset } = await api.call('POST', '/assets', { name: unique('Drill'), category_ids: [category.id] });

  // Its category's template is listed first, but the drill has no code
  // yet, so it starts with another; the tag says why it cannot label it.
  await page.goto(`/assets/${asset.asset.id}`);
  const template = page.getByLabel('Template');
  await expect(template.locator(`optgroup[label="Its category's"] option`)).toHaveText([new RegExp(`^${tag.name}`)]);
  await expect(template).not.toHaveValue(tag.id);
  await template.selectOption(tag.id);
  await expect(page.getByRole('list', { name: 'Why it cannot label these' })).toContainText(`This template prints a QR Code, and ${asset.asset.name} has none.`);
  await expect(page.getByRole('button', { name: 'Print' })).toBeDisabled();

  // Shipping labels fit no printer: said before anything is tried.
  await template.selectOption(shipping.id);
  await expect(page.getByText('No printer has labels loaded that this template fits.')).toBeVisible();
  await page.locator('summary', { hasText: printerName }).click();
  await expect(page.getByText(`${shipping.name} can't print on ${printerName}: it is laid out at 203 DPI, but Brother QL printers print at 300 DPI`)).toBeVisible();
});

test('say where an asset is as it is made, find it by its place, and move it', async ({ signedIn: page, api }) => {
  const shelf = unique('Shelf');
  const bench = unique('Bench');
  const { asset: shelfAsset } = await api.call('POST', '/assets', { name: shelf });
  await api.call('POST', '/assets', { name: bench });
  await api.call('POST', '/assets', { name: unique('Numbered'), generator_keys: [{ generator_type: 'code128', assigned_code: `${unique('WH').toUpperCase()}-0041` }] });

  // Made with where it is, and the next code.
  const name = unique('Clamp');
  await goTo(page, 'Assets');
  await page.getByRole('link', { name: 'New asset' }).click();
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByLabel('Put it in').fill(shelf);
  await page.getByRole('button', { name: 'Find' }).click();
  await page.getByRole('list', { name: 'Places found' }).getByRole('button', { name: shelf }).click();
  await expect(page.getByText(`In ${shelf}.`)).toBeVisible();
  await page.getByText('Codes (none yet)').click();
  const code128 = page.getByLabel('Code 128', { exact: true });
  await page.getByRole('button', { name: 'Assign the next code' }).nth(2).click();
  await expect(code128).toHaveValue(/[0-9]$/);
  await page.getByRole('button', { name: 'Create asset' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(name);
  const details = page.getByRole('region', { name: 'Details' });
  await expect(details.getByRole('link', { name: shelf })).toBeVisible();

  // The list says where, and searching the place finds it.
  await page.goto(`/assets?search=${encodeURIComponent(shelf)}`);
  const row = page.getByRole('row').filter({ hasText: name });
  await expect(row.getByRole('link', { name: shelf })).toBeVisible();

  // Moved to the bench; the shelf lists what it holds no more.
  await row.getByRole('link', { name }).click();
  await details.getByRole('button', { name: 'Move' }).click();
  await page.getByLabel('Move it to').fill(bench);
  await page.getByLabel('Move it to').press('Enter');
  await page.getByRole('list', { name: 'Places found' }).getByRole('button', { name: bench }).click();
  await expect(page.getByRole('status')).toHaveText(`Moved to ${bench}.`);
  await expect(details.getByRole('link', { name: bench })).toBeVisible();
  await page.goto(`/assets/${shelfAsset.asset.id}`);
  await expect(page.getByRole('region', { name: 'Contents' })).toHaveCount(0);
});
