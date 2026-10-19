import AxeBuilder from '@axe-core/playwright';
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
  await expect(page.getByText(`Fits ${printerName}.`)).toBeVisible();
  await page.locator('#tpl-height').fill('40');
  await page.locator('#tpl-height').press('Tab');
  await expect(page.getByText(/takes 29 × 90 mm/)).toBeVisible();
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
  await expect(page.getByRole('list', { name: 'Layers' }).getByRole('listitem')).toHaveCount(2);
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
  await page.getByRole('button', { name: 'Print', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Sent' })).toHaveText(`Sent 1 label to ${printerName}.`);
  expect((await printer.job()).length).toBeGreaterThan(1000);
});

test('a template with content where the printer does not print warns, and still prints', async ({ signedIn: page, api, printer }) => {
  const printerName = unique('QL');
  await api.call('POST', '/printers', {
    name: printerName,
    driver: 'brother_ql',
    model: 'QL-810W',
    connection: { type: 'tcp', address: `127.0.0.1:${printer.port}` },
    media: 'dk-11201',
  });
  // Its name runs to the top edge, where a QL does not print on 29 mm labels.
  const { template } = await api.call('POST', '/templates', {
    name: unique('Edge'), units: 'mm', width: 90, height: 29, dpi: 300,
    elements: [{ type: 'text', x: 28, y: 0, width: 58, height: 10, font_size: 14, content_binding: '{{asset.name}}' }],
  });
  const { asset } = await api.call('POST', '/assets', { name: unique('Drill') });

  await page.goto(`/templates/${template.id}/edit`);
  await page.getByLabel('Printer').selectOption({ label: printerName });
  await expect(page.getByText(`Fits ${printerName}, with 1 warning.`)).toBeVisible();
  await expect(page.getByRole('list', { name: 'Before printing' })).toContainText('where the printer does not print');
  // The canvas shades what it does not print, and names the element in it.
  await expect(page.locator('.unprinted')).toHaveCount(4);
  await expect(page.getByText('In the shaded area, which it does not print: Text 1: {{asset.name}}.')).toBeVisible();

  // A test label, without saving or choosing an asset.
  await page.getByRole('button', { name: 'Test print' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Sent a test label' })).toHaveText(`Sent a test label to ${printerName}.`);
  await expect(page.getByRole('list', { name: 'Printed, with warnings' })).toContainText('where the printer does not print');
  await printer.job(1);

  await page.goto(`/assets/${asset.asset.id}`);
  await page.getByLabel('Template').selectOption(template.id);
  await page.getByLabel('Printer').selectOption({ label: printerName });
  // Warned before printing, and after.
  await expect(page.getByRole('list', { name: 'Before printing' })).toContainText('where the printer does not print');
  await page.getByRole('button', { name: 'Print' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Sent' })).toHaveText(`Sent 1 label to ${printerName}.`);
  await expect(page.getByRole('list', { name: 'Printed, with warnings' })).toContainText('where the printer does not print');
  await printer.job(2);
});

test('make a template adaptive, see it on each printer, and print it on two rolls', async ({ signedIn: page, api, printer }) => {
  const dieCut = unique('QL 29x90');
  const tape = unique('QL tape');
  await api.call('POST', '/printers', { name: dieCut, driver: 'brother_ql', model: 'QL-810W', connection: { type: 'tcp', address: `127.0.0.1:${printer.port}` }, media: 'dk-11201' });
  await api.call('POST', '/printers', { name: tape, driver: 'brother_ql', model: 'QL-810W', connection: { type: 'tcp', address: `127.0.0.1:${printer.port}` }, media: 'dk-22205' });
  const name = unique('Any tag');
  const { template } = await api.call('POST', '/templates', {
    name, units: 'mm', width: 62, height: 29, dpi: 300,
    elements: [
      { type: 'generator', generator_type: 'qr', x: 3, y: 3, width: 23, height: 23 },
      { type: 'text', x: 28, y: 4, width: 31, height: 10, font_size: 12, content_binding: '{{asset.name}}' },
    ],
  });
  const { asset } = await api.call('POST', '/assets', { name: unique('Drill'), generator_keys: [{ generator_type: 'qr', assigned_code: unique('AD').toUpperCase() }] });

  await page.goto(`/templates/${template.id}/edit`);
  await page.getByRole('radio', { name: /Fits several label sizes/ }).check();
  await page.getByLabel('Narrowest (mm)').fill('25');
  await page.getByLabel('Narrowest (mm)').press('Tab');
  // The code pinned to the top left, the name stretching beside it.
  // Written without layering, it is stored as it was drawn: text, then the code.
  const layers = page.getByRole('list', { name: 'Layers' });
  await layers.getByRole('button', { name: 'QR code' }).click();
  await page.getByLabel('Across').selectOption('start');
  await page.getByLabel('Down').selectOption('start');
  await layers.getByRole('button', { name: 'Text: {{asset.name}}' }).click();
  await page.getByLabel('Across').selectOption('both');
  await page.getByLabel('Down').selectOption('start');

  const onPrinters = page.getByRole('list', { name: 'On each printer' });
  await expect(onPrinters.getByRole('img', { name: new RegExp(`Preview at ${dieCut}`) })).toBeVisible();
  await expect(onPrinters.getByRole('img', { name: new RegExp(`Preview at ${tape}`) })).toBeVisible();
  await expect(page.getByText(/In its shape it fits labels from/)).toBeVisible();
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();

  const { printers } = await api.call('GET', '/printers');
  for (const n of [dieCut, tape]) {
    const p = (printers as { id: string; name: string }[]).find((x) => x.name === n)!;
    await api.call('POST', `/printers/${p.id}/print`, { template_id: template.id, asset_ids: [asset.asset.id], copies: 1 });
  }
  await printer.job(2);
});

test('stack elements: the top one takes a click, Alt+click the one beneath, and layers drag', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', {
    name: unique('Stack'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list',
    elements: [
      { type: 'text', x: 2, y: 2, width: 40, height: 20, font_size: 14, content_binding: '{{asset.name}}' },
      { type: 'generator', generator_type: 'qr', x: 10, y: 5, width: 15, height: 15 },
    ],
  });
  await page.goto(`/templates/${template.id}/edit`);
  const layers = page.getByRole('list', { name: 'Layers' });
  await expect(layers.getByRole('button')).toHaveText(['QR code', 'Text: {{asset.name}}']);

  // Where they overlap, a click takes the code on top, and Alt+click the text beneath.
  const code = page.getByRole('button', { name: 'Barcode 2 (qr)' });
  const box = (await code.boundingBox())!;
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await expect(code).toHaveAttribute('aria-pressed', 'true');
  await page.keyboard.down('Alt');
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await page.keyboard.up('Alt');
  await expect(page.getByRole('button', { name: /^Text 1/, pressed: true })).toBeFocused();
  await expect(layers.getByRole('button', { name: 'Text: {{asset.name}}' })).toHaveAttribute('aria-pressed', 'true');

  // Dragging the text's layer over the code's brings it to the top.
  await layers.getByRole('listitem').filter({ hasText: 'Text:' }).dragTo(layers.getByRole('listitem').filter({ hasText: 'QR code' }));
  await expect(layers.getByRole('button')).toHaveText(['Text: {{asset.name}}', 'QR code']);
  // Page Down on the focused element sends it back again.
  await page.getByRole('button', { name: /^Text 2/ }).focus();
  await page.keyboard.press('PageDown');
  await expect(layers.getByRole('button')).toHaveText(['QR code', 'Text: {{asset.name}}']);
  await page.getByRole('button', { name: 'Bring to front' }).click();
  await expect(layers.getByRole('button')).toHaveText(['Text: {{asset.name}}', 'QR code']);

  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target).join(', ')}`)).toEqual([]);

  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = await api.call('GET', `/templates/${template.id}`);
  expect(saved.template.layering).toBe('list');
  expect(saved.template.elements.map((e: { type: string }) => e.type)).toEqual(['generator', 'text']);
});

test('turn an element with its handle, snapping to 45°, and from its settings', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', {
    name: unique('Turn'), units: 'mm', width: 62, height: 40, dpi: 300, layering: 'list',
    elements: [{ type: 'text', x: 16, y: 15, width: 30, height: 10, font_size: 14, content_binding: '{{asset.name}}' }],
  });
  await page.goto(`/templates/${template.id}/edit`);
  const text = page.getByRole('button', { name: /^Text 1/ });
  await text.click();
  const angle = page.getByLabel('Turned (degrees clockwise)');

  // Swept from above its center round to its right: a quarter turn, snapped.
  const box = (await text.boundingBox())!;
  const cx = box.x + box.width / 2;
  const cy = box.y + box.height / 2;
  const handle = page.locator('.rotate-handle');
  const h = (await handle.boundingBox())!;
  await page.mouse.move(h.x + h.width / 2, h.y + h.height / 2);
  await page.mouse.down();
  await page.mouse.move(cx + 40, cy - 30, { steps: 5 });
  await page.mouse.move(cx + 80, cy + 4, { steps: 5 });
  await page.mouse.up();
  await expect(angle).toHaveValue('90');
  await expect(text).toHaveAttribute('style', /rotate\(90deg\)/);

  // With Alt, a few degrees more does not snap back. The handle is now to
  // its right.
  const h2 = (await handle.boundingBox())!;
  await page.keyboard.down('Alt');
  await page.mouse.move(h2.x + h2.width / 2, h2.y + h2.height / 2);
  await page.mouse.down();
  await page.mouse.move(cx + 80, cy + 4, { steps: 5 });
  await page.mouse.up();
  await page.keyboard.up('Alt');
  const free = Number(await angle.inputValue());
  expect(free).toBeGreaterThan(91);
  expect(free).toBeLessThan(95);

  await angle.fill('30');
  await angle.press('Tab');
  await expect(text).toHaveAttribute('style', /rotate\(30deg\)/);
  await page.getByRole('button', { name: 'Turn 90° right' }).click();
  await expect(angle).toHaveValue('120');

  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target).join(', ')}`)).toEqual([]);

  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = await api.call('GET', `/templates/${template.id}`);
  expect(saved.template.elements[0].rotation).toBe(120);
});
