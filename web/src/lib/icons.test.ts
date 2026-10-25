import { describe, expect, it } from 'vitest';
import { boxSource, copiedSource, extendingSource, iconGroup, shownIcon, splitDiags, suggestIconName, validIconName } from './icons';
import type { Category, CategoryIcon } from './models';
import { outline } from './isolex/syntax';

const cat = (id: string, name: string, parent: string | null = null) => ({ id, name, parent_id: parent }) as Category;
const icon = (category_id: string, icon_name: string, published = true) =>
  ({ category_id, icon_name, source: published ? 'x' : null, draft: published ? null : 'x', draft_errors: [], updated_at: '' }) as CategoryIcon;

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
    expect(shownIcon(tree, icons, 'net')).toEqual({ name: 'network-gear', from: 'net' });
    expect(shownIcon(tree, icons, 'sw')).toEqual({ name: 'network-gear', from: 'net' });
    // a draft never published is not shown
    expect(shownIcon(tree, icons, 'poe')).toEqual({ name: 'network-gear', from: 'net' });
    expect(shownIcon(tree, icons, 'other')).toBeNull();
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
