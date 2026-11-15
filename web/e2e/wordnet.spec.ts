import AxeBuilder from '@axe-core/playwright';
import { expect, goTo, test } from './fixtures';

// WordNet can't be downloaded here, so this follows what happens without
// it: an address that answers nothing, and a file that isn't WordNet, are
// both refused, and nothing is kept. Seeding itself is tested in Go, with
// a small database in WordNet's format.
test("seeding from WordNet refuses what isn't WordNet, and shows its license", async ({ signedIn: page, api }) => {
  try {
    await goTo(page, 'Search terms');
    const section = page.getByRole('region', { name: 'Seed from WordNet' });
    await expect(section).toBeVisible();
    await expect(section.getByTestId('wordnet-kept')).toContainText('seeding downloads it once (16 MB) from https://wordnetcode.princeton.edu/wn3.1.dict.tar.gz');

    // An admin points it at an address that answers nothing.
    await section.getByLabel('Download it from').fill('http://127.0.0.1:9/wn3.1.dict.tar.gz');
    await section.getByRole('button', { name: 'Save address' }).click();
    await expect(page.getByRole('status').filter({ hasText: 'WordNet is downloaded from http://127.0.0.1:9/wn3.1.dict.tar.gz.' })).toBeVisible();
    await expect(section.getByTestId('wordnet-kept')).toContainText('from http://127.0.0.1:9/wn3.1.dict.tar.gz');
    await section.getByLabel('Meanings of each word').selectOption({ label: 'The most used' });
    await section.getByRole('button', { name: 'Seed from WordNet' }).click();
    await expect(page.getByRole('alert')).toContainText("WordNet couldn't be downloaded from http://127.0.0.1:9/wn3.1.dict.tar.gz");

    // A file that isn't WordNet is refused, and nothing is kept.
    await section.getByLabel('Or upload it').setInputFiles({ name: 'wn3.1.dict.tar.gz', mimeType: 'application/gzip', buffer: Buffer.from('not WordNet') });
    await section.getByRole('button', { name: 'Upload WordNet' }).click();
    await expect(page.getByRole('alert')).toContainText("That file isn't WordNet 3.1 as published");
    await expect(section.getByTestId('wordnet-kept')).toContainText('Not on this server yet');

    // Its license goes with it.
    await section.getByText("WordNet's license").click();
    await expect(section.getByText(/WordNet 3\.1 Copyright 2011 by Princeton University/)).toBeVisible();
    const whole = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
    expect(whole.violations.map((v) => v.id)).toEqual([]);
  } finally {
    // The database is shared: put Princeton's address back.
    await api.call('PUT', '/terms/wordnet', { source_url: '' });
  }
});
