import { expect, goTo, test, unique } from './fixtures';

test('design a template with the mouse and keyboard, and save it', async ({ signedIn: page, api, printer }) => {
  const printerName = unique('QL');
  await api.call('POST', '/printers', {
    name: printerName,
    driver: 'brother_ql',
    model: 'QL-810W',
    connection: { type: 'tcp', address: `127.0.0.1:${printer.port}` },
    media: 'dk-11201',
  });
  const assetName = unique('Drill');
  await api.call('POST', '/assets', { name: assetName, generator_keys: [{ generator_type: 'qr', assigned_code: unique('E').toUpperCase() }] });

  const name = unique('Tag');
  await goTo(page, 'Templates');
  await page.getByRole('link', { name: 'New template' }).click();
  await page.getByLabel('Name', { exact: true }).fill(name);

  // Size and DPI from the printer's loaded media, then its fit check.
  await page.getByLabel('Printer').selectOption({ label: printerName });
  await page.getByRole('button', { name: 'Use its label size' }).click();
  await expect(page.locator('#tpl-width')).toHaveValue('90');
  await expect(page.locator('#tpl-height')).toHaveValue('29');
  await expect(page.locator('#tpl-dpi')).toHaveValue('300');
  await expect(page.getByText('Fits this printer.')).toBeVisible();
  await page.locator('#tpl-height').fill('40');
  await page.locator('#tpl-height').press('Tab');
  await expect(page.getByText(/needs a 29 × 90 mm template/)).toBeVisible();
  await page.getByRole('button', { name: 'Undo' }).click();
  await expect(page.locator('#tpl-height')).toHaveValue('29');

  // Text: drag it, then resize it from the corner.
  await page.getByRole('button', { name: 'Add text' }).click();
  const text = page.getByRole('button', { name: /^Text 1/, pressed: true });
  const box = (await text.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 100, box.y + box.height / 2 + 20, { steps: 8 });
  await page.mouse.up();
  const left = Number(await page.locator('#el-0-x').inputValue());
  expect(left).toBeGreaterThan(2.9);
  expect((left * 2) % 1).toBe(0); // on the 0.5 mm grid
  const before = Number(await page.locator('#el-0-width').inputValue());
  const box2 = (await text.boundingBox())!;
  await page.mouse.move(box2.x + box2.width, box2.y + box2.height);
  await page.mouse.down();
  await page.mouse.move(box2.x + box2.width + 60, box2.y + box2.height, { steps: 5 });
  await page.mouse.up();
  expect(Number(await page.locator('#el-0-width').inputValue())).toBeGreaterThan(before);

  // A barcode, moved with the keyboard.
  await page.getByRole('button', { name: 'Add barcode' }).click();
  const code = page.getByRole('button', { name: /^Barcode 2/, pressed: true });
  await code.focus();
  const x0 = Number(await page.locator('#el-1-x').inputValue());
  await code.press('ArrowRight');
  await code.press('Shift+ArrowRight');
  expect(Number(await page.locator('#el-1-x').inputValue())).toBeCloseTo(x0 + 5.5, 5);

  // A binding the server rejects shows on the element.
  await page.getByLabel('Content').fill('{{asset.nope}}');
  await page.getByLabel('Content').press('Tab');
  await expect(page.getByRole('alert').filter({ hasText: 'nope' }).first()).toBeVisible();
  await page.getByLabel('Content').fill('');
  await page.getByLabel('Content').press('Tab');

  await expect(page.getByRole('img', { name: 'Label preview' })).toBeVisible();
  await page.getByRole('button', { name: 'Save' }).click();
  // The server names the template; the editor moves to it.
  await expect(page).toHaveURL(/\/templates\/[0-9a-f-]{36}\/edit$/);
  const id = new URL(page.url()).pathname.split('/')[2]!;
  await expect(page.getByText(id, { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('list', { name: 'Elements' }).getByRole('listitem')).toHaveCount(2);
  const saved = await api.call('GET', `/templates/${id}`);
  expect(saved.template.elements[0]).toMatchObject({ type: 'text', x: left });
  expect(saved.template.elements[1]).toMatchObject({ type: 'generator', generator_type: 'qr', x: x0 + 5.5 });

  // Test print from the editor against an asset.
  await page.getByLabel('Find an asset').fill(assetName);
  await page.getByRole('button', { name: 'Find' }).click();
  await page.getByRole('button', { name: assetName }).click();
  // The print panel's printer, not the canvas panel's.
  const printTo = page.locator('#label-printer');
  await printTo.selectOption({ label: printerName });
  await expect(printTo.locator('option:checked')).toHaveText(printerName);
  await page.getByRole('button', { name: 'Print' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Sent' })).toHaveText(`Sent 1 label to ${printerName}.`);
  expect((await printer.job()).length).toBeGreaterThan(1000);
});
