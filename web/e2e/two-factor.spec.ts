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
  await section.getByRole('button', { name: 'Turn off…' }).click();
  await section.getByLabel('Your password').fill(password);
  await section.getByLabel('A code from the app').fill(code(secret, Math.max(stepNow(), step + 1)));
  await section.getByRole('button', { name: 'Turn off', exact: true }).click();
  await expect(section.getByRole('status')).toHaveText('Two-factor sign-in is off. Signing in asks for your password alone.');
  await context.clearCookies();
  await signIn(page, email, password);
  await context.close();
});
