import { expect, test, unique } from './fixtures';

test('pool stock kept in two places: link the places to the pool, see where else to look, move stock and report by pool', async ({ signedIn: page, api }) => {
  const poolName = unique('M5 bolts');
  const drawerName = unique('Workshop drawer');
  const bayName = unique('Laser bay');
  const { category: drawer } = await api.call('POST', '/categories', { name: drawerName });
  const { category: bay } = await api.call('POST', '/categories', { name: bayName });
  const { category: pool } = await api.call('POST', '/categories', { name: poolName, is_consumable: true, unit: 'pcs', low_stock_threshold: 200 });
  const binName = unique('Drawer bin');
  const otherName = unique('Bay bin');
  const { asset: bin } = await api.call('POST', '/assets', { name: binName, category_ids: [drawer.id], stock: { quantity: 140, unit: 'pcs', low_stock_threshold: 50 } });
  await api.call('POST', '/assets', { name: otherName, category_ids: [bay.id], stock: { quantity: 300, unit: 'pcs', low_stock_threshold: 50 } });

  // The pool includes both places, chosen on its page.
  await page.goto(`/categories/${pool.id}?tab=counts`);
  const form = page.getByRole('form', { name: 'What it counts in' });
  for (const name of [drawerName, bayName]) {
    await form.getByLabel('Add a category it includes').selectOption({ label: name });
    await form.getByRole('button', { name: 'Add', exact: true }).nth(1).click();
  }
  await form.getByRole('button', { name: 'Save links' }).click();
  await expect(page.getByRole('status')).toHaveText('Links saved.');
  await page.getByRole('navigation', { name: 'Parts of this category' }).getByRole('link', { name: 'Stock and lending', exact: true }).click();
  await expect(page.locator('#tab-stock')).toContainText('In stock.');
  await expect(page.locator('#tab-stock')).toContainText('440 pcs here and in its subcategories and the categories it includes');

  // The drawer's category lists the link from its side.
  await page.goto(`/categories/${drawer.id}?tab=counts`);
  await expect(page.getByRole('list', { name: 'Also counts in' }).getByRole('link', { name: poolName, exact: true })).toBeVisible();

  // The bin says what it counts in, and where else to look.
  await page.goto(`/assets/${bin.asset.id}`);
  await expect(page.getByRole('list', { name: 'Counts in' })).toContainText(`${poolName} (through ${drawerName})`);
  await expect(page.getByText(`${poolName}: 440 pcs (threshold 200 pcs)`)).toBeVisible();
  const where = page.getByRole('list', { name: `Where ${poolName} is kept` });
  await expect(where.getByRole('listitem').first()).toHaveText('140 pcs here');
  await expect(where.getByRole('listitem').nth(1)).toContainText(`300 pcs in ${otherName}`);

  // Stock moves from the laser bay to it, recorded in its history, the pool's total unchanged.
  await page.getByRole('button', { name: 'Move to another place' }).click();
  await page.getByLabel('To here from').check();
  await page.getByLabel('Place', { exact: true }).selectOption({ label: `${otherName}: 300 pcs` });
  await page.getByLabel('How many to move (pcs)').fill('100');
  await page.getByRole('button', { name: 'Move', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText(`Moved 100 pcs here from ${otherName}.`);
  await expect(where.getByRole('listitem').first()).toHaveText('240 pcs here');
  await expect(page.getByText(`${poolName}: 440 pcs (threshold 200 pcs)`)).toBeVisible();
  await expect(page.getByRole('region', { name: 'History' }).getByRole('listitem').first()).toContainText(`Moved 100 pcs here from ${otherName}; 240 pcs now`);

  // Its scan page says where else to look too.
  await page.goto(`/scan/${bin.asset.id}`);
  await expect(page.getByRole('heading', { name: 'Where else to look' })).toBeVisible();
  await expect(page.getByRole('list', { name: `Where ${poolName} is kept` }).getByRole('listitem').first()).toHaveText('240 pcs here');

  // A report by pool has a line for the pool, with its places under it.
  const { report } = await api.call('POST', '/reports', {
    name: unique('Stock by pool'),
    spec: {
      filter: { category_ids: [pool.id] },
      sections: [{ type: 'table', title: 'Stock', group_by: 'pool', columns: [{ heading: 'Place', binding: '{{asset.name}}' }, { heading: 'Left', binding: '{{stock.quantity}}' }] }],
    },
  });
  await page.goto(`/reports/${report.id}`);
  const table = page.getByRole('region', { name: 'Stock' });
  await expect(table.getByRole('heading', { name: `${poolName}: 440 pcs (threshold 200 pcs)` })).toBeVisible();
  await expect(table.getByRole('link', { name: binName, exact: true })).toBeVisible();
  await expect(table.getByRole('link', { name: otherName, exact: true })).toBeVisible();

  // A link leading back to where it starts is refused.
  await expect(api.call('PUT', `/categories/${pool.id}/links`, { implies: [drawer.id] })).rejects.toThrow(/409/);
});
