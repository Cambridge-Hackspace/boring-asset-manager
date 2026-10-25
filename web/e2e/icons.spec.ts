import AxeBuilder from '@axe-core/playwright';
import type { Page } from '@playwright/test';
import { expect, test, unique } from './fixtures';

// Category icons (README §3.7): drawn on the category's page, checked as
// typed, published or kept as a draft, inherited, and guarded.

const editor = (page: Page, category: string) => page.getByRole('textbox', { name: `${category}'s icon source` });
const iconSection = (page: Page) => page.getByRole('region', { name: 'Icon' });

async function noViolations(page: Page) {
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations.map((v) => `${v.id}: ${v.help} (${v.nodes.map((n) => n.target.join(' ')).join('; ')})`)).toEqual([]);
}

test('a category is drawn an icon, checked as typed, published, and a draft kept with its problems', async ({ signedIn: page, api }) => {
  const name = unique('Crates');
  const { category } = await api.call('POST', '/categories', { name, color_hex: '#b45309' });
  await page.goto(`/categories/${category.id}`);
  const section = iconSection(page);
  await expect(section.getByText('No icon: it shows none')).toBeVisible();

  await section.getByRole('button', { name: 'Start from a box' }).click();
  await expect(editor(page, name)).toHaveValue(/box 0 0 0 1 1 1/);
  const iconName = await section.getByLabel('Its name').inputValue();
  // checked as typed: the preview comes from the check, in light and dark
  await expect(section.getByRole('img', { name: `${iconName}, light` })).toBeVisible();
  await expect(section.getByRole('img', { name: `${iconName}, dark` })).toBeVisible();
  await expect(section.getByText('No problems')).toBeVisible();
  await noViolations(page);

  await section.getByRole('button', { name: 'Save' }).click();
  await expect(section.getByText('Published.')).toBeVisible();

  // a mistake: underlined, listed, and kept as a draft when saved
  await editor(page, name).fill((await editor(page, name).inputValue()).replace('box 0 0 0 1 1 1', 'bxo 0 0 0 1 1 1'));
  const problem = section.getByRole('button', { name: /^Error, line 5, column 5: unknown .*'bxo'/ });
  await expect(problem).toBeVisible();
  await problem.click();
  await expect(editor(page, name)).toBeFocused();
  // the preview is still the published icon
  await expect(section.getByText('The published icon: this source does not compile yet.')).toBeVisible();
  await section.getByRole('button', { name: 'Save' }).click();
  await expect(section.getByText(/Kept as a draft: 1 problem/)).toBeVisible();

  // leaving with it says it is not saved, until the draft goes
  await section.getByRole('button', { name: 'Discard the draft' }).click();
  await section.getByRole('button', { name: 'Discard the draft' }).click();
  await expect(section.getByText('The draft is discarded.')).toBeVisible();
  await expect(section.getByText(`Its own icon, ${iconName}.`)).toBeVisible();
  await expect(section.getByRole('img', { name: `${name}'s icon` })).toBeVisible();

  // and its history has the version published
  await section.getByRole('button', { name: 'Edit the icon' }).click();
  await section.getByText('History').click();
  await expect(section.getByRole('button', { name: 'Load into the editor' })).toHaveCount(1);
});

test('a subcategory inherits its parent’s icon, extends it, and keeps it from being deleted', async ({ signedIn: page, api }) => {
  const parentName = unique('Storage');
  const { category: parent } = await api.call('POST', '/categories', { name: parentName });
  const childName = unique('Small storage');
  const { category: child } = await api.call('POST', '/categories', { name: childName, parent_id: parent.id });
  const parentIcon = unique('storage').toLowerCase();
  await api.call('PUT', `/categories/${parent.id}/icon`, {
    icon_name: parentIcon,
    source: `icon "${parentIcon}" { category "storage"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }`,
  });

  await page.goto(`/categories/${child.id}`);
  const section = iconSection(page);
  await expect(section.getByText(`The icon of ${parentName}, which it inherits.`)).toBeVisible();
  await section.getByRole('button', { name: `Extend ${parentName}'s icon` }).click();
  await expect(editor(page, childName)).toHaveValue(new RegExp(`extends "${parentIcon}"`));
  await section.getByRole('button', { name: 'Save' }).click();
  await expect(section.getByText('Published.')).toBeVisible();

  // the parent's page shows it among its subcategories
  await page.goto(`/categories/${parent.id}`);
  await section.getByRole('button', { name: 'Edit the icon' }).click();
  await expect(section.getByRole('complementary', { name: 'Preview' }).getByRole('link', { name: childName })).toBeVisible();

  // and will not lose the icon the child extends
  await section.getByRole('button', { name: 'Delete the icon' }).click();
  await section.getByRole('button', { name: 'Delete the icon' }).click();
  const alert = section.getByRole('alert');
  await expect(alert).toContainText('The icons would not compile after this change');
  await expect(alert.getByRole('link', { name: childName })).toBeVisible();

  // nor can the category itself be deleted
  const res = await page.request.delete(`/api/v1/categories/${parent.id}`);
  expect(res.status()).toBe(409);
});
