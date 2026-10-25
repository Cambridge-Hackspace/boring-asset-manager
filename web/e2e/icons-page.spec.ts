import AxeBuilder from '@axe-core/playwright';
import type { Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { expect, goTo, test, unique } from './fixtures';

// The Icons page (README §3.7): every icon, what needs attention, the
// library, the palette and the settings, and the lexicon out and in.

const section = (page: Page, name: string) => page.getByRole('region', { name });
const boxIcon = (name: string, more = '') => `icon "${name}" { category "storage"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1; ${more} } }`;

test('shows every icon, and what needs attention', async ({ signedIn: page, api }) => {
  const name = unique('Bins');
  const { category } = await api.call('POST', '/categories', { name, color_hex: '#0f766e' });
  const iconName = unique('bin').toLowerCase();
  await api.call('PUT', `/categories/${category.id}/icon`, { icon_name: iconName, source: boxIcon(iconName) });
  // a second save that does not compile waits as a draft
  await api.call('PUT', `/categories/${category.id}/icon`, { icon_name: iconName, source: boxIcon(iconName).replace('box 0', 'bxo 0') });

  await goTo(page, 'Icons');
  await expect(page.getByRole('heading', { name: 'Icons', level: 1 })).toBeVisible();
  const gallery = section(page, 'Every icon');
  await expect(gallery.getByRole('img', { name: iconName })).toBeVisible();
  await expect(gallery.getByRole('link', { name })).toBeVisible();
  await gallery.getByRole('searchbox', { name: 'Search icons' }).fill(iconName);
  await expect(gallery.getByRole('img')).toHaveCount(1);
  await gallery.getByRole('searchbox', { name: 'Search icons' }).fill('nothing-like-this');
  await expect(gallery.getByText('No icon matches.')).toBeVisible();

  const attention = section(page, 'Needs attention');
  await expect(attention.getByRole('link', { name })).toBeVisible();
  await expect(attention).toContainText('a draft with 1 problem');
  await expect(page.getByText('the work of Axonibyte Innovations, LLC')).toBeVisible();

  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations.map((v) => `${v.id}: ${v.help} (${v.nodes.map((n) => n.target.join(' ')).join('; ')})`)).toEqual([]);
});

test('the palette is the vocabulary: a token an icon paints with cannot go', async ({ signedIn: page, api }) => {
  await page.goto('/icons');
  const palette = section(page, 'Palette');
  const token = unique('brand').toLowerCase().replace(/[^a-z0-9-]/g, '');
  await palette.getByRole('button', { name: 'Add a token' }).click();
  const last = (await palette.getByRole('textbox', { name: /^Token \d+$/ }).count());
  await palette.getByRole('textbox', { name: `Token ${last}`, exact: true }).fill(token);
  await palette.getByRole('textbox', { name: `${token}, light`, exact: true }).fill('#c00');
  await palette.getByRole('textbox', { name: `${token}, dark`, exact: true }).fill('#f66');
  await palette.getByRole('button', { name: 'Save the palette' }).click();
  await expect(palette.getByText('The palette is saved.')).toBeVisible();

  // an icon paints with it
  const name = unique('Signs');
  const { category } = await api.call('POST', '/categories', { name });
  const iconName = unique('sign').toLowerCase();
  const saved = await api.call('PUT', `/categories/${category.id}/icon`, { icon_name: iconName, source: boxIcon(iconName, `tile 0 0 1 1 1 fill="${token}"`) });
  expect(saved.save.published).toBe(true);

  // and so it cannot go: the refusal says which icon uses it
  await page.reload();
  await palette.getByRole('button', { name: `Remove ${token}` }).click();
  await palette.getByRole('button', { name: 'Save the palette' }).click();
  const alert = palette.getByRole('alert');
  await expect(alert).toContainText('The icons would not compile after this change');
  await expect(alert.getByRole('link', { name })).toBeVisible();
});

test('settings hold every icon to them, and are refused when one cannot keep to them', async ({ signedIn: page, api }) => {
  const { category } = await api.call('POST', '/categories', { name: unique('Boxes') });
  const iconName = unique('box').toLowerCase();
  await api.call('PUT', `/categories/${category.id}/icon`, { icon_name: iconName, source: boxIcon(iconName) });

  await page.goto('/icons');
  const settings = section(page, 'Settings');
  // a box's outline is drawn at 1.2
  await settings.getByLabel('Stroke widths').fill('2');
  await settings.getByRole('button', { name: 'Save the settings' }).click();
  await expect(settings.getByRole('alert')).toContainText('The icons would not compile after this change');
  await settings.getByLabel('Stroke widths').fill('1.2, 1.4, 1.5, 2, 2.2');
  await settings.getByLabel('Shapes in one icon').fill('5000');
  await settings.getByRole('button', { name: 'Save the settings' }).click();
  await expect(settings.getByText('The settings are saved.')).toBeVisible();
  await page.reload();
  await expect(settings.getByLabel('Stroke widths')).toHaveValue('1.2, 1.4, 1.5, 2, 2.2');
  await expect(settings.getByLabel('Shapes in one icon')).toHaveValue('5000');
  // back as they were, for the rest
  await api.call('PUT', '/icons/settings', { stroke_widths: [], reserved: [], limits: {} });
});

test('library files are written here, and the lexicon goes out as a zip and comes back in', async ({ signedIn: page }) => {
  await page.goto('/icons');
  const library = section(page, 'Library');
  const path = `shared/${unique('glyphs').toLowerCase()}.ilx`;
  await library.getByRole('button', { name: 'New file' }).click();
  await library.getByLabel('Its path').fill(path);
  await library.getByRole('textbox', { name: 'New library file source' }).fill(`fragment "glyph-${path.replace(/[^a-z0-9]/g, '')}" {
  part "p" { tile 0 0 0 1 1 }
}`);
  await library.getByRole('button', { name: 'Save' }).click();
  await expect(library.getByText('Published.', { exact: true })).toBeVisible();
  await expect(library.getByText(path, { exact: true }).first()).toBeVisible();

  const [download] = await Promise.all([page.waitForEvent('download'), page.getByRole('link', { name: 'Export as a zip' }).click()]);
  expect(download.suggestedFilename()).toBe('icons.zip');
  const zip = readFileSync(await download.path());
  expect(zip.subarray(0, 2).toString()).toBe('PK');
  expect(zip.includes(Buffer.from(`library/${path}`)), `${zip.length} bytes: ${zip.toString('latin1').match(/[a-z_/.0-9-]+.ilx/g)?.join(' ')}`).toBe(true);

  // the export comes back in: the library's files replaced, the rest skipped
  await page.getByLabel('Import library files from a zip').setInputFiles({ name: 'icons.zip', mimeType: 'application/zip', buffer: zip });
  await section(page, 'The build').getByRole('button', { name: 'Import', exact: true }).click();
  await expect(page.getByText(/Imported 0 new files and replaced \d+\. Skipped \d+: _lexicon\.ilx/)).toBeVisible();
});
