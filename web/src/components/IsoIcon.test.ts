import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render } from '@testing-library/svelte';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import IsoIcon from './IsoIcon.svelte';
import { formatNumber, type IrDocument } from '../lib/isolex/ir';
import { paletteCss, tokenVar } from '../lib/isolex/palette';

afterEach(() => cleanup());

// isolex's own sample, its IR and its SVG, vendored from isolex 0.3.0
// beside the Go renderer that is held to the same bytes.
// (Vitest runs in web/.)
const testdata = resolve(process.cwd(), '../internal/isolex/testdata');
const doc = JSON.parse(readFileSync(resolve(testdata, 'sample.ir.golden.json'), 'utf8')) as IrDocument;
const golden = readFileSync(resolve(testdata, 'sample.svg.golden'), 'utf8');

/** isolex's SVG for one icon, from the golden that strings them together. */
function goldenSvg(name: string): Element {
  const start = golden.indexOf(`== ${name}.svg ==\n`);
  const text = golden.slice(golden.indexOf('\n', start) + 1, golden.indexOf('</svg>', start) + 6);
  return new DOMParser().parseFromString(text, 'image/svg+xml').documentElement;
}

/** An element as compared: its tag, and its attributes, colors read only
    as painted or not, since isolex paints hex and this a property. */
function shape(e: Element): string {
  const attrs = [...e.attributes]
    .filter((a) => a.name !== 'xmlns')
    .map((a) => [a.name, a.name === 'fill' || a.name === 'stroke' ? (a.value === 'none' ? 'none' : 'paint') : a.value])
    .sort(([a], [b]) => a!.localeCompare(b!));
  return `<${e.tagName.toLowerCase()} ${attrs.map(([k, v]) => `${k}="${v}"`).join(' ')}>` + [...e.children].map(shape).join('') + `</>`;
}

describe('IsoIcon', () => {
  it("draws every icon of isolex's sample as isolex does", () => {
    expect(doc.icons.length).toBe(7);
    for (const icon of doc.icons) {
      const { container, unmount } = render(IsoIcon, { icon, label: icon.name });
      const svg = container.querySelector('svg')!;
      const want = goldenSvg(icon.name);
      expect(svg.getAttribute('viewBox'), icon.name).toBe(want.getAttribute('viewBox'));
      const ours = [...svg.children].filter((c) => c.tagName !== 'title').map(shape);
      const theirs = [...want.children].filter((c) => c.tagName !== 'title').map(shape);
      expect(ours, icon.name).toEqual(theirs);
      unmount();
    }
  });

  it('paints tokens with the palette, and category with the color given', () => {
    const icon = doc.icons.find((i) => i.name === 'state-down')!;
    const { container } = render(IsoIcon, { icon, height: 24, label: 'Down', categoryColor: '#336699' });
    const svg = container.querySelector('svg')!;
    expect(svg.getAttribute('role')).toBe('img');
    expect(svg.getAttribute('aria-label')).toBe('Down');
    expect(svg.getAttribute('height')).toBe('24');
    expect(svg.style.getPropertyValue('--iso-category').trim()).toBe('#336699');
    expect(svg.querySelector('polygon')!.getAttribute('fill')).toBe('var(--iso-accent)');
  });

  it('is decoration without a label, and takes no color that is not one', () => {
    const icon = doc.icons[0]!;
    const { container } = render(IsoIcon, { icon, categoryColor: 'red;background:url(x)' });
    const svg = container.querySelector('svg')!;
    expect(svg.getAttribute('aria-hidden')).toBe('true');
    expect(svg.getAttribute('role')).toBeNull();
    expect(svg.getAttribute('style')).toBeNull();
  });
});

describe('formatNumber', () => {
  it("is isolex's rule: two decimals, ties to even, never -0.00", () => {
    const cases: [number, string][] = [
      [0.125, '0.12'], [0.375, '0.38'], [0.625, '0.62'], [0.875, '0.88'], [-0.125, '-0.12'], [2.5, '2.50'],
      [0.145, '0.14'], [0.135, '0.14'], [40 * 0.8660254037844386, '34.64'],
      [-0.001, '0.00'], [-0.004999, '0.00'], [-0.005001, '-0.01'], [-0, '0.00'], [12.345678, '12.35'],
    ];
    for (const [v, want] of cases) expect(formatNumber(v), String(v)).toBe(want);
  });
});

describe('the palette as CSS', () => {
  it('declares each token for light and dark, and leaves out what is not a token and a color', () => {
    const css = paletteCss([
      { token: 'ink', light: '#111', dark: '#eeeeee' },
      { token: 'Bad', light: '#111', dark: '#eee' },
      { token: 'evil', light: 'red;}body{display:none', dark: '#000' },
    ]);
    expect(css).toBe(
      ':root,.iso-light{--iso-ink:#111;}@media (prefers-color-scheme: dark){:root{--iso-ink:#eeeeee;}}.iso-light{--iso-ink:#111;}.iso-dark{--iso-ink:#eeeeee;}',
    );
    expect(tokenVar('body-top')).toBe('var(--iso-body-top)');
    expect(tokenVar('x);}')).toBe('none');
  });
});
