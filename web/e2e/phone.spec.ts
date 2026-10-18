import AxeBuilder from '@axe-core/playwright';
import type { Page } from '@playwright/test';
import { expect, test, unique } from './fixtures';

// At a phone's width (390 px): one row of header with a menu, pages that
// fit without scrolling sideways, and the help's article first.

test.use({ viewport: { width: 390, height: 844 } });

async function fitsTheScreen(page: Page, path: string): Promise<void> {
  await page.goto(path);
  await page.waitForLoadState('networkidle');
  const [scroll, width] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
  expect(scroll, `${path} scrolls sideways`).toBeLessThanOrEqual(width);
}

test('the header is one row with a menu, and pages fit the screen', async ({ signedIn: page, api }) => {
  // Something in each list.
  const name = unique('Ladder');
  const { asset } = await api.call('POST', '/assets', { name });
  const { category } = await api.call('POST', '/categories', { name: unique('Lendable'), is_lendable: true });
  const { asset: drill } = await api.call('POST', '/assets', { name: unique('Drill'), category_ids: [category.id] });
  await api.call('POST', `/assets/${drill.asset.id}/checkout`, {});
  await api.call('POST', '/spool', { asset_ids: [asset.asset.id] });

  await page.goto('/');
  const header = page.getByRole('banner');
  expect((await header.boundingBox())!.height).toBeLessThan(80);
  await expect(page.getByRole('navigation', { name: 'Main' })).toHaveCount(0);

  // The menu holds every section, Help and the account; it closes on Escape.
  const menu = page.getByRole('button', { name: /^Menu/ });
  await menu.click();
  const nav = page.getByRole('navigation', { name: 'Main' });
  await expect(nav.getByRole('link', { name: 'Roles' })).toBeVisible();
  await expect(nav.getByRole('link', { name: 'Your account' })).toBeVisible();
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations.map((v) => v.id)).toEqual([]);
  await page.keyboard.press('Escape');
  await expect(nav).toHaveCount(0);
  await expect(menu).toBeFocused();
  await menu.click();
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Users' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Users');
  await expect(page.getByRole('navigation', { name: 'Main' })).toHaveCount(0);

  // Tables become cards, with every action in reach.
  for (const path of ['/users', '/loans', '/assets', '/spool', '/roles', '/printers', '/help', `/assets/${asset.asset.id}`]) {
    await fitsTheScreen(page, path);
  }
  await page.goto('/users');
  await expect(page.getByRole('button', { name: /^Set password for/ }).first()).toBeInViewport();

  // The asset list's filters fold away.
  await page.goto('/assets');
  await expect(page.getByLabel('Condition')).toBeHidden();
  await page.getByRole('button', { name: 'Filters' }).click();
  await page.getByLabel('Condition').selectOption('problem');
  await expect(page.getByRole('button', { name: 'Filters (1)' })).toBeVisible();

  // Help shows the article first; its topics fold away.
  await page.goto('/help/assets');
  await expect(page.getByRole('heading', { level: 1 })).toBeInViewport();
  const topics = page.locator('#help-topics');
  await expect(topics).toBeHidden();
  await page.getByRole('button', { name: 'All help topics' }).click();
  await expect(topics.getByRole('link').first()).toBeVisible();
});

test('a menu near the right edge opens inward', async ({ signedIn: page }) => {
  await page.setViewportSize({ width: 700, height: 900 });
  await page.goto('/');
  await page.getByRole('navigation', { name: 'Main' }).getByRole('button', { name: 'Admin' }).click();
  const list = page.locator('#nav-admin');
  await expect(list).toBeVisible();
  const box = (await list.boundingBox())!;
  expect(box.x + box.width).toBeLessThanOrEqual(700);
});
