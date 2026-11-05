import type { Page } from '@playwright/test';
import { expect, signIn, test, unique } from './fixtures';

// Passkeys (docs/access.md#passkeys): one added on the account page, then
// asked for after the password, through Chromium's virtual authenticator.

/** A security key for page's browser, touched whenever it is asked. */
async function securityKey(page: Page) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('WebAuthn.enable');
  const { authenticatorId } = await cdp.send('WebAuthn.addVirtualAuthenticator', {
    options: { protocol: 'ctap2', transport: 'usb', hasResidentKey: false, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true },
  });
  return { credentials: async () => (await cdp.send('WebAuthn.getCredentials', { authenticatorId })).credentials };
}

test('add a passkey, sign in with it, and remove it', async ({ api, browser }) => {
  const email = `${unique('passkey')}@hackspace.org`;
  const password = 'Passkey123!';
  await api.call('POST', '/users', { email, name: unique('Passkey'), password });

  const context = await browser.newContext();
  const page = await context.newPage();
  const key = await securityKey(page);
  await signIn(page, email, password);
  await page.goto('/account');
  const section = page.getByRole('region', { name: 'Two-factor sign-in' });
  await section.getByRole('button', { name: 'Add a passkey' }).click();
  await section.getByLabel('Name this passkey').fill('YubiKey');
  await section.getByRole('button', { name: 'Add the passkey' }).click();
  // The first factor comes with recovery codes.
  await expect(section.getByRole('heading', { name: 'Your recovery codes' })).toBeFocused();
  await section.getByRole('button', { name: 'I have saved them' }).click();
  await expect(section.getByRole('list', { name: 'Second factors' })).toContainText('Passkey “YubiKey”, added');
  expect(await key.credentials()).toHaveLength(1);

  // Signed out, the password leads to the passkey, and no code.
  await context.clearCookies();
  await page.goto('/sign-in');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: 'Use your passkey' })).toBeVisible();
  await expect(page.getByLabel('Code from your authenticator app')).toHaveCount(0);
  await page.getByRole('button', { name: 'Use a passkey' }).click();
  await expect(page).not.toHaveURL(/\/sign-in/);
  expect((await key.credentials())[0]!.signCount).toBeGreaterThan(0);

  // Removed with the password and the passkey, the password alone signs in.
  await page.goto('/account');
  await section.getByRole('button', { name: 'Remove passkey YubiKey' }).click();
  await section.getByLabel('Your password').fill(password);
  await section.getByRole('button', { name: 'Remove with a passkey' }).click();
  await expect(section.getByRole('status')).toHaveText('Two-factor sign-in is off. Signing in asks for your password alone.');
  await context.clearCookies();
  await signIn(page, email, password);
  await context.close();
});
