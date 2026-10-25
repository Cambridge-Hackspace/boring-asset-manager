import { describe, expect, it } from 'vitest';
import { indent, outdent, newline, closeBrace, toggleComment, type Change } from './edit';

/** Applies a change; | marks the caret, [ ] a selection. */
function run(op: (t: string, s: number, e: number) => Change | null, marked: string): string | null {
  let text = marked;
  let s: number, e: number;
  if (text.includes('|')) {
    s = e = text.indexOf('|');
    text = text.replace('|', '');
  } else {
    s = text.indexOf('[');
    text = text.replace('[', '');
    e = text.indexOf(']');
    text = text.replace(']', '');
  }
  const c = op(text, s, e);
  if (!c) return null;
  const out = text.slice(0, c.from) + c.insert + text.slice(c.to);
  if (c.selStart === c.selEnd) return out.slice(0, c.selStart) + '|' + out.slice(c.selStart);
  return out.slice(0, c.selStart) + '[' + out.slice(c.selStart, c.selEnd) + ']' + out.slice(c.selEnd);
}

describe('indent and outdent', () => {
  it('indents at the caret, or in place of a selection within a line', () => {
    expect(run(indent, 'a|b')).toBe('a  |b');
    expect(run(indent, 'a[bc]d')).toBe('a  |d');
  });
  it('indents every line a selection touches, keeping it selected, and not the line it ends at the start of', () => {
    expect(run(indent, 'a[b\nc\n]d')).toBe('  a[b\n  c\n]d');
    expect(run(indent, '[a\n\nb]')).toBe('  [a\n\n  b]');
  });
  it('outdents by one indent, or what little there is', () => {
    expect(run(outdent, '    a|b')).toBe('  a|b');
    expect(run(outdent, '[  a\n b\nc]')).toBe('[a\nb\nc]');
    expect(run(outdent, '|a')).toBe('|a');
  });
});

describe('newline', () => {
  it('keeps the indent, and adds one after an opening brace', () => {
    expect(run(newline, '  box 1|')).toBe('  box 1\n  |');
    expect(run(newline, '  part "p" {|')).toBe('  part "p" {\n    |');
  });
  it('between braces, puts the closing one on its own line', () => {
    expect(run(newline, 'icon "a" {|}')).toBe('icon "a" {\n  |\n}');
  });
  it('replaces a selection', () => {
    expect(run(newline, '  a[bc]d')).toBe('  a\n  |d');
  });
});

describe('closeBrace', () => {
  it('takes one indent off a line of nothing but indent', () => {
    expect(run(closeBrace, '  part {\n    x\n    |')).toBe('  part {\n    x\n  }|');
  });
  it('is not for anywhere else', () => {
    expect(run(closeBrace, '  a|')).toBeNull();
    expect(run(closeBrace, '|')).toBeNull();
  });
});

describe('toggleComment', () => {
  it('comments out the lines touched at their least indent, skipping blank ones', () => {
    expect(run(toggleComment, '  [a\n\n    b]')).toBe('  // [a\n\n  //   b]');
  });
  it('uncomments them when every line with anything on it is commented', () => {
    expect(run(toggleComment, '  // [a\n\n  //   b]')).toBe('  [a\n\n    b]');
    expect(run(toggleComment, '//a|')).toBe('a|');
  });
});
