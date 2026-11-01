import fs from 'node:fs';
import { expect, goTo, test, unique } from './fixtures';

test('build a report, run it, and download a table as CSV', async ({ signedIn: page, api }) => {
  const tag = unique('Lathe');
  await api.call('POST', '/assets', { name: `${tag} chuck key`, stock: { quantity: 3, unit: 'pcs' } });
  await api.call('POST', '/assets', { name: `${tag} tool post`, stock: { quantity: 12, unit: 'pcs' } });

  await goTo(page, 'Reports');
  await expect(page.getByRole('link', { name: 'Running low', exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'New report' }).click();
  const name = unique('Lathe parts');
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByLabel('Search').fill(tag);
  await page.getByLabel('Title (optional)').fill('Parts');
  await page.getByRole('button', { name: 'Add column' }).click();
  await page.getByLabel('Heading of column 2').fill('Count');
  await page.getByLabel('What column 2 shows').fill('{{stock.quantity}}');
  await page.getByLabel('Sort by').selectOption({ label: 'Count' });
  await page.getByRole('checkbox', { name: 'Largest or last first' }).check();
  await page.getByRole('button', { name: 'Preview' }).click();
  const preview = page.getByRole('region', { name: 'Preview' });
  await expect(preview.getByRole('row').nth(1)).toContainText(`${tag} tool post12`);

  await page.getByRole('button', { name: 'Save report' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(name);
  const table = page.getByRole('region', { name: 'Parts' });
  await expect(table.getByRole('row')).toHaveCount(3);
  await expect(table.getByRole('row').nth(2)).toContainText(`${tag} chuck key3`);

  const [download] = await Promise.all([page.waitForEvent('download'), table.getByRole('link', { name: /Download CSV/ }).click()]);
  expect(download.suggestedFilename()).toBe(`${name.replace(' ', '-')}.csv`);
  const csv = fs.readFileSync((await download.path())!, 'utf8');
  expect(csv).toBe(`﻿Asset,Count\n${tag} tool post,12\n${tag} chuck key,3\n`);
});

test('chart what is in stock and what was used', async ({ signedIn: page, api }) => {
  const tag = unique('Tape');
  const { asset } = await api.call('POST', '/assets', { name: `${tag} gaffer`, stock: { quantity: 40, unit: 'm' } });
  await api.call('POST', `/assets/${asset.asset.id}/quantity`, { adjust: -15 });

  await page.goto('/reports/new');
  await page.getByLabel('Name', { exact: true }).fill(unique('Tape use'));
  await page.getByLabel('Search').fill(tag);
  await page.getByRole('button', { name: 'Add chart' }).click();
  await page.getByLabel('Grouped by').selectOption({ label: 'Unit' });
  await page.getByLabel('Counting').selectOption({ label: 'Quantity in stock, by unit' });
  await page.getByRole('button', { name: 'Add chart' }).click();
  await page.getByLabel('Chart').nth(1).selectOption({ label: 'Line over time' });
  await page.getByRole('button', { name: 'Save report' }).click();

  await expect(page.getByRole('img', { name: 'Bar chart of Quantity: m 25' })).toBeVisible();
  await expect(page.getByRole('img', { name: /^Line chart of history: Used \(m\): .* 15$/ })).toBeVisible();
});

test('scan templates: make one, and have a category choose it', async ({ signedIn: page, api }) => {
  const name = unique('Bins');
  const { category } = await api.call('POST', '/categories', { name: unique('Storage'), color_hex: '#1d4ed8' });
  const { asset: bin } = await api.call('POST', '/assets', { name: unique('Bin'), category_ids: [category.id] });

  await page.goto('/reports');
  const section = page.getByRole('region', { name: 'Scan templates' });
  await expect(section.getByRole('listitem').filter({ hasText: 'Scanned' })).toContainText('(default)');
  await section.getByRole('link', { name: 'New scan template' }).click();
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByRole('group', { name: /^Actions/ }).getByRole('checkbox', { name: 'Use some' }).check();
  await page.getByLabel('Preview with').fill(bin.asset.name);
  await page.getByRole('button', { name: 'Find' }).click();
  await page.getByRole('button', { name: 'Preview', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Preview' })).toContainText('Actions: Open its page, Use some');
  await page.getByRole('button', { name: 'Save scan template' }).click();
  await expect(page).toHaveURL(/\/reports$/);
  await expect(section).toContainText(name);

  await page.goto(`/categories/${category.id}`);
  await page.getByRole('navigation', { name: 'Parts of this category' }).getByRole('link', { name: 'Links and hooks' }).click();
  await page.getByLabel('Shows').selectOption({ label: name });
  await page.getByRole('button', { name: 'Save scan template' }).click();
  await expect(page.getByRole('status')).toHaveText('Scan template saved.');
  const saved = await api.call('GET', `/assets/${bin.asset.id}`);
  expect(saved.asset.scan_template.source_category_id).toBe(category.id);
});
