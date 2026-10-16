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
