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

test("an icon's params are bound to asset fields, and each asset drawn with its values", async ({ signedIn: page, api }) => {
  const name = unique('Servers');
  const { category } = await api.call('POST', '/categories', { name });
  const iconName = unique('server').toLowerCase();
  await api.call('PUT', `/categories/${category.id}/icon`, {
    icon_name: iconName,
    source: `base "${iconName}-unit" { category "compute"; param u 1; footprint 1 1 "u * 0.5"; part "box" { box 0 0 0 1 1 "u * 0.5" } }
icon "${iconName}" extends="${iconName}-unit" { }`,
  });
  const label = unique('Rack units');
  const { field } = await api.call('POST', '/fields', { key: unique('rack_units').replace(/-/g, '_'), label, validation_type: 'number' });
  await api.call('POST', `/categories/${category.id}/fields`, { field_id: field.id, is_required: false });
  const { asset } = await api.call('POST', '/assets', { name: unique('Web'), category_ids: [category.id], field_values: { [field.key]: '2' } });

  await page.goto(`/categories/${category.id}`);
  const params = page.getByRole('region', { name: 'Parameters' });
  // the param it inherits is offered
  await params.getByLabel('u', { exact: true }).selectOption({ label });
  await noViolations(page);
  await params.getByRole('button', { name: 'Save the parameters' }).click();
  await expect(params.getByText('Saved: each asset is drawn with its own.')).toBeVisible();

  await page.goto(`/assets/${asset.asset.id}`);
  await expect(page.locator('[data-icon-params]')).toHaveText('Drawn with u 2, from its fields.');
  // and drawn, as its wrapper
  await expect(page.locator('h1 svg')).toBeVisible();

  // the wrapper is not offered as an icon of one's own
  await page.goto('/icons');
  await page.getByRole('searchbox', { name: 'Search icons' }).fill(iconName);
  await expect(page.getByRole('region', { name: 'Every icon' }).getByRole('img')).toHaveCount(1);

  // unbound, for the rest
  await api.call('PUT', `/categories/${category.id}/icon/params`, { params: {} });
});

test("an icon's text param is bound to the asset's name, and each asset written with its own", async ({ signedIn: page, api }) => {
  const { category } = await api.call('POST', '/categories', { name: unique('Tags') });
  const iconName = unique('tag').toLowerCase();
  const font = unique('mini').toLowerCase();
  // a font of one glyph, W, and a tag that writes its label in it
  await api.call('PUT', `/categories/${category.id}/icon`, {
    icon_name: iconName,
    source: `font "${font}" { em 10; ascent 7; descent 3; kind "stroke"
  glyph "W" advance=6 { path d="M0,7 L1.5,0 L3,4 L4.5,0 L6,7" }
  notdef advance=5 { path d="M0,0 H4 V7 H0 Z" }
}
icon "${iconName}" { category "labels"; footprint 1 1 0.5
  param label (text)"?"
  part "box" { box 0 0 0 1 1 0.5 }
  part "label" { at 0.5 0.5 0.5 { text 0 0 "{label}" font="${font}" size=8 } }
}`,
  });
  const { asset } = await api.call('POST', '/assets', { name: 'WWW', category_ids: [category.id] });

  await page.goto(`/categories/${category.id}`);
  const params = page.getByRole('region', { name: 'Parameters' });
  await params.getByLabel('label text', { exact: true }).selectOption({ label: "The asset's name" });
  await noViolations(page);
  await params.getByRole('button', { name: 'Save the parameters' }).click();
  await expect(params.getByText('Saved: each asset is drawn with its own.')).toBeVisible();

  await page.goto(`/assets/${asset.asset.id}`);
  await expect(page.locator('[data-icon-texts]')).toHaveText('Written with label “WWW”.');
  // three Ws, each a glyph of its own
  await expect(page.locator('h1 svg path[d="M0,7 L1.5,0 L3,4 L4.5,0 L6,7"]')).toHaveCount(3);

  await api.call('PUT', `/categories/${category.id}/icon/params`, { params: {} });
});

test('a category is drawn a label icon, previewed as it prints, and its assets’ labels print it', async ({ signedIn: page, api }) => {
  const name = unique('Totes');
  const { category } = await api.call('POST', '/categories', { name, color_hex: '#0f766e' });
  const iconName = unique('tote').toLowerCase();
  await api.call('PUT', `/categories/${category.id}/icon`, { icon_name: iconName, source: `icon "${iconName}" { category "storage"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }` });
  const { asset } = await api.call('POST', '/assets', { name: unique('Tote'), category_ids: [category.id] });
  const { template } = await api.call('POST', '/templates', {
    name: unique('Tote label'), units: 'mm', width: 20, height: 20, dpi: 203, layering: 'list',
    elements: [{ type: 'icon', x: 0, y: 0, width: 20, height: 20, icon: '{{asset.icon}}' }],
  });
  const printed = async () => (await page.request.get(`/api/v1/assets/${asset.asset.id}/label?format=png&template_id=${template.id}`)).body();
  const before = await printed();

  await page.goto(`/categories/${category.id}`);
  const section = iconSection(page);
  await expect(section.getByRole('tab', { name: 'On screen' })).toHaveAttribute('aria-selected', 'true');
  await section.getByRole('tab', { name: 'On labels' }).click();
  await expect(section.getByText(`No label icon here or above, so labels print its screen icon, ${iconName}.`)).toBeVisible();

  // drawn from the screen icon, named for it, and previewed as it prints
  await section.getByRole('button', { name: 'Extend its screen icon' }).click();
  await expect(section.getByLabel('Its name')).toHaveValue(`${iconName}-label`);
  const source = page.getByRole('textbox', { name: `${name}'s label icon source` });
  await source.fill(`icon "${iconName}-label" { category "storage"; footprint 1 1 1; part "lid" { tile 0 0 1 1 1 } }`);
  await expect(section.getByRole('img', { name: `${iconName}-label as it prints in black and white on a 12 mm label: shaded, and as an outline` })).toBeVisible();
  await expect(section.getByText('Shaded, and as an outline, at 203 dots an inch, enlarged.')).toBeVisible();
  await noViolations(page);
  await section.getByRole('button', { name: 'Save' }).click();
  await expect(section.getByText('Published.')).toBeVisible();
  await section.getByRole('button', { name: 'Close' }).click();
  await expect(section.getByText(`Its own label icon, ${iconName}-label.`)).toBeVisible();

  // the screen icon is as it was, and labels print the label icon
  await section.getByRole('tab', { name: 'On screen' }).click();
  await expect(section.getByText(`Its own icon, ${iconName}.`)).toBeVisible();
  expect((await printed()).equals(before)).toBe(false);
  const { asset: read } = await api.call('GET', `/assets/${asset.asset.id}`);
  expect(read.resolved_icons.label.name).toBe(`${iconName}-label`);
  expect(read.resolved_icon.name).toBe(iconName);
});
