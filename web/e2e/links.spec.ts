import { expect, test, unique } from './fixtures';

test('put screws in a bin on a shelf, see where they are, and move them out', async ({ signedIn: page, api }) => {
  const shelf = unique('Shelf');
  const bin = unique('Bin');
  const screws = unique('Screws');
  const made = async (name: string) => (await api.call('POST', '/assets', { name })).asset.asset.id as string;
  const [shelfId, binId, screwsId] = [await made(shelf), await made(bin), await made(screws)];
  const { relation_types } = await api.call('GET', '/relation-types');
  const contains = relation_types.find((r: { name: string }) => r.name === 'contains');
  await api.call('POST', `/assets/${shelfId}/links`, { type_id: contains.id, asset_id: binId, outgoing: true });

  // From the screws: they are contained in the bin.
  await page.goto(`/assets/${screwsId}`);
  const links = page.getByRole('region', { name: 'Links' });
  await links.getByLabel('This asset').selectOption({ label: 'contained in' });
  await links.getByLabel('Find an asset').fill(bin);
  await links.getByRole('button', { name: 'Find' }).click();
  await links.getByRole('button', { name: 'Add link' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Now' })).toHaveText(`Now contained in ${bin}.`);
  await expect(page.getByRole('navigation', { name: 'Location' })).toHaveText(`In ${shelf} › ${bin}`);
  const history = page.getByRole('region', { name: 'History' });
  await expect(history.getByRole('listitem').first()).toContainText(`Contained in ${bin}`);

  // The bin lists them, and cannot be archived while it holds them.
  await page.getByRole('navigation', { name: 'Location' }).getByRole('link', { name: bin }).click();
  await expect(page.getByRole('region', { name: 'Links' })).toContainText(`Contains ${screws}`);
  await page.getByRole('button', { name: 'Archive' }).click();
  await page.getByRole('group', { name: /^Archive this asset/ }).getByRole('button', { name: 'Archive' }).click();
  await expect(page.getByRole('alert')).toContainText('move them out first');

  await page.getByRole('region', { name: 'Links' }).getByRole('button', { name: `Unlink ${screws}` }).click();
  await expect(page.getByRole('status').filter({ hasText: 'No longer' })).toHaveText(`No longer contains ${screws}.`);
  await page.goto(`/assets/${screwsId}`);
  await expect(page.getByRole('navigation', { name: 'Location' })).toHaveCount(0);
});
