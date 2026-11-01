import { randomBytes } from 'node:crypto';
import { createWriteStream } from 'node:fs';
import { readFile } from 'node:fs/promises';
import { expect, goTo, test, unique } from './fixtures';

// Backups from the web (docs/backups.md): one downloaded by the browser, and
// the instance's assets put back from it through the page.
test('download a backup, and replace the assets with it', async ({ signedIn: page, api }, testInfo) => {
  test.setTimeout(120_000);
  const kept = unique('Kept lathe');
  await api.call('POST', '/assets', { name: kept });

  await goTo(page, 'Backups');
  const [download] = await Promise.all([page.waitForEvent('download'), page.getByTestId('download-backup').click()]);
  expect(download.suggestedFilename()).toMatch(/^bam-.*\.bam\.tar\.gz$/);
  const path = testInfo.outputPath('backup.bam.tar.gz');
  await download.saveAs(path);
  const bytes = await readFile(path);
  expect(bytes.subarray(0, 2)).toEqual(Buffer.from([0x1f, 0x8b]));

  // Made since the backup: gone once the assets are replaced.
  const since = unique('Since drill');
  await api.call('POST', '/assets', { name: since });
  const category = unique('Since category');
  await api.call('POST', '/categories', { name: category, color_hex: '#123456' });

  await page.getByTestId('archive-file').setInputFiles(path);
  const checked = page.getByTestId('checked');
  await expect(checked).toContainText('A backup made', { timeout: 60_000 });
  for (const box of await checked.getByRole('checkbox').all()) {
    if ((await box.getAttribute('value')) !== 'assets') await box.uncheck();
  }
  const host = await checked.locator('code').first().textContent();
  const replace = page.getByRole('button', { name: 'Replace' });
  await expect(replace).toBeDisabled();
  await page.getByTestId('confirm').fill(host!);
  await replace.click();
  await expect(page.getByTestId('replaced')).toContainText('Replaced assets', { timeout: 60_000 });

  const has = async (name: string) =>
    ((await api.call('GET', `/assets?search=${encodeURIComponent(name)}`)).assets as { asset: { name: string } }[]).some((a) => a.asset.name === name);
  expect(await has(kept)).toBe(true);
  expect(await has(since)).toBe(false);
  // The catalog was not ticked: what was made since stays.
  const { categories } = await api.call('GET', '/categories');
  expect(categories.map((c: { name: string }) => c.name)).toContain(category);

  // And what it replaced can be put back.
  await page.getByRole('button', { name: 'put it back' }).click();
  await expect.poll(() => has(since), { timeout: 30_000 }).toBe(true);
});

// An upload of several hundred MB, cut off part way, picks up where it
// stopped when its file is chosen again, and arrives whole.
test('a large upload, cut off and resumed', async ({ signedIn: page }, testInfo) => {
  test.setTimeout(240_000);
  const size = 300 * 1024 * 1024;
  const path = testInfo.outputPath('large.bin');
  await new Promise<void>((resolve, reject) => {
    const out = createWriteStream(path);
    out.on('error', reject);
    out.on('finish', resolve);
    for (let left = size; left > 0; left -= 1 << 20) {
      const chunk = randomBytes(Math.min(left, 1 << 20));
      out.write(chunk);
    }
    out.end();
  });

  const starts: number[] = [];
  page.on('request', (req) => {
    const range = req.headers()['content-range'];
    if (req.method() === 'PUT' && range) starts.push(Number(/bytes (\d+)-/.exec(range)![1]));
  });

  await goTo(page, 'Backups');
  await page.getByTestId('archive-file').setInputFiles(path);
  const status = page.getByTestId('upload-status');
  await expect(status).toContainText('Uploading', { timeout: 60_000 });
  await expect.poll(() => starts.length, { timeout: 60_000 }).toBeGreaterThanOrEqual(5);

  // The connection drops: the part fails, is retried, and is given up.
  await page.context().setOffline(true);
  await expect(page.getByRole('alert')).toBeVisible({ timeout: 60_000 });
  await expect(page.getByText(/Unfinished uploads/)).toBeVisible();
  const before = starts.length;

  await page.context().setOffline(false);
  await page.getByTestId('archive-file').setInputFiles(path);
  await expect(status).toHaveText(/Uploaded 315 MB\./, { timeout: 120_000 });
  // It went on from where it stopped, not from the start.
  expect(starts[before]).toBeGreaterThan(0);
  expect(starts.slice(before)).not.toContain(0);

  // The server checked every byte against the hash; it is not an archive,
  // which the check then says, having read it all.
  await expect(page.getByRole('alert')).toContainText('not a valid archive', { timeout: 60_000 });
});
