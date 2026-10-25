import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import { tick } from 'svelte';
import IconSourceEditor from './IconSourceEditor.svelte';
import { outline } from '../lib/isolex/syntax';

afterEach(() => cleanup());

const source = 'icon "crate" {\n  category "storage"\n  footprint 1 1 1\n  part "box" {\n    bxo 0 0 0 1 1 1\n  }\n}\n';

function editor(props: Record<string, unknown> = {}) {
  render(IconSourceEditor, { value: source, label: 'Icon source', ...props });
  return screen.getByRole('textbox', { name: 'Icon source' }) as HTMLTextAreaElement;
}

function caretAt(area: HTMLTextAreaElement, at: number) {
  area.focus();
  area.setSelectionRange(at, at);
}

describe('IconSourceEditor', () => {
  it('is a textbox with its help, numbering every line', () => {
    const area = editor();
    expect(area.value).toBe(source);
    expect(area.getAttribute('aria-describedby')).toContain('-help');
    expect(screen.getByText(/Esc, then Tab, leaves the editor/)).toBeTruthy();
    expect(document.body.textContent).toContain('8');
    expect(area.getAttribute('aria-invalid')).toBe('false');
  });

  it('colors the source underneath', () => {
    editor();
    const overlay = document.querySelector('[data-overlay]')!;
    expect(overlay.textContent).toBe(source + '\n');
    const span = [...overlay.querySelectorAll('span')].find((s) => s.textContent === '"storage"')!;
    expect(span.className).toContain('text-emerald-700');
  });

  it('marks problems on their lines, underlines them, and goes to one when it is chosen', async () => {
    const area = editor({
      problems: [
        { line: 5, col: 5, message: "unknown node 'bxo' (did you mean 'box'?)", severity: 'error' },
        { line: 2, col: 3, message: 'a warning', severity: 'warning' },
      ],
    });
    expect(area.getAttribute('aria-invalid')).toBe('true');
    expect(document.querySelectorAll('[data-mark="error"]')).toHaveLength(1);
    expect(document.querySelectorAll('[data-mark="warning"]')).toHaveLength(1);
    const squiggle = [...document.querySelectorAll('[data-overlay] span')].find((s) => s.className.includes('decoration-red-600'))!;
    expect(squiggle.textContent).toBe('bxo');

    await fireEvent.click(screen.getByRole('button', { name: /^Error, line 5, column 5: unknown node 'bxo'/ }));
    expect(document.activeElement).toBe(area);
    expect(area.value.slice(area.selectionStart, area.selectionEnd)).toBe('bxo');
  });

  it('indents with Tab, and lets focus go after Esc', async () => {
    const area = editor({ value: 'a' });
    caretAt(area, 0);
    expect(await fireEvent.keyDown(area, { key: 'Tab' })).toBe(false);
    expect(area.value).toBe('  a');
    expect(await fireEvent.keyDown(area, { key: 'Tab', shiftKey: true })).toBe(false);
    expect(area.value).toBe('a');
    await fireEvent.keyDown(area, { key: 'Escape' });
    expect(await fireEvent.keyDown(area, { key: 'Tab' })).toBe(true);
    expect(area.value).toBe('a');
  });

  it('keeps the indent on Enter, and comments lines out with Ctrl+/', async () => {
    const area = editor({ value: 'icon "a" {}' });
    caretAt(area, 10);
    await fireEvent.keyDown(area, { key: 'Enter' });
    expect(area.value).toBe('icon "a" {\n  \n}');
    expect(area.selectionStart).toBe(13);
    await fireEvent.keyDown(area, { key: '/', ctrlKey: true });
    expect(area.value).toBe('icon "a" {\n  \n}');
    area.setSelectionRange(0, area.value.length);
    await fireEvent.keyDown(area, { key: '/', ctrlKey: true });
    expect(area.value).toBe('// icon "a" {\n  \n// }');
  });

  it('suggests what fits as one types, and inserts the chosen one', async () => {
    const vocabulary = { tokens: ['ink', 'accent'], definitions: outline('fragment "glyph" { }'), icons: [] };
    const area = editor({ value: 'icon "a" {\n  part "p" {\n    bo\n  }\n}', vocabulary });
    caretAt(area, 'icon "a" {\n  part "p" {\n    bo'.length);
    await fireEvent.input(area, { inputType: 'insertText', data: 'o' });
    const list = screen.getByRole('listbox', { name: 'Suggestions' });
    expect(area.getAttribute('aria-controls')).toBe(list.id);
    expect(screen.getByRole('option', { name: /box/ }).getAttribute('aria-selected')).toBe('true');
    expect(screen.getByRole('status').textContent).toBe('1 suggestions');

    await fireEvent.keyDown(area, { key: 'Enter' });
    expect(screen.queryByRole('listbox')).toBeNull();
    expect(area.value).toContain('    box 0 0 0 1 1 1\n');
    // the first argument selected, to type over
    expect(area.value.slice(area.selectionStart, area.selectionEnd)).toBe('0');
  });

  it('suggests names in strings, opens on Ctrl+Space, and closes on Escape', async () => {
    const vocabulary = { tokens: ['ink', 'accent'], definitions: [], icons: [] };
    const area = editor({ value: 'tile 0 0 0 1 1 fill=""', vocabulary });
    caretAt(area, 'tile 0 0 0 1 1 fill="'.length);
    await fireEvent.keyDown(area, { key: ' ', ctrlKey: true });
    await tick();
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual([
      expect.stringContaining('ink'),
      expect.stringContaining('accent'),
      expect.stringContaining('none'),
    ]);
    await fireEvent.keyDown(area, { key: 'ArrowDown' });
    expect(area.getAttribute('aria-activedescendant')).toMatch(/-1$/);
    await fireEvent.keyDown(area, { key: 'Escape' });
    expect(screen.queryByRole('listbox')).toBeNull();
    // that Escape only closed the list: Tab still indents
    expect(await fireEvent.keyDown(area, { key: 'Tab' })).toBe(false);
  });

  it('changes nothing when read-only', async () => {
    const area = editor({ value: 'a', readonly: true });
    caretAt(area, 0);
    expect(await fireEvent.keyDown(area, { key: 'Tab' })).toBe(true);
    expect(area.value).toBe('a');
  });
});
