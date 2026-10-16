import zlib from 'node:zlib';
import { request as playwrightRequest } from '@playwright/test';
import { ADMIN_EMAIL, expect, goTo, signIn, test, unique } from './fixtures';

test('printers: add one through the generated form and test-print', async ({ signedIn: page, api, printer }) => {
  await api.call('POST', '/templates', {
    name: unique('Rollo 2x1'),
    units: 'in',
    width: 2,
    height: 1,
    dpi: 203,
    elements: [{ type: 'text', x: 0.1, y: 0.2, width: 1.8, height: 0.5, font_size: 12, content_binding: '{{asset.name}}' }],
  });
  const assetName = unique('Box');
  await api.call('POST', '/assets', { name: assetName });

  const name = unique('Rollo');
  await goTo(page, 'Printers');
  await page.getByRole('button', { name: 'Add printer' }).click();
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByLabel('Driver').selectOption('tspl');
  await page.getByLabel('Model').selectOption('Rollo X1038');
  await page.getByLabel('Loaded media').selectOption('2x1');
  await page.getByLabel('Address').fill(`127.0.0.1:${printer.port}`);
  await page.getByLabel('density').fill('12');
  await page.getByRole('button', { name: 'Add printer' }).click();
  const card = page.getByRole('article', { name });
  await expect(card).toContainText('Rollo X1038 (tspl), 2x1');

  await card.getByRole('button', { name: 'Test print' }).click();
  await card.getByLabel('Template').selectOption({ index: 0 });
  // Pick the 2 × 1 in template made above.
  const options = await card.getByLabel('Template').locator('option').allTextContents();
  await card.getByLabel('Template').selectOption({ label: options.find((o) => o.startsWith('Rollo 2x1'))! });
  await card.getByLabel('Asset').fill(assetName);
  await card.getByRole('button', { name: 'Find' }).click();
  await card.getByRole('button', { name: assetName }).click();
  await card.getByRole('button', { name: 'Print test label' }).click();
  await expect(card.getByRole('status')).toHaveText(`Sent 1 label to ${name}.`);
  const job = (await printer.job()).toString('latin1');
  expect(job).toMatch(/^SIZE 50.8 mm,25.4 mm\r\n/);
  expect(job).toContain('DENSITY 12\r\n');
  expect(job).toMatch(/PRINT 1,1\r\n$/);
});

test('users: roles, disabling, and the anti-lockout refusal', async ({ signedIn: page }) => {
  const email = `${unique('bo')}@hackspace.org`;
  await goTo(page, 'Users');
  await page.getByRole('button', { name: 'New user' }).click();
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Name (optional)').fill('Bo Brown');
  await page.getByLabel('Initial password').fill('BoPass123!');
  await page.getByRole('button', { name: 'Create user' }).click();
  await expect(page.getByRole('status')).toHaveText('User created.');
  await expect(page.getByRole('row').filter({ hasText: email })).toContainText('Bo Brown');
  await page.getByRole('button', { name: `Rename ${email}` }).click();
  await page.getByLabel(`Name for ${email}`).fill('Bo Green');
  await page.getByRole('button', { name: 'Save name' }).click();
  await expect(page.getByRole('status')).toHaveText(`${email} is now shown as Bo Green.`);

  await page.getByRole('button', { name: `Roles for ${email}` }).click();
  await page.getByRole('checkbox', { name: 'admin' }).check();
  await page.getByRole('button', { name: 'Save roles' }).click();
  await expect(page.getByRole('status')).toHaveText(`Roles updated for ${email}.`);
  await page.getByRole('button', { name: `Disable ${email}` }).click();
  await expect(page.getByRole('row').filter({ hasText: email })).toContainText('Disabled');

  // The other admin is disabled, so the signed-in admin is the last one.
  await page.getByRole('button', { name: `Roles for ${ADMIN_EMAIL}` }).click();
  await page.getByRole('checkbox', { name: 'admin' }).uncheck();
  await page.getByRole('button', { name: 'Save roles' }).click();
  await expect(page.getByRole('alert')).toContainText('would remove the last one');
});

function tinyPNG(): Buffer {
  const crc = (b: Buffer) => {
    let c = ~0;
    for (const x of b) {
      c ^= x;
      for (let k = 0; k < 8; k++) c = (c >>> 1) ^ (0xedb88320 & -(c & 1));
    }
    return ~c >>> 0;
  };
  const chunk = (type: string, data: Buffer) => {
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const td = Buffer.concat([Buffer.from(type), data]);
    const c = Buffer.alloc(4);
    c.writeUInt32BE(crc(td));
    return Buffer.concat([len, td, c]);
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(16, 0);
  ihdr.writeUInt32BE(16, 4);
  ihdr[8] = 8;
  return Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    chunk('IHDR', ihdr),
    chunk('IDAT', zlib.deflateSync(Buffer.alloc(16 * 17))),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}

test('images: upload, see where they are used, and only delete unused ones', async ({ signedIn: page, api }) => {
  const filename = `${unique('logo')}.png`;
  await goTo(page, 'Images');
  await page.locator('input[type=file]').setInputFiles({ name: filename, mimeType: 'image/png', buffer: tinyPNG() });
  const card = page.getByRole('listitem').filter({ hasText: filename });
  await expect(card).toContainText('Not used by any template.');

  const image = (await api.call('GET', '/images')).images.find((i: { filename: string }) => i.filename === filename);
  const name = unique('Logo');
  const { template } = await api.call('POST', '/templates', {
    name, units: 'mm', width: 50, height: 25, dpi: 300,
    elements: [{ type: 'static_image', x: 1, y: 1, width: 10, height: 10, image_id: image.id }],
  });
  await page.reload();
  await expect(card).toContainText(`Used by ${name}`);
  await expect(card.getByRole('button', { name: 'Delete' })).toBeDisabled();

  await api.call('DELETE', `/templates/${template.id}`);
  await page.reload();
  await card.getByRole('button', { name: 'Delete' }).click();
  await card.getByRole('button', { name: 'Delete' }).click();
  await expect(page.getByText(filename)).toHaveCount(0);
});

test('a user changes their own password', async ({ signedIn: admin, api, browser }) => {
  const email = `${unique('pw')}@hackspace.org`;
  const user = await api.call('POST', '/users', { email, password: 'OldPass123!' });
  expect(user.user.email).toBe(email);
  await admin.close();

  const page = await browser.newPage();
  await signIn(page, email, 'OldPass123!');
  await page.getByRole('link', { name: email }).click();
  await page.getByLabel('Current password').fill('OldPass123!');
  await page.getByLabel('New password', { exact: true }).fill('NewPass456!');
  await page.getByLabel('Repeat new password').fill('NewPass456!');
  await page.getByRole('button', { name: 'Change password' }).click();
  await expect(page.getByRole('status')).toContainText('Password changed');
  await page.getByRole('button', { name: 'Sign out' }).click();
  await signIn(page, email, 'NewPass456!');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Welcome');
  await page.close();
});

test('roles: create one that only records stock, edit it, and delete it', async ({ signedIn: page }) => {
  const name = unique('Stores');
  await goTo(page, 'Roles');
  // admin cannot be changed.
  const admin = page.getByRole('listitem').filter({ has: page.getByRole('heading', { name: 'admin', exact: true }) });
  await expect(admin).toContainText('always has every permission');
  await expect(admin.getByRole('button')).toHaveCount(0);

  await page.getByRole('button', { name: 'New role' }).click();
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByRole('checkbox', { name: /Record stock/ }).check();
  await page.getByRole('button', { name: 'Create role' }).click();
  await expect(page.getByRole('status')).toHaveText(`Created ${name}.`);
  const card = page.getByRole('listitem').filter({ has: page.getByRole('heading', { name, exact: true }) });
  await expect(card).toContainText('Read assets, Record stock');

  // Users can be given it.
  await goTo(page, 'Users');
  await page.getByRole('button', { name: `Roles for ${ADMIN_EMAIL}` }).click();
  await expect(page.getByRole('checkbox', { name })).toBeVisible();
  await page.getByRole('button', { name: 'Cancel' }).click();

  await goTo(page, 'Roles');
  await card.getByRole('button', { name: `Edit ${name}` }).click();
  await page.getByLabel('Description (optional)').fill('Counts and restocks');
  await page.getByRole('button', { name: 'Save role' }).click();
  await expect(page.getByRole('status')).toHaveText(`Saved ${name}.`);
  await expect(card).toContainText('Counts and restocks');

  await card.getByRole('button', { name: 'Delete' }).click();
  await card.getByRole('group').getByRole('button', { name: 'Delete' }).click();
  await expect(page.getByRole('status')).toHaveText(`Deleted ${name}.`);
  await expect(page.getByRole('heading', { name, exact: true })).toHaveCount(0);
});

test('a role that only records stock can use stock but not edit assets', async ({ signedIn: page, api }) => {
  const { role } = await api.call('POST', '/roles', { name: unique('Counters'), description: '', permissions: ['assets.read', 'stock.record'] });
  const email = `${unique('counter')}@hackspace.org`;
  const { user } = await api.call('POST', '/users', { email, password: 'CountPass123!' });
  await api.call('PUT', `/users/${user.id}/roles`, { role_ids: [role.id] });
  const { asset } = await api.call('POST', '/assets', { name: unique('Washers'), stock: { quantity: 10, unit: 'pcs' } });

  await page.context().clearCookies();
  await signIn(page, email, 'CountPass123!');
  await page.goto(`/assets/${asset.asset.id}`);
  const stock = page.getByRole('region', { name: 'Stock' });
  await stock.getByLabel('How many (pcs)').fill('3');
  await stock.getByRole('button', { name: 'Use' }).click();
  await expect(stock).toContainText('7 pcs left');
  await expect(page.getByRole('link', { name: 'Edit' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Archive' })).toHaveCount(0);
});

test('API tokens: make one, use it from a script, and revoke it', async ({ signedIn: page, api }) => {
  const { role } = await api.call('POST', '/roles', { name: unique('Readers'), description: '', permissions: ['assets.read'] });
  const name = unique('Dashboard');
  await goTo(page, 'API tokens');
  await page.getByRole('button', { name: 'New token' }).click();
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByRole('checkbox', { name: role.name }).check();
  await page.getByRole('button', { name: 'Create token' }).click();
  const secret = await page.getByLabel('The new token').inputValue();
  expect(secret).toMatch(/^bam_[0-9a-f]{64}$/);

  // A script, with no session: it may read, and only read.
  const script = await playwrightRequest.newContext({ baseURL: new URL(page.url()).origin, extraHTTPHeaders: { Authorization: `Bearer ${secret}` } });
  expect((await script.get('/api/v1/assets')).status()).toBe(200);
  expect((await script.post('/api/v1/assets', { data: { name: 'Nope' } })).status()).toBe(403);
  expect((await script.get('/api/v1/tokens')).status()).toBe(403);

  await page.getByRole('button', { name: 'Done' }).click();
  await page.reload();
  const row = page.getByRole('row').filter({ hasText: name });
  // Last used: just now, by the script.
  await expect(row.getByRole('cell').nth(3)).not.toHaveText('Never');
  await row.getByRole('button', { name: 'Revoke' }).click();
  await row.getByRole('group').getByRole('button', { name: 'Revoke' }).click();
  await expect(page.getByRole('status')).toHaveText(`Revoked ${name}.`);
  expect((await script.get('/api/v1/assets')).status()).toBe(401);
  await script.dispose();
});

test('scopes: a keeper of the tools edits the tools and nothing else', async ({ signedIn: page, api }) => {
  const scope = unique('tools');
  const { category } = await api.call('POST', '/categories', { name: unique('Tools'), color_hex: '#1d4ed8' });
  const { asset: tool } = await api.call('POST', '/assets', { name: unique('Drill'), category_ids: [category.id] });
  const { asset: other } = await api.call('POST', '/assets', { name: unique('Kettle') });

  // The scope, and the category it labels.
  await goTo(page, 'Roles');
  await page.getByLabel('New scope').fill(scope);
  await page.getByRole('button', { name: 'Add scope' }).click();
  await expect(page.getByRole('status')).toHaveText(`Created the scope ${scope}.`);
  await page.goto(`/categories/${category.id}`);
  await page.getByRole('checkbox', { name: scope }).check();
  await page.getByRole('button', { name: 'Save scopes' }).click();
  await expect(page.getByRole('status')).toHaveText('Scopes saved.');

  // A role that reads everything but edits in the scope only.
  const name = unique('Keepers');
  await goTo(page, 'Roles');
  await page.getByRole('button', { name: 'New role' }).click();
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByRole('checkbox', { name: /^Edit assets/ }).check();
  await page.getByRole('combobox', { name: 'Where: Edit assets' }).selectOption('some');
  await page.getByRole('checkbox', { name: `Edit assets in ${scope}` }).check();
  await page.getByRole('button', { name: 'Create role' }).click();
  await expect(page.getByRole('status')).toHaveText(`Created ${name}.`);
  const card = page.getByRole('listitem').filter({ has: page.getByRole('heading', { name, exact: true }) });
  await expect(card).toContainText(`Read assets, Edit assets in ${scope}`);
  const roles = (await api.call('GET', '/roles')).roles as { id: string; name: string }[];

  const email = `${unique('keeper')}@hackspace.org`;
  const { user } = await api.call('POST', '/users', { email, name: 'Keeper', password: 'KeepPass123!' });
  await api.call('PUT', `/users/${user.id}/roles`, { role_ids: [roles.find((r) => r.name === name)!.id] });

  await page.context().clearCookies();
  await signIn(page, email, 'KeepPass123!');
  await page.goto(`/assets/${tool.asset.id}`);
  await expect(page.getByRole('heading', { name: tool.asset.name })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Edit' })).toBeVisible();
  await page.goto(`/assets/${other.asset.id}`);
  await expect(page.getByRole('heading', { name: other.asset.name })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Edit' })).toHaveCount(0);
});
