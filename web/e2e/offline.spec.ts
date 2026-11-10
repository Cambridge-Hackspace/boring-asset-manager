import type { Page } from '@playwright/test';
import { expect, signIn, test, unique } from './fixtures';

// Using BAM! offline (#214, docs/installing.md#offline): the copy a device
// keeps of what its person can see, looking things up in it with no
// connection, and restocks recorded offline, sent once the connection is
// back, with a conflict shown rather than merged.

// The service worker keeps the shell, so the app opens offline.
test.use({ serviceWorkers: 'allow' });

async function controlled(page: Page): Promise<void> {
  await page.evaluate(async () => {
    await navigator.serviceWorker.ready;
    if (!navigator.serviceWorker.controller) {
      await new Promise((resolve) => navigator.serviceWorker.addEventListener('controllerchange', resolve, { once: true }));
    }
  });
}

/** Turns on keeping a copy in this browser, and waits until it is kept. */
async function keepCopy(page: Page): Promise<void> {
  await page.goto('/offline');
  await page.getByRole('checkbox', { name: 'Keep a copy on this device' }).check();
  await expect(page.getByRole('status').filter({ hasText: /assets, made/ })).toBeVisible();
  await controlled(page);
}

/** What the device keeps, read from IndexedDB as the app wrote it. */
async function kept(page: Page): Promise<{ copy: { assets: { name: string }[] } | null; queue: unknown[] }> {
  return page.evaluate(
    () =>
      new Promise((resolve, reject) => {
        const req = indexedDB.open('bam-offline', 1);
        req.onupgradeneeded = () => req.result.createObjectStore('kept');
        req.onerror = () => reject(req.error);
        req.onsuccess = () => {
          const tx = req.result.transaction('kept', 'readonly');
          const store = tx.objectStore('kept');
          const copy = store.get('copy');
          const queue = store.get('queue');
          tx.oncomplete = () => {
            req.result.close();
            resolve({ copy: copy.result?.copy ?? null, queue: queue.result ?? [] });
          };
        };
      }),
  );
}

test('offline, a scan finds the asset in the copy, marked as possibly out of date, and search works', async ({ signedIn: page, api }) => {
  const code = unique('OFF').toUpperCase();
  const name = unique('Soldering iron');
  const { category } = await api.call('POST', '/categories', { name: unique('Bench tools'), color_hex: '#1d4ed8' });
  await api.call('POST', '/assets', { name, category_ids: [category.id], generator_keys: [{ generator_type: 'qr', assigned_code: code }], stock: { quantity: 4, unit: 'tips' } });
  await keepCopy(page);

  await page.context().setOffline(true);
  await page.goto(`/scan/${code}`);
  await expect(page.getByRole('heading', { level: 1, name })).toBeVisible();
  await expect(page.getByText('It may be out of date')).toBeVisible();
  await expect(page.getByText('4 tips')).toBeVisible();
  // The banner names what works offline.
  await expect(page.getByRole('status').filter({ hasText: 'using the copy from' })).toBeVisible();

  await page.goto('/offline');
  // Every word, in any case, over its name and code.
  await page.getByLabel('Name, code, category or place').fill(`IRON ${code.toLowerCase()}`);
  await page.getByRole('link', { name }).click();
  await expect(page.getByRole('heading', { level: 1, name })).toBeVisible();

  // A page that needs the server says so, with what works instead.
  await page.goto('/loans');
  await expect(page.getByRole('heading', { name: "You're offline" })).toBeVisible();
  await expect(page.getByRole('main').getByRole('link', { name: 'shopping list' })).toBeVisible();

  await page.context().setOffline(false);
  await page.goto('/offline');
  await page.getByRole('checkbox', { name: 'Keep a copy on this device' }).uncheck();
  await expect(page.getByRole('status').filter({ hasText: 'removed from this device' })).toBeVisible();
  expect((await kept(page)).copy).toBeNull();
});

test('the copy holds nothing outside the person’s scopes, and is cleared on signing out', async ({ signedIn: page, api, browser }) => {
  const scope = unique('stores').toLowerCase();
  const { scope: made } = await api.call('POST', '/scopes', { name: scope });
  const { category } = await api.call('POST', '/categories', { name: unique('Stores'), color_hex: '#15803d' });
  await api.call('PUT', `/categories/${category.id}/scopes`, { scope_ids: [made.id] });
  const mine = unique('Cable ties');
  const theirs = unique('Petty cash tin');
  await api.call('POST', '/assets', { name: mine, category_ids: [category.id] });
  await api.call('POST', '/assets', { name: theirs });
  const { role } = await api.call('POST', '/roles', { name: unique('Stores keepers'), permissions: [`${scope}:assets.read`, `${scope}:stock.record`] });
  const email = `${unique('keeper')}@hackspace.org`;
  const { user } = await api.call('POST', '/users', { email, name: 'Keeper', password: 'KeepPass123!' });
  await api.call('PUT', `/users/${user.id}/roles`, { role_ids: [role.id] });

  const context = await browser.newContext({ serviceWorkers: 'allow' });
  const keeper = await context.newPage();
  await signIn(keeper, email, 'KeepPass123!');
  await keepCopy(keeper);
  const names = (await kept(keeper)).copy!.assets.map((a) => a.name);
  expect(names).toContain(mine);
  expect(names).not.toContain(theirs);

  await keeper.getByRole('button', { name: 'Sign out' }).click();
  await expect(keeper).toHaveURL(/\/sign-in/);
  expect((await kept(keeper)).copy).toBeNull();
  await context.close();
});

test('a restock checked off offline is sent when back online, and a conflict is shown, not merged', async ({ signedIn: page, api }) => {
  const name = unique('Zip ties');
  const { asset } = await api.call('POST', '/assets', { name, stock: { quantity: 10, unit: 'pcs', low_stock_threshold: 100 } });
  const id = asset.asset.id;
  const other = unique('Solder');
  const { asset: solder } = await api.call('POST', '/assets', { name: other, stock: { quantity: 2, unit: 'g', low_stock_threshold: 50 } });
  await keepCopy(page);

  // Offline, both are checked off from the copy's list.
  await page.context().setOffline(true);
  await page.goto('/shopping');
  await expect(page.getByText('from the copy kept on this device')).toBeVisible();
  for (const [what, n] of [[name, '90'], [other, '100']] as const) {
    await page.locator(`#bought-${what === name ? id : solder.asset.id}`).fill(n);
    await page.getByRole('button', { name: `Check off ${what}` }).click();
    await expect(page.getByRole('status').filter({ hasText: 'recorded on this device' })).toBeVisible();
  }
  expect((await kept(page)).queue).toHaveLength(2);

  // Meanwhile someone else restocks the solder.
  await api.call('POST', `/assets/${solder.asset.id}/quantity`, { adjust: 100 });

  // Back online: the zip ties go; the solder is held, saying what came.
  await page.context().setOffline(false);
  await expect(page.getByRole('alert').filter({ hasText: 'may be the same purchase' })).toBeVisible();
  await expect.poll(async () => (await api.call('GET', `/assets/${id}`)).asset.asset.stock.quantity).toBe(100);
  expect((await api.call('GET', `/assets/${solder.asset.id}`)).asset.asset.stock.quantity).toBe(102);
  await expect(page.getByRole('alert').filter({ hasText: 'could not be recorded' })).toBeVisible();

  // Its person decides: it was another purchase.
  await page.getByRole('button', { name: 'Record mine too' }).click();
  await expect(page.getByRole('heading', { name: 'Not yet recorded' })).toHaveCount(0);
  expect((await api.call('GET', `/assets/${solder.asset.id}`)).asset.asset.stock.quantity).toBe(202);
});

test('online, the shopping list groups what is running low, and checking one off restocks it', async ({ signedIn: page, api }) => {
  const { category } = await api.call('POST', '/categories', { name: unique('Consumables'), color_hex: '#b45309' });
  const name = unique('Gaffer tape');
  const { asset } = await api.call('POST', '/assets', { name, category_ids: [category.id], stock: { quantity: 1, unit: 'rolls', low_stock_threshold: 5 } });
  await page.goto('/shopping');
  await expect(page.getByRole('heading', { level: 2, name: category.name })).toBeVisible();
  await expect(page.locator(`#bought-${asset.asset.id}`)).toHaveValue('4');
  await page.getByRole('button', { name: `Check off ${name}` }).click();
  await expect(page.getByRole('status').filter({ hasText: `Restocked ${name} by 4 rolls` })).toBeVisible();
  await expect(page.getByRole('button', { name: `Check off ${name}` })).toHaveCount(0);
  expect((await api.call('GET', `/assets/${asset.asset.id}`)).asset.asset.stock.quantity).toBe(5);
});
