import fs from 'node:fs';
import { FakePrinter, expect, goTo, test, unique } from './fixtures';

// The spool (docs/printing.md): labels gathered from a category and an asset's
// page, sorted onto a download, downloaded, then found in the Printed list
// and the assets' histories.
test('gather labels in the print queue, sort them, and download them', async ({ signedIn: page, api }) => {
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
  await page.getByRole('navigation', { name: 'Parts of this category' }).getByRole('link', { name: 'Labels' }).click();
  await page.getByRole('button', { name: 'Add its assets to the print queue' }).click();
  await expect(page.getByText('Added 2 labels to the print queue.')).toBeVisible();
  await page.goto(`/assets/${loose.asset.asset.id}`);
  await page.getByRole('button', { name: 'Add to the queue' }).click();
  await expect(page.getByText('Added 1 label to the print queue.')).toBeVisible();

  await goTo(page, 'Print queue');
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
  await expect(page.getByText('Removed 1 label from the print queue.')).toBeVisible();
  await expect(page.getByRole('link', { name: looseName })).toHaveCount(0);

  await page.getByRole('button', { name: 'Printed' }).click();
  for (const name of bins) await expect(page.getByRole('row').filter({ hasText: name })).toContainText('Download');
  await page.getByRole('link', { name: bins[0]! }).click();
  await expect(page.getByText(`Downloaded its label (${templateName})`)).toBeVisible();
});

// Printing from the print queue goes out in the background, a job per printer
// and template, and the Jobs list follows it to the end.
test('print a group from the print queue to a printer', async ({ signedIn: page, api, printer }) => {
  const templateName = unique('Rollo 2x1');
  const { template } = await api.call('POST', '/templates', {
    name: templateName, units: 'in', width: 2, height: 1, dpi: 203,
    elements: [{ type: 'text', x: 0.1, y: 0.2, width: 1.8, height: 0.5, font_size: 12, content_binding: '{{asset.name}}' }],
  });
  const printerName = unique('Rollo');
  const created = await api.call('POST', '/printers', {
    name: printerName, driver: 'tspl', model: 'Rollo X1038', media: '2x1', connection: { type: 'tcp', address: `127.0.0.1:${printer.port}` },
  });
  const names = [unique('Bolt'), unique('Nut')];
  const ids: string[] = [];
  for (const name of names) ids.push((await api.call('POST', '/assets', { name })).asset.asset.id);
  await api.call('POST', '/spool', { asset_ids: ids, template_id: template.id, destination: `printer:${created.printer.id}`, copies: 2 });

  await goTo(page, 'Print queue');
  const group = page.getByRole('region', { name: new RegExp(`${printerName} · ${templateName} · 2 labels`) });
  await group.getByRole('button', { name: 'Print 4 labels' }).click();
  await expect(page.getByText('Sent 4 labels to print in 1 job.')).toBeVisible();

  // One job, each label printed twice.
  const job = (await printer.job()).toString('latin1');
  expect(job.match(/PRINT 1,2\r\n/g)).toHaveLength(2);
  await expect(group).toHaveCount(0);

  await page.getByRole('button', { name: 'Jobs' }).click();
  const row = page.getByRole('row').filter({ hasText: printerName });
  await expect(row).toContainText('Printed');
  await expect(row).toContainText(templateName);
  await page.getByRole('button', { name: 'Printed' }).click();
  for (const name of names) await expect(page.getByRole('row').filter({ hasText: name })).toContainText(printerName);
});

// A pool shares a job's labels between its printers.
test('share a job between the printers of a pool', async ({ signedIn: page, api, printer }) => {
  const second = new FakePrinter();
  await second.start();
  try {
    const templateName = unique('Rollo 2x1');
    const { template } = await api.call('POST', '/templates', {
      name: templateName, units: 'in', width: 2, height: 1, dpi: 203,
      elements: [{ type: 'text', x: 0.1, y: 0.2, width: 1.8, height: 0.5, font_size: 12, content_binding: '{{asset.name}}' }],
    });
    const names = [unique('Left'), unique('Right')];
    for (const [i, fake] of [printer, second].entries()) {
      await api.call('POST', '/printers', { name: names[i], driver: 'tspl', model: 'Rollo X1038', media: '2x1', connection: { type: 'tcp', address: `127.0.0.1:${fake.port}` } });
    }

    const poolName = unique('Bench');
    await goTo(page, 'Printers');
    await page.getByRole('button', { name: 'New pool' }).click();
    const form = page.getByRole('form', { name: 'New pool' });
    await form.getByLabel('Name').fill(poolName);
    await form.getByLabel('Labels in a batch').fill('1');
    for (const name of names) await form.getByRole('checkbox', { name }).check();
    await form.getByRole('button', { name: 'Save pool' }).click();
    await expect(page.getByText(`Saved ${poolName}.`)).toBeVisible();
    const { pools } = await api.call('GET', '/printer-pools');
    const pool = pools.find((p: { name: string }) => p.name === poolName);

    const ids: string[] = [];
    for (let i = 0; i < 4; i++) ids.push((await api.call('POST', '/assets', { name: unique('Washer') })).asset.asset.id);
    await api.call('POST', '/spool', { asset_ids: ids, template_id: template.id, destination: `pool:${pool.id}` });

    await goTo(page, 'Print queue');
    const group = page.getByRole('region', { name: new RegExp(`${poolName} · ${templateName} · 4 labels`) });
    await group.getByRole('button', { name: 'Print 4 labels' }).click();
    await expect(page.getByText('Sent 4 labels to print in 1 job.')).toBeVisible();

    // Both printers print, four labels between them.
    await expect.poll(() => printer.jobs.length + second.jobs.length, { timeout: 10_000 }).toBe(4);
    expect(printer.jobs.length).toBeGreaterThan(0);
    expect(second.jobs.length).toBeGreaterThan(0);

    await page.getByRole('button', { name: 'Jobs' }).click();
    const row = page.getByRole('row').filter({ hasText: poolName }).filter({ has: page.getByRole('list', { name: 'Its printers' }) });
    await expect(row).toContainText('Printed');
    const parts = row.getByRole('list', { name: 'Its printers' });
    for (const name of names) await expect(parts).toContainText(new RegExp(`${name}: [1-3] printed`));
  } finally {
    await second.stop();
  }
});

test('changing a printer’s roll says which waiting labels it strands, and templates say where they print', async ({ signedIn: page, api, printer }) => {
  const name = unique('Front QL');
  const { printer: ql } = await api.call('POST', '/printers', {
    name, driver: 'brother_ql', model: 'QL-810W', connection: { type: 'tcp', address: `127.0.0.1:${printer.port}` }, media: 'dk-11201',
  });
  const tagName = unique('Bin tag');
  const { template } = await api.call('POST', '/templates', {
    name: tagName, units: 'mm', width: 90, height: 29, dpi: 300,
    elements: [{ type: 'text', x: 28, y: 4, width: 58, height: 10, font_size: 14, content_binding: '{{asset.name}}' }],
  });
  const { asset } = await api.call('POST', '/assets', { name: unique('Drill') });
  await api.call('POST', '/spool', { asset_ids: [asset.asset.id], template_id: template.id, destination: `printer:${ql.id}`, copies: 2 });

  await goTo(page, 'Templates');
  await expect(page.getByRole('row', { name: new RegExp(tagName) })).toContainText(name);

  await goTo(page, 'Printers');
  await page.getByRole('button', { name: `Edit ${name}` }).click();
  await page.getByLabel('Roll loaded').selectOption('dk-22205');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('group', { name: 'Saving this change would affect:' })).toContainText(`2 labels waiting for ${name} with ${tagName} would no longer fit.`);
  await page.getByRole('button', { name: 'Save anyway' }).click();
  await expect(page.getByRole('article', { name })).toContainText('DK-22205 62 mm continuous');

  await goTo(page, 'Templates');
  await expect(page.getByRole('row', { name: new RegExp(tagName) })).not.toContainText(name);
});

test('a category’s labels take the first of its templates that fits, and move when the roll changes', async ({ signedIn: page, api, printer }) => {
  const name = unique('Bench QL');
  const { printer: ql } = await api.call('POST', '/printers', {
    name, driver: 'brother_ql', model: 'QL-810W', connection: { type: 'tcp', address: `127.0.0.1:${printer.port}` }, media: 'dk-11201',
  });
  const narrow = unique('Narrow tag');
  const wide = unique('Wide tag');
  await api.call('POST', '/templates', {
    name: narrow, units: 'mm', width: 90, height: 29, dpi: 300,
    elements: [{ type: 'text', x: 28, y: 4, width: 58, height: 10, font_size: 14, content_binding: '{{asset.name}}' }],
  });
  await api.call('POST', '/templates', {
    name: wide, units: 'mm', width: 62, height: 40, dpi: 300,
    elements: [{ type: 'text', x: 4, y: 4, width: 54, height: 10, font_size: 14, content_binding: '{{asset.name}}' }],
  });
  const catName = unique('Clamps');
  const { category } = await api.call('POST', '/categories', { name: catName, color_hex: '#7c3aed' });

  // List both, the wide one first, then put the narrow one first.
  await page.goto(`/categories/${category.id}`);
  await page.getByRole('navigation', { name: 'Parts of this category' }).getByRole('link', { name: 'Labels' }).click();
  const add = page.getByLabel('Add a template');
  await add.selectOption({ label: wide });
  await page.getByRole('button', { name: 'Add', exact: true }).click();
  await add.selectOption({ label: narrow });
  await page.getByRole('button', { name: 'Add', exact: true }).click();
  await page.getByRole('button', { name: `Move ${narrow} up` }).click();
  await page.getByRole('button', { name: 'Save label templates' }).click();
  await expect(page.getByText('Label templates saved.')).toBeVisible();

  // Sent to the QL, a label takes the narrow one, which its roll suits.
  const { asset } = await api.call('POST', '/assets', { name: unique('Clamp'), category_ids: [category.id] });
  const { added } = await api.call('POST', '/spool', { asset_ids: [asset.asset.id], destination: `printer:${ql.id}` });
  expect(added[0].template_id).not.toBeNull();

  // Loading 62 mm tape moves it to the wide one, as the form says first.
  await goTo(page, 'Printers');
  await page.getByRole('button', { name: `Edit ${name}` }).click();
  await page.getByLabel('Roll loaded').selectOption('dk-22205');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('group', { name: 'Saving this change would affect:' })).toContainText(`1 label waiting for ${name} with ${narrow} would move to ${wide}`);
  await page.getByRole('button', { name: 'Save anyway' }).click();
  await expect(page.getByRole('article', { name })).toContainText('DK-22205 62 mm continuous');

  await goTo(page, 'Print queue');
  await expect(page.getByRole('heading', { name: new RegExp(`${name} · ${wide}`) })).toBeVisible();
});
