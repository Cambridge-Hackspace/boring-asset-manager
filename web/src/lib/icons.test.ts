import { describe, expect, it } from 'vitest';
import { boxSource, copiedSource, extendingSource, iconGroup, iconOwner, iconParams, iconTextParams, isWrapper, parseWidths, shownIcon, splitDiags, suggestIconName, validIconName } from './icons';
import type { Category, CategoryIcon } from './models';
import { outline } from './isolex/syntax';

const cat = (id: string, name: string, parent: string | null = null) => ({ id, name, parent_id: parent }) as Category;
const icon = (category_id: string, icon_name: string, published = true, rendition: 'screen' | 'label' = 'screen') =>
  ({ category_id, rendition, icon_name, source: published ? 'x' : null, draft: published ? null : 'x', draft_errors: [], updated_at: '' }) as CategoryIcon;

describe('icon names', () => {
  it('are suggested from the category, as the server would take them, not taken', () => {
    expect(suggestIconName('Storage Crates', [])).toBe('storage-crates');
    expect(suggestIconName('Café & Bar!', [])).toBe('cafe-bar');
    expect(suggestIconName('Storage', ['storage', 'storage-2'])).toBe('storage-3');
    expect(suggestIconName('🔧', [])).toBe('icon');
    expect(suggestIconName('BAM spares', [])).toBe('icon-bam-spares');
    for (const n of ['storage-crates', 'cafe-bar', 'icon', 'icon-bam-spares', suggestIconName('x'.repeat(300), [])]) {
      expect(validIconName(n), n).toBe(true);
    }
    expect(validIconName('.hidden')).toBe(false);
    expect(validIconName('bam-unknown')).toBe(false);
  });
});

describe('shownIcon', () => {
  const tree = [cat('net', 'Networking'), cat('sw', 'Switches', 'net'), cat('poe', 'PoE', 'sw'), cat('other', 'Other')];
  it('is a category’s own published icon, or the nearest one above it', () => {
    const icons = [icon('net', 'network-gear'), icon('poe', 'poe-switch', false)];
    expect(shownIcon(tree, icons, 'net')).toEqual({ name: 'network-gear', from: 'net', rendition: 'screen' });
    expect(shownIcon(tree, icons, 'sw')).toEqual({ name: 'network-gear', from: 'net', rendition: 'screen' });
    // a draft never published is not shown
    expect(shownIcon(tree, icons, 'poe')).toEqual({ name: 'network-gear', from: 'net', rendition: 'screen' });
    expect(shownIcon(tree, icons, 'other')).toBeNull();
  });
  it('for labels, is the nearest label icon above it, or else the screen icon', () => {
    const icons = [icon('net', 'network-gear'), icon('sw', 'switch'), icon('net', 'network-gear-label', true, 'label')];
    // the screen icon is the screen's alone
    expect(shownIcon(tree, icons, 'sw')).toEqual({ name: 'switch', from: 'sw', rendition: 'screen' });
    // a label icon above wins over a screen icon nearer
    expect(shownIcon(tree, icons, 'poe', 'label')).toEqual({ name: 'network-gear-label', from: 'net', rendition: 'label' });
    // with no label icon anywhere above, the screen icon
    expect(shownIcon(tree, [icon('net', 'network-gear')], 'sw', 'label')).toEqual({ name: 'network-gear', from: 'net', rendition: 'screen' });
    expect(shownIcon(tree, icons, 'other', 'label')).toBeNull();
  });
  it('survives a loop in the tree', () => {
    expect(shownIcon([cat('a', 'A', 'b'), cat('b', 'B', 'a')], [], 'a')).toBeNull();
  });
});

describe('splitDiags', () => {
  it("keeps this file's for the editor and the rest apart, errors first", () => {
    const d = (file: string, line: number) => ({ file, line, col: 1, message: 'm' });
    expect(splitDiags('categories/a.ilx', [d('categories/a.ilx', 2), d('categories/b.ilx', 3)], [d('categories/a.ilx', 1)])).toEqual({
      here: [
        { line: 2, col: 1, message: 'm', severity: 'error' },
        { line: 1, col: 1, message: 'm', severity: 'warning' },
      ],
      elsewhere: [{ file: 'categories/b.ilx', line: 3, col: 1, message: 'm', severity: 'error' }],
    });
  });
});

describe('starting sources', () => {
  it('draw an icon by the name given', () => {
    expect(outline(boxSource('crate', 'storage'))).toMatchObject([{ kind: 'icon', name: 'crate', parts: ['body'] }]);
    expect(outline(extendingSource('small-crate', 'crate'))).toMatchObject([{ kind: 'icon', name: 'small-crate', extends: 'crate' }]);
    expect(outline(boxSource('a"b', 'c'))[0]!.name).toBe('a"b');
  });
  it('copy another icon under this name, leaving the rest as it was', () => {
    const src = 'icon "crate" extends="crate-base" {\n  badge "crate" corner="ne"\n}\nicon "crate-lid" { }\n';
    expect(copiedSource(src, 'crate', 'bin')).toBe('icon "bin" extends="crate-base" {\n  badge "crate" corner="ne"\n}\nicon "crate-lid" { }\n');
    expect(copiedSource('base  "a.b" { }', 'a.b', 'c')).toBe('base  "c" { }');
  });
  it("group icons under their top category's name", () => {
    expect(iconGroup([cat('net', 'Networking'), cat('sw', 'Switches', 'net')], 'sw')).toBe('networking');
  });
});

describe('the settings', () => {
  it('read stroke widths as typed', () => {
    expect(parseWidths('1.2, 1.5 2')).toEqual({ widths: [1.2, 1.5, 2] });
    expect(parseWidths('  ')).toEqual({ widths: [] });
    expect(parseWidths('1.2, 0')).toEqual({ error: '0 is not a positive number' });
    expect(parseWidths('thick')).toEqual({ error: 'thick is not a positive number' });
  });
  it('know who owns an icon, a variant by its longest name', () => {
    const icons = [icon('a', 'switch'), icon('b', 'switch-poe')];
    expect(iconOwner(icons, 'switch')?.category_id).toBe('a');
    expect(iconOwner(icons, 'switch-down')?.category_id).toBe('a');
    expect(iconOwner(icons, 'switch-poe-down')?.category_id).toBe('b');
    expect(iconOwner(icons, 'router')).toBeUndefined();
  });
});

describe('iconParams', () => {
  it('collects an icon’s params up its chain, root first', () => {
    const defs = outline(`base "rack-unit" { param u 1; param depth 2 }
base "switch" { extends "rack-unit"; param ports 12; param u 1 }
icon "switch-l2" extends="switch" { }
icon "loop-a" extends="loop-b" { param a 1 }
icon "loop-b" extends="loop-a" { param b 1 }`);
    expect(iconParams(defs, 'switch-l2')).toEqual(['u', 'depth', 'ports']);
    expect(iconParams(defs, 'nothing')).toEqual([]);
    expect(iconParams(defs, 'loop-a')).toEqual(['b', 'a']);
  });

  it('knows which of them are text, its own or inherited', () => {
    const defs = outline(`base "tag" { param label (text)"?"; param n 1 }
icon "port-tag" extends="tag" { param port (text)"A{n}"; param n 2 }
icon "plain" { param u 1 }`);
    expect(iconParams(defs, 'port-tag')).toEqual(['label', 'n', 'port']);
    expect(iconTextParams(defs, 'port-tag')).toEqual(['port', 'label']);
    expect(iconTextParams(defs, 'plain')).toEqual([]);
  });

  it('knows a wrapper by its name', () => {
    expect(isWrapper('bam--server--u_2')).toBe(true);
    expect(isWrapper('bam-unknown')).toBe(false);
    expect(isWrapper('server')).toBe(false);
  });
});
