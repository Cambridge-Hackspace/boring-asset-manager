import AxeBuilder from '@axe-core/playwright';
import { expect, goTo, test, unique } from './fixtures';

test('search by a field, with a query, and build one from lists', async ({ signedIn: page, api }) => {
  const tag = unique('query');
  const key = `width_${tag.replace(/[^a-z0-9]/g, '_')}`;
  const shelf = await api.call('POST', '/categories', { name: `Shelf ${tag}` });
  const field = await api.call('POST', '/fields', { key, label: `Width ${tag}`, validation_type: 'number' });
  await api.call('POST', `/categories/${shelf.category.id}/fields`, { field_id: field.field.id, is_required: false });
  for (const [name, width] of [
    ['Narrow board', '10'],
    ['Wide board', '40'],
    ['Wider board', '55'],
  ]) {
    await api.call('POST', '/assets', { name: `${name} ${tag}`, category_ids: [shelf.category.id], field_values: { [key]: width } });
  }

  // Plain words find a field's value.
  await goTo(page, 'Assets');
  const box = page.getByLabel('Search');
  await box.fill('55');
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(page.getByRole('link', { name: `Wider board ${tag}` })).toBeVisible();
  await expect(page.getByRole('link', { name: `Wide board ${tag}` })).toHaveCount(0);

  // A query, sorted.
  await box.fill(`category = "Shelf ${tag}" AND field.${key} > 20 ORDER BY field.${key} DESC`);
  await page.getByRole('button', { name: 'Search' }).click();
  const names = page.getByRole('link', { name: new RegExp(`board ${tag}$`) });
  await expect(names).toHaveText([`Wider board ${tag}`, `Wide board ${tag}`]);

  // A mistake is underlined, with what is wrong.
  await box.fill(`field.${key}_nope > 20`);
  await page.getByRole('button', { name: 'Search' }).click();
  const problem = page.getByRole('alert');
  await expect(problem).toContainText(`no field has the key "${key}_nope"`);
  await expect(page.locator('mark.query-fault')).toHaveText(`field.${key}_nope`);
  await expect(box).toHaveAttribute('aria-invalid', 'true');
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations.map((v) => v.id)).toEqual([]);

  // The builder writes the query, and searches with it.
  await page.getByRole('button', { name: 'Build a query' }).click();
  await page.getByLabel('Condition 1: what to search by').selectOption({ label: `Width ${tag}` });
  await page.getByLabel('Condition 1: how to compare').selectOption('<');
  await page.getByLabel('Condition 1: value').fill('50');
  await page.getByRole('button', { name: 'Add a condition' }).click();
  await page.getByLabel('Condition 2: what to search by').selectOption({ label: 'Category' });
  await page.getByRole('combobox', { name: 'Condition 2: value' }).selectOption({ label: `Shelf ${tag}` });
  await expect(page.getByTestId('built-query')).toHaveText(`field.${key} < 50 AND category = "Shelf ${tag}"`);
  const built = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(built.violations.map((v) => v.id)).toEqual([]);
  await page.getByRole('button', { name: 'Search with this query' }).click();
  await expect(box).toHaveValue(`field.${key} < 50 AND category = "Shelf ${tag}"`);
  await expect(names).toHaveText([`Wide board ${tag}`, `Narrow board ${tag}`]);
  await expect(page.getByRole('alert')).toHaveCount(0);
});
