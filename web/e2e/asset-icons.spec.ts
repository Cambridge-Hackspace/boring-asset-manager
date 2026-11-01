import AxeBuilder from '@axe-core/playwright';
import type { Page } from '@playwright/test';
import { expect, test, unique } from './fixtures';

// Assets' icons (docs/icons.md): an asset is drawn with its nearest
// category's icon, in lists and on its page, unless it is given one of
// its own on its form.

const boxIcon = (name: string) => `icon "${name}" { category "storage"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }`;
const drawn = (page: Page, name: string) => page.locator(`svg[data-icon="${name}"]`);

async function noViolations(page: Page) {
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations.map((v) => `${v.id}: ${v.help} (${v.nodes.map((n) => n.target.join(' ')).join('; ')})`)).toEqual([]);
}

test("an asset is drawn with its category's icon, and given one of its own", async ({ signedIn: page, api }) => {
  const parentName = unique('Storage');
  const { category: parent } = await api.call('POST', '/categories', { name: parentName, color_hex: '#0f766e' });
  const { category: child } = await api.call('POST', '/categories', { name: unique('Bins'), parent_id: parent.id, color_hex: '#b45309' });
  const crate = unique('crate').toLowerCase();
  const barrel = unique('barrel').toLowerCase();
  await api.call('PUT', `/categories/${parent.id}/icon`, { icon_name: crate, source: boxIcon(crate) });
  const { category: other } = await api.call('POST', '/categories', { name: unique('Barrels') });
  await api.call('PUT', `/categories/${other.id}/icon`, { icon_name: barrel, source: boxIcon(barrel) });
  const name = unique('Blue bin');
  const { asset } = await api.call('POST', '/assets', { name, category_ids: [child.id] });

  // in the list: beside its name, and in its category's pill
  await page.goto(`/assets?search=${encodeURIComponent(name)}`);
  const row = page.getByRole('row').filter({ has: page.getByRole('link', { name }) });
  await expect(row.locator(`svg[data-icon="${crate}"]`)).toHaveCount(2);

  // on its page, saying where it comes from
  await page.goto(`/assets/${asset.asset.id}`);
  await expect(page.getByRole('heading', { level: 1, name }).locator(`svg[data-icon="${crate}"]`)).toBeVisible();
  await expect(page.getByText(`From ${parentName}.`)).toBeVisible();
  await noViolations(page);

  // one of its own, chosen on its form
  await page.getByRole('link', { name: 'Edit' }).click();
  await expect(page.getByText(`From ${parentName}.`)).toBeVisible();
  await page.getByRole('button', { name: 'Choose another…' }).click();
  const picker = page.getByRole('group', { name: 'Choose an icon' });
  await picker.getByRole('searchbox', { name: 'Search icons' }).fill(barrel);
  await expect(picker.getByRole('button')).toHaveCount(2); // it, and Close
  await noViolations(page);
  await picker.getByRole('button', { name: barrel }).click();
  await expect(picker).toBeHidden();
  await expect(page.getByText(`Its own, ${barrel}.`)).toBeVisible();
  await page.getByRole('button', { name: 'Save' }).click();

  await expect(page.getByText(`Its own, ${barrel}.`)).toBeVisible();
  await expect(page.getByRole('heading', { level: 1, name }).locator(`svg[data-icon="${barrel}"]`)).toBeVisible();
  await expect(drawn(page, crate)).toHaveCount(1); // still in its category's pill

  // and back to its category's
  await page.getByRole('link', { name: 'Edit' }).click();
  await page.getByRole('button', { name: "Use the category's" }).click();
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByText(`From ${parentName}.`)).toBeVisible();
});

// The server draws an icon as an image two ways; Chromium drawing the SVG
// is the reference the PNG is held to, pixel for pixel, near enough. The
// icon has what isolex 0.4 draws in groups too: a plate laid on a face,
// its strokes fixed, and a disc cut at the frame's edge.
test('an icon drawn as a PNG is drawn as its SVG is', async ({ signedIn: page, api }) => {
  const { category } = await api.call('POST', '/categories', { name: unique('Drums'), color_hex: '#b45309' });
  const name = unique('drum').toLowerCase();
  const source = `icon "${name}" {
  category "storage"
  footprint 1 1 1
  part "body" {
    box 0 0 0 1 1 0.5
    cylinder 0.5 0.5 0.5 0.3 0.4
    tile 0 0 0 1 1 fill="category" stroke="ink" dash="3 2.5" opacity=0.5
  }
  part "mark" {
    at 0.5 0.5 1 {
      circle "x" "y" 5 fill="ink" stroke="outline" width=1.2
      path d="M{x-8},{y+6} q8,-12 16,0" stroke="ink" width=1.4 fill="none"
    }
  }
  part "plate" {
    transform onto="front" of="box 0 0 0 1 1 0.5" strokes="fixed" {
      at 0 0 0 { rect 6 5 22 10 rx=2 fill="accent" stroke="ink" width=1.2 }
    }
    at 0.5 0.5 1 {
      transform translate="38 0" rotate=20 origin="x y" bounds="clip" {
        circle "x" "y" 7 fill="accent" stroke="ink" width=1.2
      }
    }
  }
}`;
  const saved = await api.call('PUT', `/categories/${category.id}/icon`, { icon_name: name, source });
  expect(saved.save.errors).toEqual([]);
  await page.goto('/icons');

  for (const theme of ['light', 'dark']) {
    const diff = await page.evaluate(
      async ({ name, theme }) => {
        const load = async (format: string) => {
          const res = await fetch(`/api/v1/icons/image?name=${name}&format=${format}&height=160&theme=${theme}`);
          const img = new Image();
          img.src = URL.createObjectURL(await res.blob());
          await img.decode();
          return img;
        };
        const [svg, png] = [await load('svg'), await load('png')];
        const pixels = (img: HTMLImageElement) => {
          const c = document.createElement('canvas');
          c.width = png.naturalWidth;
          c.height = png.naturalHeight;
          const ctx = c.getContext('2d')!;
          ctx.drawImage(img, 0, 0, c.width, c.height);
          return ctx.getImageData(0, 0, c.width, c.height).data;
        };
        const a = pixels(svg);
        const b = pixels(png);
        let total = 0;
        let far = 0;
        for (let i = 0; i < a.length; i += 4) {
          // compared as painted over white, as a page shows them
          let d = 0;
          for (let k = 0; k < 3; k++) {
            const over = (p: Uint8ClampedArray) => (p[i + k]! * p[i + 3]! + 255 * (255 - p[i + 3]!)) / 255;
            d = Math.max(d, Math.abs(over(a) - over(b)));
          }
          total += d;
          if (d > 96) far++;
        }
        const n = a.length / 4;
        return { mean: total / n, far: far / n, size: [png.naturalWidth, png.naturalHeight] };
      },
      { name, theme },
    );
    console.log(theme, JSON.stringify(diff));
    expect(diff.size[1]).toBe(160);
    expect(diff.mean, theme).toBeLessThan(2);
    expect(diff.far, theme).toBeLessThan(0.01);
  }
});
