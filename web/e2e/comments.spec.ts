import { expect, test, unique } from './fixtures';

// A 1×1 PNG.
const dot = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64');

test('comment on an asset in Markdown with a photo, see its category’s comments, and edit and delete one’s own', async ({ signedIn: page, api }) => {
  const kind = unique('Bandsaws');
  const { category } = await api.call('POST', '/categories', { name: kind, color_hex: '#0f766e' });
  await api.call('POST', `/categories/${category.id}/comments`, { body: 'Wear **goggles**.' });
  const name = unique('Bandsaw');
  const { asset } = await api.call('POST', '/assets', { name, category_ids: [category.id] });

  await page.goto(`/assets/${asset.asset.id}`);
  const comments = page.getByRole('region', { name: 'Comments', exact: true });
  // The category's comment, marked with the category, linking to it there.
  const inherited = comments.getByRole('article').filter({ hasText: 'goggles' });
  await expect(inherited.locator('strong')).toHaveText('goggles');
  await expect(inherited.getByRole('link', { name: kind, exact: true })).toHaveAttribute('href', new RegExp(`/categories/${category.id}\\?tab=comments#comment-`));

  // Written in Markdown, with a photo attached while writing, which goes
  // on the asset.
  const box = comments.getByLabel('Add a comment');
  await box.fill('The left motor makes a noise above **50%**.\n\n- [x] Belt ordered\n<script>alert(1)</script>');
  await comments.getByLabel('Attach a file').setInputFiles({ name: 'belt.png', mimeType: 'image/png', buffer: dot });
  await expect(box).toHaveValue(/!\[belt\.png\]\(\/api\/v1\/assets\/.+\/attachments\/.+\)/);
  await comments.getByRole('button', { name: 'Preview' }).click();
  await expect(comments.getByLabel('Preview').locator('strong')).toHaveText('50%');
  await comments.getByRole('button', { name: 'Comment', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Comment' })).toHaveText('Comment added.');

  // The admin wrote both, so both are theirs: theirs on the asset comes first.
  const mine = comments.getByRole('article').filter({ hasNotText: 'goggles' });
  await expect(mine.locator('.comment-body strong')).toHaveText('50%');
  await expect(mine.getByRole('img', { name: 'Done', exact: true })).toBeVisible();
  await expect(mine.locator('script')).toHaveCount(0);
  const photo = mine.getByRole('img', { name: 'belt.png' });
  await expect.poll(() => photo.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth)).toBe(1);
  await expect(page.getByRole('region', { name: 'Attachments' }).getByRole('img')).toHaveCount(1);
  // Its own first, then its category's.
  await expect(comments.getByRole('article').first()).not.toContainText('goggles');

  // Narrowed to its own.
  await comments.getByLabel('Only this asset’s').check();
  await expect(comments.getByRole('article')).toHaveCount(1);
  await comments.getByLabel('Only this asset’s').uncheck();

  // Edited, it says so; deleted, it says by whom; both are in history.
  await mine.getByRole('button', { name: 'Edit' }).click();
  await mine.getByLabel('Edit the comment').fill('The belt is **fitted**.');
  await mine.getByRole('button', { name: 'Save' }).click();
  await expect(mine.getByText('(edited)')).toBeVisible();
  await expect(mine.locator('.comment-body strong')).toHaveText('fitted');
  const history = page.getByRole('region', { name: 'History' });
  await expect(history.getByRole('listitem').first()).toContainText('Comment edited');
  await mine.getByRole('button', { name: 'Delete' }).click();
  await mine.getByRole('group').getByRole('button', { name: 'Delete' }).click();
  await expect(mine).toContainText('Deleted by');
  await expect(history.getByRole('listitem').first()).toContainText('It said “The belt is **fitted**.”');

  // The category's page shows its comment under Comments, and the asset's
  // search finds the asset by a comment's words.
  await inherited.getByRole('link', { name: kind, exact: true }).click();
  await expect(page).toHaveURL(/tab=comments#comment-/);
  await expect(page.getByRole('region', { name: 'Comments', exact: true }).getByRole('article')).toHaveCount(1);
  await api.call('POST', `/assets/${asset.asset.id}/comments`, { body: 'Blade from the corner shop' });
  await page.goto('/assets?search=corner+shop');
  await expect(page.getByRole('link', { name, exact: true })).toBeVisible();
});
