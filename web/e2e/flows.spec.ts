import AxeBuilder from '@axe-core/playwright';
import type { Page } from '@playwright/test';
import { expect, test, unique } from './fixtures';

// Flows (docs/flows.md), run as they are meant to be: on a phone.

test.use({ viewport: { width: 390, height: 844 } });

async function audit(page: Page, label: string): Promise<void> {
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations.map((v) => `${v.id}: ${v.help}`), `${label} has accessibility violations`).toEqual([]);
}

test('flows: leave wood in one of two kinds, long boards asked more, its fields set from the answers, then collect it by its code', async ({
  signedIn: page,
  api,
}) => {
  const { category: wood } = await api.call('POST', '/categories', { name: unique('Wood'), color_hex: '#92400e' });
  const short = unique('Short boards');
  const long = unique('Long boards');
  const { category: shortCat } = await api.call('POST', '/categories', { name: short, parent_id: wood.id, color_hex: '#15803d' });
  const { category: longCat } = await api.call('POST', '/categories', { name: long, parent_id: wood.id, color_hex: '#1d4ed8' });
  const ownerKey = `owner_${Date.now()}`;
  const untilKey = `until_${Date.now()}`;
  const { field: owner } = await api.call('POST', '/fields', { key: ownerKey, label: 'Owner', validation_type: 'text' });
  const { field: until } = await api.call('POST', '/fields', { key: untilKey, label: 'Until', validation_type: 'date' });
  await api.call('POST', `/categories/${wood.id}/fields`, { field_id: owner.id, is_required: true });
  await api.call('POST', `/categories/${longCat.id}/fields`, { field_id: until.id, is_required: false });

  const { starters } = await api.call('GET', `/flow-starters?category_ids=${shortCat.id},${longCat.id}`);
  const leave = starters[0].definition;
  // The starter asks every field the categories record, and its Set fields
  // step sets each from its answer (#174). Long boards' date moves to a
  // form of its own, asked only of them.
  const about = leave.steps[1];
  about.questions = about.questions.filter((q: { key: string }) => q.key !== untilKey);
  about.questions.find((q: { key: string }) => q.key === ownerKey).default = '{user}';
  leave.steps.splice(2, 0, {
    id: 'long_only',
    kind: 'form',
    title: 'Until when?',
    when: { value: 'category_id', op: 'is', to: longCat.id },
    questions: [{ key: untilKey, label: 'Leave until', type: 'date', help: "When you'll take it home" }],
  });
  expect(leave.steps.map((s: { kind: string }) => s.kind)).toEqual(['choose_category', 'form', 'form', 'set_fields', 'create', 'label', 'done']);
  const leaveSlug = unique('leave').toLowerCase();
  const { flow: leaveFlow } = await api.call('POST', '/flows', { slug: leaveSlug, name: 'Leave something', definition: leave });
  const collect = starters[1].definition;
  collect.steps[0].filed_by = leaveFlow.id;
  const collectSlug = unique('collect').toLowerCase();
  await api.call('POST', '/flows', { slug: collectSlug, name: 'Collect it', definition: collect });

  // Leaving it: short boards are asked what it is and whose; long ones,
  // until when too, on a page of its own.
  await page.goto(`/f/${leaveSlug}`);
  await expect(page.getByRole('heading', { name: 'What are you leaving?' })).toBeFocused();
  // How far along: choosing, then the form; setting fields and filing ask
  // nothing (#166).
  await expect(page.getByText(/· Step 1 of 2$/)).toBeVisible();
  await audit(page, 'choosing a category');
  await page.getByRole('button', { name: short }).click();
  await expect(page.getByRole('heading', { name: 'About it' })).toBeVisible();
  await expect(page.getByText(/· Step 2 of 2$/)).toBeVisible();
  await expect(page.getByLabel(/Owner/)).toHaveValue(/.+/);
  await page.getByRole('button', { name: 'Start again' }).click();
  await page.getByRole('button', { name: long }).click();
  await expect(page.getByText(/· Step 2 of 3$/)).toBeVisible();
  await audit(page, 'the form');
  const name = unique('Oak');
  await page.getByLabel('What is it?').fill(name);
  await page.getByRole('button', { name: 'Next' }).click();
  await expect(page.getByRole('heading', { name: 'Until when?' })).toBeVisible();
  await expect(page.getByText(/· Step 3 of 3$/)).toBeVisible();
  await page.getByLabel('Leave until').fill('2026-11-06');
  await page.getByRole('button', { name: 'Next' }).click();
  await expect(page.getByRole('heading', { name: 'Filed' })).toBeVisible();
  await expect(page.getByText(`Filed ${name} in ${long}.`)).toBeVisible();
  await audit(page, 'done');
  const [scroll, width] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
  expect(scroll, 'the flow scrolls sideways').toBeLessThanOrEqual(width);

  // Filed with the fields Set fields gave it.
  const { assets } = await api.call('GET', `/assets?search=${encodeURIComponent(name)}`);
  const { asset: filed } = await api.call('GET', `/assets/${assets[0].asset.id}`);
  expect(filed.field_values[untilKey]).toBe('2026-11-06');
  expect(filed.field_values[ownerKey]).toBeTruthy();
  // Give it a code, as a label would carry.
  const code = unique('WOOD').toUpperCase();
  await api.call('PUT', `/assets/${assets[0].asset.id}`, { generator_keys: [{ generator_type: 'code128', assigned_code: code }] });

  // Collecting it, by its code, typed as a scanner types it.
  await page.goto(`/f/${collectSlug}`);
  await expect(page.getByRole('heading', { name: 'What are you collecting?' })).toBeVisible();
  await audit(page, 'finding');
  await page.getByLabel('Its code').fill(code);
  await page.getByLabel('Its code').press('Enter');
  await expect(page.getByRole('heading', { name: 'Collect it?' })).toBeVisible();
  await expect(page.getByText(`${name} will be taken off the list.`)).toBeVisible();
  await audit(page, 'confirming');
  await page.getByRole('button', { name: 'Yes, go ahead' }).click();
  await expect(page.getByRole('heading', { name: 'Collected' })).toBeVisible();
  await expect(page.getByText(`Archived ${name}.`)).toBeVisible();
  const { asset } = await api.call('GET', `/assets/${assets[0].asset.id}`);
  expect(asset.asset.archived_at).toBeTruthy();
});

test.describe('the flow editor', () => {
  test.use({ viewport: { width: 1400, height: 1000 } });

  test('make a flow from a starter, adjust its form, save it, print its poster, and run it', async ({ signedIn: page, api }) => {
    const shelfName = unique('Shelf');
    await api.call('POST', '/categories', { name: shelfName, color_hex: '#7c3aed' });
    const flowName = unique('Drop off');

    await page.goto('/flows');
    await page.getByRole('link', { name: 'New flow' }).click();
    await page.getByLabel('Name', { exact: true }).first().fill(flowName);
    // Chosen in the picker, found by name among however many there are.
    await page.getByRole('button', { name: 'Choose categories' }).click();
    await page.getByLabel('Find a category').fill(shelfName);
    await page.getByRole('checkbox', { name: shelfName }).check();
    const picker = page.getByRole('group', { name: 'Files into, or takes from' });
    await picker.getByRole('button', { name: 'Done' }).click();
    await expect(picker.getByRole('button', { name: `Remove ${shelfName}` })).toBeVisible();
    await page.getByLabel('Start from').selectOption('leave');
    await page.getByRole('button', { name: 'Use this starter' }).click();
    // One category: nothing to choose, so it starts with its form, which
    // asks what it is, as {name}.
    const steps = page.getByRole('list', { name: 'Steps' });
    await expect(steps.getByRole('heading').first()).toHaveText('1. Form: About it');
    await expect(steps.getByText('Used as {name}')).toBeVisible();
    await steps.getByRole('button', { name: 'Add a question' }).click();
    // The question first; its key is made from it (#171).
    await steps.getByRole('textbox', { name: 'Question 2', exact: true }).fill('Anything else?');
    await expect(steps.getByText('Used as {anything_else}')).toBeVisible();
    await steps.getByLabel('Type of question 2').selectOption('long_text');
    await expect(page.getByRole('figure', { name: 'Preview, as a phone shows it' })).toContainText('Anything else?');
    // Create's name suggests, as { is typed, the values set before it (#169).
    await steps.getByRole('button', { name: /^\d+\. Create$/ }).click();
    const named = steps.getByRole('combobox', { name: 'Its name' });
    await expect(named).toHaveValue('{name}');
    await named.click();
    await named.press('End');
    await named.pressSequentially(', {any');
    const suggested = steps.getByRole('listbox', { name: 'Values Its name can use' });
    await expect(suggested.getByRole('option')).toHaveCount(1);
    await expect(suggested.getByRole('option')).toContainText('{anything_else}');
    await expect(suggested.getByRole('option')).toContainText('Form: About it');
    await named.press('Enter');
    await expect(named).toHaveValue('{name}, {anything_else}');
    await expect(suggested).toBeHidden();
    // Add a step is a menu of the kinds, each saying what it does (#172).
    const addStep = page.getByRole('button', { name: 'Add a step' });
    await addStep.click();
    const kinds = page.getByRole('menu', { name: 'Add a step' });
    await expect(kinds.getByRole('menuitem', { name: 'Choose category' })).toBeFocused();
    await expect(kinds.getByRole('menuitem', { name: 'Change' })).toHaveAccessibleDescription(/sets its condition/);
    await page.keyboard.press('End');
    await expect(kinds.getByRole('menuitem', { name: 'Confirm' })).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(kinds).toBeHidden();
    await expect(addStep).toBeFocused();
    const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
    expect(results.violations.map((v) => `${v.id}: ${v.help}`), 'the editor').toEqual([]);
    await page.getByRole('button', { name: 'Save flow' }).click();
    await expect(page.getByRole('status')).toHaveText(`Saved ${flowName}.`);

    await page.getByRole('link', { name: 'Poster' }).click();
    await expect(page.getByRole('heading', { name: flowName })).toBeVisible();
    await expect(page.getByRole('img', { name: `QR code opening ${flowName}` })).toBeVisible();

    await page.goto('/flows');
    await page.getByRole('link', { name: `Run ${flowName}` }).click();
    await page.getByLabel('What is it?').fill(unique('Parcel'));
    await page.getByLabel('Anything else?').fill('fragile');
    await page.getByRole('button', { name: 'Next' }).click();
    await expect(page.getByRole('heading', { name: 'Filed' })).toBeVisible();
  });
});

test('flows: scanning what was left offers Collect it, which starts with it found', async ({ signedIn: page, api }) => {
  const { category } = await api.call('POST', '/categories', { name: unique('Bench'), color_hex: '#0f766e' });
  const { starters } = await api.call('GET', `/flow-starters?category_ids=${category.id}`);
  const leaveSlug = unique('bench-leave').toLowerCase();
  const { flow: leave } = await api.call('POST', '/flows', { slug: leaveSlug, name: 'Leave on the bench', definition: starters[0].definition });
  const collect = starters[1].definition;
  collect.steps[0].filed_by = leave.id;
  await api.call('POST', '/flows', { slug: unique('bench-collect').toLowerCase(), name: 'Collect from the bench', definition: collect });

  const name = unique('Project');
  const { run } = await api.call('POST', `/flows/${leaveSlug}/runs`);
  await api.call('POST', `/flows/${leaveSlug}/runs/${run.id}`, { step: 'about', answers: { name } });
  const { assets } = await api.call('GET', `/assets?search=${encodeURIComponent(name)}`);

  await page.goto(`/scan/${assets[0].asset.id}`);
  await page.getByRole('link', { name: 'Collect from the bench' }).click();
  await expect(page.getByRole('heading', { name: 'Collect it?' })).toBeVisible();
  // Found for them, so its one question is all there is: nothing to count.
  await expect(page.getByText(/Step \d+ of/)).toHaveCount(0);
  await page.getByRole('button', { name: 'Yes, go ahead' }).click();
  await expect(page.getByText(`Archived ${name}.`)).toBeVisible();
});

test('flows: add a tool to a kit, moving it on to one not listed yet, then link it to several things, with Add another and Done (#264)', async ({
  signedIn: page,
  api,
}) => {
  const { category: tools } = await api.call('POST', '/categories', { name: unique('Tools'), color_hex: '#b45309' });
  const kitsName = unique('Kits');
  const { category: kits } = await api.call('POST', '/categories', { name: kitsName, color_hex: '#4338ca' });
  const drill = unique('Drill');
  const kitA = unique('Kit A');
  const kitB = unique('Kit B');
  const kitC = unique('Kit C');
  await api.call('POST', '/assets', { name: drill, category_ids: [tools.id] });
  await api.call('POST', '/assets', { name: kitA, category_ids: [kits.id] });
  await api.call('POST', '/assets', { name: kitB, category_ids: [kits.id] });

  // Add to a kit, finding kits among the kits, and adding one not listed.
  const { starters } = await api.call('GET', `/flow-starters?category_ids=${tools.id}`);
  const kit = starters[2].definition;
  expect(kit.steps.map((s: { kind: string }) => s.kind)).toEqual(['find', 'link', 'done']);
  kit.steps[0].find_by = ['list'];
  Object.assign(kit.steps[1], { find: { find_by: ['list'], categories: [kits.id] }, allow_create: true, create_category: kits.id });
  const kitSlug = unique('kit').toLowerCase();
  await api.call('POST', '/flows', { slug: kitSlug, name: 'Add to a kit', definition: kit });

  await page.goto(`/f/${kitSlug}`);
  await expect(page.getByRole('heading', { name: 'What are you adding?' })).toBeVisible();
  await page.getByRole('button', { name: drill }).click();
  await expect(page.getByRole('heading', { name: 'Which kit does it go in?' })).toBeFocused();
  await expect(page.getByText(/· Step 2 of 2$/)).toBeVisible();
  // Required, it cannot be skipped.
  await expect(page.getByRole('button', { name: 'Skip' })).toHaveCount(0);
  await audit(page, 'linking');
  await page.getByRole('button', { name: kitA }).click();
  await expect(page.getByRole('heading', { name: 'Added' })).toBeVisible();
  await expect(page.getByText(`${drill} is in ${kitA}.`)).toBeVisible();
  await expect(page.getByText(`Linked ${drill}: contained in ${kitA}.`)).toBeVisible();

  // Again, into a kit not listed yet: it moves out of the first, and says so.
  await page.getByRole('button', { name: 'Start again' }).click();
  await page.getByRole('button', { name: drill }).click();
  await expect(page.getByText(`Linking it moves it out of ${kitA}.`)).toBeVisible();
  await page.getByLabel(`Not listed? Add it to ${kitsName}`).fill(kitC);
  await page.getByRole('button', { name: 'Add it' }).click();
  await expect(page.getByText(`Filed ${kitC} in ${kitsName}.`)).toBeVisible();
  await expect(page.getByText(`Linked ${drill}: contained in ${kitC}. Moved ${drill} out of ${kitA}.`)).toBeVisible();
  const { assets } = await api.call('GET', `/assets?search=${encodeURIComponent(drill)}`);
  const { asset } = await api.call('GET', `/assets/${assets[0].asset.id}`);
  expect(asset.location.map((p: { name: string }) => p.name)).toContain(kitC);

  // Depends on any number of things: Add another, then Done.
  const { relation_types: types } = await api.call('GET', '/relation-types');
  const dependsOn = types.find((t: { name: string }) => t.name === 'depends on');
  const needsSlug = unique('needs').toLowerCase();
  await api.call('POST', '/flows', {
    slug: needsSlug,
    name: 'What it needs',
    definition: {
      version: 2,
      category_ids: [tools.id],
      steps: [
        { id: 'find', kind: 'find', title: 'Which tool?', find_by: ['list'] },
        { id: 'link', kind: 'link', title: 'What does it need?', relation_type: dependsOn.id, outgoing: true, repeat: true, find: { find_by: ['list'], categories: [kits.id] } },
        { id: 'done', kind: 'done', title: 'Linked', text: '{asset} needs {linked}.' },
      ],
    },
  });
  await page.goto(`/f/${needsSlug}`);
  await page.getByRole('button', { name: drill }).click();
  await expect(page.getByRole('heading', { name: 'What does it need?' })).toBeVisible();
  await page.getByRole('button', { name: kitA }).click();
  const linked = page.getByRole('list', { name: 'Linked so far' });
  await expect(linked).toContainText(`depends on ${kitA}`);
  await audit(page, 'linking another');
  await page.getByRole('button', { name: 'Add another' }).click();
  await page.getByRole('button', { name: kitB }).click();
  await expect(linked.getByRole('listitem')).toHaveCount(2);
  await page.getByRole('button', { name: 'Done' }).click();
  await expect(page.getByText(`${drill} needs ${kitA}, ${kitB}.`)).toBeVisible();
});
