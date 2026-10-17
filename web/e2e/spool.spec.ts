import fs from 'node:fs';
import { expect, goTo, test, unique } from './fixtures';

// The spool (README §6.2): labels gathered from a category and an asset's
// page, sorted onto a download, downloaded, then found in the Printed list
// and the assets' histories.
test('gather labels in the spool, sort them, and download them', async ({ signedIn: page, api }) => {
  // A size no test printer takes, so nothing is sent to one by itself.
  const templateName = unique('Bin tag');
  const { template } = await api.call('POST', '/templates', {
    name: templateName, units: 'mm', width: 45, height: 25, dpi: 300,
    elements: [{ type: 'text', x: 2, y: 2, width: 40, height: 10, font_size: 12, content_binding: '{{asset.name}}' }],
  });
  const cat = await api.call('POST', '/categories', { name: unique('Bins') });
  await api.call('PUT', `/categories/${cat.category.id}/label-template`, { label_template_id: template.id });
  const bins = [unique('Bin A'), unique('Bin B')];
  for (const name of bins) await api.call('POST', '/assets', { name, category_ids: [cat.category.id] });
  const looseName = unique('Loose');
  const loose = await api.call('POST', '/assets', { name: looseName });

  await page.goto(`/categories/${cat.category.id}`);
  await page.getByRole('button', { name: 'Add its assets to the spool' }).click();
  await expect(page.getByText('Added 2 labels to the spool.')).toBeVisible();
  await page.goto(`/assets/${loose.asset.asset.id}`);
  await page.getByRole('button', { name: 'Add its label to the spool' }).click();
  await expect(page.getByText('Added 1 label to the spool.')).toBeVisible();

  await goTo(page, 'Spool');
  const looseRow = page.getByRole('row').filter({ hasText: looseName });
  await expect(looseRow).toContainText('No template chosen');
  await expect(looseRow).toContainText('No printer chosen');
  const waiting = page.getByRole('region', { name: new RegExp(`No printer chosen · ${templateName} · 2 labels`) });
  await waiting.getByRole('checkbox', { name: `Select every label for No printer chosen, ${templateName}` }).check();
  const bar = page.getByRole('region', { name: 'Change the selected labels' });
  await bar.getByLabel('Print on').selectOption('download');
  await bar.getByRole('button', { name: 'Set printer' }).click();
  await expect(page.getByText('Printer set for 2 labels.')).toBeVisible();

  const ready = page.getByRole('region', { name: new RegExp(`Download · ${templateName} · 2 labels`) });
  const [download] = await Promise.all([page.waitForEvent('download'), ready.getByRole('button', { name: 'Download PDF' }).click()]);
  expect(fs.readFileSync((await download.path())!).subarray(0, 5).toString()).toBe('%PDF-');
  await expect(page.getByText('Downloaded 2 labels.')).toBeVisible();
  await expect(ready).toHaveCount(0);

  // The loose label goes; the bins are in the Printed list and their histories.
  await page.getByRole('checkbox', { name: `Select ${looseName}` }).check();
  await page.getByRole('button', { name: 'Remove', exact: true }).click();
  await page.getByRole('button', { name: 'Remove 1 label' }).click();
  await expect(page.getByText('Removed 1 label from the spool.')).toBeVisible();
  await expect(page.getByRole('link', { name: looseName })).toHaveCount(0);

  await page.getByRole('button', { name: 'Printed' }).click();
  for (const name of bins) await expect(page.getByRole('row').filter({ hasText: name })).toContainText('Download');
  await page.getByRole('link', { name: bins[0]! }).click();
  await expect(page.getByText(`Downloaded its label (${templateName})`)).toBeVisible();
});
