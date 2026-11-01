import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { contrast, luminance, parseHex } from './lib/color';

// The Supply look's colors (#156), read from app.css, in the pairs the app
// uses them in: text at least 4.5:1 on what it sits on, and borders and
// focus rings at least 3:1 against the page (WCAG 1.4.3 and 1.4.11).

const css = readFileSync('src/app.css', 'utf8');

function vars(block: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const m of block.matchAll(/--([a-z0-9-]+):\s*(#[0-9a-f]{6})\b/gi)) out[m[1]!] = m[2]!;
  return out;
}

const theme = vars(css.match(/@theme \{([\s\S]*?)\n\}/)![1]!);
const light: Record<string, string> = { ...theme, white: '#ffffff', ...vars(css.match(/:root \{([\s\S]*?)\}/)![1]!) };
// Dark when chosen, or when the system is and light was not chosen (#168):
// two blocks, which must agree.
const chosenDark = css.match(/:root\[data-theme='dark'\] \{([\s\S]*?)\}/)![1]!;
const systemDark = css.match(/@media \(prefers-color-scheme: dark\) \{\s*:root:not\(\[data-theme='light'\]\) \{([\s\S]*?)\}/)![1]!;
const dark: Record<string, string> = { ...theme, white: '#ffffff', ...vars(chosenDark) };

function ratio(scheme: Record<string, string>, fg: string, bg: string): number {
  const a = parseHex(scheme[fg] ?? '');
  const b = parseHex(scheme[bg] ?? '');
  if (!a || !b) throw new Error(`no color ${a ? bg : fg}`);
  return contrast(luminance(a), luminance(b));
}

// [foreground, background, least ratio]
const text = 4.5;
const edge = 3;
const pairs: Record<'light' | 'dark', [string, string, number][]> = {
  light: [
    ...['color-zinc-50', 'color-zinc-100', 'white', 'color-highlight'].map((bg): [string, string, number] => ['color-zinc-900', bg, text]),
    ...['color-zinc-50', 'color-zinc-100', 'white'].flatMap((bg): [string, string, number][] => [
      ['color-zinc-600', bg, text],
      ['color-zinc-700', bg, text],
      ['color-zinc-900', bg, edge],
      ['bam-focus', bg, edge],
    ]),
    ['white', 'color-action', text],
    ['white', 'color-action-hover', text],
  ],
  dark: [
    ...['color-zinc-950', 'color-zinc-900', 'color-zinc-800'].flatMap((bg): [string, string, number][] => [
      ['color-zinc-100', bg, text],
      ['color-zinc-300', bg, text],
      ['color-zinc-400', bg, text],
      ['bam-focus', bg, edge],
    ]),
    ['color-zinc-400', 'color-zinc-900', edge],
    ['color-zinc-900', 'color-highlight', text],
    ['white', 'color-action', text],
    ['white', 'color-action-hover', text],
    // Table headings: paper on zinc-800.
    ['color-zinc-50', 'color-zinc-800', text],
  ],
};

describe('the theme', () => {
  for (const [name, scheme] of [['light', light], ['dark', dark]] as const) {
    it(`meets its contrast in ${name}`, () => {
      const short = pairs[name]
        .map(([fg, bg, least]) => ({ fg, bg, least, got: ratio(scheme, fg, bg) }))
        .filter((p) => p.got < p.least)
        .map((p) => `${p.fg} on ${p.bg}: ${p.got.toFixed(2)}, wants ${p.least}`);
      expect(short).toEqual([]);
    });
  }

  it('reads every color it checks', () => {
    expect(Object.keys(theme)).toEqual(expect.arrayContaining(['color-zinc-50', 'color-zinc-950', 'color-action', 'color-highlight']));
    expect(light['bam-focus']).not.toEqual(dark['bam-focus']);
  });

  it('is the same dark whether chosen or the system’s', () => {
    expect(vars(systemDark)).toEqual(vars(chosenDark));
  });
});
