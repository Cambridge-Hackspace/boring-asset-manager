import AxeBuilder from '@axe-core/playwright';
import type { Page } from '@playwright/test';
import { expect, test, unique } from './fixtures';

// Assets' icons (README §3.7): an asset is drawn with its nearest
// category's icon, in lists and on its page, unless it is given one of
// its own on its form.

const boxIcon = (name: string) => `icon "${name}" { category "storage"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }`;
const drawn = (page: Page, name: string) => page.locator(`svg[data-icon="${name}"]`);

async function noViolations(page: Page) {
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations.map((v) => `${v.id}: ${v.help} (${v.nodes.map((n) => n.target.join(' ')).join('; ')})`)).toEqual([]);
}

test("an asset is drawn with its category's icon, and given one of its own", async ({ signedIn: page, api }) => {
  const parentName = unique('Storage');
  const { category: parent } = await api.call('POST', '/categories', { name: parentName, color_hex: '#0f766e' });
  const { category: child } = await api.call('POST', '/categories', { name: unique('Bins'), parent_id: parent.id, color_hex: '#b45309' });
  const crate = unique('crate').toLowerCase();
  const barrel = unique('barrel').toLowerCase();
  await api.call('PUT', `/categories/${parent.id}/icon`, { icon_name: crate, source: boxIcon(crate) });
  const { category: other } = await api.call('POST', '/categories', { name: unique('Barrels') });
  await api.call('PUT', `/categories/${other.id}/icon`, { icon_name: barrel, source: boxIcon(barrel) });
  const name = unique('Blue bin');
  const { asset } = await api.call('POST', '/assets', { name, category_ids: [child.id] });

  // in the list: beside its name, and in its category's pill
  await page.goto(`/assets?search=${encodeURIComponent(name)}`);
  const row = page.getByRole('row').filter({ has: page.getByRole('link', { name }) });
  await expect(row.locator(`svg[data-icon="${crate}"]`)).toHaveCount(2);

  // on its page, saying where it comes from
  await page.goto(`/assets/${asset.asset.id}`);
  await expect(page.getByRole('heading', { level: 1, name }).locator(`svg[data-icon="${crate}"]`)).toBeVisible();
  await expect(page.getByText(`From ${parentName}.`)).toBeVisible();
  await noViolations(page);

  // one of its own, chosen on its form
  await page.getByRole('link', { name: 'Edit' }).click();
  await expect(page.getByText(`From ${parentName}.`)).toBeVisible();
  await page.getByRole('button', { name: 'Choose another…' }).click();
  const picker = page.getByRole('group', { name: 'Choose an icon' });
  await picker.getByRole('searchbox', { name: 'Search icons' }).fill(barrel);
  await expect(picker.getByRole('button')).toHaveCount(2); // it, and Close
  await noViolations(page);
  await picker.getByRole('button', { name: barrel }).click();
  await expect(picker).toBeHidden();
  await expect(page.getByText(`Its own, ${barrel}.`)).toBeVisible();
  await page.getByRole('button', { name: 'Save' }).click();

  await expect(page.getByText(`Its own, ${barrel}.`)).toBeVisible();
  await expect(page.getByRole('heading', { level: 1, name }).locator(`svg[data-icon="${barrel}"]`)).toBeVisible();
  await expect(drawn(page, crate)).toHaveCount(1); // still in its category's pill

  // and back to its category's
  await page.getByRole('link', { name: 'Edit' }).click();
  await page.getByRole('button', { name: "Use the category's" }).click();
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByText(`From ${parentName}.`)).toBeVisible();
});
