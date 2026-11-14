import AxeBuilder from '@axe-core/playwright';
import { expect, goTo, test, unique } from './fixtures';

// Other specs' terms and assets share the database, so every word here
// carries this run's tag.
test('keep search terms, and search finds things through them', async ({ signedIn: page, api }) => {
  const tag = unique('terms');
  const flexible = `Bendy ${tag}`;
  const tpu = `Stretchy ${tag}`;
  await api.call('POST', '/assets', { name: `${tpu} spool` });
  await api.call('POST', '/assets', { name: `Rigid ${tag} rod` });
  const rigid = await api.call('POST', '/terms', { label: `Rigid ${tag}` });

  // A term, then a narrower one, linked from its own form.
  await goTo(page, 'Search terms');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Search terms');
  await page.getByRole('button', { name: 'New term' }).click();
  await page.getByLabel('Preferred label').fill(flexible);
  await page.getByRole('button', { name: 'Add term' }).click();
  await expect(page.getByRole('status').filter({ hasText: `Added ${flexible}.` })).toBeVisible();
  await page.getByLabel('Add one of the related terms').selectOption({ label: `Rigid ${tag}` });
  await page.getByRole('heading', { name: 'Related terms' }).locator('..').getByRole('button', { name: 'Link' }).click();
  await expect(page.getByRole('button', { name: `Unlink Rigid ${tag} from related terms` })).toBeVisible();
  await page.getByRole('button', { name: 'Done' }).click();

  await page.getByRole('button', { name: 'New term' }).click();
  await page.getByLabel('Preferred label').fill(tpu);
  await page.getByLabel('Other names, one per line').fill(`springy ${tag}`);
  await page.getByRole('button', { name: 'Add term' }).click();
  await page.getByLabel('Add one of the broader terms').selectOption({ label: flexible });
  await page.getByRole('heading', { name: 'Broader terms' }).locator('..').getByRole('button', { name: 'Link' }).click();
  await expect(page.getByRole('button', { name: `Unlink ${flexible} from broader terms` })).toBeVisible();
  await page.getByRole('button', { name: 'Done' }).click();

  // A term already called that is refused.
  await page.getByRole('button', { name: 'New term' }).click();
  await page.getByLabel('Preferred label').fill(tpu.toUpperCase());
  await page.getByRole('button', { name: 'Add term' }).click();
  await expect(page.getByRole('alert')).toContainText(`Another term is already called ${tpu.toUpperCase()}`);
  await page.getByRole('button', { name: 'Cancel' }).click();

  // Try a search before anyone searches.
  await page.getByLabel('Find a term').fill(tag);
  await expect(page.getByRole('list', { name: 'Terms' }).getByRole('listitem')).toHaveCount(3);
  await page.getByLabel('Words').fill(flexible);
  await page.getByRole('button', { name: 'See what it finds' }).click();
  await expect(page.getByTestId('expansion')).toContainText(`${tpu}, narrower than ${flexible}`);
  await expect(page.getByText(`Suggests: Rigid ${tag}.`)).toBeVisible();
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations.map((v) => v.id)).toEqual([]);

  // Search finds the narrower term's asset, saying why, and suggests the related one.
  await goTo(page, 'Assets');
  await page.getByLabel('Search').fill(flexible.toLowerCase());
  await page.getByRole('button', { name: 'Search' }).click();
  const found = page.getByRole('link', { name: `${tpu} spool` });
  await expect(found).toBeVisible();
  await expect(found.locator('xpath=ancestor::tr').getByTestId('match-why')).toHaveText(`Found as ${tpu}, narrower than ${flexible}`);
  await expect(page.getByRole('link', { name: `Rigid ${tag} rod` })).toHaveCount(0);
  await page.getByTestId('search-suggestions').getByRole('button', { name: `Rigid ${tag}` }).click();
  await expect(page.getByRole('link', { name: `Rigid ${tag} rod` })).toBeVisible();

  // ~~ in a query expands; ~ doesn't.
  await page.getByLabel('Search').fill(`name ~~ "springy ${tag}"`);
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(found).toBeVisible();
  await page.getByLabel('Search').fill(`name ~ "springy ${tag}"`);
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(page.getByText('No assets match.')).toBeVisible();

  // A typing mistake is forgiven when nothing matches as typed.
  await page.getByLabel('Search').fill(`Strecthy ${tag}`);
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(found).toBeVisible();
  await expect(found.locator('xpath=ancestor::tr').getByTestId('match-why')).toContainText(`close to Strecthy ${tag}`);
  expect(rigid.term.id).toBeTruthy();
});

test('import search terms from a file, and remove them as one', async ({ signedIn: page }) => {
  const tag = unique('termimport');
  const name = `Words ${tag}`;
  await goTo(page, 'Search terms');
  await page.getByLabel('File').setInputFiles({
    name: 'words.json',
    mimeType: 'application/json',
    buffer: Buffer.from(JSON.stringify([{ label: `Torch ${tag}`, alt_labels: [`flashlight ${tag}`], broader: [`Light ${tag}`] }])),
  });
  await page.getByLabel('What it is').fill(name);
  await page.getByRole('button', { name: 'Import', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: `Imported ${name}: 2 terms and 1 link.` })).toBeVisible();
  const torch = page.getByRole('list', { name: 'Terms' }).getByRole('listitem').filter({ has: page.getByRole('heading', { name: `Torch ${tag}` }) });
  await expect(torch).toContainText(`Also called flashlight ${tag}.`);
  await expect(torch).toContainText(`From the import ${name}.`);

  const row = page.getByRole('listitem').filter({ hasText: name }).filter({ has: page.getByRole('button', { name: 'Remove import' }) });
  await row.getByRole('button', { name: 'Remove import' }).click();
  await row.getByRole('button', { name: 'Remove import' }).click();
  await expect(page.getByRole('status').filter({ hasText: `Removed the import ${name}.` })).toBeVisible();
  await expect(page.getByRole('heading', { name: `Torch ${tag}` })).toHaveCount(0);
});
