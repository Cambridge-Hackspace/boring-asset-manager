import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render } from '@testing-library/svelte';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import IsoIcon from './IsoIcon.svelte';
import { formatNumber, type IrDocument } from '../lib/isolex/ir';
import { paletteCss, tokenVar } from '../lib/isolex/palette';

afterEach(() => cleanup());

// isolex's own sample, its IR and its SVG, vendored from isolex 0.3.1
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
    as painted or not, since isolex paints hex and this a property, and a
    clip path's name only as one, since this names them for the page. */
function shape(e: Element): string {
  const attrs = [...e.attributes]
    .filter((a) => a.name !== 'xmlns')
    .map((a) => {
      if (a.name === 'fill' || a.name === 'stroke') return [a.name, a.value === 'none' ? 'none' : 'paint'];
      if (a.name === 'id') return [a.name, 'clip'];
      if (a.name === 'clip-path') return [a.name, 'url(#clip)'];
      return [a.name, a.value];
    })
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

  // isolex 0.4's sample, transforms and text, in IR version 2: groups
  // drawn through matrices, cut to clips, with strokes that keep their
  // width; each drawn here as isolex 0.4.0 draws it.
  it("draws IR version 2 as isolex 0.4 does", () => {
    const v2 = resolve(testdata, 'isolex-0.6.0');
    const fixtures: [string, (name: string, golden: string) => string][] = [
      ['sample', (name, golden) => {
        const start = golden.indexOf(`== ${name}.svg ==\n`);
        return golden.slice(golden.indexOf('\n', start) + 1, golden.indexOf('</svg>', start) + 6);
      }],
      ['transforms', (name, golden) => {
        const start = golden.indexOf(`<!-- ${name} -->\n`);
        return golden.slice(golden.indexOf('\n', start) + 1, golden.indexOf('</svg>', start) + 6);
      }],
      ['text', (name, golden) => {
        const start = golden.indexOf(`<!-- ${name} -->\n`);
        return golden.slice(golden.indexOf('\n', start) + 1, golden.indexOf('</svg>', start) + 6);
      }],
    ];
    let clipped = 0;
    let fixed = 0;
    for (const [fixture, svgOf] of fixtures) {
      const doc = JSON.parse(readFileSync(resolve(v2, `${fixture}.ir.golden.json`), 'utf8')) as IrDocument;
      const golden = readFileSync(resolve(v2, `${fixture}.svg.golden`), 'utf8');
      expect(doc.ir_version).toBe(2);
      for (const icon of doc.icons) {
        const { container, unmount } = render(IsoIcon, { icon, label: icon.name });
        const svg = container.querySelector('svg')!;
        const want = new DOMParser().parseFromString(svgOf(icon.name, golden), 'image/svg+xml').documentElement;
        const ours = [...svg.children].filter((c) => c.tagName !== 'title').map(shape);
        const theirs = [...want.children].filter((c) => c.tagName !== 'title').map(shape);
        expect(ours, `${fixture} ${icon.name}`).toEqual(theirs);
        // each clip path is named once, and used by the group it cuts
        for (const clip of svg.querySelectorAll('clipPath')) {
          clipped++;
          expect(svg.querySelectorAll(`[clip-path="url(#${clip.id})"]`).length).toBe(1);
        }
        fixed += svg.querySelectorAll('[vector-effect="non-scaling-stroke"]').length;
        unmount();
      }
    }
    expect(clipped).toBeGreaterThan(0);
    expect(fixed).toBeGreaterThan(0);
  });

  it('names the clip paths of an icon drawn twice apart', () => {
    const doc = JSON.parse(readFileSync(resolve(testdata, 'isolex-0.6.0/transforms.ir.golden.json'), 'utf8')) as IrDocument;
    const icon = doc.icons.find((i) => i.name === 'clipped')!;
    // both on one page, as a list draws the same icon twice
    render(IsoIcon, { icon });
    render(IsoIcon, { icon });
    const ids = [...document.querySelectorAll('clipPath')].map((c) => c.id);
    expect(ids.length).toBe(2);
    expect(new Set(ids).size).toBe(2);
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
