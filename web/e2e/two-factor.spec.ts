import { createHmac } from 'node:crypto';
import { expect, signIn, test, unique } from './fixtures';

// Two-factor sign-in (docs/access.md): an authenticator app set up on the
// account page, then asked for after the password.

/** The secret an app is given as text: base32, in groups of four. */
function secretOf(text: string): Buffer {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  let bits = '';
  for (const ch of text.replace(/\s/g, '')) bits += alphabet.indexOf(ch).toString(2).padStart(5, '0');
  const bytes = [];
  for (let i = 0; i + 8 <= bits.length; i += 8) bytes.push(parseInt(bits.slice(i, i + 8), 2));
  return Buffer.from(bytes);
}

/** The code an app shows at a step (RFC 6238: SHA-1, six digits). */
function code(secret: Buffer, step: number): string {
  const msg = Buffer.alloc(8);
  msg.writeBigUInt64BE(BigInt(step));
  const sum = createHmac('sha1', secret).update(msg).digest();
  const offset = sum[sum.length - 1]! & 0x0f;
  return String((sum.readUInt32BE(offset) & 0x7fffffff) % 1_000_000).padStart(6, '0');
}

const stepNow = () => Math.floor(Date.now() / 30_000);

test('set up an authenticator app, sign in with a code from it, and turn it off', async ({ api, browser }) => {
  // It may wait for the app's next code, up to 30 seconds.
  test.slow();
  const email = `${unique('totp')}@hackspace.org`;
  const password = 'TwoFactor123!';
  await api.call('POST', '/users', { email, name: unique('Totp'), password });

  const context = await browser.newContext();
  const page = await context.newPage();
  await signIn(page, email, password);
  await page.goto('/account');
  const section = page.getByRole('region', { name: 'Two-factor sign-in' });
  await section.getByRole('button', { name: 'Set up an authenticator app' }).click();
  await expect(section.getByRole('img', { name: 'QR code to scan into an authenticator app' })).toBeVisible();
  const secret = secretOf((await section.locator('code').textContent()) ?? '');
  const confirmed = stepNow();
  await section.getByLabel('Code from the app').fill(code(secret, confirmed));
  await section.getByRole('button', { name: 'Turn on' }).click();
  await expect(section.getByRole('status')).toHaveText('Two-factor sign-in is on. Signing in now asks for a code from your app.');
  await expect(section).toContainText('Authenticator app, set up');

  // Signed out, the password leads to a code.
  await context.clearCookies();
  await page.goto('/sign-in');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  const codeField = page.getByLabel('Code from your authenticator app');
  await expect(codeField).toBeFocused();
  await codeField.fill('000000' === code(secret, stepNow()) ? '111111' : '000000');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('alert')).toHaveText('That code is not right. Enter the code your app shows now.');
  // The code that turned it on was used already: the next one, or now's.
  const step = Math.max(stepNow(), confirmed + 1);
  await codeField.fill(code(secret, step));
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).not.toHaveURL(/\/sign-in/);

  // Turned off with the password and a code, the password alone signs in.
  // Each code is taken once, and one more step ahead than now's is too far:
  // wait for the step signed in with, if it is still to come.
  await expect.poll(stepNow, { timeout: 35_000 }).toBeGreaterThanOrEqual(step);
  await page.goto('/account');
  await section.getByRole('button', { name: 'Remove the authenticator app' }).click();
  await section.getByLabel('Your password').fill(password);
  await section.getByLabel('A code from the app').fill(code(secret, Math.max(stepNow(), step + 1)));
  await section.getByRole('button', { name: 'Remove', exact: true }).click();
  await expect(section.getByRole('status')).toHaveText('Two-factor sign-in is off. Signing in asks for your password alone.');
  await context.clearCookies();
  await signIn(page, email, password);
  await context.close();
});

test('save the recovery codes, and sign in with one in place of the app', async ({ api, browser }) => {
  const email = `${unique('recovery')}@hackspace.org`;
  const password = 'Recovery123!';
  await api.call('POST', '/users', { email, name: unique('Recovery'), password });

  const context = await browser.newContext();
  const page = await context.newPage();
  await signIn(page, email, password);
  await page.goto('/account');
  const section = page.getByRole('region', { name: 'Two-factor sign-in' });
  await section.getByRole('button', { name: 'Set up an authenticator app' }).click();
  const secret = secretOf((await section.locator('code').textContent()) ?? '');
  await section.getByLabel('Code from the app').fill(code(secret, stepNow()));
  await section.getByRole('button', { name: 'Turn on' }).click();

  // Shown once, to keep.
  await expect(section.getByRole('heading', { name: 'Your recovery codes' })).toBeFocused();
  const codes = await section.getByRole('list', { name: 'Recovery codes' }).getByRole('listitem').allTextContents();
  expect(codes).toHaveLength(10);
  await expect(section.getByRole('link', { name: 'Download' })).toHaveAttribute('download', 'bam-recovery-codes.txt');
  await section.getByRole('button', { name: 'I have saved them' }).click();
  await expect(section).toContainText('10 left');

  await context.clearCookies();
  await page.goto('/sign-in');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.getByRole('button', { name: 'Use a recovery code' }).click();
  await page.getByLabel('Recovery code').fill(codes[0]!.toUpperCase());
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).not.toHaveURL(/\/sign-in/);

  await page.goto('/account');
  await expect(section).toContainText('9 left');
  await context.close();
});

test('a role requiring two-factor sign-in has someone without a factor set one up before anything else', async ({ api, browser }) => {
  const email = `${unique('held')}@hackspace.org`.toLowerCase();
  const password = 'HeldBack123!';
  const { role } = await api.call('POST', '/roles', { name: unique('Lathe'), permissions: ['assets.read'], requires_two_factor: true });
  const { user } = await api.call('POST', '/users', { email, name: unique('Held'), password });
  await api.call('PUT', `/users/${user.id}/roles`, { roles: [{ role_id: role.id, scope_ids: [] }] });

  const context = await browser.newContext();
  const page = await context.newPage();
  await signIn(page, email, password);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Set up two-factor sign-in');
  await expect(page.getByRole('navigation', { name: 'Main' })).toHaveCount(0);
  // Nothing else opens meanwhile.
  expect((await page.request.get('/api/v1/assets')).status()).toBe(403);

  const section = page.getByRole('region', { name: 'Two-factor sign-in' });
  await section.getByRole('button', { name: 'Set up an authenticator app' }).click();
  const secret = secretOf((await section.locator('code').textContent()) ?? '');
  await section.getByLabel('Code from the app').fill(code(secret, stepNow()));
  await section.getByRole('button', { name: 'Turn on' }).click();
  await section.getByRole('button', { name: 'I have saved them' }).click();
  await expect(page.getByRole('navigation', { name: 'Main' })).toBeVisible();
  expect((await page.request.get('/api/v1/assets')).status()).toBe(200);
  await context.close();
});
