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
  await layers.getByRole('button', { name: 'QR code', exact: true }).click();
  await page.getByLabel('Across', { exact: true }).selectOption('start');
  await page.getByLabel('Down', { exact: true }).selectOption('start');
  await layers.getByRole('button', { name: 'Text: {{asset.name}}', exact: true }).click();
  await page.getByLabel('Across', { exact: true }).selectOption('both');
  await page.getByLabel('Down', { exact: true }).selectOption('start');

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
  await expect(layers.locator('li > button:not([aria-label^="Lock"])')).toHaveText(['QR code', 'Text: {{asset.name}}']);

  // Where they overlap, a click takes the code on top, and Alt+click the text beneath.
  const code = page.getByRole('button', { name: 'Barcode 2 (qr)' });
  const box = (await code.boundingBox())!;
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await expect(code).toHaveAttribute('aria-pressed', 'true');
  await page.keyboard.down('Alt');
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await page.keyboard.up('Alt');
  await expect(page.getByRole('button', { name: /^Text 1/, pressed: true })).toBeFocused();
  await expect(layers.getByRole('button', { name: 'Text: {{asset.name}}', exact: true })).toHaveAttribute('aria-pressed', 'true');

  // Dragging the text's layer over the code's brings it to the top. Moved
  // onto the code's layer twice: dragTo's single move does not reliably
  // fire dragover, and without it there is no drop.
  const codeLayer = layers.getByRole('listitem').filter({ hasText: 'QR code' });
  await layers.getByRole('listitem').filter({ hasText: 'Text:' }).hover();
  await page.mouse.down();
  await codeLayer.hover();
  await codeLayer.hover();
  await page.mouse.up();
  await expect(layers.locator('li > button:not([aria-label^="Lock"])')).toHaveText(['Text: {{asset.name}}', 'QR code']);
  // Page Down on the focused element sends it back again.
  await page.getByRole('button', { name: /^Text 2/ }).focus();
  await page.keyboard.press('PageDown');
  await expect(layers.locator('li > button:not([aria-label^="Lock"])')).toHaveText(['QR code', 'Text: {{asset.name}}']);
  await page.getByRole('button', { name: 'Bring to front' }).click();
  await expect(layers.locator('li > button:not([aria-label^="Lock"])')).toHaveText(['Text: {{asset.name}}', 'QR code']);

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

test('set a code’s margins in its settings and on the canvas, and where it sits', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', {
    name: unique('Margins'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list',
    elements: [{ type: 'generator', generator_type: 'qr', x: 2, y: 2, width: 25, height: 25, content_binding: 'HELLO' }],
  });
  await page.goto(`/templates/${template.id}/edit`);
  await page.getByRole('button', { name: /^Barcode 1/ }).click();
  const top = page.getByRole('slider', { name: 'Top margin' });
  await expect(top).toHaveAttribute('aria-valuenow', '4');

  // No margin on the left, in its settings: the editor warns, and still saves.
  await page.getByRole('checkbox', { name: 'Each side on its own' }).check();
  await page.getByLabel('Left side').fill('0');
  await page.getByLabel('Left side').press('Tab');
  await expect(page.getByRole('status').filter({ hasText: 'Less space than the code’s standard quiet zone on its left side' })).toBeVisible();

  // The top margin's handle, with the keyboard and dragged.
  await top.focus();
  await page.keyboard.press('ArrowUp');
  await expect(page.getByLabel('Top side')).toHaveValue('5');
  const handle = (await top.boundingBox())!;
  await page.mouse.move(handle.x + handle.width / 2, handle.y + handle.height / 2);
  await page.mouse.down();
  await page.mouse.move(handle.x + handle.width / 2, handle.y + handle.height / 2 + 15, { steps: 5 });
  await page.mouse.up();
  expect(Number(await page.getByLabel('Top side').inputValue())).toBeGreaterThan(5);

  await page.getByRole('group', { name: 'Where it sits across its box' }).getByRole('button', { name: 'Left' }).click();
  await page.getByRole('group', { name: 'Where it sits down its box' }).getByRole('button', { name: 'Bottom' }).click();

  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target).join(', ')}`)).toEqual([]);

  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = await api.call('GET', `/templates/${template.id}`);
  expect(saved.template.elements[0]).toMatchObject({ margin: { left: 0 }, align: 'left', vertical_align: 'bottom' });
  expect(saved.template.elements[0].margin.top).toBeGreaterThan(5);
});

test('draw shapes with the mouse and the keyboard, and edit a polygon’s points', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', {
    name: unique('Shapes'), units: 'mm', width: 60, height: 40, dpi: 300, layering: 'list', elements: [],
  });
  await page.goto(`/templates/${template.id}/edit`);
  const canvas = page.getByRole('img', { name: 'Label preview' });
  await expect(canvas).toBeVisible();
  const c = (await canvas.boundingBox())!;
  const at = (x: number, y: number) => [c.x + (x / 60) * c.width, c.y + (y / 40) * c.height] as const;

  // A square, dragged out with Shift.
  await page.getByRole('button', { name: 'Rectangle' }).click();
  await expect(page.getByRole('button', { name: 'Rectangle' })).toHaveAttribute('aria-pressed', 'true');
  await page.mouse.move(...at(5, 5));
  await page.mouse.down();
  await page.keyboard.down('Shift');
  await page.mouse.move(...at(20, 12), { steps: 6 });
  await page.mouse.up();
  await page.keyboard.up('Shift');
  await expect(page.getByRole('heading', { name: 'Shape 1 (rectangle)' })).toBeVisible();
  await expect(page.locator('#el-0-width')).toHaveValue('15');
  await expect(page.locator('#el-0-height')).toHaveValue('15');
  await page.getByLabel('Fill').selectOption('black');

  // A triangle, a click at each point and Enter.
  await page.getByRole('button', { name: 'Polygon' }).click();
  for (const [x, y] of [[40, 5], [55, 30], [30, 30]] as const) await page.mouse.click(...at(x, y));
  await page.keyboard.press('Enter');
  await expect(page.getByRole('heading', { name: 'Shape 2 (polygon)' })).toBeVisible();
  // Its points: a fourth added on its first side, then moved with the keyboard.
  await page.getByRole('button', { name: 'Add a point between points 1 and 2' }).click();
  await expect(page.getByRole('button', { name: /^Point \d of 4$/ })).toHaveCount(4);
  await page.getByRole('button', { name: 'Point 2 of 4' }).focus();
  await page.keyboard.press('Shift+ArrowRight');
  await page.keyboard.press('Delete');
  await expect(page.getByRole('button', { name: /^Point \d of 3$/ })).toHaveCount(3);

  // From the keyboard, a line in the middle of the label.
  await page.getByRole('button', { name: 'Line' }).focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('heading', { name: 'Shape 3 (line)' })).toBeVisible();

  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target).join(', ')}`)).toEqual([]);

  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = await api.call('GET', `/templates/${template.id}`);
  expect(saved.template.elements.map((e: { shape: string }) => e.shape)).toEqual(['rectangle', 'polygon', 'line']);
  expect(saved.template.elements[0]).toMatchObject({ width: 15, height: 15, fill: 'black' });
  expect(saved.template.elements[1].points).toHaveLength(3);
  expect(saved.template.elements[2]).toMatchObject({ height: 0 });
});

test('turn a template portrait and landscape, and choose which way it is fed', async ({ signedIn: page, api, printer }) => {
  const printerName = unique('QL 62');
  await api.call('POST', '/printers', {
    name: printerName, driver: 'brother_ql', model: 'QL-810W', connection: { type: 'tcp', address: `127.0.0.1:${printer.port}` }, media: 'dk-22205',
  });
  const { template } = await api.call('POST', '/templates', {
    name: unique('Turnable'), units: 'mm', width: 62, height: 40, dpi: 300, layering: 'list',
    elements: [{ type: 'text', x: 2, y: 2, width: 40, height: 10, font_size: 12, content_binding: '{{asset.name}}' }],
  });
  await page.goto(`/templates/${template.id}/edit`);
  await page.getByRole('radio', { name: 'Portrait' }).check();
  await expect(page.locator('#tpl-width')).toHaveValue('40');
  await expect(page.locator('#tpl-height')).toHaveValue('62');
  await expect(page.getByRole('button', { name: /^Text 1/ })).toHaveAttribute('style', /rotate\(90deg\)/);

  // On a 62 mm roll, fed as it fits, then with its width along the roll.
  await page.getByLabel('Printer').selectOption({ label: printerName });
  await expect(page.getByText(`Fits ${printerName}. Fed with its width along the roll.`)).toBeVisible();
  await page.getByRole('radio', { name: 'Landscape' }).check();
  await expect(page.getByText(`Fits ${printerName}. Fed with its width across the roll.`)).toBeVisible();
  await page.getByLabel('Feed').selectOption('along');
  await expect(page.getByText(/fed with its width along the roll, but the roll loaded/)).toBeVisible();
  await page.getByLabel('Feed').selectOption('auto');

  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target).join(', ')}`)).toEqual([]);

  await page.getByRole('button', { name: 'Turn right' }).click();
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = await api.call('GET', `/templates/${template.id}`);
  expect(saved.template).toMatchObject({ width: 40, height: 62 });
  expect(saved.template.elements[0].rotation).toBe(90);
});

test('import SVG artwork as shapes, and save them', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', {
    name: unique('Artwork'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list', elements: [],
  });
  await page.goto(`/templates/${template.id}/edit`);
  const svg = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill-rule="evenodd" d="M12 3 L22 20 H2 Z M11 9 h2 v6 h-2 z"/>' +
    '<circle cx="12" cy="12" r="11" fill="none" stroke="#000"/><text x="2" y="23">Caution</text></svg>';
  await page.getByLabel('Import SVG').setInputFiles({ name: 'warning.svg', mimeType: 'image/svg+xml', buffer: Buffer.from(svg) });
  await expect(page.getByRole('status').filter({ hasText: 'Imported 2 shapes from warning.svg.' })).toContainText('Left out: text');
  await expect(page.getByRole('list', { name: 'Layers' }).getByRole('button', { name: 'Artwork', exact: true })).toHaveCount(2);

  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target).join(', ')}`)).toEqual([]);

  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = await api.call('GET', `/templates/${template.id}`);
  expect(saved.template.elements.map((e: { shape: string; fill_rule?: string }) => [e.shape, e.fill_rule ?? ''])).toEqual([['path', 'evenodd'], ['path', '']]);
});

test('zoom, place guides, line up and group elements, copy them, and see every shortcut', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', {
    name: unique('Workspace'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list',
    elements: [
      { type: 'text', x: 3, y: 3, width: 20, height: 8, font_size: 10, content_binding: 'One' },
      { type: 'text', x: 30, y: 12, width: 20, height: 8, font_size: 10, content_binding: 'Two' },
    ],
  });
  await page.goto(`/templates/${template.id}/edit`);
  const view = page.getByRole('toolbar', { name: 'View' });
  await view.getByRole('button', { name: 'Zoom in' }).click();
  await expect(view.getByText('125%')).toBeVisible();
  await view.getByRole('button', { name: 'Fit' }).click();
  await expect(view.getByText('100%')).toBeVisible();

  // A guide dragged down out of the top ruler, then moved with the keyboard.
  const ruler = page.locator('.ruler').first();
  const r = (await ruler.boundingBox())!;
  const label = (await page.getByRole('img', { name: 'Label preview' }).boundingBox())!;
  await page.mouse.move(r.x + 50, r.y + r.height / 2);
  await page.mouse.down();
  await page.mouse.move(r.x + 50, label.y + label.height / 2, { steps: 8 });
  await page.mouse.up();
  const guide = page.getByRole('slider', { name: 'Horizontal guide' });
  await expect(guide).toHaveCount(1);
  await guide.focus();
  const before = Number(await guide.getAttribute('aria-valuenow'));
  await page.keyboard.press('ArrowDown');
  await expect(guide).toHaveAttribute('aria-valuenow', String(before + 0.5));

  // Both, by a box drawn round them, lined up by their tops, and grouped.
  await page.mouse.move(label.x + 2, label.y + 2);
  await page.mouse.down();
  await page.mouse.move(label.x + label.width - 2, label.y + label.height - 2, { steps: 6 });
  await page.mouse.up();
  await expect(page.getByRole('heading', { name: '2 elements' })).toBeVisible();
  await page.getByRole('group', { name: 'Line up' }).getByRole('button', { name: 'Tops' }).click();
  await page.keyboard.press('Control+g');
  await expect(page.getByRole('heading', { name: 'Group group-1' })).toBeVisible();

  // A click on one takes the group; copied and pasted, it comes back as another.
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: /^Text 1/ }).click();
  await expect(page.getByRole('heading', { name: 'Group group-1' })).toBeVisible();
  await page.keyboard.press('Control+c');
  await page.keyboard.press('Control+v');
  await expect(page.getByRole('heading', { name: 'Group group-2' })).toBeVisible();

  await page.keyboard.press('?');
  const dialog = page.getByRole('dialog', { name: 'Keyboard shortcuts' });
  await expect(dialog).toBeVisible();
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target).join(', ')}`)).toEqual([]);
  await dialog.getByRole('button', { name: 'Close' }).click();

  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = await api.call('GET', `/templates/${template.id}`);
  expect(saved.template.elements.map((e: { group?: string; y: number }) => [e.group, e.y])).toEqual([
    ['group-1', 3], ['group-1', 3], ['group-2', 5], ['group-2', 5],
  ]);
  expect(saved.template.guides.y).toHaveLength(1);
});

test('the editor fits a phone', async ({ signedIn: page, api }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const { template } = await api.call('POST', '/templates', {
    name: unique('Phone'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list',
    elements: [{ type: 'text', x: 3, y: 3, width: 20, height: 8, font_size: 10, content_binding: 'One' }],
  });
  await page.goto(`/templates/${template.id}/edit`);
  await expect(page.getByRole('img', { name: 'Label preview' })).toBeVisible();
  // Nothing reaches past the screen's edge.
  const wide = await page.evaluate(() => document.documentElement.scrollWidth);
  expect(wide).toBeLessThanOrEqual(390);
});

test('the label redraws as it will print while an element is dragged, in the browser', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', {
    name: unique('Live'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list',
    elements: [{ type: 'text', x: 2, y: 2, width: 30, height: 10, font_size: 14, content_binding: 'LIVE' }],
  });
  const wasm = page.waitForResponse((r) => r.url().endsWith('.wasm'));
  await page.goto(`/templates/${template.id}/edit`);
  const preview = page.getByRole('img', { name: 'Label preview' });
  // The live renderer's canvas, once it has loaded; the server's image before.
  await expect(preview.locator('canvas')).toBeVisible();
  // It came compressed, as WebAssembly.
  const served = (await wasm).headers();
  expect([served['content-encoding'], served['content-type']]).toEqual(['br', 'application/wasm']);
  await expect(page.getByRole('status').filter({ hasText: /live preview/i })).toHaveCount(0);
  const dots = () =>
    preview.locator('canvas').evaluate((c: HTMLCanvasElement) => {
      const { data } = c.getContext('2d')!.getImageData(0, 0, c.width, c.height);
      let black = 0;
      let sum = 0;
      for (let i = 0; i < data.length; i += 4) if (data[i] === 0) (black++, (sum += (i / 4) % c.width));
      return { black, meanX: black ? sum / black : 0 };
    });
  await expect.poll(async () => (await dots()).black).toBeGreaterThan(100);
  const before = await dots();

  // Dragged, and not yet let go: the canvas has already redrawn, and the
  // server was not asked.
  const asked: string[] = [];
  page.on('request', (r) => r.url().includes('/templates/preview') && !r.url().includes('preview-data') && asked.push(r.url()));
  const box = (await page.getByRole('button', { name: /^Text 1/ }).boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 120, box.y + box.height / 2, { steps: 8 });
  await expect.poll(async () => (await dots()).meanX).toBeGreaterThan(before.meanX + 100);
  expect((await dots()).black).toBe(before.black);
  await page.mouse.up();
  expect(asked).toEqual([]);
});
