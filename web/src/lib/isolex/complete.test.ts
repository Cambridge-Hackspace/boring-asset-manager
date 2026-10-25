import { describe, expect, it } from 'vitest';
import { complete, type Vocabulary } from './complete';
import { outline } from './syntax';

const vocab: Vocabulary = {
  tokens: ['ink', 'body-top', 'accent', 'category'],
  definitions: outline(`
base "rack-unit" { part "chassis" { box 0 0 0 2 1 1 } }
icon "switch" extends="rack-unit" { part "ports" { } }
fragment "glyph" { }
axis "state" { variant "down" { } }
`),
  icons: ['switch', 'switch-down'],
};

/** Completes at | and gives the labels offered. */
function at(marked: string): string[] {
  const caret = marked.indexOf('|');
  return complete(marked.replace('|', ''), caret, vocab)?.items.map((s) => s.label) ?? [];
}

describe('complete', () => {
  it('offers definitions at the top, and nodes inside them', () => {
    expect(at('ic|')).toEqual(['icon']);
    expect(at('icon "a" {\n  fo|\n}')).toEqual(['footprint']);
    expect(at('icon "a" {\n  part "p" {\n    b|\n  }\n}')).toEqual(['box']);
    // shapes go in parts; definitions' own nodes do not
    expect(at('icon "a" {\n  part "p" {\n    cat|\n  }\n}')).toEqual([]);
  });

  it('offers the palette for paint, as typed in quotes or not', () => {
    expect(at('icon "a" {\n  part "p" { box 0 0 0 1 1 1 fill="|" }\n}')).toEqual(['ink', 'body-top', 'accent', 'category', 'none']);
    expect(at('  tile 0 0 0 1 1 stroke="ac|')).toEqual(['accent']);
    const c = complete('tile 0 0 0 1 1 fill=bo', 22, vocab)!;
    expect(c.items[0]).toMatchObject({ label: 'body-top', insert: '"body-top"' });
    expect([c.from, c.to]).toEqual([20, 22]);
  });

  it('offers what can be extended, inserted, badged and taken variants from', () => {
    expect(at('icon "a" extends="|"')).toEqual(['rack-unit', 'switch']);
    expect(at('icon "a" {\n  extends "s|"\n}')).toEqual(['switch']);
    expect(at('icon "a" {\n  part "p" { insert "|" }\n}')).toEqual(['glyph']);
    expect(at('icon "a" {\n  badge "switch-|"\n}')).toEqual(['switch-down']);
    expect(at('icon "a" {\n  axes "|"\n}')).toEqual(['state']);
  });

  it("offers what it extends' parts, all the way up, to replace or remove", () => {
    expect(at('icon "router" extends="switch" {\n  replace "|"\n}')).toEqual(['ports', 'chassis']);
    expect(at('icon "router" extends="switch" {\n  remove "ch|"\n}')).toEqual(['chassis']);
  });

  it('offers properties after a node, and finds words inside names too', () => {
    expect(at('  box 0 0 0 1 1 1 fi|')).toEqual(['fill=', 'fills=']);
    expect(at('  tile 0 0 0 1 1 fill="dy|"')).toEqual(['body-top']);
  });

  it('inserts a node with its arguments, the first selected to type over', () => {
    const c = complete('icon "a" {\n  part "p" {\n    bo', 27, vocab)!;
    const box = c.items.find((s) => s.label === 'box')!;
    expect(box.insert).toBe('box 0 0 0 1 1 1');
    expect(box.select).toEqual([4, 5]);
  });

  it('offers nothing in a comment, or where nothing fits', () => {
    expect(at('// fill="|')).toEqual([]);
    expect(at('icon "a" { box 0 0 0 1 1 1 opacity=|')).toEqual([]);
  });
});
