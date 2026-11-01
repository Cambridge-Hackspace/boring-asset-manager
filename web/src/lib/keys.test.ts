import { describe, expect, it } from 'vitest';
import { keyFrom, valueFrom } from './keys';

describe('keyFrom', () => {
  it('makes a key as field keys are made', () => {
    expect(keyFrom('What is it?')).toBe('what_is_it');
    expect(keyFrom('  Light up its drawer!  ')).toBe('light_up_its_drawer');
    expect(keyFrom('Café shelf')).toBe('cafe_shelf');
  });
  it('starts with a letter, and keeps to its length', () => {
    expect(keyFrom('2nd shelf')).toBe('nd_shelf');
    expect(keyFrom('42')).toBe('');
    expect(keyFrom('a very long label indeed', 6)).toBe('a_very');
    expect(keyFrom('ab cd', 3)).toBe('ab');
  });
});

describe('valueFrom', () => {
  it('may start with a digit, and falls back to the label itself', () => {
    expect(valueFrom('Needs a look')).toBe('needs_a_look');
    expect(valueFrom('10 mm')).toBe('10_mm');
    expect(valueFrom(' ★ ')).toBe('★');
  });
});
