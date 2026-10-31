import zlib from 'node:zlib';
import AxeBuilder from '@axe-core/playwright';
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
  // And one for another roll.
  await api.call('POST', '/templates', { name: unique('Tape 62'), units: 'mm', width: 62, height: 29, dpi: 300, elements: [] });
  const assetName = unique('Box');
  await api.call('POST', '/assets', { name: assetName });

  const name = unique('Rollo');
  await goTo(page, 'Printers');
  await page.getByRole('button', { name: 'New printer' }).click();
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByLabel('Printer model').selectOption('tspl|Rollo X1038');
  await page.getByLabel('Roll loaded').selectOption('2x1');
  await page.getByLabel('Address').fill(`127.0.0.1:${printer.port}`);
  await page.getByLabel('Darkness').fill('12');
  await page.getByRole('button', { name: 'Create printer' }).click();
  const card = page.getByRole('article', { name });
  await expect(card).toContainText('Rollo X1038 (TSPL), 2 × 1 in');

  await card.getByRole('button', { name: 'Test print' }).click();
  // Pick the 2 × 1 in template made above. Templates for other rolls are
  // listed, but cannot be chosen.
  const template = card.getByLabel('Template');
  const options = await template.locator('option:not(:disabled)').allTextContents();
  await template.selectOption({ label: options.find((o) => o.startsWith('Rollo 2x1'))! });
  await expect(template.locator('option:disabled', { hasText: 'does not fit its roll' }).first()).toBeAttached();
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

test('label stock: add a custom size, load it in a Rollo, and print on it', async ({ signedIn: page, api, printer }) => {
  const stockName = unique('Bin labels');
  const name = unique('Bin Rollo');
  await goTo(page, 'Printers');
  await page.getByRole('button', { name: 'New label stock' }).click();
  const form = page.getByRole('form', { name: 'New label stock' });
  await form.getByLabel('Name').fill(stockName);
  await form.getByLabel('Width (mm)').fill('60');
  await form.getByLabel('Length (mm)').fill('40');
  await form.getByLabel('Gap (mm)').fill('2.5');
  await form.getByRole('button', { name: 'Save stock' }).click();
  await expect(page.getByRole('article', { name: stockName })).toContainText('60 × 40 mm, 2.5 mm gap');

  await page.getByRole('button', { name: 'New printer' }).click();
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByLabel('Printer model').selectOption('tspl|Rollo X1038');
  await page.getByLabel('Roll loaded').selectOption({ label: `${stockName} (60 × 40 mm, 2.5 mm gap)` });
  await page.getByLabel('Address').fill(`127.0.0.1:${printer.port}`);
  await page.getByRole('button', { name: 'Create printer' }).click();
  await expect(page.getByRole('article', { name })).toContainText(`Rollo X1038 (TSPL), ${stockName}`);
  await expect(page.getByRole('article', { name: stockName })).toContainText(`Loaded in ${name}`);

  // Loaded stock is not deleted.
  const stockCard = page.getByRole('article', { name: stockName });
  await stockCard.getByRole('button', { name: 'Delete' }).click();
  await stockCard.getByRole('button', { name: `Delete ${stockName}` }).click();
  await expect(page.getByRole('alert')).toContainText(`loaded in a printer: ${name}`);

  // A label printed on it is sent at its size and gap.
  const { template } = await api.call('POST', '/templates', {
    name: unique('Bin tag'), units: 'mm', width: 60, height: 40, dpi: 203,
    elements: [{ type: 'text', x: 2, y: 2, width: 56, height: 10, font_size: 14, content_binding: '{{asset.name}}' }],
  });
  const { asset } = await api.call('POST', '/assets', { name: unique('Resistors') });
  const { printers } = await api.call('GET', '/printers');
  const rollo = (printers as { id: string; name: string }[]).find((p) => p.name === name)!;
  await api.call('POST', `/printers/${rollo.id}/print`, { template_id: template.id, asset_ids: [asset.asset.id], copies: 1 });
  const job = (await printer.job()).toString('latin1');
  expect(job).toMatch(/^SIZE 60 mm,40 mm\r\nGAP 2.5 mm,0 mm\r\n/);
});

test('users: roles, disabling, and the anti-lockout refusal', async ({ signedIn: page }) => {
  const email = `${unique('bo')}@hackspace.org`;
  await goTo(page, 'Users');
  await page.getByRole('button', { name: 'New user' }).click();
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Name', { exact: true }).fill('Bo Brown');
  await page.getByLabel('Initial password').fill('BoPass123!');
  await page.getByRole('button', { name: 'Create user' }).click();
  await expect(page.getByRole('status')).toHaveText('User created.');
  await expect(page.getByRole('row').filter({ hasText: email })).toContainText('Bo Brown');
  await page.getByRole('button', { name: `Rename ${email}` }).click();
  await page.getByLabel(`Name for ${email}`).fill('Bo Green');
  await page.getByRole('button', { name: 'Save name' }).click();
  await expect(page.getByRole('status')).toHaveText(`${email} is now shown as Bo Green.`);

  await page.getByRole('button', { name: `Change roles for ${email}` }).click();
  await page.getByRole('checkbox', { name: 'admin' }).check();
  await page.getByRole('button', { name: 'Save roles' }).click();
  await expect(page.getByRole('status')).toHaveText(`Roles updated for ${email}.`);
  await page.getByRole('button', { name: `Disable ${email}` }).click();
  await expect(page.getByRole('row').filter({ hasText: email })).toContainText('Disabled');

  // The other admin is disabled, so the signed-in admin is the last one.
  await page.getByRole('button', { name: `Change roles for ${ADMIN_EMAIL}` }).click();
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
  const user = await api.call('POST', '/users', { email, name: 'Pat Password', password: 'OldPass123!' });
  expect(user.user.email).toBe(email);
  await admin.close();

  const page = await browser.newPage();
  await signIn(page, email, 'OldPass123!');
  await page.getByRole('link', { name: 'Pat Password' }).click();
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
  await page.getByRole('button', { name: `Change roles for ${ADMIN_EMAIL}` }).click();
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
  const { user } = await api.call('POST', '/users', { email, name: 'Cat Counter', password: 'CountPass123!' });
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
  await page.getByRole('button', { name: 'Create scope' }).click();
  await expect(page.getByRole('status')).toHaveText(`Created the scope ${scope}.`);
  await page.goto(`/categories/${category.id}`);
  await page.getByRole('navigation', { name: 'Parts of this category' }).getByRole('link', { name: 'Who can see it' }).click();
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

test('users: a role given in one scope only acts there, and allows nothing else', async ({ signedIn: page, api }) => {
  const scope = unique('bench');
  const { scope: made } = await api.call('POST', '/scopes', { name: scope });
  const { category } = await api.call('POST', '/categories', { name: unique('Bench'), color_hex: '#15803d' });
  await api.call('PUT', `/categories/${category.id}/scopes`, { scope_ids: [made.id] });
  const { asset: tool } = await api.call('POST', '/assets', { name: unique('Vise'), category_ids: [category.id] });
  const { asset: other } = await api.call('POST', '/assets', { name: unique('Mug') });
  const editors = unique('Editors');
  await api.call('POST', '/roles', { name: editors, permissions: ['assets.read', 'assets.write', 'labels.print'] });
  const readers = unique('Readers');
  await api.call('POST', '/roles', { name: readers, permissions: ['assets.read'] });
  const email = `${unique('cy')}@hackspace.org`;
  await api.call('POST', '/users', { email, name: 'Cy', password: 'CyPass123!' });

  await goTo(page, 'Users');
  await page.getByRole('button', { name: `Change roles for ${email}` }).click();
  const row = page.getByRole('row').filter({ hasText: email });
  await row.getByRole('checkbox', { name: editors }).check();
  await row.getByRole('checkbox', { name: readers }).check();
  const where = row.getByRole('group', { name: 'Only in these scopes' }).first();
  await expect(where).toContainText('None ticked: in every scope');
  await where.getByRole('checkbox', { name: scope }).check();
  await expect(where).toContainText('Leaves out: Print labels.');
  const results = await new AxeBuilder({ page }).include('table').withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations.map((v) => `${v.id}: ${v.help}`)).toEqual([]);
  await row.getByRole('button', { name: 'Save roles' }).click();
  await expect(page.getByRole('status')).toHaveText(`Roles updated for ${email}.`);
  await expect(row).toContainText(`${editors} in ${scope}, ${readers}`);

  // Signed in, they edit what is on the bench and only read the rest.
  await page.context().clearCookies();
  await signIn(page, email, 'CyPass123!');
  await page.goto(`/assets/${tool.asset.id}`);
  await expect(page.getByRole('heading', { name: tool.asset.name })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Edit' })).toBeVisible();
  await page.goto(`/assets/${other.asset.id}`);
  await expect(page.getByRole('heading', { name: other.asset.name })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Edit' })).toHaveCount(0);
  await page.goto('/account');
  await expect(page.getByText(`Your roles: ${editors} in ${scope}, ${readers}.`)).toBeVisible();
});

test('users: set a password for someone who forgot theirs, who is told on sign-in whom to ask', async ({ signedIn: page, api, browser }) => {
  const email = `${unique('forgetful')}@hackspace.org`;
  await api.call('POST', '/users', { email, name: 'Fran Forgetful', password: 'Forgotten123!' });

  await goTo(page, 'Users');
  await page.getByRole('button', { name: `Set password for ${email}` }).click();
  await page.getByLabel(`New password for ${email}`).fill('BrandNew456!');
  await page.getByRole('button', { name: 'Save password' }).click();
  await expect(page.getByRole('status')).toHaveText(`Password set for ${email}. They are signed out everywhere; give them the new one.`);

  // They sign in with the new one, from a page that says whom to ask.
  const fran = await browser.newPage();
  await fran.goto('/sign-in');
  await expect(fran.getByText('Forgot your password? Ask whoever manages users here to set a new one.')).toBeVisible();
  await fran.getByLabel('Email').fill(email);
  await fran.getByLabel('Password').fill('BrandNew456!');
  await fran.getByRole('button', { name: 'Sign in' }).click();
  await expect(fran.getByRole('heading', { level: 1 })).toHaveText('Welcome');

  // Their account says what they may do, in words; and a page they may
  // not open says what it needs.
  await fran.goto('/account');
  await expect(fran.getByRole('region', { name: 'What you may do' })).toContainText('You hold no roles yet.');
  await fran.goto('/users');
  await expect(fran.getByRole('main')).toContainText('Users needs Manage users, which your account does not have.');
  await fran.close();
});
