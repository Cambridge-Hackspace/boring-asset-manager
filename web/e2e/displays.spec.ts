import { request as playwrightRequest } from '@playwright/test';
import { expect, goTo, test, unique } from './fixtures';

test('displays: add an e-paper display, show an asset on it, and let it fetch its image with its own token', async ({ signedIn: page, api, baseURL }) => {
  // A template the size of a 2.9-inch screen, at its resolution.
  const { template } = await api.call('POST', '/templates', {
    name: unique('Shelf screen'),
    units: 'mm',
    width: (296 * 25.4) / 112,
    height: (128 * 25.4) / 112,
    dpi: 112,
    elements: [{ type: 'text', x: 2, y: 2, width: 50, height: 10, font_size: 14, fill: 'red', content_binding: '{{asset.name}}' }],
  });
  const { asset } = await api.call('POST', '/assets', { name: unique('M3 screws') });

  // A display is a printer, added with a display's model; it fetches its image, so it has no address.
  const name = unique('Shelf');
  await goTo(page, 'Printers');
  await page.getByRole('button', { name: 'New printer' }).click();
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByLabel('Printer model').selectOption('display|2.9in-296x128-bwr');
  await expect(page.getByLabel('Screen')).toHaveValue('2.9in-296x128-bwr');
  await page.getByLabel('Type').selectOption('pull');
  await expect(page.getByLabel('Address')).toHaveCount(0);
  // Only a display's connections are offered.
  await expect(page.getByLabel('Type').locator('option')).toHaveText(['The display fetches it', 'Webhook']);
  await page.getByRole('button', { name: 'Create printer' }).click();
  await expect(page.getByRole('article', { name })).toContainText('2.9in-296x128-bwr (E-paper display)');

  // Printed to, it remembers what it shows.
  const { printers } = await api.call('GET', '/printers');
  const display = (printers as { id: string; name: string }[]).find((p) => p.name === name)!;
  await api.call('POST', `/printers/${display.id}/print`, { template_id: template.id, asset_ids: [asset.asset.id] });

  await goTo(page, 'Displays');
  const card = page.getByRole('article', { name });
  await expect(card).toContainText(`${asset.asset.name}, with ${template.name}, since`);
  await expect(card).toContainText('Has not fetched its image yet; asks again every 900 seconds.');
  await expect(card).toContainText('None yet: the display cannot fetch its image.');

  // A token is shown once, in the address the display fetches.
  await card.getByLabel('Token name').fill('Shelf frame');
  await card.getByRole('button', { name: 'Make a token' }).click();
  const url = await page.getByLabel('The address the display fetches its image from').inputValue();
  expect(url).toContain(`/api/v1/displays/${display.id}/image?token=bamd_`);
  await page.getByRole('button', { name: 'Done' }).click();

  // The display, signed in as no one, fetches it, and is told when it has not changed.
  const device = await playwrightRequest.newContext({ baseURL });
  const path = url.slice(url.indexOf('/api/'));
  const image = await device.get(path);
  expect(image.status()).toBe(200);
  expect(image.headers()['content-type']).toBe('image/png');
  const etag = image.headers()['etag']!;
  expect((await device.get(path, { headers: { 'If-None-Match': etag } })).status()).toBe(304);
  expect((await device.get(`/api/v1/displays/${display.id}`, { headers: { Authorization: `Bearer ${url.split('token=')[1]}` } })).status()).toBe(401);
  await device.dispose();

  await page.reload();
  await expect(card).toContainText('Last fetched its image at');
  await expect(card).toContainText('Shelf frame, made');

  // Refreshed by hand, through the print queue.
  await card.getByRole('button', { name: `Refresh ${name} now` }).click();
  await expect(page.getByRole('status')).toHaveText(`${name} will be refreshed through the print queue.`);

  // The instance's settings for keeping displays current.
  await page.getByLabel('Wait for the rest of a burst (seconds)').fill('45');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Saved the display settings.');
  await api.call('PUT', '/settings/displays', { refresh_window_seconds: 30, pull_refresh_seconds: 900 });

  // Revoked, the token fetches nothing.
  await card.getByRole('button', { name: 'Revoke', exact: true }).click();
  await card.getByRole('button', { name: 'Revoke Shelf frame' }).click();
  await expect(page.getByRole('status')).toContainText('Revoked Shelf frame');
  const after = await playwrightRequest.newContext({ baseURL });
  expect((await after.get(path)).status()).toBe(401);
  await after.dispose();
});

test('displays: a hook refreshes the displays showing an asset', async ({ signedIn: page }) => {
  const label = unique('Restocked');
  await goTo(page, 'Hooks');
  await page.getByRole('button', { name: 'New hook' }).click();
  await page.getByLabel('Label').fill(label);
  await page.getByLabel('Refresh displays').check();
  await page.getByRole('button', { name: 'Save hook' }).click();
  await expect(page.getByRole('status')).toHaveText(`Saved ${label}.`);
  await expect(page.getByRole('listitem').filter({ hasText: label })).toContainText('Refreshes displays');
});
