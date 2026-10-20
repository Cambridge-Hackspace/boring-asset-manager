import AxeBuilder from '@axe-core/playwright';
import type { Page } from '@playwright/test';
import { expect, goTo, test, unique } from './fixtures';

// The template editor (#54): a toolbox, the tool's options, the label,
// panels and a status bar, as in Krita.

/** The label's own outline of an element, as opposed to its row in the layers. */
const onLabel = (page: Page, name: RegExp | string) => page.locator('[data-element]').and(page.getByRole('button', { name }));

async function axe(page: Page) {
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target).join(', ')}`)).toEqual([]);
}

/** A tool in the toolbox. */
const tool = (page: Page, name: string) => page.getByRole('toolbar', { name: 'Tools' }).getByRole('button', { name, exact: true });

/** Drags with the mouse from one point to another, in steps. */
async function drag(page: Page, from: [number, number], to: [number, number]) {
  await page.mouse.move(...from);
  await page.mouse.down();
  await page.mouse.move(...to, { steps: 8 });
  await page.mouse.up();
}

/** Where on the screen a point of the label is, as fractions of its width and height. */
async function onPaper(page: Page) {
  const label = page.getByRole('img', { name: 'Label preview' });
  await expect(label).toBeVisible();
  const r = (await label.boundingBox())!;
  return (fx: number, fy: number): [number, number] => [r.x + r.width * fx, r.y + r.height * fy];
}

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
  await page.locator('#tpl-name').fill(name);

  // Size and DPI from the printer's loaded media, then its fit check.
  await page.locator('#tpl-printer').selectOption({ label: printerName });
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

  // Text: drawn with the text tool, dragged, then resized from its corner.
  const at = await onPaper(page);
  await tool(page, 'Text').click();
  await expect(page.getByText(/Drag to draw a text box/)).toBeVisible();
  await drag(page, at(0.05, 0.1), at(0.45, 0.4));
  // Typed at once, as Krita's text tool; Escape finishes.
  await expect(page.locator('[data-text-editor] textarea')).toBeFocused();
  await page.keyboard.type('{{asset.name}}');
  await page.keyboard.press('Escape');
  await expect(tool(page, 'Select and move')).toHaveAttribute('aria-pressed', 'true');
  const text = onLabel(page, /^Text 1/);
  await expect(text).toHaveAttribute('aria-pressed', 'true');
  const box = (await text.boundingBox())!;
  await drag(page, [box.x + box.width / 2, box.y + box.height / 2], [box.x + box.width / 2 + 100, box.y + box.height / 2 + 20]);
  const left = Number(await page.locator('#el-0-x').inputValue());
  expect(left).toBeGreaterThan(10);
  expect((left * 2) % 1).toBe(0); // on the 0.5 mm grid
  const before = Number(await page.locator('#el-0-width').inputValue());
  const corner = (await page.locator('[data-handle="se"]').boundingBox())!;
  await drag(page, [corner.x + 4, corner.y + 4], [corner.x + 64, corner.y + 4]);
  expect(Number(await page.locator('#el-0-width').inputValue())).toBeGreaterThan(before);
  await expect(page.locator('#el-0-x')).toHaveValue(String(left));

  // A code, from the keyboard, moved with the arrow keys.
  await tool(page, 'Barcode or QR code').press('Enter');
  await expect(onLabel(page, /^Barcode 2/)).toHaveAttribute('aria-pressed', 'true');
  const x0 = Number(await page.locator('#el-1-x').inputValue());
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('Shift+ArrowRight');
  await expect(page.locator('#el-1-x')).toHaveValue(String(x0 + 5.5));

  // A binding the server rejects shows on the element.
  await page.locator('#el-1-binding').fill('{{asset.nope}}');
  await page.locator('#el-1-binding').press('Tab');
  await expect(page.getByRole('alert').filter({ hasText: 'nope' }).first()).toBeVisible();
  await page.locator('#el-1-binding').fill('');
  await page.locator('#el-1-binding').press('Tab');

  await axe(page);
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

  // An asset's label, from the editor.
  await page.getByLabel('Find an asset').fill(assetName);
  await page.getByRole('button', { name: 'Find' }).click();
  await page.getByRole('button', { name: assetName }).click();
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
  await page.locator('#tpl-printer').selectOption({ label: printerName });
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
  await expect(page.getByRole('list', { name: 'Before printing' })).toContainText('where the printer does not print');
  await page.getByRole('button', { name: 'Print' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Sent' })).toHaveText(`Sent 1 label to ${printerName}.`);
  await printer.job(2);
});

test('make a template adaptive, see it on each printer, and print it on two rolls', async ({ signedIn: page, api, printer }) => {
  const dieCut = unique('QL 29x90');
  const tape = unique('QL tape');
  await api.call('POST', '/printers', { name: dieCut, driver: 'brother_ql', model: 'QL-810W', connection: { type: 'tcp', address: `127.0.0.1:${printer.port}` }, media: 'dk-11201' });
  await api.call('POST', '/printers', { name: tape, driver: 'brother_ql', model: 'QL-810W', connection: { type: 'tcp', address: `127.0.0.1:${printer.port}` }, media: 'dk-22205' });
  const { template } = await api.call('POST', '/templates', {
    name: unique('Any tag'), units: 'mm', width: 62, height: 29, dpi: 300,
    elements: [
      { type: 'generator', generator_type: 'qr', x: 3, y: 3, width: 23, height: 23 },
      { type: 'text', x: 28, y: 4, width: 31, height: 10, font_size: 12, content_binding: '{{asset.name}}' },
    ],
  });
  const { asset } = await api.call('POST', '/assets', { name: unique('Drill'), generator_keys: [{ generator_type: 'qr', assigned_code: unique('AD').toUpperCase() }] });

  await page.goto(`/templates/${template.id}/edit`);
  await page.getByRole('radio', { name: /Fits several sizes/ }).check();
  await page.locator('#tpl-min_width').fill('25');
  await page.locator('#tpl-min_width').press('Tab');
  // The code pinned to the top left, the name stretching beside it.
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
  await axe(page);
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();

  const { printers } = await api.call('GET', '/printers');
  for (const n of [dieCut, tape]) {
    const p = (printers as { id: string; name: string }[]).find((x) => x.name === n)!;
    await api.call('POST', `/printers/${p.id}/print`, { template_id: template.id, asset_ids: [asset.asset.id], copies: 1 });
  }
  await printer.job(2);
});

test('the top element takes a click, Escape puts back a drag, and layers drag', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', {
    name: unique('Stack'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list',
    elements: [
      { type: 'text', x: 2, y: 2, width: 40, height: 20, font_size: 14, content_binding: '{{asset.name}}' },
      { type: 'generator', generator_type: 'qr', x: 10, y: 5, width: 15, height: 15 },
    ],
  });
  await page.goto(`/templates/${template.id}/edit`);
  const layers = page.getByRole('list', { name: 'Layers' });
  const order = () => layers.locator('li button[title="Double-click to rename"]');
  await expect(order()).toHaveText(['QR code', 'Text: {{asset.name}}']);

  // Where they overlap, a click takes the code on top.
  const code = onLabel(page, 'Barcode 2 (qr)');
  const box = (await code.boundingBox())!;
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await expect(code).toHaveAttribute('aria-pressed', 'true');

  // A drag stopped with Escape puts it back, with nothing to redo.
  const x = await page.locator('#el-1-x').inputValue();
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 80, box.y + box.height / 2, { steps: 5 });
  await expect(page.locator('#el-1-x')).not.toHaveValue(x);
  await page.keyboard.press('Escape');
  await page.mouse.up();
  await expect(page.locator('#el-1-x')).toHaveValue(x);
  await expect(page.getByRole('button', { name: 'Redo' })).toBeDisabled();

  // Dragging the text's layer over the code's brings it to the top.
  const codeLayer = layers.getByRole('listitem').filter({ hasText: 'QR code' });
  await layers.getByRole('listitem').filter({ hasText: 'Text:' }).hover();
  await page.mouse.down();
  await codeLayer.hover();
  await codeLayer.hover();
  await page.mouse.up();
  await expect(order()).toHaveText(['Text: {{asset.name}}', 'QR code']);
  await layers.getByRole('button', { name: 'Text: {{asset.name}}', exact: true }).click();
  await page.getByRole('button', { name: 'Send to back' }).click();
  await expect(order()).toHaveText(['QR code', 'Text: {{asset.name}}']);
  // As saved: nothing to save. Undone, the text is on top again.
  await expect(page.getByRole('button', { name: 'Save' })).toBeDisabled();
  await page.keyboard.press('Control+z');
  await expect(order()).toHaveText(['Text: {{asset.name}}', 'QR code']);

  await axe(page);
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = await api.call('GET', `/templates/${template.id}`);
  expect(saved.template.elements.map((e: { type: string }) => e.type)).toEqual(['generator', 'text']);
});

test('draw shapes on the label, and polygons point by point', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', { name: unique('Shapes'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list', elements: [] });
  await page.goto(`/templates/${template.id}/edit`);
  const at = await onPaper(page);

  await tool(page, 'Rectangle').click();
  await drag(page, at(0.1, 0.1), at(0.4, 0.6));
  await tool(page, 'Ellipse').click();
  await drag(page, at(0.5, 0.1), at(0.7, 0.5));
  await tool(page, 'Line').click();
  await drag(page, at(0.1, 0.9), at(0.4, 0.7));
  // A click alone places one at its usual size.
  await tool(page, 'Ellipse').click();
  await page.mouse.click(...at(0.05, 0.05));

  await tool(page, 'Polygon').click();
  await page.mouse.click(...at(0.75, 0.6));
  await page.mouse.click(...at(0.95, 0.6));
  await expect(page.getByText(/Click to place the next point/)).toBeVisible();
  await page.mouse.click(...at(0.85, 0.95));
  await page.keyboard.press('Enter');
  await expect(page.getByRole('list', { name: 'Layers' }).getByRole('listitem')).toHaveCount(5);

  // Escape lets go of a polygon begun, and then of the tool.
  await tool(page, 'Polygon').click();
  await page.mouse.click(...at(0.5, 0.8));
  await page.keyboard.press('Escape');
  await page.keyboard.press('Escape');
  await expect(tool(page, 'Select and move')).toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByRole('list', { name: 'Layers' }).getByRole('listitem')).toHaveCount(5);

  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = await api.call('GET', `/templates/${template.id}`);
  const shapes = saved.template.elements as { shape: string; flip?: boolean; width: number; points?: number[][] }[];
  expect(shapes.map((s) => s.shape)).toEqual(['rectangle', 'ellipse', 'line', 'ellipse', 'polygon']);
  expect(shapes[0]!.width).toBeGreaterThan(15);
  expect(shapes[2]!.flip).toBe(true);
  expect(shapes[4]!.points).toHaveLength(3);
});

test('pad a code, resize it with its handles, and see it fill its box less the padding', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', {
    name: unique('Padding'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list',
    elements: [{ type: 'generator', generator_type: 'qr', x: 5, y: 2, width: 20, height: 20, content_binding: 'HELLO' }],
  });
  await page.goto(`/templates/${template.id}/edit`);
  const live = page.getByRole('img', { name: 'Label preview' }).locator('canvas');
  await expect(live).toBeVisible();
  await page.getByRole('list', { name: 'Layers' }).getByRole('button', { name: 'QR code: HELLO', exact: true }).click();
  // Its padding shaded on the label.
  await expect(page.locator('[data-padding="0"]')).toBeAttached();
  // Resized with its ordinary handle.
  const corner = (await page.locator('[data-handle="se"]').boundingBox())!;
  await drag(page, [corner.x + 4, corner.y + 4], [corner.x + 60, corner.y + 4]);
  const width = Number(await page.locator('#el-0-width').inputValue());
  expect(width).toBeGreaterThan(20);
  // 2 mm all round.
  await page.locator('#el-0-padding').fill('2mm');
  await page.locator('#el-0-padding').press('Enter');
  // The code's dots keep inside the box less its padding: 2 mm is 23.6 dots at 300 DPI.
  const inside = await live.evaluate((c: HTMLCanvasElement, box: { x: number; y: number; w: number; h: number }) => {
    const { data } = c.getContext('2d')!.getImageData(0, 0, c.width, c.height);
    const dot = 300 / 25.4;
    let [minX, minY, maxX, maxY] = [Infinity, Infinity, -1, -1];
    for (let y = 0; y < c.height; y++)
      for (let x = 0; x < c.width; x++)
        if (data[(y * c.width + x) * 4] === 0) [minX, minY, maxX, maxY] = [Math.min(minX, x), Math.min(minY, y), Math.max(maxX, x), Math.max(maxY, y)];
    return { left: minX - box.x * dot, top: minY - box.y * dot, right: (box.x + box.w) * dot - maxX - 1, bottom: (box.y + box.h) * dot - maxY - 1 };
  }, { x: 5, y: 2, w: width, h: 20 });
  for (const side of ['left', 'top', 'right', 'bottom'] as const) expect(inside[side]).toBeGreaterThanOrEqual(23);
  // Where it sits: against the left, then nothing on its left, warned of.
  // (Centered in its wide box, the space beside it would be more than enough.)
  await page.locator('#el-0-code-align').selectOption('left');
  await page.locator('#el-0-code-valign').selectOption('bottom');
  await page.getByRole('button', { name: 'Each side' }).click();
  await page.locator('#el-0-padding-left').fill('0');
  await page.locator('#el-0-padding-left').press('Tab');
  // Its bottom too: 2 mm is less than this code's standard, with nothing left over below it.
  await expect(page.getByText("Less than the code's standard quiet zone on its bottom and left: some scanners need it.")).toBeVisible();
  await axe(page);
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = await api.call('GET', `/templates/${template.id}`);
  expect(saved.template.elements[0]).toMatchObject({ margin: { left: 0, top: 2, right: 2, bottom: 2 }, margin_unit: 'length', align: 'left', vertical_align: 'bottom' });
});

test('turn a template portrait and landscape, and choose which way it is fed', async ({ signedIn: page, api, printer }) => {
  const printerName = unique('QL');
  await api.call('POST', '/printers', { name: printerName, driver: 'brother_ql', model: 'QL-810W', connection: { type: 'tcp', address: `127.0.0.1:${printer.port}` }, media: 'dk-22205' });
  const { template } = await api.call('POST', '/templates', {
    name: unique('Turned'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list',
    elements: [{ type: 'text', x: 2, y: 2, width: 40, height: 10, font_size: 12, content_binding: '{{asset.name}}' }],
  });
  await page.goto(`/templates/${template.id}/edit`);
  await page.getByRole('radio', { name: 'Portrait' }).click();
  await expect(page.locator('#tpl-width')).toHaveValue('29');
  await expect(page.locator('#tpl-height')).toHaveValue('62');
  await page.locator('#tpl-printer').selectOption({ label: printerName });
  await page.locator('#tpl-feed').selectOption('along');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = await api.call('GET', `/templates/${template.id}`);
  expect(saved.template).toMatchObject({ width: 29, height: 62, feed: 'along' });
  expect(saved.template.elements[0]).toMatchObject({ rotation: 90 });
});

test('import SVG artwork as shapes, and save them', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', { name: unique('Artwork'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list', elements: [] });
  await page.goto(`/templates/${template.id}/edit`);
  await page.getByLabel('Import SVG').setInputFiles({
    name: 'arrow.svg',
    mimeType: 'image/svg+xml',
    buffer: Buffer.from('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 10"><path d="M0 5 L15 5 L15 0 L20 5 L15 10 L15 5 Z"/><text x="1" y="9">hi</text></svg>'),
  });
  await expect(page.getByText(/Imported 1 shape from arrow.svg. Left out: /)).toBeVisible();
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = await api.call('GET', `/templates/${template.id}`);
  expect(saved.template.elements).toHaveLength(1);
  expect(saved.template.elements[0]).toMatchObject({ type: 'shape', shape: 'path' });
});

test('Tab hides everything but the label, which stays where it was', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', {
    name: unique('Canvas only'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list',
    elements: [{ type: 'text', x: 3, y: 3, width: 30, height: 10, font_size: 12, content_binding: 'Still' }],
  });
  await page.goto(`/templates/${template.id}/edit`);
  const label = page.getByRole('img', { name: 'Label preview' });
  await expect(label).toBeVisible();
  const before = (await label.boundingBox())!;
  // With nothing focused, as after clicking off the label.
  await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
  await page.keyboard.press('Tab');
  await expect(page.getByRole('complementary', { name: 'Panels' })).toBeHidden();
  const leave = page.getByRole('button', { name: /Canvas only. Press Tab/ });
  await expect(leave).toHaveCSS('opacity', '1');
  await expect.poll(async () => {
    const after = (await label.boundingBox())!;
    return Math.max(Math.abs(after.x - before.x), Math.abs(after.y - before.y));
  }).toBeLessThan(1);
  // The message fades, and Tab brings everything back.
  await expect(leave).toHaveCSS('opacity', '0', { timeout: 5000 });
  await page.keyboard.press('Tab');
  await expect(page.getByRole('complementary', { name: 'Panels' })).toBeVisible();
});

test('the editor fits a phone', async ({ signedIn: page, api }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const { template } = await api.call('POST', '/templates', {
    name: unique('Phone'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list',
    elements: [{ type: 'text', x: 3, y: 3, width: 20, height: 8, font_size: 10, content_binding: 'One' }],
  });
  await page.goto(`/templates/${template.id}/edit`);
  await expect(page.getByRole('img', { name: 'Label preview' })).toBeVisible();
  // Nothing reaches past the screen's edge; the panels are below the label.
  const wide = await page.evaluate(() => document.documentElement.scrollWidth);
  expect(wide).toBeLessThanOrEqual(390);
  const label = (await page.getByRole('img', { name: 'Label preview' }).boundingBox())!;
  const layers = (await page.getByRole('button', { name: 'Layers', expanded: true }).boundingBox())!;
  expect(layers.y).toBeGreaterThan(label.y + label.height);
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
  const box = (await onLabel(page, /^Text 1/).boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 120, box.y + box.height / 2, { steps: 8 });
  await expect.poll(async () => (await dots()).meanX).toBeGreaterThan(before.meanX + 50);
  expect((await dots()).black).toBe(before.black);
  await page.mouse.up();
  expect(asked).toEqual([]);
});

test('zoom toward the pointer, pan, the zoom keys, rulers, the overview and print size', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', {
    name: unique('View'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list',
    elements: [{ type: 'shape', shape: 'rectangle', x: 10, y: 5, width: 10, height: 10, stroke: 'black', stroke_width: 0.5 }],
  });
  await page.goto(`/templates/${template.id}/edit`);
  const label = page.getByRole('img', { name: 'Label preview' });
  await expect(label).toBeVisible();
  const zoom = page.locator('#zoom-level');
  const rect = () => onLabel(page, /^Shape 1/).boundingBox().then((b) => b!);

  // Ctrl+wheel zooms in toward the pointer: the corner under it stays there.
  const r0 = await rect();
  await page.mouse.move(r0.x, r0.y);
  const before = await zoom.inputValue();
  await page.keyboard.down('Control');
  await page.mouse.wheel(0, -300);
  await page.keyboard.up('Control');
  await expect(zoom).not.toHaveValue(before);
  const r1 = await rect();
  expect(r1.width).toBeGreaterThan(r0.width * 1.5);
  expect(Math.abs(r1.x - r0.x)).toBeLessThan(2);
  expect(Math.abs(r1.y - r0.y)).toBeLessThan(2);

  // Space and drag, or the middle button, moves the view; the wheel scrolls it.
  await page.mouse.move(r1.x + 200, r1.y + 50);
  await page.keyboard.down(' ');
  await drag(page, [r1.x + 200, r1.y + 50], [r1.x + 260, r1.y + 90]);
  await page.keyboard.up(' ');
  let r2 = await rect();
  expect(Math.round(r2.x - r1.x)).toBe(60);
  expect(Math.round(r2.y - r1.y)).toBe(40);
  await page.mouse.move(r2.x + 200, r2.y + 50);
  await page.mouse.down({ button: 'middle' });
  await page.mouse.move(r2.x + 170, r2.y + 50, { steps: 4 });
  await page.mouse.up({ button: 'middle' });
  const r3 = await rect();
  expect(Math.round(r3.x - r2.x)).toBe(-30);
  await page.mouse.wheel(0, 50);
  r2 = await rect();
  expect(Math.round(r3.y - r2.y)).toBe(50);
  // Nothing was moved on the label.
  await expect(page.getByRole('button', { name: 'Undo' })).toBeDisabled();

  // 2 fits the label; 0 goes back to the zoom before, and 0 again fits.
  await page.mouse.move(5, 5);
  await page.keyboard.press('2');
  const fitted = (await label.boundingBox())!;
  const fittedZoom = await zoom.inputValue();
  await page.keyboard.press('0');
  await expect(zoom).not.toHaveValue(fittedZoom);
  await page.keyboard.press('0');
  await expect(zoom).toHaveValue(fittedZoom);
  // 3 fills the width, 32 pixels in from each side; 1 is 100%, a printer
  // dot a screen pixel: 62 mm at 300 DPI.
  await page.keyboard.press('1');
  await page.keyboard.press('3');
  const desk = (await page.locator('[data-ruler="x"]').boundingBox())!;
  const wide = (await label.boundingBox())!;
  expect(Math.round(wide.x - desk.x)).toBe(32);
  expect(Math.round(wide.width)).toBe(Math.round(desk.width - 64));
  expect(wide.width).toBeGreaterThanOrEqual(fitted.width - 1);
  await page.keyboard.press('1');
  await expect(zoom).toHaveValue('100%');
  expect(Math.round((await label.boundingBox())!.width)).toBe(732);

  // The rulers mark the pointer.
  const r4 = await rect();
  await page.mouse.move(r4.x + 5, r4.y + 5);
  const marker = page.locator('[data-ruler="x"] line.stroke-blue-600');
  expect(Math.abs(Number(await marker.getAttribute('x1')) - (r4.x + 5 - (await page.locator('[data-ruler="x"]').boundingBox())!.x))).toBeLessThan(1.5);

  // Dragging in the overview moves the view there.
  await page.keyboard.press('Control+Shift+A');
  const overview = page.locator('[data-overview-view]').locator('..');
  const o = (await overview.boundingBox())!;
  const l0 = (await label.boundingBox())!;
  await page.mouse.click(o.x + o.width * 0.9, o.y + o.height * 0.5);
  const l1 = (await label.boundingBox())!;
  expect(l1.x).toBeLessThan(l0.x - 50);

  // Print size: at 100%, the label is its real size, by the browser's inch until calibrated.
  await page.getByRole('button', { name: 'Print size' }).click();
  await page.mouse.move(5, 5);
  await page.keyboard.press('1');
  await expect(page.getByRole('button', { name: 'Approximate: calibrate' })).toBeVisible();
  expect(Math.round((await label.boundingBox())!.width)).toBe(Math.round((62 * 96) / 25.4));
  await axe(page);
});

test('turn from outside a corner, resize with the modifiers, select with a box, and the menu', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', {
    name: unique('Transform'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list',
    elements: [
      { type: 'shape', shape: 'rectangle', x: 4, y: 4, width: 10, height: 10, stroke: 'black', stroke_width: 0.5 },
      { type: 'shape', shape: 'ellipse', x: 30, y: 4, width: 10, height: 10, stroke: 'black', stroke_width: 0.5 },
      { type: 'shape', shape: 'rectangle', x: 33, y: 7, width: 4, height: 4, stroke: 'black', stroke_width: 0.5 },
    ],
  });
  await page.goto(`/templates/${template.id}/edit`);
  await expect(page.getByRole('img', { name: 'Label preview' })).toBeVisible();
  const rect = onLabel(page, /^Shape 1/);
  const center = async () => {
    const b = (await rect.boundingBox())!;
    return [b.x + b.width / 2, b.y + b.height / 2] as const;
  };
  await rect.click();

  // Turning: grabbed just outside the top right corner, swept a quarter turn round.
  const zone = (await page.locator('[data-rotate="ne"]').boundingBox())!;
  const [cx, cy] = await center();
  const grab: [number, number] = [zone.x + zone.width / 2, zone.y + zone.height / 2];
  const r = Math.hypot(grab[0] - cx, grab[1] - cy);
  const a0 = Math.atan2(grab[1] - cy, grab[0] - cx);
  const toward = (deg: number): [number, number] => [cx + r * Math.cos(a0 + (deg * Math.PI) / 180), cy + r * Math.sin(a0 + (deg * Math.PI) / 180)];
  await page.mouse.move(...grab);
  await expect(page.getByText(/Drag to turn/)).toBeVisible();
  await page.mouse.down();
  await page.mouse.move(...toward(43), { steps: 6 });
  // Near 45°: snapped to it, and said beside the pointer.
  await expect(page.locator('#el-0-rotation')).toHaveValue('45');
  await expect(page.locator('[data-readout]')).toHaveText('45°');
  // Shift turns freely; Ctrl in steps of 15°, changing at once as the keys go down.
  await page.keyboard.down('Shift');
  await expect(page.locator('#el-0-rotation')).not.toHaveValue('45');
  await page.keyboard.up('Shift');
  await page.mouse.move(...toward(52), { steps: 3 });
  await page.keyboard.down('Control');
  await expect(page.locator('#el-0-rotation')).toHaveValue('45');
  await page.mouse.move(...toward(58), { steps: 3 });
  await expect(page.locator('#el-0-rotation')).toHaveValue('60');
  await page.keyboard.up('Control');
  await page.mouse.up();
  await page.locator('#el-0-rotation').fill('0');
  await page.locator('#el-0-rotation').press('Enter');

  // Resizing from the bottom right corner: Shift keeps proportions while held.
  const corner = (await page.locator('[data-handle="se"]').boundingBox())!;
  await page.mouse.move(corner.x + 4, corner.y + 4);
  await page.mouse.down();
  await page.keyboard.down('Shift');
  await page.mouse.move(corner.x + 4 + 80, corner.y + 4 + 10, { steps: 6 });
  const w = Number(await page.locator('#el-0-width').inputValue());
  expect(w).toBeGreaterThan(10);
  await expect(page.locator('#el-0-height')).toHaveValue(String(w));
  await expect(page.locator('[data-readout]')).toContainText('×');
  // Let go of Shift mid-drag, and the height follows the pointer again.
  await page.keyboard.up('Shift');
  await expect(page.locator('#el-0-height')).not.toHaveValue(String(w));
  await page.mouse.up();
  // Ctrl resizes from the center: the left side moves out too.
  const x0 = Number(await page.locator('#el-0-x').inputValue());
  const east = (await page.locator('[data-handle="e"]').boundingBox())!;
  await page.mouse.move(east.x + 4, east.y + 4);
  await page.mouse.down();
  await page.keyboard.down('Control');
  await page.mouse.move(east.x + 4 + 40, east.y + 4, { steps: 4 });
  // Let go of the mouse first: Ctrl let go mid-drag puts the left side back.
  await page.mouse.up();
  await page.keyboard.up('Control');
  expect(Number(await page.locator('#el-0-x').inputValue())).toBeLessThan(x0);

  // A box dragged left to right takes what it covers; right to left, what it touches.
  const paper = await onPaper(page);
  await page.keyboard.press('Control+Shift+A');
  await drag(page, paper(0.4, 0.05), paper(0.7, 0.6));
  await expect(page.locator('[data-element][aria-pressed="true"]')).toHaveCount(2);
  await page.keyboard.press('Escape');
  await page.mouse.move(...paper(0.7, 0.05));
  await page.mouse.down();
  await page.mouse.move(...paper(0.55, 0.3), { steps: 4 });
  await expect(page.locator('[data-band="touching"]')).toBeVisible();
  await page.mouse.up();
  await expect(page.locator('[data-element][aria-pressed="true"]')).toHaveCount(2);

  // Ctrl+click takes the element beneath the one selected there, round to the top.
  const small = (await onLabel(page, /^Shape 3/).boundingBox())!;
  const spot: [number, number] = [small.x + small.width / 2, small.y + small.height / 2];
  await page.mouse.click(...spot);
  await expect(onLabel(page, /^Shape 3/)).toHaveAttribute('aria-pressed', 'true');
  await page.keyboard.down('Control');
  await page.mouse.click(...spot);
  await page.keyboard.up('Control');
  await expect(onLabel(page, /^Shape 2/)).toHaveAttribute('aria-pressed', 'true');

  // The right-click menu.
  await page.mouse.click(...spot, { button: 'right' });
  const menu = page.getByRole('menu', { name: 'Selection' });
  await expect(menu.getByRole('menuitem').first()).toBeFocused();
  await axe(page);
  // Right-clicked, the square on top was taken, as it was not selected.
  await expect(onLabel(page, /^Shape 3/)).toHaveAttribute('aria-pressed', 'true');
  await menu.getByRole('menuitem', { name: 'Send to back' }).click();
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = await api.call('GET', `/templates/${template.id}`);
  expect(saved.template.elements.map((e: { shape: string; width: number }) => `${e.shape} ${e.width}`)[0]).toBe('rectangle 4');
});

test('layers show thumbnails, hide while editing, rename, and the history goes back', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', {
    name: unique('Layers'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list',
    elements: [{ type: 'shape', shape: 'rectangle', x: 4, y: 4, width: 20, height: 20, stroke: 'none', fill: 'black' }],
  });
  await page.goto(`/templates/${template.id}/edit`);
  const live = page.getByRole('img', { name: 'Label preview' }).locator('canvas');
  await expect(live).toBeVisible();
  const black = (canvas: ReturnType<typeof page.locator>) =>
    canvas.evaluate((c: HTMLCanvasElement) => {
      const { data } = c.getContext('2d')!.getImageData(0, 0, c.width, c.height);
      let n = 0;
      for (let i = 0; i < data.length; i += 4) if (data[i]! < 128) n++;
      return n;
    });
  // The layer's thumbnail shows the black square.
  const thumb = page.getByRole('list', { name: 'Layers' }).locator('canvas').first();
  await expect.poll(() => black(thumb)).toBeGreaterThan(200);
  await expect.poll(() => black(live)).toBeGreaterThan(10000);

  // Hidden, it leaves the label drawn while editing; it still prints.
  await page.getByRole('button', { name: 'Hide Rectangle' }).click();
  await expect.poll(() => black(live)).toBe(0);
  await expect(page.getByText('Hidden elements still print.')).toBeVisible();
  await page.getByRole('button', { name: 'Hide Rectangle' }).click();
  await expect.poll(() => black(live)).toBeGreaterThan(10000);

  // Renamed by double-clicking its layer.
  await page.getByRole('list', { name: 'Layers' }).getByRole('button', { name: 'Rectangle', exact: true }).dblclick();
  await page.getByLabel('Name of Rectangle').fill('Background');
  await page.getByLabel('Name of Rectangle').press('Enter');
  await expect(page.getByRole('list', { name: 'Layers' }).getByRole('button', { name: 'Background', exact: true })).toBeVisible();

  // The history names each change, and goes back to any.
  await page.getByRole('list', { name: 'Layers' }).getByRole('button', { name: 'Background', exact: true }).click();
  await page.getByRole('button', { name: 'Turn 90° right' }).click();
  const history = page.getByRole('list', { name: 'History' });
  await expect(history.getByRole('button')).toHaveText(['As opened', 'Rename', 'Change Background']);
  await history.getByRole('button', { name: 'Rename' }).click();
  await expect(page.locator('#el-0-rotation')).toHaveValue('0');
  await expect(history.getByRole('button', { name: 'Change Background' })).toHaveClass(/italic/);
  await axe(page);

  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = await api.call('GET', `/templates/${template.id}`);
  expect(saved.template.elements[0]).toMatchObject({ name: 'Background' });
  expect(saved.template.elements[0].rotation).toBeUndefined();
});

test('guides out of the rulers, snapping to them and the grid, and dragging them off', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', {
    name: unique('Guides'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list',
    elements: [{ type: 'shape', shape: 'rectangle', x: 4, y: 4, width: 10, height: 10, stroke: 'black', stroke_width: 0.5 }],
  });
  await page.goto(`/templates/${template.id}/edit`);
  const at = await onPaper(page);
  const ruler = (await page.locator('[data-ruler="x"]').boundingBox())!;

  // Out of the top ruler, down onto the label: a guide across, at about 14.5 mm down.
  const [, gy] = at(0.5, 14.5 / 29);
  await page.mouse.move(at(0.5, 0)[0], ruler.y + ruler.height / 2);
  await page.mouse.down();
  await page.mouse.move(at(0.5, 0)[0], gy, { steps: 6 });
  // A line across has no height: there, rather than "visible".
  await expect(page.locator('[data-guide-dragging]')).toBeAttached();
  await page.mouse.up();
  await expect(page.locator('[data-guide^="y"]')).toHaveCount(1);
  await expect(page.getByRole('list', { name: 'History' }).getByRole('button').last()).toHaveText('Add a guide');
  const guideY = (await api.call('GET', `/templates/${template.id}`)).template.guides; // not saved yet
  expect(guideY).toBeUndefined();

  // The square dragged down near it: its bottom edge snaps to it, the line shown.
  const sq = (await onLabel(page, /^Shape 1/).boundingBox())!;
  const from: [number, number] = [sq.x + sq.width / 2, sq.y + sq.height / 2];
  const guideLine = (await page.locator('line.guide').first().boundingBox())!;
  await page.mouse.move(...from);
  await page.mouse.down();
  await page.mouse.move(from[0], from[1] + (guideLine.y - (sq.y + sq.height)) + 3, { steps: 6 });
  await expect(page.locator('[data-snapped="y"]')).toBeAttached();
  await page.mouse.up();
  await expect(page.locator('[data-snapped="y"]')).toHaveCount(0);
  const snappedY = Number(await page.locator('#el-0-y').inputValue());
  // With Shift, it moves freely: along one axis, off every line.
  const sq2 = (await onLabel(page, /^Shape 1/).boundingBox())!;
  await page.mouse.move(sq2.x + sq2.width / 2, sq2.y + sq2.height / 2);
  await page.mouse.down();
  await page.keyboard.down('Shift');
  await page.mouse.move(sq2.x + sq2.width / 2 + 2, sq2.y + sq2.height / 2 + 7, { steps: 4 });
  await expect(page.locator('[data-snapped]')).toHaveCount(0);
  await page.keyboard.up('Shift');
  await page.mouse.up();
  await page.keyboard.press('Control+z');
  await expect(page.locator('#el-0-y')).toHaveValue(String(snappedY));
  // Saved: its bottom edge is exactly on the guide.
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const guided = (await api.call('GET', `/templates/${template.id}`)).template;
  expect(guided.elements[0].y + guided.elements[0].height).toBe(guided.guides.y[0]);

  // The grid, shown, every 5 mm.
  await page.getByRole('checkbox', { name: 'Show it' }).check();
  await page.locator('#grid-x').fill('5');
  await page.locator('#grid-x').press('Enter');
  await expect(page.locator('[data-grid] line')).toHaveCount(13 + 6);

  // Dragged off the label, the guide comes off.
  const line = (await page.locator('[data-guide^="y"]').boundingBox())!;
  await page.mouse.move(at(0.3, 0)[0], line.y + line.height / 2);
  await page.mouse.down();
  await page.mouse.move(at(0.3, 0)[0], ruler.y - 40, { steps: 6 });
  await page.mouse.up();
  await expect(page.locator('[data-guide]')).toHaveCount(0);
  await page.getByRole('list', { name: 'History' }).getByRole('button', { name: 'Remove a guide' }).waitFor();
  await axe(page);
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = await api.call('GET', `/templates/${template.id}`);
  expect(saved.template.guides).toBeUndefined();
  expect(saved.template.grid).toEqual({ x: 5, y: 5, visible: true });
});

test('draw with the modifiers, the pen and the measure, and edit the points', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', { name: unique('Drawing'), units: 'mm', width: 60, height: 30, dpi: 300, layering: 'list', elements: [] });
  await page.goto(`/templates/${template.id}/edit`);
  // Snapping off, to draw exactly where the pointer goes.
  await page.getByRole('button', { name: 'Snap to ▾' }).click();
  for (const item of ['The grid', 'Guides', 'Other elements', 'The label']) await page.getByRole('menuitemcheckbox', { name: new RegExp(item) }).click();
  await page.keyboard.press('Escape');
  const at = await onPaper(page);
  const mm = (x: number, y: number) => at(x / 60, y / 30);

  // A square with Shift, from its center with Ctrl; Alt moves it while drawing.
  await tool(page, 'Rectangle').click();
  await page.mouse.move(...mm(10, 10));
  await page.mouse.down();
  await page.keyboard.down('Shift');
  await page.keyboard.down('Control');
  await page.mouse.move(...mm(14, 12), { steps: 4 });
  await expect(page.locator('[data-readout]')).toHaveText('8.00 × 8.00 mm');
  await page.keyboard.up('Control');
  await page.keyboard.up('Shift');
  await page.keyboard.down('Alt');
  await page.mouse.move(...mm(16, 14), { steps: 4 });
  await page.keyboard.up('Alt');
  await page.mouse.up();
  // Let go of Shift and Ctrl, it runs from where it was pressed, which Alt
  // moved 2 across and down: from (12, 12) to (16, 14).
  await expect(page.locator('#el-0-x')).toHaveValue('12');
  await expect(page.locator('#el-0-y')).toHaveValue('12');

  // A line in 15° steps with Shift.
  await tool(page, 'Line').click();
  await page.mouse.move(...mm(30, 25));
  await page.mouse.down();
  await page.keyboard.down('Shift');
  await page.mouse.move(...mm(40, 20.5), { steps: 4 });
  await expect(page.locator('[data-readout]')).toContainText('at 30°');
  await page.mouse.up();
  await page.keyboard.up('Shift');

  // The pen: a corner, a curve pulled out, a corner, then the first point again to close it.
  await tool(page, 'Pen').click();
  await page.mouse.click(...mm(40, 3));
  await page.mouse.move(...mm(55, 5));
  await page.mouse.down();
  await page.mouse.move(...mm(58, 12), { steps: 4 });
  await page.mouse.up();
  await page.mouse.click(...mm(42, 14));
  await expect(page.locator('[data-pen]')).toBeAttached();
  await page.mouse.click(...mm(40, 3));
  // An open path: a point taken back with a right-click, finished with a double-click.
  await tool(page, 'Pen').click();
  await page.mouse.click(...mm(2, 26));
  await page.mouse.click(...mm(10, 22));
  await page.mouse.click(...mm(12, 24), { button: 'right' });
  await page.mouse.click(...mm(11, 29));
  await page.mouse.dblclick(...mm(20, 26));
  await expect(page.getByRole('list', { name: 'Layers' }).getByRole('listitem')).toHaveCount(4);

  // Editing the closed path's points: a double-click enters; one on the outline adds a point.
  const closed = onLabel(page, /^Shape 3/);
  await closed.click();
  await closed.dblclick();
  await expect(page.locator('[data-point-editor] [data-node]')).toHaveCount(3);
  await page.mouse.dblclick(...mm(41, 8.5));
  await expect(page.locator('[data-point-editor] [data-node]')).toHaveCount(4);
  // A point dragged; Shift+click makes one smooth; Delete takes the chosen one off.
  const node = (await page.locator('[data-node="0-0"]').boundingBox())!;
  await page.mouse.move(node.x + 4, node.y + 4);
  await page.mouse.down();
  await page.mouse.move(node.x + 4, node.y - 20, { steps: 4 });
  await page.mouse.up();
  await expect(page.locator('#el-2-y')).not.toHaveValue('3');
  await page.keyboard.down('Shift');
  await page.locator('[data-node="0-2"]').click();
  await page.keyboard.up('Shift');
  await expect(page.locator('[data-node-handle^="0-2-"]')).toHaveCount(2);
  await page.locator('[data-node="0-3"]').click();
  await page.keyboard.press('Delete');
  await expect(page.locator('[data-point-editor] [data-node]')).toHaveCount(3);
  await page.keyboard.press('Escape');
  await expect(page.locator('[data-point-editor]')).toHaveCount(0);

  // The measure: nothing is added.
  await tool(page, 'Measure').click();
  await drag(page, mm(10, 5), mm(13, 9));
  await expect(page.locator('[data-measured]')).toContainText('5.00 mm at 306.9°');
  await expect(page.getByRole('list', { name: 'Layers' }).getByRole('listitem')).toHaveCount(4);
  await axe(page);

  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = (await api.call('GET', `/templates/${template.id}`)).template.elements as { shape: string; d?: string; fill?: string; width: number; height: number }[];
  expect(saved.map((e) => e.shape)).toEqual(['rectangle', 'line', 'path', 'path']);
  expect(saved[0]).toMatchObject({ width: 4, height: 2 });
  expect(saved[2]!.d).toMatch(/C.* Z$/);
  // From (2, 26) down to (11, 29) and up to (20, 26): 18 across, 3 down.
  expect(saved[3]!.d).toBe('M0 0 L0.5 1 L1 0');
  expect(saved[3]!.fill).toBe('none');
});

test('type on the label, with the text bar, and fill the box', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', { name: unique('Typing'), units: 'mm', width: 60, height: 30, dpi: 300, layering: 'list', elements: [] });
  await page.goto(`/templates/${template.id}/edit`);
  const live = page.getByRole('img', { name: 'Label preview' }).locator('canvas');
  await expect(live).toBeVisible();
  const black = () =>
    live.evaluate((c: HTMLCanvasElement) => {
      const { data } = c.getContext('2d')!.getImageData(0, 0, c.width, c.height);
      let n = 0;
      for (let i = 0; i < data.length; i += 4) if (data[i] === 0) n++;
      return n;
    });
  const at = await onPaper(page);

  // A click with the text tool: a single line, typed at once.
  await tool(page, 'Text').click();
  await page.mouse.click(...at(0.1, 0.2));
  const area = page.locator('[data-text-editor] textarea');
  await expect(area).toBeFocused();
  await expect(page.getByRole('toolbar', { name: 'Text' })).toBeVisible();
  await page.keyboard.type('Tag ');
  // A field from the text bar, at the caret.
  await page.getByLabel('Insert a field').selectOption('{{asset.name}}');
  await expect(area).toHaveValue('Tag {{asset.name}}');
  await expect(area).toBeFocused();
  await page.getByRole('button', { name: 'Bold' }).click();
  await expect(area).toBeFocused();
  // While typed, it is left out of the label drawn, and shown in the font it prints in.
  expect(await black()).toBe(0);
  expect(await area.evaluate((el) => getComputedStyle(el).fontFamily)).toContain('BAM Printed Sans');
  await page.keyboard.press('Escape');
  await expect(area).toHaveCount(0);
  await expect.poll(black).toBeGreaterThan(500);

  // Double-clicked, typed again; filling its box, the text grows at once.
  const text = onLabel(page, /^Text 1/);
  await text.dblclick();
  await expect(area).toBeFocused();
  // Short, it has room to grow: it filled the box's width already.
  await page.keyboard.press('Control+a');
  await page.keyboard.type('Tag');
  await expect.poll(black).toBe(0);
  const before = await area.evaluate((el) => parseFloat(getComputedStyle(el).fontSize));
  await page.getByRole('button', { name: 'Fill the box' }).click();
  await expect.poll(() => area.evaluate((el) => parseFloat(getComputedStyle(el).fontSize))).toBeGreaterThan(before);
  await page.keyboard.press('Escape');

  // A box dragged out wraps; one left empty comes off.
  await tool(page, 'Text').click();
  await drag(page, at(0.1, 0.6), at(0.9, 0.9));
  await expect(area).toBeFocused();
  await page.keyboard.type('Wrapped across the box');
  await page.keyboard.press('Escape');
  await tool(page, 'Text').click();
  await page.mouse.click(...at(0.8, 0.1));
  await expect(area).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('list', { name: 'Layers' }).getByRole('listitem')).toHaveCount(2);
  await axe(page);

  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = (await api.call('GET', `/templates/${template.id}`)).template.elements;
  expect(saved[0]).toMatchObject({ type: 'text', content_binding: 'Tag', font_weight: 'bold', fit: 'fill' });
  // A single line: no wrapping, which the server leaves out.
  expect(saved[0].wrap).toBeUndefined();
  expect(saved[1]).toMatchObject({ type: 'text', content_binding: 'Wrapped across the box', wrap: true });
});

test('a label that prints mirrored and turned, shown as printed and as applied, and mirrored for one print', async ({ signedIn: page, api, printer }) => {
  const printerName = unique('QL');
  await api.call('POST', '/printers', { name: printerName, driver: 'brother_ql', model: 'QL-810W', connection: { type: 'tcp', address: `127.0.0.1:${printer.port}` }, media: 'dk-22205' });
  const code = unique('G').toUpperCase();
  const { asset } = await api.call('POST', '/assets', { name: unique('Window'), generator_keys: [{ generator_type: 'qr', assigned_code: code }] });
  const { template } = await api.call('POST', '/templates', {
    name: unique('Glass'), units: 'mm', width: 62, height: 29, dpi: 300, layering: 'list',
    elements: [
      { type: 'text', x: 0, y: 0, width: 30, height: 8, font_size: 12, content_binding: '{{asset.name}}', name: 'Corner' },
      { type: 'generator', generator_type: 'qr', x: 38, y: 4, width: 20, height: 20 },
    ],
  });
  await page.goto(`/templates/${template.id}/edit`);

  // Mirrored, to read through glass; turned with the dial, by its keys and
  // dragged, snapping, and by its field.
  await page.locator('#tpl-print-mirror').selectOption('across');
  const dial = page.getByRole('slider', { name: 'Turned as it prints' });
  await dial.focus();
  await page.keyboard.press('Shift+ArrowRight');
  await expect(page.locator('#tpl-print-rotation')).toHaveValue('15');
  const d = (await dial.boundingBox())!;
  // To just short of the right, which snaps to a quarter turn.
  await drag(page, [d.x + d.width / 2, d.y + 2], [d.x + d.width + 30, d.y + d.height / 2 - 2]);
  await expect(page.locator('#tpl-print-rotation')).toHaveValue('90');
  await page.locator('#tpl-print-rotation').fill('10');
  await page.locator('#tpl-print-rotation').press('Tab');
  await expect(dial).toHaveAttribute('aria-valuenow', '10');

  // The design stays the right way round and level, the label outlined
  // where it lies once on, and what goes off it is warned of.
  await expect(page.locator('[data-applied-outline]')).toBeVisible();
  await expect(page.getByRole('list', { name: 'As it prints' })).toContainText('“Corner” goes partly off the label');
  const shown = page.locator('[data-applied-label]');
  await expect(shown).toHaveAttribute('data-applied-label', 'printed');
  await page.getByRole('radio', { name: 'As applied' }).click();
  await expect(shown).toHaveAttribute('data-applied-label', 'applied');
  await expect(page.getByRole('img', { name: 'The label as applied' })).toBeVisible();

  // Any element mirrors in its box; a mirrored code is warned of.
  await onLabel(page, /^Barcode 2/).click();
  await page.getByRole('group', { name: 'Mirrored' }).getByRole('button', { name: 'Left for right' }).click();
  await expect(page.locator('[data-mirrored-code]')).toBeVisible();
  await expect(page.getByRole('list', { name: 'As it prints' })).toContainText('is a mirrored code');
  await page.getByRole('group', { name: 'Mirrored' }).getByRole('button', { name: 'Left for right' }).click();
  await expect(page.locator('[data-mirrored-code]')).toHaveCount(0);
  await axe(page);

  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible();
  const saved = (await api.call('GET', `/templates/${template.id}`)).template;
  expect(saved).toMatchObject({ print_mirror: 'across', print_rotation: 10 });
  expect(saved.elements[1].mirror).toBeUndefined();

  // The print screen shows it as printed or as applied, and prints it the
  // right way round this once, warning of what is cut off.
  await page.goto(`/assets/${asset.asset.id}`);
  await page.getByLabel('Template').selectOption(template.id);
  const labels = page.getByRole('region', { name: 'Labels' });
  await expect(labels.locator('[data-applied-label]')).toHaveAttribute('data-applied-label', 'printed');
  await labels.getByRole('radio', { name: 'As applied' }).click();
  await expect(labels.getByRole('img', { name: /as applied/ })).toBeVisible();
  await page.getByLabel('Printer').selectOption({ label: printerName });
  await page.locator('#label-mirror').selectOption('none');
  const sent = page.waitForRequest((r) => r.url().includes('/print') && r.method() === 'POST');
  await page.getByRole('button', { name: 'Print' }).click();
  expect((await sent).postDataJSON()).toMatchObject({ template_id: template.id, mirror: 'none' });
  await expect(page.getByRole('status').filter({ hasText: 'Sent' })).toBeVisible();
  await expect(labels).toContainText('goes partly off the label');
  await printer.job();
});
