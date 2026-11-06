import { expect, goTo, test, unique } from './fixtures';

test('create a vendor with contacts, find it, edit it, and reorder from it when a category runs low', async ({ signedIn: page, api }) => {
  const name = unique('Acme supplies');
  await goTo(page, 'Vendors');
  await page.getByRole('link', { name: 'New vendor' }).click();
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByLabel('Website (optional)').fill('https://acme.example/order');
  await page.getByLabel('Account number (optional)').fill('ACC-42');
  await page.getByLabel('Notes (optional)').fill('Ask for **trade** prices.');
  await page.getByRole('button', { name: 'Add a contact' }).click();
  await page.getByLabel('Name', { exact: true }).nth(1).fill('Ada');
  await page.getByLabel('Role (optional)').fill('sales');
  await page.getByLabel('Email (optional)').fill('ada@acme.example');
  await page.getByRole('button', { name: 'Add a contact' }).click();
  await page.getByLabel('Name', { exact: true }).nth(2).fill('Bob');
  await page.getByLabel('Phone (optional)').nth(1).fill('+44 1223 555010');
  await page.getByRole('button', { name: 'Create vendor' }).click();
  await expect(page).toHaveURL(/\/vendors\/[0-9a-f-]{36}$/);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(name);
  await expect(page.getByRole('link', { name: 'ada@acme.example' })).toHaveAttribute('href', 'mailto:ada@acme.example');
  await expect(page.getByRole('link', { name: '+44 1223 555010' })).toHaveAttribute('href', 'tel:+441223555010');
  await expect(page.getByRole('region', { name: 'Notes' }).locator('strong')).toHaveText('trade');
  const id = page.url().split('/').pop()!;

  // Found by a contact's name, and its first contact shown in the list.
  await goTo(page, 'Vendors');
  await page.getByLabel('Search').fill('ada sales');
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(page).toHaveURL(/\?q=ada(\+|%20)sales$/);
  const row = page.getByRole('listitem').filter({ hasText: name });
  await expect(row).toContainText('Ada (sales)');
  await expect(row.getByRole('link', { name: 'acme.example/order' })).toHaveAttribute('target', '_blank');
  await page.getByLabel('Search').fill(unique('nothing like it'));
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(page.getByText('No vendors match.')).toBeVisible();

  // Edited: the second contact first, and the first gone.
  await page.goto(`/vendors/${id}`);
  await page.getByRole('button', { name: 'Edit' }).click();
  await page.getByRole('button', { name: 'Move Bob up' }).click();
  await page.getByRole('button', { name: 'Remove Ada' }).click();
  await page.getByLabel('Account number (optional)').fill('ACC-43');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Saved.');
  await expect(page.getByText('ACC-43', { exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'ada@acme.example' })).toHaveCount(0);
  await page.getByRole('navigation', { name: 'Parts of this vendor' }).getByRole('link', { name: 'History' }).click();
  const latest = page.getByRole('region', { name: 'History' }).getByRole('listitem').first();
  await expect(latest).toContainText('Account number: ACC-42 → ACC-43');
  await expect(latest).toContainText('Contact removed: Ada (sales)');

  // A category bought from it, running low, offers to reorder from it at home.
  const category = unique('Sandpaper');
  const { category: cat } = await api.call('POST', '/categories', { name: category, color_hex: '#7c3aed', is_consumable: true });
  await page.goto(`/categories/${cat.id}?tab=vendors`);
  await page.getByLabel('Add a vendor').selectOption({ label: name });
  await page.getByRole('button', { name: 'Add vendor' }).click();
  await page.getByRole('button', { name: 'Save vendors' }).click();
  await expect(page.getByRole('status')).toHaveText('Vendors saved.');
  await api.call('PUT', `/categories/${cat.id}/running-low`, { running_low: true });

  await page.goto('/');
  const vendors = page.getByRole('list', { name: `Vendors of ${category}` });
  await expect(vendors.getByRole('link', { name, exact: true })).toHaveAttribute('href', `/vendors/${id}`);
  const reorder = vendors.getByRole('link', { name: `Reorder ${category} from ${name}` });
  await expect(reorder).toHaveAttribute('href', 'https://acme.example/order');
  await expect(reorder).toHaveAttribute('rel', 'noopener noreferrer');
  await expect(reorder).toHaveAttribute('target', '_blank');

  // And the vendor's page names the category it is preferred for.
  await page.goto(`/vendors/${id}`);
  await expect(page.getByRole('region', { name: 'Preferred for' }).getByRole('link', { name: category })).toBeVisible();
});
