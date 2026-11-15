import AxeBuilder from '@axe-core/playwright';
import { expect, goTo, test, unique } from './fixtures';

test('save a search, share it, run it from the home page, and see when it needs fixing', async ({ signedIn: page, api }) => {
  const tag = unique('saved');
  const key = `length_${tag.replace(/[^a-z0-9]/g, '_')}`;
  const shelf = await api.call('POST', '/categories', { name: `Rack ${tag}` });
  const field = await api.call('POST', '/fields', { key, label: `Length ${tag}`, validation_type: 'number' });
  await api.call('POST', `/categories/${shelf.category.id}/fields`, { field_id: field.field.id, is_required: false });
  for (const [name, length] of [
    ['Short rod', '10'],
    ['Long rod', '90'],
  ]) {
    await api.call('POST', '/assets', { name: `${name} ${tag}`, category_ids: [shelf.category.id], field_values: { [key]: length } });
  }
  const query = `category = "Rack ${tag}" AND field.${key} > 50`;
  const title = `Long rods ${tag}`;
  // The list of saved searches folds away; this opens it when it is shut.
  const showSaved = async () => {
    const details = page.locator('details', { has: page.getByText(/^Saved searches \(\d+\)$/) });
    if (!(await details.evaluate((d) => (d as HTMLDetailsElement).open))) await details.locator('summary').click();
  };

  // Search, then save what the list shows under a name, shared.
  await goTo(page, 'Assets');
  await page.getByLabel('Search', { exact: true }).fill(query);
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  await expect(page.getByRole('link', { name: `Long rod ${tag}` })).toBeVisible();
  await page.getByRole('button', { name: 'Save as…' }).click();
  await expect(page.getByLabel('Title', { exact: true })).toBeFocused();
  await page.getByLabel('Title', { exact: true }).fill(title);
  await page.getByLabel('Share with everyone who can read assets').check();
  const form = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(form.violations.map((v) => v.id)).toEqual([]);
  await page.getByRole('button', { name: 'Save', exact: true }).click();

  // Opened from the list, it searches again.
  await page.getByLabel('Search', { exact: true }).fill('');
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  await showSaved();
  const entry = page.getByRole('listitem').filter({ has: page.getByRole('link', { name: title, exact: true }) });
  await expect(entry).toContainText('shared');
  await entry.getByRole('link', { name: title, exact: true }).click();
  await expect(page.getByLabel('Search', { exact: true })).toHaveValue(query);
  await expect(page.getByRole('link', { name: new RegExp(`rod ${tag}$`) })).toHaveText([`Long rod ${tag}`]);

  // The home page lists it too.
  await page.goto('/');
  const card = page.getByRole('region', { name: 'Saved searches' });
  await card.getByRole('link', { name: title, exact: true }).click();
  await expect(page).toHaveURL(/\/assets\?search=/);
  await expect(page.getByRole('link', { name: `Long rod ${tag}` })).toBeVisible();

  // A field it names deleted, it says it needs fixing, and opened, where.
  await api.call('DELETE', `/fields/${field.field.id}`);
  // Opened afresh, the list reads them again.
  await page.goto('/assets');
  await showSaved();
  await expect(entry).toContainText('needs fixing');
  await entry.getByRole('link', { name: title, exact: true }).click();
  await expect(page.getByRole('alert')).toContainText(`no field has the key "${key}"`);
  await expect(page.locator('mark.query-fault')).toHaveText(`field.${key}`);

  // Deleted, it is gone.
  await showSaved();
  await entry.getByRole('button', { name: 'Delete', exact: true }).click();
  await page.getByRole('button', { name: `Delete ${title}` }).click();
  await expect(page.getByRole('link', { name: title, exact: true })).toHaveCount(0);
});
