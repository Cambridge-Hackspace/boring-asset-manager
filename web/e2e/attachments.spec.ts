import { expect, goTo, test, unique } from './fixtures';

// A 1×1 PNG.
const dot = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64');

test('attach photos and a document to an asset, see a photo in the list, and remove one', async ({ signedIn: page, api }) => {
  const name = unique('Bandsaw');
  const { asset } = await api.call('POST', '/assets', { name });
  await page.goto(`/assets/${asset.asset.id}`);
  const files = page.getByRole('region', { name: 'Attachments' });
  await expect(files).toContainText('No attachments.');

  await files.getByLabel('Add a file').setInputFiles({ name: 'front.png', mimeType: 'image/png', buffer: dot });
  await expect(page.getByRole('status').filter({ hasText: 'Attachment' })).toHaveText('Attachment added.');
  await files.getByLabel('Add a file').setInputFiles({ name: 'side.png', mimeType: 'image/png', buffer: dot });
  await expect(files.getByRole('img')).toHaveCount(2);
  const first = files.getByRole('img', { name: `Photo 1 of ${name}, shown in lists` });
  await expect.poll(() => first.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth)).toBe(1);
  await expect(page.getByRole('region', { name: 'History' }).getByRole('listitem').first()).toContainText('Photo added: side.png');

  // A document downloads, as itself.
  await files.getByLabel('Add a file').setInputFiles({ name: 'manual.txt', mimeType: 'text/plain', buffer: Buffer.from('Keep the guard down.') });
  await expect(page.getByRole('region', { name: 'History' }).getByRole('listitem').first()).toContainText('Attachment added: manual.txt');
  const download = page.waitForEvent('download');
  await files.getByRole('link', { name: 'Download manual.txt' }).click();
  expect((await download).suggestedFilename()).toBe('manual.txt');

  // The list shows the first photo as a thumbnail.
  await goTo(page, 'Assets');
  await page.getByLabel('Search').fill(name);
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(page.getByRole('row', { name: new RegExp(name) }).locator('img')).toHaveCount(1);

  await page.getByRole('link', { name }).click();
  await files.getByRole('button', { name: 'Move side.png earlier' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Attachment' })).toHaveText('Attachment moved earlier.');
  const item = files.getByRole('listitem').first();
  await item.getByRole('button', { name: 'Remove' }).click();
  await item.getByRole('group').getByRole('button', { name: 'Remove' }).click();
  await expect(files.getByRole('img')).toHaveCount(1);
  await expect(page.getByRole('region', { name: 'History' }).getByRole('listitem').first()).toContainText('Photo removed: side.png');
});
