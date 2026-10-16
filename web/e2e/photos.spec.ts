import { expect, goTo, test, unique } from './fixtures';

// A 1×1 PNG.
const dot = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64');

test('add photos to an asset, see one in the list, and remove one', async ({ signedIn: page, api }) => {
  const name = unique('Bandsaw');
  const { asset } = await api.call('POST', '/assets', { name });
  await page.goto(`/assets/${asset.asset.id}`);
  const photos = page.getByRole('region', { name: 'Photos' });
  await expect(photos).toContainText('No photos.');

  await photos.getByLabel('Add a photo').setInputFiles({ name: 'front.png', mimeType: 'image/png', buffer: dot });
  await expect(page.getByRole('status').filter({ hasText: 'Photo' })).toHaveText('Photo added.');
  await photos.getByLabel('Add a photo').setInputFiles({ name: 'side.png', mimeType: 'image/png', buffer: dot });
  await expect(photos.getByRole('img')).toHaveCount(2);
  const first = photos.getByRole('img', { name: `Photo 1 of ${name}, shown in lists` });
  await expect.poll(() => first.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth)).toBe(1);
  await expect(page.getByRole('region', { name: 'History' }).getByRole('listitem').first()).toContainText('Photo added: side.png');

  // The list shows the first as a thumbnail.
  await goTo(page, 'Assets');
  await page.getByLabel('Search').fill(name);
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(page.getByRole('row', { name: new RegExp(name) }).locator('img')).toHaveCount(1);

  await page.getByRole('link', { name }).click();
  await photos.getByRole('button', { name: 'Move photo 2 earlier' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Photo' })).toHaveText('Photo moved earlier.');
  const item = photos.getByRole('listitem').first();
  await item.getByRole('button', { name: 'Remove' }).click();
  await item.getByRole('group').getByRole('button', { name: 'Remove' }).click();
  await expect(photos.getByRole('img')).toHaveCount(1);
  await expect(page.getByRole('region', { name: 'History' }).getByRole('listitem').first()).toContainText('Photo removed: side.png');
});
