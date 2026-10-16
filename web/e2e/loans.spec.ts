import { expect, signIn, test, unique } from './fixtures';

test('lend a tool; the borrower checks it in, then borrows it themselves', async ({ signedIn: page, api }) => {
  const { category } = await api.call('POST', '/categories', { name: unique('Power tools'), color_hex: '#0ea5e9', is_consumable: false, is_lendable: true });
  const name = unique('Drill');
  const { asset } = await api.call('POST', '/assets', { name, category_ids: [category.id] });
  const { role } = await api.call('POST', '/roles', { name: unique('Members'), description: '', permissions: ['assets.read', 'assets.track', 'assets.borrow'] });
  const email = `${unique('member')}@hackspace.org`;
  const member = unique('Member');
  const { user } = await api.call('POST', '/users', { email, name: member, password: 'MemberPass123!' });
  await api.call('PUT', `/users/${user.id}/roles`, { role_ids: [role.id] });

  // The admin lends it, due back on a day they pick.
  await page.goto(`/assets/${asset.asset.id}`);
  const panel = page.getByRole('region', { name: 'Loan' });
  await expect(panel).toContainText('Available to borrow.');
  await panel.getByLabel('Due back (optional)').fill('2099-01-31');
  await panel.getByLabel('Lend to').selectOption({ label: member });
  await panel.getByRole('button', { name: 'Lend' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Lent' })).toHaveText(`Lent to ${member}.`);
  await expect(panel).toContainText(`Lent to ${member} since`);
  const history = page.getByRole('region', { name: 'History' });
  await expect(history.getByRole('listitem').first()).toContainText(`Lent to ${member}, due back`);
  // A lent asset cannot be archived.
  await page.getByRole('button', { name: 'Archive' }).click();
  await page.getByRole('group', { name: /^Archive this asset/ }).getByRole('button', { name: 'Archive' }).click();
  await expect(page.getByRole('alert')).toContainText('check it in first');

  // The member finds it among their loans and brings it back.
  await page.context().clearCookies();
  await signIn(page, email, 'MemberPass123!');
  await page.getByRole('link', { name: 'Loans' }).first().click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Your loans');
  await page.getByRole('button', { name: `Check in ${name}` }).click();
  await expect(page.getByRole('status')).toHaveText(`Checked in ${name}.`);
  await expect(page.getByRole('row', { name: new RegExp(name) })).toContainText('Returned');

  // Now they borrow it themselves.
  await page.getByRole('link', { name }).click();
  await page.getByRole('region', { name: 'Loan' }).getByRole('button', { name: 'Borrow it myself' }).click();
  await expect(page.getByRole('region', { name: 'Loan' })).toContainText('Lent to you since');
  await expect(history.getByRole('listitem').first()).toContainText(`Lent to ${member}`);
});
