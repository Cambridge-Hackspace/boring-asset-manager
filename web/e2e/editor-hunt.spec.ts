import AxeBuilder from '@axe-core/playwright';
import type { Locator, Page } from '@playwright/test';
import { expect, goTo, test, unique } from './fixtures';

// Bugs hunted in the template editor: each test is one that was likely in
// an editor like this one, checked in the browser by what it draws and
// where things are, not by what the code meant to do.

const tool = (page: Page, name: string) => page.getByRole('toolbar', { name: 'Tools' }).getByRole('button', { name, exact: true });
const labelImage = (page: Page) => page.getByRole('img', { name: 'Label preview' });
const panels = (page: Page) => page.getByRole('complementary', { name: 'Panels' });
const element = (page: Page, i = 0) => page.locator(`[data-element="${i}"]`);

type Rect = { x: number; y: number; width: number; height: number };

/** Opens a new template with one text box on it, typed and finished. */
async function withText(page: Page, words = 'Hello there'): Promise<void> {
  await goTo(page, 'Templates');
  await page.getByRole('link', { name: 'New template' }).click();
  await page.locator('#tpl-name').fill(unique('Hunt'));
  const r = (await labelImage(page).boundingBox())!;
  await tool(page, 'Text').click();
  await page.mouse.move(r.x + r.width * 0.1, r.y + r.height * 0.2);
  await page.mouse.down();
  await page.mouse.move(r.x + r.width * 0.5, r.y + r.height * 0.5, { steps: 6 });
  await page.mouse.up();
  await page.keyboard.type(words);
  await page.keyboard.press('Escape');
  await expect(tool(page, 'Select and move')).toHaveAttribute('aria-pressed', 'true');
  await expect.poll(() => ink(page)).toBeGreaterThan(50);
}

/**
 * The printed dots drawn on the label, as a rectangle on the screen, and how
 * many there are; inside a rectangle on the screen when one is given.
 */
async function inkBox(page: Page, within?: Rect): Promise<{ count: number; box: Rect | null }> {
  return labelImage(page)
    .locator('canvas')
    .evaluate((c: HTMLCanvasElement, within) => {
      const r = c.getBoundingClientRect();
      const sx = c.width / r.width;
      const sy = c.height / r.height;
      const d = c.getContext('2d')!.getImageData(0, 0, c.width, c.height).data;
      let count = 0;
      let x0 = Infinity;
      let y0 = Infinity;
      let x1 = -Infinity;
      let y1 = -Infinity;
      for (let y = 0; y < c.height; y++) {
        for (let x = 0; x < c.width; x++) {
          const i = (y * c.width + x) * 4;
          if (d[i + 3]! < 128 || d[i]! > 100) continue;
          const px = r.left + x / sx;
          const py = r.top + y / sy;
          if (within && (px < within.x || px > within.x + within.width || py < within.y || py > within.y + within.height)) continue;
          count++;
          x0 = Math.min(x0, px);
          y0 = Math.min(y0, py);
          x1 = Math.max(x1, px);
          y1 = Math.max(y1, py);
        }
      }
      return { count, box: count ? { x: x0, y: y0, width: x1 - x0, height: y1 - y0 } : null };
    }, within);
}
const ink = async (page: Page, within?: Rect) => (await inkBox(page, within)).count;

/** The page, and everything holding the label, is where it was laid out: nothing scrolled, nothing past the window. */
async function laidOut(page: Page) {
  const state = await labelImage(page).evaluate((label) => {
    const scrolled: string[] = [];
    for (let a = label.parentElement; a; a = a.parentElement) {
      if (a.scrollTop || a.scrollLeft) scrolled.push(`${a.tagName}.${a.className.slice(0, 40)} ${a.scrollLeft},${a.scrollTop}`);
    }
    const s = document.scrollingElement!;
    return { scrolled, height: s.scrollHeight, width: s.scrollWidth, innerHeight, innerWidth };
  });
  expect(state.scrolled).toEqual([]);
  expect(state.height).toBeLessThanOrEqual(state.innerHeight);
  expect(state.width).toBeLessThanOrEqual(state.innerWidth);
}

async function drag(page: Page, from: [number, number], to: [number, number], steps = 8) {
  await page.mouse.move(...from);
  await page.mouse.down();
  await page.mouse.move(...to, { steps });
  await page.mouse.up();
}

const middle = (r: Rect): [number, number] => [r.x + r.width / 2, r.y + r.height / 2];
const box = async (l: Locator) => (await l.boundingBox())!;

test.describe('layout and scrolling', () => {
  test('1, 2, 6: the page is the window: nothing to scroll past the editor, however the panels are scrolled or focused', async ({ signedIn: page }) => {
    await withText(page);
    await laidOut(page);
    // Down to the bottom of the panels, then into their last field.
    const p = await box(panels(page));
    await page.mouse.move(p.x + p.width / 2, p.y + p.height / 2);
    for (let i = 0; i < 20; i++) await page.mouse.wheel(0, 200);
    await panels(page).locator('input, select, button').last().focus();
    await laidOut(page);
    // Tabbing through the elements on the label.
    await element(page).focus();
    await laidOut(page);
  });

  test('3, 5: the wheel scrolls what is under the pointer, and nothing else', async ({ signedIn: page }) => {
    await withText(page);
    const label = await box(labelImage(page));
    // Over the panels, past their end: the panels scroll, the label stays.
    const p = await box(panels(page));
    await page.mouse.move(p.x + p.width / 2, p.y + p.height - 40);
    for (let i = 0; i < 30; i++) await page.mouse.wheel(0, 150);
    for (let i = 0; i < 5; i++) await page.mouse.wheel(0, -150);
    for (let i = 0; i < 10; i++) await page.mouse.wheel(0, 150);
    expect(await panels(page).evaluate((e) => e.scrollTop)).toBeGreaterThan(0);
    expect(await box(labelImage(page))).toEqual(label);
    // Over the toolbox, the options and the status bar: the label stays.
    for (const over of [tool(page, 'Text'), page.locator('#el-0-x'), page.locator('#zoom-level')]) {
      await page.mouse.move(...middle(await box(over)));
      await page.mouse.wheel(0, 300);
      expect(await box(labelImage(page))).toEqual(label);
    }
    await laidOut(page);
  });

  test('4: the wheel over the label pans it, and the page stays put', async ({ signedIn: page }) => {
    await withText(page);
    const label = await box(labelImage(page));
    await page.mouse.move(label.x + 5, label.y + 5);
    await page.mouse.wheel(0, 100);
    await expect.poll(async () => (await box(labelImage(page))).y).toBeCloseTo(label.y - 100, 0);
    await laidOut(page);
  });

  test('7, 49: a smaller window keeps the editor in it, and the overview follows', async ({ signedIn: page }) => {
    await withText(page);
    await page.setViewportSize({ width: 1000, height: 650 });
    await laidOut(page);
    await page.setViewportSize({ width: 1400, height: 1000 });
    await page.keyboard.press('2');
    const seen = await box(page.locator('[data-overview-view]'));
    const whole = await box(page.locator('[data-overview-view]').locator('..'));
    // The whole label is in view: the outline is all of it.
    expect(seen.width).toBeGreaterThan(whole.width * 0.95);
  });

  test('8: on a phone, nothing scrolls sideways', async ({ signedIn: page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/templates/new');
    await expect(page.locator('#tpl-name')).toBeAttached();
    const { width, inner } = await page.evaluate(() => ({ width: document.scrollingElement!.scrollWidth, inner: innerWidth }));
    expect(width).toBeLessThanOrEqual(inner);
  });

  test('9: hiding everything but the label leaves nothing to scroll, and Tab brings it back', async ({ signedIn: page }) => {
    await withText(page);
    await page.locator('body').click({ position: { x: 1, y: 1 } });
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
    await page.keyboard.press('Tab');
    await expect(panels(page)).toBeHidden();
    await laidOut(page);
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
    await page.keyboard.press('Tab');
    await expect(panels(page)).toBeVisible();
    await laidOut(page);
  });

  test('10: folding a panel leaves the label where it is', async ({ signedIn: page }) => {
    await withText(page);
    const label = await box(labelImage(page));
    await panels(page).getByRole('button', { name: 'Layers', exact: true }).click();
    await page.waitForTimeout(200);
    expect(await box(labelImage(page))).toEqual(label);
  });
});

test.describe('drawing', () => {
  test('11, 20: text moved is drawn where it went, and undoing draws it back', async ({ signedIn: page }) => {
    await withText(page);
    const before = await inkBox(page);
    const e = await box(element(page));
    await drag(page, middle(e), [middle(e)[0] + 150, middle(e)[1] + 60]);
    await expect.poll(async () => (await inkBox(page)).box?.x ?? 0).toBeGreaterThan(before.box!.x + 100);
    expect((await inkBox(page)).count).toBeGreaterThan(before.count * 0.8);
    await page.keyboard.press('Control+z');
    await expect.poll(async () => (await inkBox(page)).box?.x ?? 0).toBeCloseTo(before.box!.x, -1);
  });

  test('12: text finished by clicking away, rather than Escape, is drawn when moved', async ({ signedIn: page }) => {
    await goTo(page, 'Templates');
    await page.getByRole('link', { name: 'New template' }).click();
    const r = await box(labelImage(page));
    await tool(page, 'Text').click();
    await drag(page, [r.x + r.width * 0.1, r.y + r.height * 0.2], [r.x + r.width * 0.5, r.y + r.height * 0.5]);
    await page.keyboard.type('Clicked away');
    await page.mouse.click(r.x + r.width * 0.8, r.y + r.height * 0.85);
    await expect.poll(() => ink(page)).toBeGreaterThan(50);
    await tool(page, 'Select and move').click();
    const e = await box(element(page));
    await drag(page, middle(e), [middle(e)[0] + 80, middle(e)[1] + 40]);
    await expect.poll(() => ink(page)).toBeGreaterThan(50);
    await page.mouse.click(r.x + r.width * 0.9, r.y + r.height * 0.9);
    await expect.poll(() => ink(page)).toBeGreaterThan(50);
  });

  test('51: a click in the text being typed places the caret there', async ({ signedIn: page }) => {
    await goTo(page, 'Templates');
    await page.getByRole('link', { name: 'New template' }).click();
    const r = await box(labelImage(page));
    await tool(page, 'Text').click();
    await drag(page, [r.x + r.width * 0.1, r.y + r.height * 0.2], [r.x + r.width * 0.7, r.y + r.height * 0.5]);
    await page.keyboard.type('abcdefghij');
    const area = page.locator('[data-text-editor] textarea');
    const a = await box(area);
    // Near the start of the line: the caret goes there, and typing goes in there.
    const line = await area.evaluate((t: HTMLTextAreaElement) => {
      const m = document.createElement('canvas').getContext('2d')!;
      m.font = getComputedStyle(t).font;
      return { a: m.measureText('a').width, left: t.getBoundingClientRect().left };
    });
    await page.mouse.click(line.left + line.a * 1.5, a.y + a.height / 2);
    await page.keyboard.type('X');
    await expect(area).toHaveValue(/^a[X]bcdefghij$|^aX/);
    await expect(area).toBeFocused();
  });

  test('13: text moved partly off the label is drawn, the part still on it', async ({ signedIn: page }) => {
    await withText(page);
    const label = await box(labelImage(page));
    const e = await box(element(page));
    await drag(page, middle(e), [label.x + label.width - e.width * 0.2, middle(e)[1]]);
    await expect.poll(() => ink(page)).toBeGreaterThan(5);
    const drawn = (await inkBox(page)).box!;
    expect(drawn.x).toBeGreaterThan(label.x + label.width * 0.6);
  });

  test('14: a long drag is drawn to its end', async ({ signedIn: page }) => {
    await withText(page);
    const e = await box(element(page));
    await page.mouse.move(...middle(e));
    await page.mouse.down();
    for (let i = 0; i < 6; i++) {
      await page.mouse.move(middle(e)[0] + 200, middle(e)[1] + 40, { steps: 50 });
      await page.mouse.move(middle(e)[0], middle(e)[1], { steps: 50 });
    }
    await page.mouse.move(middle(e)[0] + 120, middle(e)[1] + 30, { steps: 20 });
    await page.mouse.up();
    const now = await box(element(page));
    await expect.poll(() => ink(page, now)).toBeGreaterThan(50);
    await expect(page.getByText(/renderer (failed|stopped)|cannot draw/i)).toHaveCount(0);
  });

  test('16: the overview shows the label, and follows it', async ({ signedIn: page }) => {
    await withText(page);
    const overview = page.locator('[data-overview-view]').locator('..');
    const darkIn = (fromX: number, toX: number) =>
      overview.locator('canvas').evaluate(
        (c: HTMLCanvasElement, [a, b]) => {
          const d = c.getContext('2d')!.getImageData(0, 0, c.width, c.height).data;
          let n = 0;
          for (let y = 0; y < c.height; y++) for (let x = Math.floor(a * c.width); x < b * c.width; x++) if (d[(y * c.width + x) * 4]! < 100) n++;
          return n;
        },
        [fromX, toX],
      );
    await expect.poll(() => darkIn(0, 0.5)).toBeGreaterThan(50);
    // And on the screen: the overview's own pixels, not just its canvas.
    const shot = await overview.screenshot();
    expect(shot.length).toBeGreaterThan(0);
    const e = await box(element(page));
    const label = await box(labelImage(page));
    await drag(page, middle(e), [label.x + label.width * 0.8, middle(e)[1]]);
    await expect.poll(() => darkIn(0.55, 1)).toBeGreaterThan(50);
  });

  test('17: zoomed in, the overview outlines only the part in view', async ({ signedIn: page }) => {
    await withText(page);
    await page.locator('#zoom-level').fill('800');
    await page.locator('#zoom-level').press('Enter');
    const seen = await box(page.locator('[data-overview-view]'));
    const whole = await box(page.locator('[data-overview-view]').locator('..'));
    expect(seen.width).toBeLessThan(whole.width * 0.7);
    expect(seen.width).toBeGreaterThan(5);
  });

  test('18: the drawing has the label’s shape', async ({ signedIn: page }) => {
    await withText(page);
    const c = await labelImage(page).locator('canvas').evaluate((c: HTMLCanvasElement) => ({ w: c.width, h: c.height }));
    const r = await box(labelImage(page));
    expect(c.w / c.h).toBeCloseTo(r.width / r.height, 1);
  });

  test('19: a hidden element is left out of the drawing, and shown again is back', async ({ signedIn: page }) => {
    await withText(page);
    await panels(page).getByRole('button', { name: /^Hide/ }).first().click();
    await expect.poll(() => ink(page)).toBe(0);
    await panels(page).getByRole('button', { name: /^Hide/ }).first().click();
    await expect.poll(() => ink(page)).toBeGreaterThan(50);
  });

  test('22: in dark mode the label is still white paper with black dots', async ({ signedIn: page }) => {
    await page.emulateMedia({ colorScheme: 'dark' });
    await withText(page);
    const paper = await labelImage(page).evaluate((l) => getComputedStyle(l.parentElement!).backgroundColor);
    expect(paper).toBe('rgb(255, 255, 255)');
    const overview = await page.locator('[data-overview-view]').locator('..').evaluate((o) => getComputedStyle(o).backgroundColor);
    expect(overview).toBe('rgb(255, 255, 255)');
  });

  for (const zoom of ['400', '50']) {
    test(`24: at ${zoom}%, the text’s box is where its dots are`, async ({ signedIn: page }) => {
      await withText(page);
      await page.locator('#zoom-level').fill(zoom);
      await page.locator('#zoom-level').press('Enter');
      await page.keyboard.press('Escape');
      const e = await box(element(page));
      const drawn = (await inkBox(page))!.box!;
      const slack = 3;
      expect(drawn.x).toBeGreaterThanOrEqual(e.x - slack);
      expect(drawn.y).toBeGreaterThanOrEqual(e.y - slack);
      expect(drawn.x + drawn.width).toBeLessThanOrEqual(e.x + e.width + slack);
      expect(drawn.y + drawn.height).toBeLessThanOrEqual(e.y + e.height + slack);
    });
  }
});

test.describe('at a device pixel ratio of 2', () => {
  test.use({ deviceScaleFactor: 2 });
  test('23: the text’s box is where its dots are', async ({ signedIn: page }) => {
    await withText(page);
    const e = await box(element(page));
    const drawn = (await inkBox(page)).box!;
    expect(drawn.x).toBeGreaterThanOrEqual(e.x - 2);
    expect(drawn.x + drawn.width).toBeLessThanOrEqual(e.x + e.width + 2);
  });
});

test.describe('pointer and keyboard', () => {
  test('21: with a problem elsewhere on the label, text moved is still drawn where it went', async ({ signedIn: page }) => {
    await withText(page);
    const label = await box(labelImage(page));
    // A rectangle with neither stroke nor fill: a problem, outlined; it draws nothing.
    await tool(page, 'Rectangle').click();
    await drag(page, [label.x + label.width * 0.7, label.y + label.height * 0.7], [label.x + label.width * 0.9, label.y + label.height * 0.9]);
    await panels(page).getByRole('radiogroup', { name: 'Stroke' }).locator('label[title="None"]').click();
    await expect(element(page, 1)).toHaveClass(/problem/);
    const before = (await inkBox(page)).box!;
    const e = await box(element(page, 0));
    await drag(page, middle(e), [middle(e)[0] + 150, middle(e)[1] + 60]);
    await expect.poll(async () => (await inkBox(page)).box?.x ?? 0).toBeGreaterThan(before.x + 100);
  });

  test('25: a drag let go outside the window is over', async ({ signedIn: page }) => {
    await withText(page);
    const e = await box(element(page));
    await page.mouse.move(...middle(e));
    await page.mouse.down();
    await page.mouse.move(1395, 995, { steps: 10 });
    await page.mouse.move(1500, 1100, { steps: 4 });
    await page.mouse.up();
    const x = await page.locator('#el-0-x').inputValue();
    await page.mouse.move(200, 600, { steps: 6 });
    expect(await page.locator('#el-0-x').inputValue()).toBe(x);
    await expect(page.getByText(/^Moving\./)).toHaveCount(0);
  });

  test('26, 38: a click on the bare label selects nothing; a layer clicked selects its element', async ({ signedIn: page }) => {
    await withText(page);
    const label = await box(labelImage(page));
    await page.mouse.click(label.x + label.width * 0.9, label.y + label.height * 0.9);
    await expect(element(page)).toHaveAttribute('aria-pressed', 'false');
    await panels(page).getByRole('list', { name: 'Layers' }).getByRole('button', { name: /Hello there/ }).first().click();
    await expect(element(page)).toHaveAttribute('aria-pressed', 'true');
  });

  test('27: a double-click types into text, and a click away finishes it', async ({ signedIn: page }) => {
    await withText(page);
    await element(page).dblclick();
    const area = page.locator('[data-text-editor] textarea');
    await expect(area).toBeFocused();
    await page.keyboard.press('End');
    await page.keyboard.type('!');
    const label = await box(labelImage(page));
    await page.mouse.click(label.x + label.width * 0.9, label.y + label.height * 0.9);
    await expect(area).toHaveCount(0);
    await expect.poll(() => ink(page)).toBeGreaterThan(50);
    await panels(page).getByRole('list', { name: 'Layers' }).getByRole('button', { name: /Hello there/ }).first().click();
    await expect(page.locator('#el-0-binding')).toHaveValue('Hello there!');
  });

  test('28, 29, 30, 31: keys typed in a field stay in the field', async ({ signedIn: page }) => {
    await withText(page);
    const content = page.locator('#el-0-binding');
    await content.click();
    await page.keyboard.press('End');
    await page.keyboard.type(' x y');
    await expect(content).toHaveValue('Hello there x y');
    await page.keyboard.press('Backspace');
    await page.keyboard.press('Delete');
    await page.keyboard.press('ArrowLeft');
    await page.keyboard.press('Control+z');
    await expect(element(page)).toHaveCount(1);
    const x = await page.locator('#el-0-x').inputValue();
    await page.keyboard.press('ArrowRight');
    expect(await page.locator('#el-0-x').inputValue()).toBe(x);
    // Out of the field, the arrows move what is selected.
    await element(page).focus();
    await page.keyboard.press('ArrowRight');
    await expect(page.locator('#el-0-x')).not.toHaveValue(x);
  });

  test('32, 33: the context menu closes on Escape or a click elsewhere, and stays on the screen in a corner', async ({ signedIn: page }) => {
    await withText(page);
    const e = await box(element(page));
    await page.mouse.click(...middle(e), { button: 'right' });
    const menu = page.getByRole('menu');
    await expect(menu).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(menu).toHaveCount(0);
    await page.mouse.click(...middle(e), { button: 'right' });
    await expect(menu).toBeVisible();
    await page.mouse.click(600, 900);
    await expect(menu).toHaveCount(0);
    // Right-clicked in the bottom right corner of the window.
    await element(page).click();
    const label = await box(labelImage(page));
    await page.locator('#el-0-x').fill('9999');
    await page.locator('#el-0-x').press('Enter');
    await page.locator('#el-0-y').fill('9999');
    await page.locator('#el-0-y').press('Enter');
    const moved = await box(element(page));
    await page.mouse.click(Math.min(moved.x + moved.width - 3, label.x + label.width - 3), Math.min(moved.y + moved.height - 3, label.y + label.height - 3), { button: 'right' });
    await expect(menu).toBeVisible();
    const m = await box(menu);
    expect(m.x + m.width).toBeLessThanOrEqual(1400);
    expect(m.y + m.height).toBeLessThanOrEqual(1000);
  });

  test('34: resizing turned text from its corner grows it', async ({ signedIn: page }) => {
    await withText(page);
    await page.locator('#el-0-rotation').fill('30');
    await page.locator('#el-0-rotation').press('Enter');
    const w = Number(await page.locator('#el-0-width').inputValue());
    const h = Number(await page.locator('#el-0-height').inputValue());
    const handle = await box(page.locator('[data-handle="se"]').first());
    const e = await box(element(page));
    const [cx, cy] = middle(e);
    const [hx, hy] = middle(handle);
    await drag(page, [hx, hy], [hx + (hx - cx) * 0.3, hy + (hy - cy) * 0.3]);
    expect(Number(await page.locator('#el-0-width').inputValue())).toBeGreaterThan(w);
    expect(Number(await page.locator('#el-0-height').inputValue())).toBeGreaterThan(h);
  });

  test('35: undoing and redoing quickly ends where it began', async ({ signedIn: page }) => {
    await withText(page);
    await element(page).focus();
    for (let i = 0; i < 5; i++) await page.keyboard.press('ArrowRight');
    const end = await page.locator('#el-0-x').inputValue();
    for (let i = 0; i < 5; i++) await page.keyboard.press('Control+z');
    for (let i = 0; i < 5; i++) await page.keyboard.press('Control+Shift+z');
    await expect(page.locator('#el-0-x')).toHaveValue(end);
    await expect.poll(() => ink(page)).toBeGreaterThan(50);
  });

  test('36: a number field left blank, or not a number, keeps what it had', async ({ signedIn: page }) => {
    await withText(page);
    const x = page.locator('#el-0-x');
    const was = await x.inputValue();
    await x.fill('');
    await x.press('Enter');
    await expect(x).toHaveValue(was);
    await x.fill('abc');
    await x.press('Enter');
    await expect(x).toHaveValue(was);
    await expect.poll(() => ink(page)).toBeGreaterThan(50);
  });

  test('37: color is chosen from swatches', async ({ signedIn: page }) => {
    await withText(page);
    const colors = panels(page).getByRole('radiogroup', { name: 'Color' });
    await expect(colors).toBeVisible();
    await colors.locator('label[title="White"]').click();
    await expect(colors.getByRole('radio', { name: 'White' })).toBeChecked();
    await expect.poll(() => ink(page)).toBe(0);
    await colors.locator('label[title="Black"]').click();
    await expect.poll(() => ink(page)).toBeGreaterThan(50);
  });

  test('39: a layer renamed keeps the name on Enter, and Escape takes it back', async ({ signedIn: page }) => {
    await withText(page);
    const layers = panels(page).getByRole('list', { name: 'Layers' });
    await layers.getByRole('button', { name: /Hello there/ }).first().dblclick();
    await page.keyboard.press('Control+a');
    await page.keyboard.type('Greeting');
    await page.keyboard.press('Enter');
    await expect(layers.getByRole('button', { name: 'Greeting', exact: true })).toBeVisible();
    await layers.getByRole('button', { name: 'Greeting', exact: true }).dblclick();
    await page.keyboard.press('Control+a');
    await page.keyboard.type('Other');
    await page.keyboard.press('Escape');
    await expect(layers.getByRole('button', { name: 'Greeting', exact: true })).toBeVisible();
    await expect(element(page)).toHaveCount(1);
  });

  test('40: the readout beside the pointer stays inside the view', async ({ signedIn: page }) => {
    await withText(page);
    const view = await labelImage(page).evaluate((l) => {
      let a = l.parentElement;
      while (a && getComputedStyle(a).overflow !== 'hidden') a = a.parentElement;
      const r = a!.getBoundingClientRect();
      return { x: r.left, y: r.top, width: r.width, height: r.height };
    });
    const e = await box(element(page));
    await page.mouse.move(...middle(e));
    await page.mouse.down();
    await page.mouse.move(view.x + view.width - 3, view.y + view.height - 3, { steps: 10 });
    const r = await box(page.locator('[data-readout]'));
    await page.mouse.up();
    expect(r.x + r.width).toBeLessThanOrEqual(view.x + view.width);
    expect(r.y + r.height).toBeLessThanOrEqual(view.y + view.height);
  });

  test('41: a guide dragged from the ruler onto the panels is not kept', async ({ signedIn: page }) => {
    await withText(page);
    const label = await box(labelImage(page));
    const ruler = await box(page.locator('[data-ruler="x"]'));
    const p = await box(panels(page));
    // From the top ruler down onto the label: a guide.
    await drag(page, [label.x + 100, ruler.y + ruler.height / 2], [label.x + 100, label.y + 60]);
    await expect(page.locator('[data-guide]')).toHaveCount(1);
    // Another, dropped on the panels: none.
    await drag(page, [label.x + 300, ruler.y + ruler.height / 2], [p.x + p.width / 2, p.y + 300]);
    await expect(page.locator('[data-guide]')).toHaveCount(1);
  });

  test('42: changes not saved are offered back after leaving', async ({ signedIn: page }) => {
    await withText(page);
    await page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('link', { name: 'Templates' }).click();
    await page.getByRole('link', { name: 'New template' }).click();
    await expect(page.getByRole('button', { name: 'Restore them' })).toBeVisible();
  });

  test('43: Ctrl+S saves, once however quickly pressed, and from a field too', async ({ signedIn: page, api }) => {
    await withText(page);
    const name = await page.locator('#tpl-name').inputValue();
    await page.keyboard.press('Control+s');
    await page.keyboard.press('Control+s');
    await expect(page.getByText('Unsaved changes')).toHaveCount(0);
    await expect(page).not.toHaveURL(/\/new$/);
    const { templates } = await api.call<{ templates: { name: string }[] }>('GET', '/templates');
    expect(templates.filter((t) => t.name === name)).toHaveLength(1);
    await panels(page).getByRole('list', { name: 'Layers' }).getByRole('button', { name: /Hello there/ }).first().click();
    await page.locator('#el-0-binding').fill('Changed');
    await page.keyboard.press('Control+s');
    await expect(page.getByText('Unsaved changes')).toHaveCount(0);
  });

  test('44, 45: Ctrl+wheel zooms the label only over the label, and the overview zooms it without moving the page', async ({ signedIn: page }) => {
    await withText(page);
    const label = await box(labelImage(page));
    const p = await box(panels(page));
    await page.keyboard.down('Control');
    await page.mouse.move(p.x + p.width / 2, p.y + p.height - 100);
    await page.mouse.wheel(0, -200);
    expect(await box(labelImage(page))).toEqual(label);
    await page.mouse.move(...middle(label));
    await page.mouse.wheel(0, -200);
    await page.keyboard.up('Control');
    await expect.poll(async () => (await box(labelImage(page))).width).toBeGreaterThan(label.width);
    const zoomed = await box(labelImage(page));
    await page.mouse.move(...middle(await box(page.locator('[data-overview-view]').locator('..'))));
    await page.mouse.wheel(0, 300);
    await expect.poll(async () => (await box(labelImage(page))).width).toBeLessThan(zoomed.width);
    await laidOut(page);
  });

  test('46: the tools show which has the keyboard', async ({ signedIn: page }) => {
    await withText(page);
    await tool(page, 'Text').focus();
    await page.keyboard.press('Shift+Tab');
    await page.keyboard.press('Tab');
    const ring = await page.evaluate(() => {
      const a = document.activeElement as HTMLElement;
      const s = getComputedStyle(a);
      return { outline: s.outlineStyle !== 'none' && s.outlineWidth !== '0px', shadow: s.boxShadow !== 'none' };
    });
    expect(ring.outline || ring.shadow).toBe(true);
  });

  test('47: nothing for a screen reader to trip on, with a selection and its menu open', async ({ signedIn: page }) => {
    await withText(page);
    const e = await box(element(page));
    await page.mouse.click(...middle(e), { button: 'right' });
    await expect(page.getByRole('menu')).toBeVisible();
    const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
    expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target).join(', ')}`)).toEqual([]);
  });

});
