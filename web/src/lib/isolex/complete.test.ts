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
font "plain" { em 10; ascent 7; descent 3; kind "stroke" }
axis "weight" { variant "bold" { } }
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
    // and where the letters are inside a name, after those that begin with them
    expect(at('icon "a" {\n  fo|\n}')).toEqual(['footprint', 'transform-bounds', 'transform']);
    expect(at('icon "a" {\n  part "p" {\n    b|\n  }\n}')).toEqual(['box']);
    // shapes go in parts; definitions' own nodes do not
    expect(at('icon "a" {\n  part "p" {\n    cat|\n  }\n}')).toEqual([]);
  });

  it('offers the palette for paint, as typed in quotes or not', () => {
    expect(at('icon "a" {\n  part "p" { tile 0 0 0 1 1 fill="|" }\n}')).toEqual(['ink', 'body-top', 'accent', 'category', 'none']);
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
    expect(at('icon "a" {\n  axes "|"\n}')).toEqual(['state', 'weight']);
  });

  it("offers what it extends' parts, all the way up, to replace or remove", () => {
    expect(at('icon "router" extends="switch" {\n  replace "|"\n}')).toEqual(['ports', 'chassis']);
    expect(at('icon "router" extends="switch" {\n  remove "ch|"\n}')).toEqual(['chassis']);
  });

  it('offers properties after a node, and finds words inside names too', () => {
    expect(at('  tile 0 0 0 1 1 fi|')).toEqual(['fill=']);
    expect(at('  tile 0 0 0 1 1 fill="dy|"')).toEqual(['body-top']);
  });

  it('offers only the properties the node takes, as isolex checks them', () => {
    // a box paints its faces with fills=; isolex refuses fill= on one
    expect(at('  box 0 0 0 1 1 1 fi|')).toEqual(['fills=']);
    expect(at('  cylinder 0.5 0.5 0 0.4 1 s|')).toEqual(['side=']);
    expect(at('  shadow 0.5 0.5 0.48 0.48 o|')).toEqual([]);
    expect(at('  rect "x" "y" 4 4 r|')).toEqual(['rx=', 'stroke=']);
    expect(at('icon "a" {\n  part "p" { box 0 0 0 1 1 1 w|')).toEqual(['when=']);
    expect(at('icon "a" e|')).toEqual(['extends=']);
    expect(at('  grant "accent" r|')).toEqual(['reason=']);
    // a fragment's params are its own
    expect(at('  insert "glyph" w|')).toEqual([]);
  });

  it('inserts a node with its arguments, the first selected to type over', () => {
    const c = complete('icon "a" {\n  part "p" {\n    bo', 27, vocab)!;
    const box = c.items.find((s) => s.label === 'box')!;
    expect(box.insert).toBe('box 0 0 0 1 1 1');
    expect(box.select).toEqual([4, 5]);
  });

  // isolex 0.4's: fonts and text, transforms and quarter-turns
  it('offers fonts, their own nodes inside them, and text set in them', () => {
    expect(at('fo|')).toEqual(['font']);
    expect(at('font "f" {\n  gl|\n}')).toEqual(['glyph']);
    // a font holds no parts, and an icon no glyphs
    expect(at('font "f" {\n  par|\n}')).toEqual(['param']);
    expect(at('icon "a" {\n  gly|\n}')).toEqual([]);
    // a glyph's body is drawn
    expect(at('font "f" {\n  glyph "A" advance=6 {\n    pa|\n  }\n}')).toEqual(['path']);
    expect(at('  glyph "A" a|')).toEqual(['advance=']);
    expect(at('font "f" {\n  kind "|"\n}')).toEqual(['stroke', 'outline']);
    expect(at('icon "a" {\n  part "p" {\n    te|\n  }\n}')).toEqual(['text']);
    expect(at('    text "x" "y" "U1" font="|"')).toEqual(['plain']);
    expect(at('    text "x" "y" "U1" style="b|"')).toEqual(['bold']);
    expect(at('    text "x" "y" "U1" anchor="|"')).toEqual(['start', 'middle', 'end']);
    expect(at('    text "x" "y" "U1" ba|')).toEqual(['baseline=']);
  });

  it('offers transforms and turns, with what they take', () => {
    expect(at('icon "a" {\n  part "p" {\n    tr|\n  }\n}')).toEqual(['transform']);
    expect(at('icon "a" {\n  part "p" {\n    tu|\n  }\n}')).toEqual(['turn']);
    expect(at('  transform sk|')).toEqual(['skew-x=', 'skew-y=']);
    expect(at('  transform onto="|"')).toEqual(['top', 'front', 'right']);
    expect(at('  transform bounds="|"')).toEqual(['error', 'grow', 'scale', 'clip']);
    expect(at('  turn 90 o|')).toEqual(['origin=']);
    expect(at('icon "a" {\n  vi|\n}')).toEqual(['view']);
    expect(at('icon "a" {\n  transform-bounds "g|"\n}')).toEqual(['grow']);
  });

  it('offers nothing in a comment, or where nothing fits', () => {
    expect(at('// fill="|')).toEqual([]);
    expect(at('icon "a" { box 0 0 0 1 1 1 opacity=|')).toEqual([]);
  });
});
