import AxeBuilder from '@axe-core/playwright';
import { expect, test, unique } from './fixtures';

// Scenes (README §3.7): what is in a location, drawn where it stands; one
// thing in it chosen, opened, looked inside, and shown in place.

test('a location is seen from inside, things in it chosen, entered, and one shown in place', async ({ signedIn: page, api }) => {
  const { category: rooms } = await api.call('POST', '/categories', { name: unique('Rooms') });
  const room = unique('room').toLowerCase();
  await api.call('PUT', `/categories/${rooms.id}/icon`, { icon_name: room, source: `icon "${room}" { category "places"; footprint 4 3 2; part "floor" { tile 0 0 0 4 3 } }` });
  const { category: servers } = await api.call('POST', '/categories', { name: unique('Servers') });
  const server = unique('server').toLowerCase();
  await api.call('PUT', `/categories/${servers.id}/icon`, { icon_name: server, source: `icon "${server}" { category "compute"; footprint 2 1 1; part "box" { box 0 0 0 2 1 1 } }` });

  const lab = (await api.call('POST', '/assets', { name: unique('Lab'), category_ids: [rooms.id] })).asset.asset;
  const rack = (await api.call('POST', '/assets', { name: unique('Rack'), category_ids: [servers.id], generator_keys: [] })).asset.asset;
  const chair = (await api.call('POST', '/assets', { name: unique('Chair') })).asset.asset;
  const disk = (await api.call('POST', '/assets', { name: unique('Disk') })).asset.asset;
  const { relation_types } = await api.call('GET', '/relation-types');
  const contains = (relation_types as { id: string; name: string }[]).find((t) => t.name === 'contains')!;
  for (const [from, to] of [
    [lab.id, rack.id],
    [lab.id, chair.id],
    [rack.id, disk.id],
  ]) {
    await api.call('POST', `/assets/${from}/links`, { type_id: contains.id, asset_id: to, outgoing: true });
  }
  await api.call('PUT', `/assets/${chair.id}/condition`, { condition: 'out_of_service', note: 'Wobbles' });

  // from the lab's page, inside it
  await page.goto(`/assets/${lab.id}`);
  await page.getByRole('link', { name: 'See inside' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(`Inside ${lab.name}`);
  const drawing = page.getByRole('group', { name: `${lab.name}, its contents drawn where they stand` });
  await expect(drawing.getByRole('button')).toHaveCount(2);
  const rackItem = drawing.getByRole('button', { name: `${rack.name}, holds 1` });
  const chairItem = drawing.getByRole('button', { name: `${chair.name}, out of service` });
  await expect(rackItem).toBeVisible();
  await expect(rackItem.locator('polygon').first()).toBeVisible();
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations.map((v) => `${v.id}: ${v.help} (${v.nodes.map((n) => n.target.join(' ')).join('; ')})`)).toEqual([]);

  // chosen: what it is, and the way in
  await chairItem.focus();
  const chosen = page.getByRole('complementary', { name: 'Chosen' });
  await expect(chosen.getByRole('heading')).toHaveText(chair.name);
  await expect(chosen).toContainText('Out of service');
  await expect(chosen.getByRole('link', { name: 'Look inside' })).toHaveCount(0);
  await rackItem.focus();
  await chosen.getByRole('link', { name: 'Look inside' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(`Inside ${rack.name}`);
  // and back out by the breadcrumb
  await page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('link', { name: lab.name }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(`Inside ${lab.name}`);

  // zooming changes the view; Fit brings it back
  const box = () => page.locator('svg[aria-label$="drawn where they stand"]').getAttribute('viewBox');
  const fitted = await box();
  await page.getByRole('button', { name: 'Zoom in' }).click();
  expect(await box()).not.toBe(fitted);
  await page.getByRole('button', { name: 'Fit' }).click();
  expect(await box()).toBe(fitted);
  // so do the wheel and a drag across it
  const area = (await page.locator('svg[aria-label$="drawn where they stand"]').boundingBox())!;
  await page.mouse.move(area.x + 20, area.y + 20);
  await page.mouse.wheel(0, -200);
  await expect.poll(box).not.toBe(fitted);
  const zoomed = await box();
  await page.mouse.down();
  await page.mouse.move(area.x + 120, area.y + 80, { steps: 5 });
  await page.mouse.up();
  await expect.poll(box).not.toBe(zoomed);
  await page.getByRole('button', { name: 'Fit' }).click();

  // the disk's page shows it in place: outlined, the rest muted
  await page.goto(`/assets/${disk.id}`);
  await page.getByRole('link', { name: 'Show in place' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(`Inside ${rack.name}`);
  await expect(page.locator(`[data-outline="${disk.id}"]`)).toBeVisible();
  await expect(page.getByRole('complementary', { name: 'Chosen' }).getByRole('heading')).toHaveText(disk.name);

  // a double click goes inside what holds things, and opens the rest
  await page.goto(`/assets/${lab.id}/scene`);
  await page.getByRole('button', { name: `${rack.name}, holds 1` }).dblclick();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(`Inside ${rack.name}`);
  await page.goto(`/assets/${lab.id}/scene`);
  // the rack, in front, covers the middle of the chair: double-click the chair itself
  await page.getByRole('button', { name: `${chair.name}, out of service` }).dispatchEvent('dblclick');
  await expect(page).toHaveURL(new RegExp(`/assets/${chair.id}$`));
});
