import type { Page } from '@playwright/test';
import { expect, signIn, test } from './fixtures';

// Installing BAM! on a phone from the instance itself (#213,
// docs/installing.md#on-a-phone): the manifest and its icons, the service
// worker that keeps the shell, offline, and updates.

// The service worker runs here, though the other specs block it.
test.use({ serviceWorkers: 'allow' });

const defaults = { name: '', short_name: '', theme_color: '#faf7f0', theme_color_dark: '#0b0b0a', background_color: '#faf7f0', icon_image_id: '' };

/** Waits until the service worker has installed the shell and runs the page. */
async function controlled(page: Page): Promise<void> {
  await page.evaluate(async () => {
    await navigator.serviceWorker.ready;
    if (!navigator.serviceWorker.controller) {
      await new Promise((resolve) => navigator.serviceWorker.addEventListener('controllerchange', resolve, { once: true }));
    }
  });
}

test('the manifest is valid and its icons exist; the worker is served from the root', async ({ page, request }) => {
  await page.goto('/sign-in');
  await expect(page.locator('link[rel="manifest"]')).toHaveAttribute('href', '/manifest.webmanifest');
  await expect(page.locator('link[rel="apple-touch-icon"]')).toHaveAttribute('href', '/app-icons/apple-touch-icon.png');

  const res = await request.get('/manifest.webmanifest');
  expect(res.status()).toBe(200);
  expect(res.headers()['content-type']).toBe('application/manifest+json');
  const manifest = await res.json();
  expect(manifest).toMatchObject({ id: '/', start_url: '/', scope: '/', display: 'standalone', theme_color: '#faf7f0', background_color: '#faf7f0' });
  expect(manifest.name).toBeTruthy();
  expect(manifest.shortcuts.map((s: { name: string; url: string }) => `${s.name} ${s.url}`)).toEqual(['Scan /scan', 'Shopping list /shopping', 'Loans /loans']);
  const sizes = manifest.icons.map((i: { sizes: string; purpose: string }) => `${i.sizes} ${i.purpose}`);
  expect(sizes).toEqual(expect.arrayContaining(['192x192 any', '512x512 any', '192x192 maskable', '512x512 maskable']));
  for (const icon of [...manifest.icons, { src: '/app-icons/apple-touch-icon.png' }]) {
    const got = await request.get(icon.src);
    expect(got.status(), icon.src).toBe(200);
    expect(got.headers()['content-type'], icon.src).toBe('image/png');
    // A PNG, whatever its size.
    expect((await got.body()).subarray(1, 4).toString(), icon.src).toBe('PNG');
  }

  const sw = await request.get('/sw.js');
  expect(sw.status()).toBe(200);
  expect(sw.headers()['content-type']).toContain('javascript');
  expect(sw.headers()['cache-control']).toBe('no-cache');
  expect(sw.headers()['service-worker-allowed']).toBe('/');
  expect(sw.headers()['content-security-policy']).toContain("default-src 'self'");
});

test('the worker registers, and opens the app offline with a page that says so', async ({ signedIn: page }) => {
  await page.goto('/');
  await controlled(page);
  expect(await page.evaluate(async () => (await navigator.serviceWorker.getRegistration())?.scope)).toBe(new URL('/', page.url()).href);

  // Losing the signal on a page already open says so.
  await page.context().setOffline(true);
  await expect(page.getByRole('status').filter({ hasText: "You're offline" })).toBeVisible();

  // Opening a page with none: the kept shell, then a page saying why,
  // rather than the browser's error, and still at that page's address.
  const res = await page.goto('/loans');
  expect(res?.status()).toBe(200);
  await expect(page.getByRole('heading', { name: "You're offline" })).toBeVisible();
  await expect(page).toHaveURL(/\/loans$/);
  // Under the same Content Security Policy as ever.
  expect(res?.headers()['content-security-policy']).toContain("default-src 'self'");

  // It asks to try again, and does so itself once the connection is back.
  await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible();
  await page.context().setOffline(false);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Loans');
});

test('a new release is fetched in the background, then Reload to update takes it', async ({ signedIn: page }) => {
  await page.goto('/');
  await controlled(page);
  await expect(page.getByRole('button', { name: 'Reload to update' })).toHaveCount(0);

  // The next release: another worker script for the same scope, as the
  // browser finds when it checks /sw.js and its bytes differ. (Playwright's
  // routing does not reach a worker's own script, so the test names it anew.)
  await page.evaluate(async () => {
    await navigator.serviceWorker.register('/sw.js?release=next', { scope: '/' });
  });
  const reload = page.getByRole('button', { name: 'Reload to update' });
  await expect(reload).toBeVisible();

  await Promise.all([page.waitForEvent('load'), reload.click()]);
  await expect(page.getByRole('heading', { name: 'Welcome' })).toBeVisible();
  // The page runs on the new release's worker. (The app then registers
  // /sw.js again, which here differs from the test's address, and so is
  // offered as yet another release.)
  expect(await page.evaluate(() => navigator.serviceWorker.controller?.scriptURL)).toContain('release=next');
});

test('Install where the browser allows, and how on an iPhone', async ({ signedIn: page, browser }) => {
  // Chrome's offer, as it makes it on Android, held for the button.
  await page.goto('/');
  await page.evaluate(() => {
    const offer = new Event('beforeinstallprompt', { cancelable: true });
    Object.assign(offer, {
      prompt: async () => ((window as unknown as { prompted: boolean }).prompted = true),
      userChoice: Promise.resolve({ outcome: 'accepted' }),
    });
    window.dispatchEvent(offer);
  });
  await page.getByRole('button', { name: 'Install' }).click();
  expect(await page.evaluate(() => (window as unknown as { prompted?: boolean }).prompted)).toBe(true);
  await expect(page.getByRole('button', { name: 'Install' })).toHaveCount(0);

  const iphone = await browser.newContext({
    userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1',
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
    serviceWorkers: 'block',
  });
  const phone = await iphone.newPage();
  await signIn(phone);
  await expect(phone.getByRole('region', { name: 'Install BAM!' })).toContainText('Add to Home Screen');
  await iphone.close();
});

test("an admin names the app and colors it, and the manifest and page follow", async ({ signedIn: page, api }) => {
  try {
    await page.goto('/installed-app');
    await page.getByLabel('Short name').fill('Stores');
    await page.getByLabel('Bar above the app, light').fill('#d41920');
    await page.getByRole('button', { name: 'Save' }).click();
    await expect(page.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();

    const manifest = await (await page.context().request.get('/manifest.webmanifest')).json();
    expect(manifest.short_name).toBe('Stores');
    expect(manifest.theme_color).toBe('#d41920');
    await page.reload();
    await expect(page.locator('meta[name="theme-color"]').first()).toHaveAttribute('content', '#d41920');
  } finally {
    await api.call('PUT', '/settings/app', defaults);
  }
});
