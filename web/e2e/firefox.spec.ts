import { expect, test } from './fixtures';

// Layout that Firefox alone gets wrong: it breaks a squeezed line after a
// slash, where Chromium does not.

test.use({ browserName: 'firefox' });

test("keeps a new flow's /f/ on one line beside its address, at any width", async ({ signedIn: page }) => {
  await page.goto('/flows/new');
  const prefix = page.getByRole('main').getByText('/f/', { exact: true });
  for (const width of [360, 640, 800, 1024, 1280, 1400]) {
    await page.setViewportSize({ width, height: 1000 });
    const oneLine = await prefix.evaluate((el) => el.getBoundingClientRect().height <= parseFloat(getComputedStyle(el).lineHeight));
    expect(oneLine, `/f/ on one line at ${width}px`).toBe(true);
  }
});
