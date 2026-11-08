import { expect, test, unique } from './fixtures';

test('follow what an asset needs and what needs it, through several hops, drawn and listed', async ({ signedIn: page, api }) => {
  const { category } = await api.call('POST', '/categories', { name: unique('Workshop') });
  const names = { bench: unique('Bench'), laser: unique('Laser cutter'), extractor: unique('Extractor'), supply: unique('Three-phase'), breaker: unique('Main breaker') };
  const ids: Record<string, string> = {};
  for (const [key, name] of Object.entries(names)) {
    ids[key] = (await api.call('POST', '/assets', { name, category_ids: [category.id] })).asset.asset.id;
  }
  const { relation_types } = await api.call('GET', '/relation-types');
  const dependsOn = relation_types.find((r: { name: string }) => r.name === 'depends on');
  expect(dependsOn.is_followed).toBe(true);
  for (const [from, to] of [['bench', 'laser'], ['laser', 'extractor'], ['extractor', 'supply'], ['supply', 'breaker']] as const) {
    await api.call('POST', `/assets/${ids[from]}/links`, { type_id: dependsOn.id, asset_id: ids[to], outgoing: true });
  }
  await api.call('PUT', `/assets/${ids.supply}/condition`, { condition: 'out_of_service', note: 'Tripped' });

  // The extractor's Graph tab: what needs it to the left, what it needs to the right.
  await page.goto(`/assets/${ids.extractor}`);
  await page.getByRole('navigation', { name: 'Asset page sections', exact: true }).getByRole('link', { name: 'Graph', exact: true }).click();
  await expect(page).toHaveURL(/\?tab=graph$/);
  const drawing = page.getByRole('region', { name: 'Drawing' });
  await expect(drawing.getByRole('link', { name: `${names.supply}, Out of service, 1 hop`, exact: true })).toBeVisible();
  await expect(drawing.getByRole('link', { name: `${names.breaker}, 2 hops`, exact: true })).toBeVisible();
  await expect(drawing.getByRole('link', { name: `${names.bench}, 2 hops`, exact: true })).toBeVisible();

  // The list shows the same assets as the drawing.
  const list = page.getByRole('region', { name: 'As a list' });
  await expect(list.getByRole('heading', { name: `What ${names.extractor} needs`, exact: true })).toBeVisible();
  const drawn = await drawing.locator('[data-graph-asset]').evaluateAll((els) => els.map((e) => e.getAttribute('data-graph-asset')).sort());
  const listed = await list.locator('[data-graph-listed]').evaluateAll((els) => els.map((e) => e.getAttribute('data-graph-listed')));
  expect(new Set([...listed, ids.extractor])).toEqual(new Set(drawn));
  expect(drawn).toHaveLength(5);

  // What stops if it does, one hop: only the laser cutter.
  await page.getByLabel('Which way', { exact: true }).selectOption({ label: 'What needs it' });
  await page.getByLabel('Hops', { exact: true }).selectOption('1');
  await page.getByRole('button', { name: 'Draw', exact: true }).click();
  await expect(drawing.locator('[data-graph-asset]')).toHaveCount(2);
  await expect(drawing.getByRole('link', { name: `${names.laser}, 1 hop`, exact: true })).toBeVisible();

  // The drawing downloads as PDF and SVG.
  for (const name of ['Drawing as PDF', 'Drawing as SVG']) {
    const href = await page.getByRole('link', { name, exact: true }).getAttribute('href');
    expect(href).toContain('direction=in');
    const res = await page.context().request.get(href!);
    expect(res.status()).toBe(200);
    expect(res.headers()['content-type']).toMatch(name.endsWith('PDF') ? /pdf/ : /svg/);
  }

  // Clicking an asset opens it, its graph drawn around it.
  await drawing.getByRole('link', { name: `${names.laser}, 1 hop`, exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/assets/${ids.laser}\\?tab=graph$`));
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(names.laser);
  await expect(page.getByRole('region', { name: 'Drawing' }).getByRole('link', { name: `${names.breaker}, 3 hops`, exact: true })).toBeVisible();

  // The category's map: every asset in it, with the links among them.
  await page.goto(`/categories/${category.id}`);
  await page.getByRole('link', { name: 'Graph', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(`Graph of ${category.name}`);
  await expect(page.getByRole('region', { name: 'Drawing' }).locator('[data-graph-asset]')).toHaveCount(5);
  await expect(page.getByRole('heading', { name: 'Every asset, with what it needs', exact: true })).toBeVisible();
});

test('mark a link type as followed while links use it', async ({ signedIn: page, api }) => {
  const name = unique('powered by');
  const inverse = unique('powers');
  const { relation_type } = await api.call('POST', '/relation-types', { name, inverse_name: inverse });
  const a = (await api.call('POST', '/assets', { name: unique('Lamp') })).asset.asset.id;
  const b = (await api.call('POST', '/assets', { name: unique('Socket') })).asset.asset.id;
  await api.call('POST', `/assets/${a}/links`, { type_id: relation_type.id, asset_id: b, outgoing: true });

  await page.goto('/relations');
  await page.getByRole('button', { name: `Edit ${name}`, exact: true }).click();
  await page.getByRole('checkbox', { name: /Follow its links through several hops/ }).check();
  await page.getByRole('button', { name: 'Save relation', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText(`Saved ${name}.`);
  const card = page.getByRole('listitem').filter({ has: page.getByRole('heading', { name: `${name} / ${inverse}`, exact: true }) });
  await expect(card).toContainText('Followed through several hops');

  await page.goto(`/assets/${a}?tab=graph`);
  await expect(page.getByRole('region', { name: 'Drawing' }).locator('[data-graph-asset]')).toHaveCount(2);
});
