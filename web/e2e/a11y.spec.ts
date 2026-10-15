import AxeBuilder from '@axe-core/playwright';
import type { Page } from '@playwright/test';
import { ADMIN_EMAIL, ADMIN_PASSWORD, expect, test, unique } from './fixtures';

// Automated WCAG 2.1 A and AA checks on every view, in light and dark mode,
// and keyboard-only walks through the main flows.

async function audit(page: Page, label: string): Promise<void> {
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  const summary = results.violations.map((v) => `${v.id}: ${v.help} (${v.nodes.map((n) => n.target.join(' ')).join('; ')})`);
  expect(summary, `${label} has accessibility violations`).toEqual([]);
}

for (const scheme of ['light', 'dark'] as const) {
  test(`every view passes axe in ${scheme} mode`, async ({ signedIn: page }) => {
    const request = page.context().request;
    const post = async (path: string, body: unknown) => (await (await request.post('/api/v1' + path, { data: body })).json()) as any;
    const cat = await post('/categories', { name: unique('A11y'), color_hex: '#7c3aed', is_consumable: true, is_running_low: true });
    const asset = await post('/assets', { name: unique('Ladder'), category_ids: [cat.category.id] });
    // Counted by quantity and running low, with some history.
    const screws = await post('/assets', { name: unique('Screws'), stock: { quantity: 30, unit: 'pcs', low_stock_threshold: 20 } });
    await post(`/assets/${screws.asset.asset.id}/quantity`, { adjust: -15 });
    const { template } = await post('/templates', {
      name: unique('A11y'), units: 'mm', width: 62, height: 29, dpi: 300,
      elements: [{ type: 'text', x: 2, y: 2, width: 50, height: 10, font_size: 12, content_binding: '{{asset.name}}' }],
    });

    await page.emulateMedia({ colorScheme: scheme });
    const views: [string, string][] = [
      ['/', 'Welcome'],
      ['/assets', 'Assets'],
      ['/assets/new', 'New asset'],
      ['/assets/import', 'Import assets'],
      [`/assets/${asset.asset.asset.id}`, ''],
      [`/assets/${asset.asset.asset.id}/edit`, 'Edit asset'],
      [`/assets/${screws.asset.asset.id}`, ''],
      [`/assets/${screws.asset.asset.id}/edit`, 'Edit asset'],
      ['/categories', 'Categories'],
      [`/categories/${cat.category.id}`, ''],
      ['/categories/new', 'New category'],
      ['/fields', 'Fields'],
      ['/templates', 'Label templates'],
      ['/templates/new', 'New template'],
      [`/templates/${template.id}/edit`, ''],
      ['/images', 'Images'],
      ['/printers', 'Printers'],
      ['/notifications', 'Notifications'],
      ['/users', 'Users'],
      ['/account', 'Your account'],
      ['/no-such-page', 'Page not found'],
    ];
    for (const [path, heading] of views) {
      await page.goto(path);
      if (heading) await expect(page.getByRole('heading', { level: 1 })).toHaveText(heading);
      else await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
      await page.waitForLoadState('networkidle');
      await audit(page, `${path} (${scheme})`);
    }

    await page.context().clearCookies();
    await page.goto('/sign-in');
    await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
    await audit(page, `/sign-in (${scheme})`);
  });
}

test('keyboard only: sign in, find the assets, and create one', async ({ page }) => {
  await page.goto('/sign-in');
  await page.keyboard.press('Tab');
  await expect(page.getByLabel('Email')).toBeFocused();
  await page.keyboard.type(ADMIN_EMAIL);
  await page.keyboard.press('Tab');
  await page.keyboard.type(ADMIN_PASSWORD);
  await page.keyboard.press('Enter');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Welcome');

  // The skip link comes first, then the navigation.
  await page.keyboard.press('Tab');
  await expect(page.getByRole('link', { name: 'Skip to content' })).toBeFocused();
  await page.keyboard.press('Tab');
  await page.keyboard.press('Tab');
  await expect(page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Assets' })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Assets');
  // After navigating, focus moves to the page content.
  await expect(page.locator('#main')).toBeFocused();

  // Tab to "New asset" and fill the form without a mouse.
  for (let i = 0; i < 10 && !(await page.getByRole('link', { name: 'New asset' }).evaluate((el) => el === document.activeElement)); i++) {
    await page.keyboard.press('Tab');
  }
  await page.keyboard.press('Enter');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('New asset');
  // Focus is on the page content: Tab past the breadcrumb to the first field.
  await expect(page.getByLabel('Name')).toBeVisible();
  for (let i = 0; i < 5 && !(await page.getByLabel('Name').evaluate((el) => el === document.activeElement)); i++) {
    await page.keyboard.press('Tab');
  }
  await expect(page.getByLabel('Name')).toBeFocused();
  const name = unique('Keyboard ladder');
  await page.keyboard.type(name);
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(/\/assets\/[0-9a-f-]{36}$/);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(name);
});

test('keyboard only: confirm buttons keep focus where it belongs', async ({ signedIn: page, api }) => {
  await api.call('POST', '/fields', { key: unique('kb').replace(/-/g, '_'), label: unique('Keyboard field'), validation_type: 'text' });
  await page.goto('/fields');
  const del = page.getByRole('button', { name: 'Delete' }).first();
  await del.focus();
  await page.keyboard.press('Enter');
  // Asking moves focus to the confirming button; cancelling returns it.
  await expect(page.getByRole('button', { name: 'Delete' }).first()).toBeFocused();
  await page.keyboard.press('Tab');
  await page.keyboard.press('Enter');
  await expect(page.getByRole('button', { name: 'Delete' }).first()).toBeFocused();
});
