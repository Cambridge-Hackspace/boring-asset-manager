import AxeBuilder from '@axe-core/playwright';
import { expect, test, unique } from './fixtures';

test('a public search page on a kiosk: found signed out, opened in place, and cleared for the next person', async ({ signedIn: page, api, browser }) => {
  const tag = unique('kiosk');
  const { scopes } = await api.call('GET', '/scopes');
  const publicScope = (scopes as { id: string; name: string }[]).find((s) => s.name === 'public')!;
  const { category: shelf } = await api.call('POST', '/categories', { name: `Shelf ${tag}`, color_hex: '#1d4ed8' });
  await api.call('PUT', `/categories/${shelf.id}/scopes`, { scope_ids: [publicScope.id] });
  // Not public: the cupboard is in no scope the public role reads.
  const { category: cupboard } = await api.call('POST', '/categories', { name: `Cupboard ${tag}`, color_hex: '#b91c1c' });
  const { asset: room } = await api.call('POST', '/assets', { name: `Room ${tag}`, category_ids: [shelf.id] });
  const { asset: torch } = await api.call('POST', '/assets', { name: `Torch ${tag}`, category_ids: [shelf.id] });
  const { relation_types } = await api.call('GET', '/relation-types');
  const contains = (relation_types as { id: string; name: string }[]).find((r) => r.name === 'contains')!;
  await api.call('POST', `/assets/${room.asset.id}/links`, { type_id: contains.id, asset_id: torch.asset.id, outgoing: true });
  await api.call('POST', '/assets', { name: `Head torch ${tag}`, category_ids: [shelf.id] });
  await api.call('POST', '/assets', { name: `Safe torch ${tag}`, category_ids: [cupboard.id] });
  const slug = tag.toLowerCase().replace(/[^a-z0-9-]/g, '-');

  // Made in the editor, as a search page open to signed-out visitors.
  await page.goto('/flows/new');
  await page.getByLabel('Name', { exact: true }).fill(`Find it ${tag}`);
  await page.getByLabel('Address').fill(slug);
  await page.getByRole('button', { name: 'Choose categories' }).click();
  for (const name of [`Shelf ${tag}`, `Cupboard ${tag}`]) {
    await page.getByLabel('Find a category').fill(name);
    await page.getByRole('checkbox', { name }).check();
  }
  const picker = page.getByRole('group', { name: 'Files into, or takes from' });
  await picker.getByRole('button', { name: 'Done' }).click();
  await page.getByRole('radio', { name: /A search page/ }).check();
  await page.getByLabel('Heading').fill('Where is it');
  await page.getByLabel('Clears after this many seconds unused').fill('10');
  await page.getByRole('checkbox', { name: /Open to signed-out visitors/ }).check();
  await page.getByRole('button', { name: 'Save flow' }).click();
  await expect(page.getByRole('status').filter({ hasText: `Saved Find it ${tag}` })).toBeVisible();

  // Signed in, it searches with the admin's own permissions, inside the
  // usual page.
  await page.goto(`/f/${slug}`);
  await expect(page.getByRole('heading', { name: 'Where is it' })).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Main' }).first()).toBeAttached();
  await page.getByRole('searchbox', { name: 'Search' }).fill('torch');
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(page.getByRole('link', { name: `Safe torch ${tag}` })).toBeVisible();

  // Signed out, on a kiosk: nothing but the page and signing in, and only
  // what the public role may read.
  const kiosk = await (await browser.newContext()).newPage();
  await kiosk.goto(`/f/${slug}`);
  await expect(kiosk.getByRole('heading', { name: 'Where is it' })).toBeVisible();
  await expect(kiosk.getByRole('navigation')).toHaveCount(0);
  await expect(kiosk.getByRole('contentinfo')).toHaveCount(0);
  await expect(kiosk.getByRole('link', { name: 'Sign in' })).toBeVisible();
  const box = kiosk.getByRole('searchbox', { name: 'Search' });
  await box.fill('torch');
  await kiosk.getByRole('button', { name: 'Search' }).click();
  await expect(kiosk.getByRole('link', { name: `Torch ${tag}`, exact: true })).toBeVisible();
  await expect(kiosk.getByRole('link', { name: `Head torch ${tag}` })).toBeVisible();
  await expect(kiosk.getByText(`Safe torch ${tag}`)).toHaveCount(0);
  // Where it is shows as text, leading nowhere.
  await expect(kiosk.getByRole('cell', { name: `Room ${tag}` })).toBeVisible();
  await expect(kiosk.getByRole('link', { name: `Room ${tag}` })).toHaveCount(0);
  const results = await new AxeBuilder({ page: kiosk }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations.map((v) => v.id)).toEqual([]);

  // A result opens here, and Back returns to the results.
  await kiosk.getByRole('link', { name: `Torch ${tag}`, exact: true }).click();
  await expect(kiosk.getByRole('heading', { name: `Torch ${tag}` })).toBeVisible();
  await expect(kiosk).toHaveURL(new RegExp(`/f/${slug}\\?search=torch&open=`));
  await kiosk.goBack();
  await expect(kiosk.getByRole('link', { name: `Head torch ${tag}` })).toBeVisible();
  await kiosk.getByRole('link', { name: `Head torch ${tag}` }).click();
  await kiosk.getByRole('link', { name: 'Back to the results' }).click();
  await expect(kiosk.getByRole('link', { name: `Torch ${tag}`, exact: true })).toBeVisible();

  // Left alone for its ten seconds, it starts over for the next person.
  await expect(kiosk).toHaveURL(new RegExp(`/f/${slug}$`), { timeout: 20_000 });
  await expect(box).toHaveValue('');
  await expect(kiosk.getByRole('link', { name: `Torch ${tag}`, exact: true })).toHaveCount(0);
  await kiosk.context().close();
});
