import AxeBuilder from '@axe-core/playwright';
import type { Page } from '@playwright/test';
import { ADMIN_EMAIL, ADMIN_PASSWORD, expect, test, unique } from './fixtures';
import { seedViews } from './views';

// Automated WCAG 2.1 A and AA checks on every view, in light and dark mode,
// and keyboard-only walks through the main flows.

async function audit(page: Page, label: string): Promise<void> {
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  const summary = results.violations.map((v) => `${v.id}: ${v.help} (${v.nodes.map((n) => n.target.join(' ')).join('; ')})`);
  expect(summary, `${label} has accessibility violations`).toEqual([]);
}

for (const scheme of ['light', 'dark'] as const) {
  test(`every view passes axe in ${scheme} mode`, async ({ signedIn: page }) => {
    // It audits every view in turn, so it takes longer than most tests.
    test.setTimeout(120_000);
    await page.emulateMedia({ colorScheme: scheme });
    const views = await seedViews(page);
    for (const [path, heading] of views) {
      await page.goto(path);
      if (heading) await expect(page.getByRole('heading', { level: 1 })).toHaveText(heading);
      else await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
      await page.waitForLoadState('networkidle');
      await audit(page, `${path} (${scheme})`);
    }

    // The spool's other views are tabs of one page.
    for (const tab of ['Jobs', 'Printed']) {
      await page.goto('/spool');
      await page.getByRole('button', { name: tab }).click();
      await page.waitForLoadState('networkidle');
      await audit(page, `/spool ${tab} (${scheme})`);
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
  await expect(page.getByLabel('Name', { exact: true })).toBeVisible();
  for (let i = 0; i < 5 && !(await page.getByLabel('Name', { exact: true }).evaluate((el) => el === document.activeElement)); i++) {
    await page.keyboard.press('Tab');
  }
  await expect(page.getByLabel('Name', { exact: true })).toBeFocused();
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

test('keyboard only: a section opens its first page, with a row of its pages', async ({ signedIn: page }) => {
  await page.goto('/');
  const nav = page.getByRole('navigation', { name: 'Main' });
  const admin = nav.getByRole('link', { name: 'Admin', exact: true });
  // No row on a page outside the sections.
  await expect(page.getByRole('navigation', { name: 'Admin' })).toHaveCount(0);
  await admin.focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Users');
  // The section is marked, and its row lists its pages with this one current.
  await expect(admin).toHaveAttribute('aria-current', 'true');
  const row = page.getByRole('navigation', { name: 'Admin' });
  await expect(row.getByRole('link')).toHaveText(['Users', 'Roles', 'API tokens', 'Notifications', 'Backups', 'Import & export']);
  await expect(row.getByRole('link', { name: 'Users' })).toHaveAttribute('aria-current', 'page');

  // Its links take Tab in order, and lead to their pages.
  await row.getByRole('link', { name: 'Users' }).focus();
  await page.keyboard.press('Tab');
  const roles = row.getByRole('link', { name: 'Roles' });
  await expect(roles).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Roles');
  await expect(page.getByRole('navigation', { name: 'Admin' }).getByRole('link', { name: 'Roles' })).toHaveAttribute('aria-current', 'page');
  await expect(admin).toHaveAttribute('aria-current', 'true');

  // Another section's page shows that section's row instead.
  await nav.getByRole('link', { name: 'Catalog', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Categories');
  await expect(page.getByRole('navigation', { name: 'Admin' })).toHaveCount(0);
  await expect(page.getByRole('navigation', { name: 'Catalog' }).getByRole('link', { name: 'Categories' })).toHaveAttribute('aria-current', 'page');
});

test('the home page lists the same pages, in the same groups', async ({ signedIn: page }) => {
  await page.goto('/');
  const main = page.locator('#main');
  for (const [group, links] of [
    ['Catalog', ['Categories', 'Fields', 'Link types', 'Actions', 'Icons']],
    ['Labels', ['Templates', 'Images', 'Printers']],
    ['Admin', ['Users', 'Roles', 'API tokens', 'Notifications', 'Backups', 'Import & export']],
  ] as const) {
    const section = main.getByRole('region', { name: group });
    await expect(section.getByRole('link')).toHaveText([...links]);
  }
});
