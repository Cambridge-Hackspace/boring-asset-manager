import { expect, test, unique } from './fixtures';

test('review states: turn them on for a category, confirm, return to draft, verify, and find drafts in the list', async ({ signedIn: page, api }) => {
  const catName = unique('Tools');
  const { category } = await api.call('POST', '/categories', { name: catName });
  const sawName = unique('Band saw');

  // Turned on, with review dates, on the category's Review tab.
  await page.goto(`/categories/${category.id}?tab=review`);
  const form = page.getByRole('form', { name: 'Review settings' });
  await form.getByLabel('Review states').selectOption('on');
  await form.getByLabel('Review every (months)').selectOption({ label: 'Every 12 months' });
  await form.getByRole('button', { name: 'Save review settings' }).click();
  await expect(page.getByRole('status')).toHaveText('Saved the review settings.');
  await expect(page.getByLabel('In effect')).toContainText('Every 12 months');

  // What is added is a draft, and the list finds it by its state.
  const { asset } = await api.call('POST', '/assets', { name: sawName, category_ids: [category.id] });
  expect(asset.review.state).toBe('draft');
  await page.goto('/assets?review=draft');
  await expect(page.getByLabel('Review', { exact: true })).toHaveValue('draft');
  await expect(page.getByRole('link', { name: sawName, exact: true })).toBeVisible();

  // Confirmed on its page, with a date to review it by; whoever confirmed it cannot verify it.
  await page.goto(`/assets/${asset.asset.id}`);
  const review = page.getByRole('region', { name: 'Review' });
  await expect(review.getByText('Draft', { exact: true })).toBeVisible();
  await review.getByRole('button', { name: 'Confirm', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Confirmed.');
  await expect(review.getByText('Confirmed', { exact: true })).toBeVisible();
  await expect(review).toContainText('Next review by');
  await expect(review.getByRole('button', { name: 'Verify', exact: true })).toBeDisabled();
  await expect(review).toContainText('You confirmed it, so someone else verifies it.');

  // Returned to draft, saying why, as its history records.
  await review.getByRole('button', { name: 'Return to draft…' }).click();
  await page.getByLabel('What is wrong with it?').fill('The blade size is wrong');
  await page.getByRole('button', { name: 'Return to draft', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Returned to draft.');
  await expect(page.getByRole('region', { name: 'History' })).toContainText('Returned to draft: “The blade size is wrong”');

  // Where the category lets whoever confirmed it verify it, they can.
  await api.call('PUT', `/categories/${category.id}/review`, { enabled: true, months: 12, on_edit: null, self_verify: true });
  await api.call('POST', `/assets/${asset.asset.id}/review`, { move: 'confirm' });
  await page.reload();
  await review.getByRole('button', { name: 'Verify', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Verified.');
  await expect(review.locator('[data-review-state="verified"]')).toContainText('Verified');

  // Editing it returns it to draft, as the category says by default.
  await api.call('PUT', `/assets/${asset.asset.id}`, { name: sawName + ' 2' });
  await page.reload();
  await expect(review.getByText('Draft', { exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: 'History' })).toContainText('Returned to draft by an edit');
});
