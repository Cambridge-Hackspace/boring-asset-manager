import AxeBuilder from '@axe-core/playwright';
import { expect, test, unique } from './fixtures';

test('a search that finds one thing opens its scan page, as the space or the person chooses, and leads back to the list', async ({ signedIn: page, api }) => {
  const { report } = await api.call('POST', '/reports', {
    name: unique('Bench page'),
    kind: 'scan',
    spec: { sections: [{ type: 'actions', actions: ['open'] }] },
  });
  const { category } = await api.call('POST', '/categories', { name: unique('Benches') });
  await api.call('PUT', `/categories/${category.id}/scan-template`, { scan_template_id: report.id });
  const name = unique('Oak bench');
  const { asset: { asset } } = await api.call('POST', '/assets', { name, category_ids: [category.id] });

  try {
    // Listed, with its scan page beside it, until someone says otherwise.
    await page.goto(`/assets?search=${encodeURIComponent(name)}`);
    const row = page.getByRole('row').filter({ has: page.getByRole('link', { name, exact: true }) });
    await expect(row.getByRole('link', { name: 'Scan page' })).toHaveAttribute('href', `/scan/${asset.id}`);

    // The space turns it on, under Reports.
    await page.goto('/reports');
    await page.getByRole('checkbox', { name: "Open a search's one result straight away" }).check();
    await page.getByRole('button', { name: 'Save search settings' }).click();
    await expect(page.getByText('Saved.')).toBeVisible();

    // From the home page's search, straight to its scan page.
    await page.goto('/');
    const field = page.getByRole('searchbox', { name: 'Search' });
    await field.fill(name);
    await field.press('Enter');
    await expect(page).toHaveURL(new RegExp(`/scan/${asset.id}\\?search=`));
    await expect(page.getByRole('heading', { level: 1 })).toHaveText(name);
    const scanned = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
    expect(scanned.violations.map((v) => v.id)).toEqual([]);

    // All the results, listed, without being sent on again.
    await page.getByRole('link', { name: `All results for “${name}”` }).click();
    await expect(page).toHaveURL(/\/assets\?search=.*&all=1$/);
    await expect(page.getByRole('link', { name, exact: true })).toBeVisible();

    // Back goes to the home page, past the list it went through.
    await page.goBack();
    await page.goBack();
    await expect(page).toHaveURL(/\/$/);

    // A person may choose to list instead, on their account.
    await page.goto('/account');
    await page.getByLabel('When a search finds exactly one thing').selectOption({ label: 'List it' });
    await expect(page.getByText('Saved.')).toBeVisible();
    await page.goto('/assets');
    await page.getByLabel('Search', { exact: true }).fill(name);
    await page.getByRole('button', { name: 'Search', exact: true }).click();
    await expect(page.getByRole('link', { name, exact: true })).toBeVisible();
    await expect(page).toHaveURL(/\/assets\?search=/);
  } finally {
    await api.call('PUT', '/auth/search-direct', { direct: null });
    await api.call('PUT', '/settings/search', { direct: false });
  }
});
