import AxeBuilder from '@axe-core/playwright';
import type { Page } from '@playwright/test';
import { expect, test, unique } from './fixtures';

// Flows (README §6.7), run as they are meant to be: on a phone.

test.use({ viewport: { width: 390, height: 844 } });

async function audit(page: Page, label: string): Promise<void> {
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations.map((v) => `${v.id}: ${v.help}`), `${label} has accessibility violations`).toEqual([]);
}

test('flows: leave wood in one of two kinds, the form changing to fit, then collect it by its code', async ({ signedIn: page, api }) => {
  const { category: wood } = await api.call('POST', '/categories', { name: unique('Wood'), color_hex: '#92400e' });
  const short = unique('Short boards');
  const long = unique('Long boards');
  const { category: shortCat } = await api.call('POST', '/categories', { name: short, parent_id: wood.id, color_hex: '#15803d' });
  const { category: longCat } = await api.call('POST', '/categories', { name: long, parent_id: wood.id, color_hex: '#1d4ed8' });
  const ownerKey = `owner_${Date.now()}`;
  const untilKey = `until_${Date.now()}`;
  const { field: owner } = await api.call('POST', '/fields', { key: ownerKey, label: 'Owner', validation_type: 'text' });
  const { field: until } = await api.call('POST', '/fields', { key: untilKey, label: 'Until', validation_type: 'date' });
  await api.call('POST', `/categories/${wood.id}/fields`, { field_id: owner.id, is_required: true });
  await api.call('POST', `/categories/${longCat.id}/fields`, { field_id: until.id, is_required: false });

  const { starters } = await api.call('GET', `/flow-starters?category_ids=${shortCat.id},${longCat.id}`);
  const leave = starters[0].definition;
  leave.steps[1].fields = [{ key: untilKey, label: 'Leave until' }, { key: ownerKey, default: '{user}' }];
  const leaveSlug = unique('leave').toLowerCase();
  const { flow: leaveFlow } = await api.call('POST', '/flows', { slug: leaveSlug, name: 'Leave something', definition: leave });
  const collect = starters[1].definition;
  collect.steps[0].filed_by = leaveFlow.id;
  const collectSlug = unique('collect').toLowerCase();
  await api.call('POST', '/flows', { slug: collectSlug, name: 'Collect it', definition: collect });

  // Leaving it: short boards ask only the owner; long ones ask until too.
  await page.goto(`/f/${leaveSlug}`);
  await expect(page.getByRole('heading', { name: 'What are you leaving?' })).toBeFocused();
  await audit(page, 'choosing a category');
  await page.getByRole('button', { name: short }).click();
  await expect(page.getByRole('heading', { name: 'About it' })).toBeVisible();
  await expect(page.getByLabel('Leave until')).toHaveCount(0);
  await page.getByRole('button', { name: 'Start again' }).click();
  await page.getByRole('button', { name: long }).click();
  await expect(page.getByLabel('Leave until')).toBeVisible();
  await audit(page, 'the form');
  const name = unique('Oak');
  await page.getByLabel('What is it?').fill(name);
  await page.getByLabel('Leave until').fill('2026-11-06');
  await page.getByRole('button', { name: 'Next' }).click();
  await expect(page.getByRole('heading', { name: 'Filed' })).toBeVisible();
  await expect(page.getByText(`Filed ${name} in ${long}.`)).toBeVisible();
  await audit(page, 'done');
  const [scroll, width] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
  expect(scroll, 'the flow scrolls sideways').toBeLessThanOrEqual(width);

  // Give it a code, as a label would carry.
  const { assets } = await api.call('GET', `/assets?search=${encodeURIComponent(name)}`);
  const code = unique('WOOD').toUpperCase();
  await api.call('PUT', `/assets/${assets[0].asset.id}`, { generator_keys: [{ generator_type: 'code128', assigned_code: code }] });

  // Collecting it, by its code, typed as a scanner types it.
  await page.goto(`/f/${collectSlug}`);
  await expect(page.getByRole('heading', { name: 'What are you collecting?' })).toBeVisible();
  await audit(page, 'finding');
  await page.getByLabel('Its code').fill(code);
  await page.getByLabel('Its code').press('Enter');
  await expect(page.getByRole('heading', { name: 'Collect it?' })).toBeVisible();
  await expect(page.getByText(`${name} will be taken off the list.`)).toBeVisible();
  await audit(page, 'confirming');
  await page.getByRole('button', { name: 'Yes, go ahead' }).click();
  await expect(page.getByRole('heading', { name: 'Collected' })).toBeVisible();
  await expect(page.getByText(`Archived ${name}.`)).toBeVisible();
  const { asset } = await api.call('GET', `/assets/${assets[0].asset.id}`);
  expect(asset.asset.archived_at).toBeTruthy();
});
