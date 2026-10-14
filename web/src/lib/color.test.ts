import { describe, expect, it } from 'vitest';
import { contrast, luminance, parseHex, textOn } from './color';

describe('color', () => {
  it('parses hex colors', () => {
    expect(parseHex('#ff8800')).toEqual([255, 136, 0]);
    expect(parseHex('0f0')).toEqual([0, 255, 0]);
    expect(parseHex('#ggg')).toBeNull();
    expect(parseHex('')).toBeNull();
  });

  it('matches the WCAG contrast of black on white', () => {
    expect(contrast(luminance([0, 0, 0]), luminance([255, 255, 255]))).toBeCloseTo(21, 5);
  });

  it('picks the text color with more contrast', () => {
    expect(textOn('#ffffff')).toBe('#000000');
    expect(textOn('#000000')).toBe('#ffffff');
    expect(textOn('#fbbf24')).toBe('#000000'); // amber
    expect(textOn('#1d4ed8')).toBe('#ffffff'); // blue
    expect(textOn('#6b7280')).toBe('#ffffff'); // the Uncategorized gray
    expect(textOn('not a color')).toBe('#000000');
  });

  it('always reaches at least 4.5:1 for the chosen text', () => {
    for (let v = 0; v < 256; v += 15) {
      for (const hex of [`#${v.toString(16).padStart(2, '0').repeat(3)}`, `#${v.toString(16).padStart(2, '0')}8040`]) {
        const bg = luminance(parseHex(hex)!);
        const fg = textOn(hex) === '#000000' ? 0 : 1;
        expect(contrast(bg, fg)).toBeGreaterThanOrEqual(4.5);
      }
    }
  });
});
