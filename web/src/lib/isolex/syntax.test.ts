import { describe, expect, it } from 'vitest';
import { tokenize, outline, offsetOf, rangeAt } from './syntax';

const kinds = (text: string) => tokenize(text).filter((t) => t.kind !== 'space').map((t) => `${t.kind}:${text.slice(t.start, t.end)}`);

describe('tokenize', () => {
  it('covers the text end to end, in order', () => {
    const text = 'icon "a" {\n  box 0 0 0 1 1 1 fill="ink" // a box\n}\n';
    const tokens = tokenize(text);
    expect(tokens[0]!.start).toBe(0);
    expect(tokens.at(-1)!.end).toBe(text.length);
    for (let k = 1; k < tokens.length; k++) expect(tokens[k]!.start).toBe(tokens[k - 1]!.end);
  });

  it('tells nodes, arguments, properties, strings, numbers and comments apart', () => {
    expect(kinds('icon "crate" {\n  box 0 -0.5 0 1 1 "u * 2" fill="ink" when=#true // a box\n}')).toEqual([
      'node:icon', 'string:"crate"', 'punct:{',
      'node:box', 'number:0', 'number:-0.5', 'number:0', 'number:1', 'number:1', 'string:"u * 2"',
      'property:fill', 'punct:=', 'string:"ink"', 'property:when', 'punct:=', 'keyword:#true', 'comment:// a box',
      'punct:}',
    ]);
  });

  it('starts a node after ; and {, and not after a line continuation', () => {
    expect(kinds('a 1; b 2 { c }')).toEqual(['node:a', 'number:1', 'punct:;', 'node:b', 'number:2', 'punct:{', 'node:c', 'punct:}']);
    expect(kinds('path d="M0,0" \\\n  stroke="ink"')).toEqual(['node:path', 'property:d', 'punct:=', 'string:"M0,0"', 'property:stroke', 'punct:=', 'string:"ink"']);
  });

  it('reads raw, multi-line and escaped strings, nested block comments, and stops a broken string at its line', () => {
    expect(kinds('a #"say "hi""# """\nx\n""" "a\\"b" /* x /* y */ z */ b')).toEqual([
      'node:a', 'string:#"say "hi""#', 'string:"""\nx\n"""', 'string:"a\\"b"', 'comment:/* x /* y */ z */', 'ident:b',
    ]);
    const broken = tokenize('a "open\nb 1');
    expect(broken.find((t) => t.kind === 'string')!.end).toBe('a "open'.length);
    expect(tokenize('a "x\\"y"').find((t) => t.kind === 'string')!.value).toBe('x"y');
  });
});

describe('outline', () => {
  it('reads definitions with what they extend, their parts, params and variants', () => {
    const defs = outline(`
base "rack-unit" { param u 1; part "chassis" { box 0 0 0 2 1 1 } }
icon "switch" extends="rack-unit" {
  part "ports" { repeat 4 "i" { part "not-a-part" { } } }
  replace "chassis" { }
}
icon "router" {
  extends "switch"
}
fragment "glyph" { }
axis "state" { variant "down" { } variant "planned" { } }
`);
    expect(defs).toEqual([
      { kind: 'base', name: 'rack-unit', parts: ['chassis'], params: ['u'], variants: [] },
      { kind: 'icon', name: 'switch', extends: 'rack-unit', parts: ['ports', 'chassis'], params: [], variants: [] },
      { kind: 'icon', name: 'router', extends: 'switch', parts: [], params: [], variants: [] },
      { kind: 'fragment', name: 'glyph', parts: [], params: [], variants: [] },
      { kind: 'axis', name: 'state', parts: [], params: [], variants: ['down', 'planned'] },
    ]);
  });

  it('ignores what is commented out, and survives half-typed text', () => {
    expect(outline('// icon "a" { }\n/* base "b" { } */\nicon "c" {').map((d) => d.name)).toEqual(['c']);
    expect(outline('icon')).toEqual([]);
  });
});

describe('offsetOf and rangeAt', () => {
  const text = 'icon "a" {\n  bxo 0 0 0\n}\n';
  it('finds a line and column as the compiler counts them', () => {
    expect(offsetOf(text, 2, 3)).toBe(text.indexOf('bxo'));
    expect(offsetOf(text, 9, 1)).toBe(text.length);
    // columns count characters, not UTF-16 units
    expect(offsetOf('"𝐱" x', 1, 5)).toBe('"𝐱" '.length);
  });
  it('underlines the token there, never past its line', () => {
    expect(rangeAt(text, text.indexOf('bxo'))).toEqual([text.indexOf('bxo'), text.indexOf('bxo') + 3]);
    const open = 'a "never closed\nb';
    expect(rangeAt(open, 2)).toEqual([2, open.indexOf('\n')]);
    expect(rangeAt(text, text.length)).toEqual([text.length - 1, text.length]);
  });
});
