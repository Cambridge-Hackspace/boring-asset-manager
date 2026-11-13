import fs from 'node:fs';
import AxeBuilder from '@axe-core/playwright';
import { expect, goTo, test, unique } from './fixtures';

test('export assets, edit the file, and import it back', async ({ signedIn: page, api }) => {
  const tag = unique('csv');
  const parent = await api.call('POST', '/categories', { name: `Shop ${tag}` });
  const child = await api.call('POST', '/categories', { name: 'Saws', parent_id: parent.category.id });
  const fieldKey = `note_${tag.replace(/[^a-z0-9]/g, '_')}`;
  await api.call('POST', '/fields', { key: fieldKey, label: `Note ${tag}`, validation_type: 'text' });
  const saw = await api.call('POST', '/assets', {
    name: `Saw ${tag}`,
    category_ids: [child.category.id],
    field_values: { [fieldKey]: 'sharp' },
  });

  // Export what the filtered list shows.
  await goTo(page, 'Assets');
  await page.getByLabel('Search').fill(tag);
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(page.getByRole('link', { name: `Saw ${tag}` })).toBeVisible();
  const [download] = await Promise.all([page.waitForEvent('download'), page.getByRole('link', { name: 'Export CSV' }).click()]);
  const exported = fs.readFileSync((await download.path())!).toString('utf8');
  expect(exported.charCodeAt(0)).toBe(0xfeff);
  const lines = exported.slice(1).trim().split('\r\n');
  expect(lines).toHaveLength(2);
  expect(lines[0]).toBe(`id,name,categories,code.qr,code.micro_qr,code.code128,code.nfc,field.${fieldKey}`);
  expect(lines[1]).toBe(`${saw.asset.asset.id},Saw ${tag},Shop ${tag} > Saws,,,,,sharp`);

  // Change the saw, add a row, and a column the importer does not know.
  const edited =
    `${lines[0]},Notes\r\n` +
    `${saw.asset.asset.id},,,,,,,blunt,ignored\r\n` +
    `,File ${tag},Shop ${tag} > Saws,,,F-${tag},,,ignored\r\n`;

  await page.getByRole('link', { name: 'Import CSV' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Import assets');
  await page.getByLabel('CSV file').setInputFiles({ name: 'assets.csv', mimeType: 'text/csv', buffer: Buffer.from(edited) });
  await expect(page.getByText('Header, Notes: column "Notes" is not mapped; choose where it goes or ignore it')).toBeVisible();
  await expect(page.getByLabel(`Where field.${fieldKey} goes`)).toHaveValue(`field.${fieldKey}`);
  await expect(page.getByRole('button', { name: /^Import/ })).toBeDisabled();

  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations.map((v) => v.id)).toEqual([]);

  await page.getByLabel('Where Notes goes').selectOption('ignore');
  await page.getByRole('button', { name: 'Check again' }).click();
  await expect(page.getByText('2 rows: 1 new, 1 updated, 0 unchanged.')).toBeVisible();
  await page.getByRole('button', { name: 'Import 2 assets' }).click();
  await expect(page.getByText('Imported: 1 new, 1 updated, 0 unchanged.')).toBeVisible();

  const after = await api.call('GET', `/assets/${saw.asset.asset.id}`);
  expect(after.asset.field_values[fieldKey]).toBe('blunt');
  expect(after.asset.categories.map((c: { name: string }) => c.name)).toEqual(['Saws']);
  const files = await api.call('GET', `/assets?search=${encodeURIComponent('File ' + tag)}`);
  expect(files.assets).toHaveLength(1);
  expect(files.assets[0].generator_keys[0]).toMatchObject({ generator_type: 'code128', assigned_code: `F-${tag}` });
});

test('fill a bin from a spreadsheet, then empty it again', async ({ signedIn: page }) => {
  const tag = unique('links');
  const bin = `Bin ${tag}`;
  const screws = `Screws ${tag}`;
  const file = (rows: string) => ({ name: 'links.csv', mimeType: 'text/csv', buffer: Buffer.from(rows) });

  // The bin and its contents, made by one file: the link names the bin.
  await goTo(page, 'Assets');
  await page.getByRole('link', { name: 'Import CSV' }).click();
  await page.getByLabel('CSV file').setInputFiles(file(`name,links\r\n${bin},\r\n${screws},contained_in:${bin}\r\n`));
  await expect(page.getByText('2 rows: 2 new, 0 updated, 0 unchanged; 1 link added.')).toBeVisible();
  await page.getByText('Link changes').click();
  await expect(page.getByText(`Add: ${bin} contains ${screws} (line 3)`)).toBeVisible();
  await page.getByRole('button', { name: 'Import 2 assets' }).click();
  await expect(page.getByText('Imported: 2 new, 0 updated, 0 unchanged; 1 link added.')).toBeVisible();

  // The export writes it from both ends.
  await goTo(page, 'Assets');
  await page.getByLabel('Search').fill(tag);
  await page.getByRole('button', { name: 'Search' }).click();
  const [download] = await Promise.all([page.waitForEvent('download'), page.getByRole('link', { name: 'Export CSV' }).click()]);
  const exported = fs.readFileSync((await download.path())!).toString('utf8').slice(1).trim().split('\r\n');
  expect(exported[0].endsWith(',links')).toBe(true);
  const screwsRow = exported.find((l) => l.includes(`,${screws},`))!;
  expect(screwsRow.endsWith(`,contained_in:${bin}`)).toBe(true);

  // The screws' row with no links empties the bin, but only with the option.
  const id = screwsRow.split(',')[0];
  await page.getByRole('link', { name: 'Import CSV' }).click();
  await page.getByLabel('CSV file').setInputFiles(file(`id,links\r\n${id},\r\n`));
  await expect(page.getByText('1 row: 0 new, 0 updated, 1 unchanged.')).toBeVisible();
  await page.getByRole('checkbox', { name: 'Also remove links that no row states' }).check();
  await page.getByRole('button', { name: 'Check again' }).click();
  await expect(page.getByText('1 row: 0 new, 0 updated, 1 unchanged; 1 removed.')).toBeVisible();
  await page.getByRole('button', { name: 'Import the links' }).click();
  await expect(page.getByText('Imported: 0 new, 0 updated, 1 unchanged; 1 removed.')).toBeVisible();
});

test('import a bin and what goes in it from two files, in order', async ({ signedIn: page, api }) => {
  const tag = unique('two');
  const bin = `Bin ${tag}`;
  const screws = `Screws ${tag}`;
  const file = (name: string, rows: string) => ({ name, mimeType: 'text/csv', buffer: Buffer.from(rows) });

  await goTo(page, 'Assets');
  await page.getByRole('link', { name: 'Import CSV' }).click();
  // Chosen with the contents first, which names a bin that does not exist yet.
  await page.getByLabel('CSV file').setInputFiles([file('screws.csv', `name,links\r\n${screws},contained_in:${bin}\r\n`), file('bins.csv', `name\r\n${bin}\r\n`)]);
  const screwsFile = page.getByRole('region', { name: /screws\.csv/ });
  await expect(screwsFile.getByText(`Line 2, links: no asset is "${bin}"`)).toBeVisible();
  await expect(screwsFile.getByText(/put it before this one/)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Import 2 files in order' })).toBeDisabled();

  await page.getByRole('button', { name: 'Move bins.csv up' }).click();
  await expect(screwsFile.getByText(/this one can be imported after it/)).toBeVisible();
  await page.getByRole('button', { name: 'Import 2 files in order' }).click();
  await expect(page.getByText('Imported all 2 files.')).toBeVisible();
  await expect(screwsFile.getByText('Imported: 1 new, 0 updated, 0 unchanged; 1 link added.')).toBeVisible();

  const found = await api.call('GET', `/assets?search=${encodeURIComponent(screws)}`);
  expect(found.assets[0].links.map((l: { asset: { name: string } }) => l.asset.name)).toEqual([bin]);
});

test('export to Excel, and the workbook imports again unchanged', async ({ signedIn: page, api }) => {
  const tag = unique('Lathe');
  await api.call('POST', '/assets', { name: `Lathe ${tag}` });
  await goTo(page, 'Assets');
  await page.getByLabel('Search').fill(tag);
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(page.getByRole('link', { name: `Lathe ${tag}` })).toBeVisible();
  const [download] = await Promise.all([page.waitForEvent('download'), page.getByRole('link', { name: 'Export Excel' }).click()]);
  expect(download.suggestedFilename()).toMatch(/^assets-\d{8}\.xlsx$/);

  await page.getByRole('link', { name: 'Import CSV' }).click();
  await page.getByLabel('CSV file').setInputFiles({
    name: download.suggestedFilename(),
    mimeType: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    buffer: fs.readFileSync((await download.path())!),
  });
  const all = page.getByRole('region', { name: /› All/ });
  await expect(all.getByText('1 row: 0 new, 0 updated, 1 unchanged.')).toBeVisible();
});

test('import a sheet of an Excel workbook, leaving the other out', async ({ signedIn: page }) => {
  await goTo(page, 'Assets');
  await page.getByRole('link', { name: 'Import CSV' }).click();
  await page.getByLabel('CSV file').setInputFiles('../internal/xlsx/testdata/excel.xlsx');
  const bins = page.getByRole('region', { name: /excel › Bins/ });
  await expect(bins.getByText('1 row: 1 new, 0 updated, 0 unchanged.')).toBeVisible();
  await expect(page.getByRole('region', { name: /excel › Tools/ })).toBeVisible();
  await page.getByRole('button', { name: 'Remove excel › Tools' }).click();
  await page.getByRole('button', { name: 'Import 1 asset' }).click();
  await expect(bins.getByText('Imported: 1 new, 0 updated, 0 unchanged.')).toBeVisible();
});
