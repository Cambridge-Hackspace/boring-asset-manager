import { readFile } from 'node:fs/promises';
import { gunzipSync } from 'node:zlib';
import AxeBuilder from '@axe-core/playwright';
import { expect, goTo, test, unique } from './fixtures';

// Packages (docs/packages.md): part of the instance chosen as an installer's
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

// Importing one (docs/backups.md): uploaded, checked, what clashes chosen for,
// put under a category, and imported as checked.
test('upload a package, choose for what clashes and where it goes, and import it', async ({ signedIn: page, api }) => {
  const parent = unique('Bench');
  const { category: top } = await api.call('POST', '/categories', { name: parent, color_hex: '#123456' });
  const key = unique('torque').replace(/-/g, '_');
  const label = unique('Torque');
  const { field } = await api.call('POST', '/fields', { key, label, validation_type: 'number' });
  await api.call('POST', `/categories/${top.id}/fields`, { field_id: field.id, is_required: false });
  const made = await api.call('POST', '/exports', { selection: { subtrees: [top.id] }, name: unique('Bench package') });
  const res = await page.context().request.get(made.download);
  expect(res.ok()).toBe(true);
  const body = await res.body();
  const lab = unique('Lab');
  const { category: anchor } = await api.call('POST', '/categories', { name: lab, color_hex: '#654321' });

  await goTo(page, 'Import & export');
  await page.getByTestId('archive-file').setInputFiles({ name: 'bench.bam.tar.gz', mimeType: 'application/gzip', buffer: body });
  const plan = page.getByTestId('import-plan');
  await expect(plan).toBeVisible({ timeout: 30_000 });

  // the field is here already, the same: used as it is, unless chosen otherwise
  const choice = page.getByRole('combobox', { name: `What to do with the field ${label} (${key})` });
  await expect(choice).toHaveValue('existing');
  await choice.selectOption('copy');
  await page.getByTestId('anchor').selectOption({ label: lab });
  await page.getByTestId('check-again').click();
  await expect(page.getByTestId('import')).toBeEnabled({ timeout: 30_000 });

  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations).toEqual([]);

  await page.getByTestId('import').click();
  await expect(page.getByTestId('imported')).toContainText(`under ${lab}`, { timeout: 30_000 });
  const { categories } = await api.call('GET', '/categories');
  expect(categories.filter((c: { name: string; parent_id?: string }) => c.name === parent && c.parent_id === anchor.id)).toHaveLength(1);
  const { fields } = await api.call('GET', '/fields');
  expect(fields.map((f: { key: string }) => f.key)).toContain(`${key}_2`);
});

// An icon set (#146): the icons as a zipped isolex library folder, exported
// and taken back, replacing what changed.
test('export the icons as a library folder, and import it back', async ({ signedIn: page }) => {
  const res = await page.context().request.get('/api/v1/icons/export');
  expect(res.ok()).toBe(true);
  const zip = await res.body();
  expect(zip.subarray(0, 2).toString()).toBe('PK');

  await goTo(page, 'Import & export');
  await page.getByTestId('archive-file').setInputFiles({ name: 'icons.zip', mimeType: 'application/zip', buffer: zip });
  const plan = page.getByTestId('import-plan');
  await expect(plan).toContainText('the icon set', { timeout: 30_000 });
  await expect(page.getByTestId('anchor')).toHaveCount(0);
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations).toEqual([]);
  await page.getByTestId('import').click();
  await expect(page.getByTestId('imported')).toContainText('Imported the icon set', { timeout: 30_000 });
});
