import { readFile } from 'node:fs/promises';
import { gunzipSync } from 'node:zlib';
import AxeBuilder from '@axe-core/playwright';
import { expect, goTo, test, unique } from './fixtures';

// Packages (README §2.3): part of the instance chosen as an installer's
// components are, what it needs ticked and marked, and downloaded by the
// browser as the server writes it.
test('choose a category, see what it needs, and download it as a package', async ({ signedIn: page, api }, testInfo) => {
  const parent = unique('Workshop');
  const child = unique('Lathes');
  const { category: top } = await api.call('POST', '/categories', { name: parent, color_hex: '#123456' });
  await api.call('POST', '/categories', { name: child, color_hex: '#654321', parent_id: top.id });
  const label = unique('Spindle speed');
  const { field } = await api.call('POST', '/fields', { key: unique('rpm').replace(/-/g, '_'), label, validation_type: 'number' });
  await api.call('POST', `/categories/${top.id}/fields`, { field_id: field.id, is_required: false });

  await goTo(page, 'Import & export');
  await expect(page.getByRole('heading', { name: 'Export a package' })).toBeVisible();
  const tree = page.getByTestId('category-tree');
  await tree.getByRole('checkbox', { name: parent, exact: true }).check();
  await expect(tree.getByRole('checkbox', { name: child, exact: true })).toBeChecked();

  // the field comes with the category, ticked, locked and saying why
  const serial = page.getByTestId('group-field').getByRole('checkbox', { name: label, exact: true });
  await expect(serial).toBeChecked();
  await expect(serial).toBeDisabled();
  // Lathes inherits it from Workshop, so both need it
  await expect(serial).toHaveAccessibleDescription(`Needed by ${child} and ${parent}`);

  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations).toEqual([]);

  const exportButton = page.getByTestId('export');
  await expect(exportButton).toBeDisabled();
  const name = unique('Workshop package');
  await page.getByRole('textbox', { name: 'Name' }).fill(name);
  await page.getByRole('textbox', { name: 'License' }).fill('CC0-1.0');
  const [download] = await Promise.all([page.waitForEvent('download'), exportButton.click()]);
  expect(download.suggestedFilename()).toBe(`${name}.bam.tar.gz`);
  const path = testInfo.outputPath('package.bam.tar.gz');
  await download.saveAs(path);

  // a tar, gzipped: its header first, naming it a package
  const tar = gunzipSync(await readFile(path));
  expect(tar.subarray(0, 11).toString()).toBe('header.json');
  const size = parseInt(tar.subarray(124, 136).toString().replace(/\0.*$/, '').trim() || '0', 8);
  const header = JSON.parse(tar.subarray(512, 512 + size).toString());
  expect(header.type).toBe('package');
  expect(header.package).toMatchObject({ name, license: 'CC0-1.0' });
  expect(header.secrets).toBe(false);
  expect(header.kinds).not.toContain('people');
  await expect(page.getByText(`Your browser is saving ${name}.`)).toBeVisible();
});
