import { expect, goTo, test, unique } from './fixtures';

test('thresholds, running-low actions and notification channels', async ({ signedIn: page, api }) => {
  const tag = unique('tape');
  const name = `Tape ${tag}`;

  // A consumable category with a threshold, through the form. With nothing
  // in it, it is running low from the start.
  await page.goto('/categories/new');
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByLabel('Consumable').check();
  await page.getByLabel('Low stock threshold (optional)').fill('1');
  await page.getByRole('button', { name: 'Create category' }).click();
  await expect(page).toHaveURL(/\/categories\/[0-9a-f-]{36}$/);
  const id = page.url().split('/').pop()!;
  const stock = page.getByRole('region', { name: 'Stock' });
  await expect(stock).toContainText('Running low. 0 active assets here and in its subcategories; flagged automatically below 1.');

  // A channel, subscribed to both events.
  await goTo(page, 'Notifications');
  await page.getByRole('button', { name: 'Add channel' }).click();
  const form = page.getByRole('form', { name: 'New channel' });
  await form.getByLabel('Name', { exact: true }).fill(`Stores ${tag}`);
  await form.getByLabel('Webhook URL').fill('https://discord.com/api/webhooks/123/e2e-token');
  await form.getByRole('button', { name: 'Add channel' }).click();
  await expect(page.getByText('Channel saved.')).toBeVisible();
  await expect(page.getByText('Discord, enabled. A category is running low; A category is restocked; An asset counted by quantity is running low; An asset counted by quantity is restocked; An asset needs maintenance, is out of service, or is working again; A loan is overdue.')).toBeVisible();
  await expect(page.getByText('e2e-token')).toHaveCount(0);

  // Adding an asset restocks it automatically; then flag it by hand.
  await api.call('POST', '/assets', { name: `Roll ${tag}`, category_ids: [id] });
  await page.goto(`/categories/${id}`);
  await expect(stock).toContainText('In stock. 1 active asset here');
  await stock.getByRole('button', { name: 'Mark running low' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Marked as running low.' })).toBeVisible();
  await expect(stock).toContainText('Running low.');

  // The home page lists it.
  await page.goto('/');
  await expect(page.getByRole('region', { name: 'Running low' }).getByRole('link', { name })).toBeVisible();

  // Both events were queued for the channel. Outgoing HTTPS is blocked in
  // this battery, so they wait to be retried rather than being sent.
  await goTo(page, 'Notifications');
  const log = page.getByRole('region', { name: 'Recent deliveries' });
  await expect(log.getByRole('row').filter({ hasText: `A category is restocked: ${name}` })).toContainText(`Stores ${tag}`);
  await expect(log.getByRole('row').filter({ hasText: `A category is running low: ${name}` })).toContainText(/Waiting to (send|retry)/);

  // The test button reports the failure, which shows nothing left the container.
  await page.getByRole('button', { name: `Send a test message to Stores ${tag}` }).click();
  await expect(page.getByText(/The test message was not delivered: .*proxyconnect/)).toBeVisible();
});
